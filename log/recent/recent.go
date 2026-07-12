// Package recent provides an optional in-memory recent-log tail handler.
package recent

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// LevelPolicy configures retention for one slog level bucket. Both Cap and
// MaxAge must be positive for that level to be retained.
type LevelPolicy struct {
	// Cap is the maximum number of entries retained for this level.
	Cap int
	// MaxAge is the maximum age retained for this level.
	MaxAge time.Duration
}

// Policy configures retention independently for each canonical slog level.
// Levels with a zero policy are not retained.
type Policy struct {
	// Debug configures retention for debug records.
	Debug LevelPolicy
	// Info configures retention for info records.
	Info LevelPolicy
	// Warn configures retention for warn records.
	Warn LevelPolicy
	// Error configures retention for error records.
	Error LevelPolicy
}

// Entry is one retained log record returned by Snapshot.
type Entry struct {
	// Time is the record timestamp.
	Time time.Time
	// Level is the uppercase canonical slog level.
	Level string
	// Service is the logger service name.
	Service string
	// Message is the redacted log message.
	Message string
	// Attrs contains redacted record attributes, excluding service.
	Attrs map[string]string
}

type retainedEntry struct {
	entry Entry
	seq   uint64
}

type state struct {
	mu      sync.Mutex
	policy  map[slog.Level]LevelPolicy
	buffers map[slog.Level][]retainedEntry
	nextSeq uint64
}

// Handler is an slog.Handler that stores a bounded recent tail for configured
// levels. It is intended for use via github.com/david-aggeler/keel/log.Config.Handlers.
type Handler struct {
	state  *state
	attrs  []slog.Attr
	groups []string
}

// NewHandler creates a recent-log handler with the supplied per-level policy.
//
// DHF-REQ: keel/requirement-56
func NewHandler(policy Policy) *Handler {
	return &Handler{
		state: &state{
			policy:  activePolicies(policy),
			buffers: make(map[slog.Level][]retainedEntry),
		},
	}
}

func activePolicies(policy Policy) map[slog.Level]LevelPolicy {
	out := make(map[slog.Level]LevelPolicy)
	for level, p := range map[slog.Level]LevelPolicy{
		slog.LevelDebug: policy.Debug,
		slog.LevelInfo:  policy.Info,
		slog.LevelWarn:  policy.Warn,
		slog.LevelError: policy.Error,
	} {
		if p.Cap > 0 && p.MaxAge > 0 {
			out[level] = p
		}
	}
	return out
}

// Enabled reports whether the handler retains records in level's canonical
// bucket.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	if h == nil || h.state == nil {
		return false
	}
	_, ok := h.state.policy[bucketLevel(level)]
	return ok
}

// Handle records one log entry when its canonical level has an active policy.
func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	if h == nil || h.state == nil {
		return nil
	}
	level := bucketLevel(record.Level)
	policy, ok := h.state.policy[level]
	if !ok {
		return nil
	}

	entry := Entry{
		Time:    record.Time,
		Level:   level.String(),
		Service: serviceFromAttrs(h.attrs),
		Message: record.Message,
		Attrs:   h.attrsMap(record),
	}

	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	h.state.nextSeq++
	entries := append(h.state.buffers[level], retainedEntry{entry: entry, seq: h.state.nextSeq})
	if len(entries) > policy.Cap {
		entries = append([]retainedEntry(nil), entries[len(entries)-policy.Cap:]...)
	}
	h.state.buffers[level] = entries
	return nil
}

// WithAttrs returns a handler that attaches attrs to subsequently retained
// entries.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if h == nil {
		return h
	}
	next := h.clone()
	next.attrs = append(slicesClone(h.attrs), prefixAttrs(h.groups, attrs)...)
	return next
}

// WithGroup returns a handler that prefixes subsequent attr keys with name.
func (h *Handler) WithGroup(name string) slog.Handler {
	if h == nil || strings.TrimSpace(name) == "" {
		return h
	}
	next := h.clone()
	next.groups = append(slicesClone(h.groups), name)
	return next
}

func (h *Handler) clone() *Handler {
	return &Handler{
		state:  h.state,
		attrs:  slicesClone(h.attrs),
		groups: slicesClone(h.groups),
	}
}

// Snapshot returns retained entries newest-first after lazily pruning entries
// older than their level's MaxAge.
//
// DHF-REQ: keel/requirement-56
func (h *Handler) Snapshot() []Entry {
	if h == nil || h.state == nil {
		return nil
	}
	now := time.Now()
	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	var retained []retainedEntry
	for level, entries := range h.state.buffers {
		policy, ok := h.state.policy[level]
		if !ok {
			continue
		}
		kept := entries[:0]
		for _, entry := range entries {
			if now.Sub(entry.entry.Time) <= policy.MaxAge {
				kept = append(kept, entry)
				retained = append(retained, entry)
			}
		}
		h.state.buffers[level] = kept
	}
	sort.Slice(retained, func(i, j int) bool {
		return retained[i].seq > retained[j].seq
	})

	out := make([]Entry, 0, len(retained))
	for _, entry := range retained {
		out = append(out, cloneEntry(entry.entry))
	}
	return out
}

func (h *Handler) attrsMap(record slog.Record) map[string]string {
	var out map[string]string
	add := func(attr slog.Attr) {
		attr.Value = attr.Value.Resolve()
		if attr.Key == "" || attr.Key == "service" {
			return
		}
		if out == nil {
			out = make(map[string]string)
		}
		out[attr.Key] = attr.Value.String()
	}
	for _, attr := range h.attrs {
		add(attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		for _, prefixed := range prefixAttrs(h.groups, []slog.Attr{attr}) {
			add(prefixed)
		}
		return true
	})
	return out
}

func bucketLevel(level slog.Level) slog.Level {
	switch {
	case level >= slog.LevelError:
		return slog.LevelError
	case level >= slog.LevelWarn:
		return slog.LevelWarn
	case level >= slog.LevelInfo:
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}

func serviceFromAttrs(attrs []slog.Attr) string {
	for i := len(attrs) - 1; i >= 0; i-- {
		attr := attrs[i]
		if attr.Key == "service" {
			return attr.Value.Resolve().String()
		}
	}
	return ""
}

func prefixAttrs(groups []string, attrs []slog.Attr) []slog.Attr {
	if len(groups) == 0 || len(attrs) == 0 {
		return slicesClone(attrs)
	}
	prefix := strings.Join(groups, ".") + "."
	out := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		if attr.Key != "" {
			attr.Key = prefix + attr.Key
		}
		out = append(out, attr)
	}
	return out
}

func cloneEntry(entry Entry) Entry {
	if len(entry.Attrs) == 0 {
		return entry
	}
	attrs := make(map[string]string, len(entry.Attrs))
	for key, value := range entry.Attrs {
		attrs[key] = value
	}
	entry.Attrs = attrs
	return entry
}

func slicesClone[S ~[]E, E any](in S) S {
	if len(in) == 0 {
		return nil
	}
	return append(S(nil), in...)
}
