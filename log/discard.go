package log

import (
	"log/slog"
)

// Discard returns a logger that drops every record at every level. It is the
// one way to spell "produce no output" at an injection point that takes a
// logger, replacing the ad-hoc slog.New(slog.NewTextHandler(io.Discard, nil))
// self-bootstrap that consumers otherwise hand-roll.
//
// DHF-REQ: keel/requirement-122
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
