package log

// recentlog.go — an in-process, bounded, redacted ring buffer of recent
// warn/error log records, plus the slog.Handler tee that feeds it. This is the
// shared data source behind every service's /diag recent_logs surface and the
// mcp-server admin recent-logs tool (openbrain/requirement-186, change_request-381).
//
// INVARIANT: no recentEntry field carries a secret. Message and every attr
// value pass through the same redaction path as the JSON/console sinks
// (RedactString + sensitive-key suppression), applied at ingest — so a record
// is scrubbed once, when captured, never on read.
//
// Retention is process-lifetime only and capped at the buffer capacity: this is
// a recent-tail diagnostic, not a log store. Only warn- and error-level records
// are retained; info/debug flow through untouched to the wrapped handler.

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// recentEntry is one retained warn/error log record in /diag-ready form.
type recentEntry struct {
	Time    string            `json:"time"`            // RFC3339Nano emit time
	Level   string            `json:"level"`           // uppercase: "WARN" | "ERROR"
	Service string            `json:"service"`         // emitting service
	Message string            `json:"message"`         // redacted
	Attrs   map[string]string `json:"attrs,omitempty"` // redacted key→value (service omitted)
}

// recentBuffer is a thread-safe fixed-capacity ring buffer of recentEntry.
// Entries are inserted oldest→newest; Entries() reads them back newest-first.
type recentBuffer struct {
	mu       sync.Mutex
	buf      []recentEntry
	capacity int
	head     int // index of the next write position
	count    int // number of valid entries (≤ capacity)
}

// NewRecentBuffer creates a buffer with the given capacity. Capacities ≤ 0 are
// clamped to 1 to keep the modular arithmetic in Add well-defined.
func newRecentBuffer(capacity int) *recentBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &recentBuffer{
		buf:      make([]recentEntry, capacity),
		capacity: capacity,
	}
}

// DHF-REQ: keel/requirement-20
// Add inserts an entry, evicting the oldest when full. The entry is expected to
// be already redacted (the recentHandler does this at capture time).
func (b *recentBuffer) Add(e recentEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e.Level = strings.ToUpper(strings.TrimSpace(e.Level))
	b.buf[b.head] = e
	b.head = (b.head + 1) % b.capacity
	if b.count < b.capacity {
		b.count++
	}
}

// Entries returns retained entries newest-first. level filters to a single
// level ("WARN" or "ERROR"; case-insensitive); empty returns all levels. limit
// caps the result count; limit ≤ 0 returns every retained (matching) entry.
func (b *recentBuffer) Entries(limit int, level string) []recentEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	level = strings.ToUpper(strings.TrimSpace(level))
	out := make([]recentEntry, 0, b.count)
	for i := 0; i < b.count; i++ {
		// Walk backwards from the most-recent write. +2*capacity keeps the
		// index non-negative before the modulo for any valid head/i.
		idx := (b.head - 1 - i + 2*b.capacity) % b.capacity
		e := b.buf[idx]
		if level != "" && e.Level != level {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Len returns the number of entries currently retained.
func (b *recentBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// recentHandler is an slog.Handler that captures warn/error records into a
// recentBuffer (redacted) before forwarding to the wrapped handler. Records
// below warn are forwarded untouched and never retained.
type recentHandler struct {
	inner   slog.Handler
	buf     *recentBuffer
	service string
	attrs   []slog.Attr // accumulated via WithAttrs (the "service" attr lives here)
}

// TeeRecent wraps logger so that warn/error records are also captured into buf,
// stamped with service. The returned logger writes to the original destination
// and the buffer both.
func teeRecent(logger *slog.Logger, buf *recentBuffer, service string) *slog.Logger {
	return slog.New(&recentHandler{inner: logger.Handler(), buf: buf, service: service})
}

func (h *recentHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *recentHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		h.buf.Add(h.toEntry(r))
	}
	return h.inner.Handle(ctx, r)
}

// DHF-REQ: keel/requirement-20
// toEntry renders a warn/error record into a redacted recentEntry.
func (h *recentHandler) toEntry(r slog.Record) recentEntry {
	level := "WARN"
	if r.Level >= slog.LevelError {
		level = "ERROR"
	}
	e := recentEntry{
		Time:    r.Time.Format(time.RFC3339Nano),
		Level:   level,
		Service: h.service,
		Message: redactString(r.Message),
	}
	var attrs map[string]string
	add := func(a slog.Attr) {
		if a.Key == "" || a.Key == "service" {
			return
		}
		if attrs == nil {
			attrs = make(map[string]string)
		}
		attrs[a.Key] = redactAttrValue(a)
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		add(a)
		return true
	})
	e.Attrs = attrs
	return e
}

// redactAttrValue mirrors replaceForOpenBrain's value handling: sensitive keys
// are suppressed wholesale; every other value is RedactString-scrubbed.
func redactAttrValue(a slog.Attr) string {
	if isSensitiveAttrKey(a.Key) {
		return "[REDACTED]"
	}
	return redactString(a.Value.Resolve().String())
}

func (h *recentHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := slicesClone(h.attrs)
	next = append(next, attrs...)
	return &recentHandler{inner: h.inner.WithAttrs(attrs), buf: h.buf, service: h.service, attrs: next}
}

func (h *recentHandler) WithGroup(name string) slog.Handler {
	return &recentHandler{inner: h.inner.WithGroup(name), buf: h.buf, service: h.service, attrs: h.attrs}
}
