package log

// Internal tests for the unexported console-rendering helpers, closing the
// coverage gaps found under keel/change_request-4 (keel/ac-37).

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFromContextAndWithLogger(t *testing.T) {
	base := slog.Default()
	if got := fromContext(context.Background()); got != base {
		t.Error("empty context should fall back to slog.Default")
	}
	l, _ := newForTesting("svc")
	ctx := withLogger(context.Background(), l)
	if got := fromContext(ctx); got != l {
		t.Error("stored logger not returned")
	}
	if got := fromContext(withLogger(context.Background(), nil)); got != base {
		t.Error("nil stored logger should fall back to slog.Default")
	}
}

func TestDiscard(t *testing.T) {
	l := discard()
	if l == nil {
		t.Fatal("Discard returned nil")
	}
	l.Info("goes nowhere", "k", "v") // must not panic
}

// DHF-TEST: keel/requirement-11
func TestLoggerContextMethodsWithGroupAndCaptureAllJSON(t *testing.T) {
	l, rc := newForTesting("svc")
	ctx := context.Background()

	grouped := l.WithGroup("request").With("id", "req-1")
	grouped.DebugContext(ctx, "debug message")
	grouped.InfoContext(ctx, "info message")
	grouped.WarnContext(ctx, "warn message")
	grouped.ErrorContext(ctx, "error message")

	records := rc.AllJSON()
	if len(records) != 4 {
		t.Fatalf("AllJSON returned %d records, want 4: %#v", len(records), records)
	}
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	for i, level := range levels {
		if records[i]["level"] != level {
			t.Fatalf("record %d level = %v, want %s: %#v", i, records[i]["level"], level, records[i])
		}
		request, ok := records[i]["request"].(map[string]any)
		if !ok || request["id"] != "req-1" {
			t.Fatalf("record %d grouped attrs = %#v, want request.id", i, records[i]["request"])
		}
	}

	rc.Reset()
	if got := rc.AllJSON(); len(got) != 0 {
		t.Fatalf("AllJSON after Reset = %#v, want empty", got)
	}
}

// DHF-TEST: keel/requirement-11
func TestSparseFieldValueAndRedactValueCoverStructuredKinds(t *testing.T) {
	when := time.Date(2026, 7, 18, 20, 0, 0, 123, time.UTC)
	group := slog.GroupValue(
		slog.String("token", "Bearer secret-token"),
		slog.Duration("elapsed", 250*time.Millisecond),
		slog.Time("when", when),
		slog.Uint64("count", 7),
	)

	sparse := sparseFieldValue(group).(map[string]any)
	if sparse["token"] != "[REDACTED]" {
		t.Fatalf("sparse token = %#v, want redacted", sparse["token"])
	}
	if sparse["elapsed"] != "250ms" || sparse["when"] != when.Format(time.RFC3339Nano) || sparse["count"] != uint64(7) {
		t.Fatalf("sparse structured fields = %#v", sparse)
	}

	redacted := redactValue("credentials", group).Group()
	if got := redacted[0].Value.String(); got != "[REDACTED]" {
		t.Fatalf("redacted group token = %q, want redacted", got)
	}
	if got := redactValue("api_token", slog.StringValue("plain-secret")).String(); got != "[REDACTED]" {
		t.Fatalf("sensitive key redaction = %q, want [REDACTED]", got)
	}
	if got := sparseFieldValue(slog.AnyValue(errors.New("Bearer secret-token"))); got != "Bearer [REDACTED]" {
		t.Fatalf("sparse Any error = %#v, want redacted", got)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-20
func TestSparseAIHandlerWithGroupNestsAttrsAndIgnoresEmptyGroup(t *testing.T) {
	var out strings.Builder
	base := newSparseAIHandler(&out, slog.LevelDebug)
	if got := base.WithGroup(""); got != base {
		t.Fatal("WithGroup empty should return the same sparse handler")
	}
	grouped := base.WithGroup("request").WithAttrs([]slog.Attr{slog.String("id", "req-1")})
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "handled", 0)
	rec.AddAttrs(slog.String("event_type", "request_done"))
	if err := grouped.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle grouped sparse record: %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, `"event":"request_done"`) || !strings.Contains(rendered, `"id":"req-1"`) {
		t.Fatalf("grouped sparse output = %s", rendered)
	}
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
	rc := &recordCapture{}
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
	var logger *Logger
	logger.Header("title", "v1")
	logger.Section("sec")
	logger.Field("label", 1)
	logger.Fields([]FieldRow{{Label: "a", Value: 1}, {Label: "longer", Value: 2}})
	logger.Emit("event")
	logger.LogBuildIdentity("v1", "c")
}

// DHF-TEST: keel/requirement-11, keel/requirement-29
func TestLoggerLifecycleHelpersCoverNilAndFileBranches(t *testing.T) {
	var logger *Logger
	if logger.TextLogPath() != "" || logger.JSONLLogPath() != "" || logger.RunLogPath() != "" || logger.RunLogLine() != 0 {
		t.Fatal("nil logger accessors should return zero values")
	}

	var closed []string
	closeAll([]io.Closer{
		ioCloserFunc(func() error {
			closed = append(closed, "first")
			return nil
		}),
		ioCloserFunc(func() error {
			closed = append(closed, "second")
			return nil
		}),
	})
	if strings.Join(closed, ",") != "second,first" {
		t.Fatalf("closeAll order = %v, want reverse registration order", closed)
	}

	var jsonClosed bool
	if err := (&jsonFileHandler{}).Close(); err != nil {
		t.Fatalf("nil jsonFileHandler close = %v, want nil", err)
	}
	if err := (&jsonFileHandler{close: func() error {
		jsonClosed = true
		return nil
	}}).Close(); err != nil || !jsonClosed {
		t.Fatalf("jsonFileHandler close = %v closed=%v, want nil and called", err, jsonClosed)
	}

	f, err := os.CreateTemp(t.TempDir(), "regular")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	t.Setenv("NO_COLOR", "")
	if colorEnabled(f, false, false) {
		t.Fatal("regular file should not enable color without force")
	}
}

type ioCloserFunc func() error

func (f ioCloserFunc) Close() error { return f() }

func TestHumanAndJSONFileHandlerLifecycle(t *testing.T) {
	dir := t.TempDir()

	hh, _, err := newTextFileHandler(dir, "svc", false, slog.LevelDebug)
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

	jh, _, _, err := newJSONFileHandler(dir, "svc", false, false, slog.LevelDebug)
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
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
