package log

// Internal tests for the unexported console-rendering helpers, closing the
// coverage gaps found under keel/change_request-4 (keel/ac-37).

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestFromContextAndWithLogger(t *testing.T) {
	base := slog.Default()
	if got := FromContext(context.Background()); got != base {
		t.Error("empty context should fall back to slog.Default")
	}
	l, _ := newForTesting("svc")
	ctx := WithLogger(context.Background(), l)
	if got := FromContext(ctx); got != l {
		t.Error("stored logger not returned")
	}
	if got := FromContext(WithLogger(context.Background(), nil)); got != base {
		t.Error("nil stored logger should fall back to slog.Default")
	}
}

func TestDiscard(t *testing.T) {
	l := Discard()
	if l == nil {
		t.Fatal("Discard returned nil")
	}
	l.Info("goes nowhere", "k", "v") // must not panic
}

// DHF-TEST: keel/requirement-2
func TestConsoleMessageProgressDetailHook(t *testing.T) {
	l, rc := newConsoleForTesting("svc")

	l.Info("builder progress", "detail", "reading files", "event_type", "agent_message")
	if out := rc.LastRaw(); !strings.Contains(out, "builder detail: reading files") {
		t.Errorf("curated detail missing: %q", out)
	}
	if out := rc.LastRaw(); strings.Contains(out, "event_type=") {
		t.Errorf("event_type should be skipped in console message: %q", out)
	}

	rc.Reset()
	l.Info("builder progress", "detail", "   ", "event_type", "noop")
	if out := rc.LastRaw(); !strings.Contains(out, "builder progress") {
		t.Errorf("blank detail should keep the generic message: %q", out)
	}

	rc.Reset()
	l.Info("builder progress", "detail", 42)
	if out := rc.LastRaw(); !strings.Contains(out, "builder detail: 42") {
		t.Errorf("non-string detail should be formatted: %q", out)
	}
}

func TestFormatConsoleValueKinds(t *testing.T) {
	l, rc := newConsoleForTesting("svc")
	when := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	l.Info("kinds",
		"t", when,
		"d", 1500*time.Millisecond,
		"n", 7,
		"b", true,
		"g", slog.GroupValue(slog.String("inner", "x"), slog.Int("count", 2)),
		"s", "postgres://u:pw@host/db",
	)
	out := rc.LastRaw()
	for _, want := range []string{
		"t=2026-07-07T12:00:00Z",
		"d=1.5s",
		"n=7",
		"b=true",
		"g={inner=x count=2}",
		"s=postgres://***:***@host/db",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("console rendering missing %q in %q", want, out)
		}
	}
}

// DHF-TEST: keel/requirement-20
func TestConsoleLevelsAndColor(t *testing.T) {
	if consoleLevel(slog.LevelError) != "ERROR" || consoleLevel(slog.LevelWarn) != "WARN" ||
		consoleLevel(slog.LevelDebug) != "DEBUG" || consoleLevel(slog.LevelInfo) != "INFO" {
		t.Error("consoleLevel mapping broken")
	}
	for _, lvl := range []slog.Level{slog.LevelError, slog.LevelWarn, slog.LevelDebug, slog.LevelInfo} {
		if levelColor(lvl) == "" {
			t.Errorf("no color for %v", lvl)
		}
	}

	// Non-file writer: never colored. Disable and NO_COLOR win over force.
	rc := &RecordCapture{}
	if colorEnabled(rc, false, false) {
		t.Error("non-file writer should not enable color")
	}
	if colorEnabled(rc, true, true) {
		t.Error("disable must beat force")
	}
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(rc, true, false) {
		t.Error("NO_COLOR must beat force")
	}
	t.Setenv("NO_COLOR", "")
	if !colorEnabled(rc, true, false) {
		t.Error("force should enable color for non-file writers")
	}
}

func TestHeaderSectionFieldsNilLogger(t *testing.T) {
	// Nil loggers fall back to slog.Default and must not panic.
	Header(nil, "title", "v1")
	Section(nil, "sec")
	Field(nil, "label", 1)
	Fields(nil, []FieldRow{{Label: "a", Value: 1}, {Label: "longer", Value: 2}})
}

func TestHumanAndJSONFileHandlerLifecycle(t *testing.T) {
	dir := t.TempDir()

	hh, err := NewHumanFileHandler(dir, "svc")
	if err != nil {
		t.Fatal(err)
	}
	withAttrs := hh.WithAttrs([]slog.Attr{slog.String("k", "v")})
	grouped := withAttrs.WithGroup("grp")
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	if err := grouped.Handle(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if !hh.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("human file handler should accept DEBUG")
	}
	if c, ok := hh.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Error("human file handler should be closable")
	}

	jh, err := NewJSONFileHandler(dir, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if err := jh.Handle(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if c, ok := jh.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err == nil {
			// double close on the same descriptor errors; both behaviors are
			// acceptable, we only require no panic.
			_ = err
		}
	} else {
		t.Error("json file handler should be closable")
	}
}

func TestParseLevelBranches(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo, "": slog.LevelInfo,
		"warn": slog.LevelWarn, "warning": slog.LevelWarn,
		"error": slog.LevelError, "bogus": slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
