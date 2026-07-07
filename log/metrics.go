package log

import (
	"context"
	"log/slog"
)

// MetricKind is the value of the "kind" field on all metrics log lines.
// Use jq 'select(.kind == "metric")' to isolate all metrics lines from the log stream.
const MetricKind = "metric"

// Event name constants. The slog msg field carries these values.
// Field naming convention: _ms for durations, _count for counts.
const (
	EventVaultWriteTiming = "vault_write_timing"
	EventToolCall         = "tool_call"
)

// Metric returns the slog.Attr that tags a log line as a metrics event.
// Always pass it as the first attr to Emit or logger.LogAttrs so the kind
// field appears near the start of the JSON object.
func Metric() slog.Attr {
	return slog.String("kind", MetricKind)
}

// Emit logs a metrics event at Info level. The event name becomes the slog
// msg field. Metric() is prepended automatically so callers only supply
// domain-specific attrs.
//
// Example:
//
//	logging.Emit(logger, logging.EventVaultWriteTiming,
//	    slog.String("op", "create"),
//	    slog.Int64("pull_ms", pullDur.Milliseconds()),
//	)
func Emit(logger *slog.Logger, event string, attrs ...slog.Attr) {
	all := make([]slog.Attr, 0, 1+len(attrs))
	all = append(all, Metric())
	all = append(all, attrs...)
	logger.LogAttrs(context.Background(), slog.LevelInfo, event, all...)
}
