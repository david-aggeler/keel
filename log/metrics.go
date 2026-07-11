package log

import (
	"context"
	"log/slog"
)

// Use jq 'select(.kind == "metric")' to isolate all metrics lines from the log stream.
const metricKind = "metric"

// Always pass it as the first attr to Emit or logger.LogAttrs so the kind
// field appears near the start of the JSON object.
func metric() slog.Attr {
	return slog.String("kind", metricKind)
}

// emit logs a metrics event at Info level. The event name becomes the slog
// msg field. metric() is prepended automatically so callers only supply
// domain-specific attrs.
//
// Example:
//
//	logging.emit(logger, "sync_timing",
//	    slog.String("op", "create"),
//	    slog.Int64("pull_ms", pullDur.Milliseconds()),
//	)
//
// DHF-REQ: keel/requirement-31
func emit(logger *slog.Logger, event string, attrs ...slog.Attr) {
	all := make([]slog.Attr, 0, 1+len(attrs))
	all = append(all, metric())
	all = append(all, attrs...)
	logger.LogAttrs(context.Background(), slog.LevelInfo, event, all...)
}
