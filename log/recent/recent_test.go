package recent_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/log/recent"
)

// DHF-TEST: keel/requirement-56
func TestHandlerSnapshotAppliesPerLevelCapAgeAndRedaction(t *testing.T) {
	handler := recent.NewHandler(recent.Policy{
		Info:  recent.LevelPolicy{Cap: 2, MaxAge: time.Hour},
		Warn:  recent.LevelPolicy{Cap: 1, MaxAge: time.Hour},
		Error: recent.LevelPolicy{Cap: 2, MaxAge: time.Nanosecond},
	})
	logger, err := logging.New(logging.Config{
		Service:          "api",
		Console:          logging.ConsoleNone,
		ConsoleVerbosity: slog.LevelDebug,
		Handlers:         []slog.Handler{handler},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Debug("not retained")
	logger.Info("first info")
	logger.Info("second info")
	logger.Info("third info", "user", "alice")
	logger.Warn("first warn")
	logger.Warn("second warn", "token", "super-secret")
	logger.Error("expired error")
	time.Sleep(time.Millisecond)

	snapshot := handler.Snapshot()
	infos := entriesAtLevel(snapshot, "INFO")
	if len(infos) != 2 {
		t.Fatalf("INFO entries = %d, want 2 capped entries in snapshot %#v", len(infos), snapshot)
	}
	if infos[0].Message != "third info" || infos[1].Message != "second info" {
		t.Fatalf("INFO entries = %#v, want newest capped entries third info, second info", infos)
	}
	warns := entriesAtLevel(snapshot, "WARN")
	if len(warns) != 1 || warns[0].Message != "second warn" {
		t.Fatalf("WARN entries = %#v, want capped newest warn only", warns)
	}
	if got := warns[0].Attrs["token"]; got != "[REDACTED]" || strings.Contains(got, "super-secret") {
		t.Fatalf("WARN token attr was not redacted: %#v", warns[0].Attrs)
	}
	if got := entriesAtLevel(snapshot, "ERROR"); len(got) != 0 {
		t.Fatalf("ERROR entries = %#v, want TTL-expired error absent", got)
	}
	if got := entriesAtLevel(snapshot, "DEBUG"); len(got) != 0 {
		t.Fatalf("DEBUG entries = %#v, want level without policy absent", got)
	}
	for _, entry := range snapshot {
		if entry.Time.IsZero() {
			t.Fatalf("entry missing time: %#v", entry)
		}
		if entry.Service != "api" {
			t.Fatalf("entry service = %q, want api", entry.Service)
		}
	}
}

func entriesAtLevel(entries []recent.Entry, level string) []recent.Entry {
	var out []recent.Entry
	for _, entry := range entries {
		if entry.Level == level {
			out = append(out, entry)
		}
	}
	return out
}

// DHF-TEST: keel/requirement-56
func TestHandlerSnapshotPreservesGroupsAndReturnsCopies(t *testing.T) {
	handler := recent.NewHandler(recent.Policy{
		Info: recent.LevelPolicy{Cap: 4, MaxAge: time.Hour},
	})
	logger, err := logging.New(logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleNone,
		Handlers: []slog.Handler{handler},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.With("session", "one").WithGroup("request").Info("grouped", "id", "42")

	snapshot := handler.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snapshot))
	}
	if got := snapshot[0].Attrs["session"]; got != "one" {
		t.Fatalf("session attr = %q, want one", got)
	}
	if got := snapshot[0].Attrs["request.id"]; got != "42" {
		t.Fatalf("grouped attr = %q, want 42", got)
	}

	snapshot[0].Attrs["session"] = "mutated"
	again := handler.Snapshot()
	if got := again[0].Attrs["session"]; got != "one" {
		t.Fatalf("Snapshot did not return an attr copy: %q", got)
	}
}

// DHF-TEST: keel/requirement-56
func TestHandlerDisabledAndNilPathsDoNotRetain(t *testing.T) {
	ctx := context.Background()
	disabled := recent.NewHandler(recent.Policy{})
	if disabled.Enabled(ctx, slog.LevelInfo) {
		t.Fatal("handler without active policy reported enabled")
	}
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "ignored", 0)
	if err := disabled.Handle(ctx, record); err != nil {
		t.Fatalf("Handle without active policy: %v", err)
	}
	if got := disabled.Snapshot(); len(got) != 0 {
		t.Fatalf("disabled snapshot = %#v, want empty", got)
	}

	var nilHandler *recent.Handler
	if nilHandler.Enabled(ctx, slog.LevelInfo) {
		t.Fatal("nil handler reported enabled")
	}
	if err := nilHandler.Handle(ctx, record); err != nil {
		t.Fatalf("nil Handle: %v", err)
	}
	if got := nilHandler.Snapshot(); got != nil {
		t.Fatalf("nil Snapshot = %#v, want nil", got)
	}
}
