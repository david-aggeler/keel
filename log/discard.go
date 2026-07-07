package log

import (
	"io"
	"log/slog"
)

// Discard returns a logger that drops every record. It replaces the ad-hoc
// slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)) self-bootstrap
// previously duplicated in the codex adapter and consumers' worktree glue.
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
