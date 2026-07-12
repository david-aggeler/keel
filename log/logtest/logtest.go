// Package logtest provides keel/log's optional test capture handler.
package logtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

type captureState struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *captureState) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// Capture is an slog.Handler that records JSON log records for tests.
type Capture struct {
	state   *captureState
	handler slog.Handler
}

// NewCapture creates a capture handler for use in log.Config.Handlers.
//
// DHF-REQ: keel/requirement-56
func NewCapture() *Capture {
	state := &captureState{}
	return &Capture{
		state: state,
		handler: slog.NewJSONHandler(state, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}),
	}
}

// Enabled reports whether the capture records a log event at level.
func (c *Capture) Enabled(ctx context.Context, level slog.Level) bool {
	return c.handler.Enabled(ctx, level)
}

// Handle records one log event.
func (c *Capture) Handle(ctx context.Context, record slog.Record) error {
	return c.handler.Handle(ctx, record)
}

// WithAttrs returns a capture handler that attaches attrs to future records.
func (c *Capture) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Capture{state: c.state, handler: c.handler.WithAttrs(attrs)}
}

// WithGroup returns a capture handler that nests future attrs under name.
func (c *Capture) WithGroup(name string) slog.Handler {
	return &Capture{state: c.state, handler: c.handler.WithGroup(name)}
}

// LastJSON returns the most recent captured record as a decoded map. It returns
// nil when no record has been captured.
func (c *Capture) LastJSON() map[string]any {
	lines := c.lines()
	if len(lines) == 0 {
		return nil
	}
	return decodeLine(lines[len(lines)-1])
}

// AllJSON returns every captured record in emission order.
func (c *Capture) AllJSON() []map[string]any {
	lines := c.lines()
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		records = append(records, decodeLine(line))
	}
	return records
}

// Reset clears all captured records.
func (c *Capture) Reset() {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.buf.Reset()
}

func (c *Capture) lines() []string {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	raw := strings.TrimSpace(c.state.buf.String())
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	out := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return append([]string(nil), out...)
}

func decodeLine(line string) map[string]any {
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		panic(fmt.Sprintf("keel/log/logtest: decode captured JSON record: %v", err))
	}
	return record
}
