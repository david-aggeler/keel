package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// rfc3339NanoRegex matches a subset of RFC3339Nano timestamps.
var rfc3339NanoRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

type captureHandler struct {
	state  *captureState
	attrs  map[string]any
	groups []string
}

type captureState struct {
	records []slog.Record
	attrs   map[string]any
}

func newCaptureHandler() *captureHandler {
	return &captureHandler{state: &captureState{attrs: map[string]any{}}}
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.state.records = append(h.state.records, r.Clone())
	for k, v := range h.attrs {
		h.state.attrs[k] = v
	}
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &captureHandler{state: h.state, attrs: make(map[string]any, len(h.attrs)+len(attrs)), groups: h.groups}
	for k, v := range h.attrs {
		next.attrs[k] = v
	}
	for _, attr := range attrs {
		next.attrs[attr.Key] = attr.Value.Any()
	}
	return next
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.groups = append(append([]string(nil), h.groups...), name)
	return &next
}

func mustNewLogger(t testing.TB, cfg logging.Config) *logging.Logger {
	t.Helper()
	logger, err := logging.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return logger
}

// DHF-TEST: keel/requirement-68
func TestContextLoggerAccessorsRoundTripAndDefault(t *testing.T) {
	if got := logging.FromContext(context.Background()); got == nil {
		t.Fatal("FromContext returned nil for an empty context")
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	ctx := logging.WithLogger(context.Background(), logger)

	if got := logging.FromContext(ctx); got != logger {
		t.Fatal("FromContext did not return the logger stored by WithLogger")
	}
}

// DHF-TEST: keel/requirement-68
func TestContextLoggerAccessorsCarryRequestScopedLogger(t *testing.T) {
	var console bytes.Buffer
	textDir := t.TempDir()
	jsonlDir := t.TempDir()
	base := mustNewLogger(t, logging.Config{
		Service:          "svc",
		ConsoleVerbosity: slog.LevelDebug,
		FileVerbosity:    slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           &console,
		TextDir:          textDir,
		JSONLDir:         jsonlDir,
	})
	ctx := logging.WithLogger(context.Background(), base.Slog().With("request_id", "req-123"))

	logging.FromContext(ctx).With("user_id", "u-7").InfoContext(ctx, "request complete")
	if err := base.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	var consoleRec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(console.Bytes()), &consoleRec); err != nil {
		t.Fatalf("decode console record: %v; body=%q", err, console.String())
	}
	assertRequestScopedRecord(t, "console sink", consoleRec)

	textBytes, err := os.ReadFile(base.TextLogPath())
	if err != nil {
		t.Fatalf("read text sink: %v", err)
	}
	text := string(textBytes)
	for _, want := range []string{"request complete", "request_id=req-123", "user_id=u-7"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text sink = %q, want %q", text, want)
		}
	}

	jsonlBytes, err := os.ReadFile(base.JSONLLogPath())
	if err != nil {
		t.Fatalf("read jsonl sink: %v", err)
	}
	var jsonlRec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(jsonlBytes), &jsonlRec); err != nil {
		t.Fatalf("decode jsonl record: %v; body=%q", err, string(jsonlBytes))
	}
	assertRequestScopedRecord(t, "jsonl sink", jsonlRec)
}

func assertRequestScopedRecord(t testing.TB, sink string, rec map[string]any) {
	t.Helper()
	for key, want := range map[string]any{
		"request_id": "req-123",
		"user_id":    "u-7",
		"msg":        "request complete",
	} {
		if rec[key] != want {
			t.Fatalf("%s %s = %v, want %v; record=%#v", sink, key, rec[key], want, rec)
		}
	}
}

func newJSONCaptureLogger(t testing.TB, service string) (*slog.Logger, *recordCapture) {
	t.Helper()
	rc := &recordCapture{}
	return mustNewLogger(t, logging.Config{
		Service:          service,
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           rc,
	}).Slog(), rc
}

func newConsoleCaptureLogger(t testing.TB, service string) (*slog.Logger, *recordCapture) {
	t.Helper()
	rc := &recordCapture{}
	return mustNewLogger(t, logging.Config{
		Service:          service,
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsolePlain,
		Writer:           rc,
	}).Slog(), rc
}

func TestNewConfigJSONCaptureHelper_ReturnsLoggerAndCapture(t *testing.T) {
	logger, capture := newJSONCaptureLogger(t, "mcp")
	if logger == nil {
		t.Fatal("newJSONCaptureLogger returned nil logger")
	}
	if capture == nil {
		t.Fatal("newJSONCaptureLogger returned nil capture")
	}
}

// DHF-TEST: keel/requirement-11
func TestLoggerExportedContextMethodsAndWithGroup(t *testing.T) {
	capture := &recordCapture{}
	logger := mustNewLogger(t, logging.Config{
		Service:          "svc",
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           capture,
	})

	grouped := logger.WithGroup("request").With("id", "req-1")
	grouped.DebugContext(context.Background(), "debug message")
	grouped.InfoContext(context.Background(), "info message")
	grouped.WarnContext(context.Background(), "warn message")
	grouped.ErrorContext(context.Background(), "error message")

	records := capture.AllJSON()
	if len(records) != 4 {
		t.Fatalf("captured records = %d, want 4: %#v", len(records), records)
	}
	for i, want := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if records[i]["level"] != want {
			t.Fatalf("record %d level = %v, want %s: %#v", i, records[i]["level"], want, records[i])
		}
		request, ok := records[i]["request"].(map[string]any)
		if !ok || request["id"] != "req-1" {
			t.Fatalf("record %d request group = %#v, want id=req-1", i, records[i]["request"])
		}
	}
}

// DHF-TEST: keel/requirement-30
func TestConfigPerRunDocDescribesImplementedBehavior(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "logging.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse logging.go: %v", err)
	}

	var comment string
	ast.Inspect(file, func(node ast.Node) bool {
		typeSpec, ok := node.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != "Config" {
			return true
		}
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range structType.Fields.List {
			for _, name := range field.Names {
				if name.Name == "PerRun" && field.Doc != nil {
					comment = field.Doc.Text()
					return false
				}
			}
		}
		return false
	})

	if comment == "" {
		t.Fatal("Config.PerRun has no doc comment")
	}
	lower := strings.ToLower(comment)
	for _, forbidden := range []string{"reserved", "until requirement-19"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("Config.PerRun doc = %q; must describe implemented per-run JSONL behavior, not future-only behavior", comment)
		}
	}
	for _, want := range []string{"per-invocation", "JSONLDir", "RunLogPath", "RunLogLine", "daily"} {
		if !strings.Contains(comment, want) {
			t.Fatalf("Config.PerRun doc = %q; want mention of %q", comment, want)
		}
	}
}

// DHF-TEST: keel/requirement-29
func TestNewConfigReturnsErrorWhenFileSinkCannotOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  func(string) logging.Config
	}{
		{
			name: "TextDir",
			cfg: func(blocker string) logging.Config {
				return logging.Config{Service: "svc", Console: logging.ConsoleNone, TextDir: blocker}
			},
		},
		{
			name: "JSONLDir",
			cfg: func(blocker string) logging.Config {
				return logging.Config{Service: "svc", Console: logging.ConsoleNone, JSONLDir: blocker}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocker := filepath.Join(t.TempDir(), "not-a-directory")
			if err := os.WriteFile(blocker, []byte("file blocks log directory"), 0o600); err != nil {
				t.Fatalf("write blocker: %v", err)
			}

			logger, err := logging.New(tc.cfg(blocker))
			if err == nil {
				if logger != nil {
					_ = logger.Close()
				}
				t.Fatalf("New returned nil error for a blocked %s", tc.name)
			}
			if logger != nil {
				t.Fatalf("New returned logger %v with error %v; want no logger", logger, err)
			}
		})
	}
}

// DHF-TEST: keel/requirement-16
func TestNewConfigExposesFourSinkLoggerSurface(t *testing.T) {
	var console bytes.Buffer
	textDir := t.TempDir()
	jsonlDir := t.TempDir()

	logger := mustNewLogger(t, logging.Config{
		Service:          "svc",
		ConsoleVerbosity: slog.LevelDebug,
		FileVerbosity:    slog.LevelDebug,
		Console:          logging.ConsolePlain,
		Writer:           &console,
		TextDir:          textDir,
		JSONLDir:         jsonlDir,
		PerRun:           false,
		SourceInFiles:    true,
	})
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	logger.Info("hello", "password", "secret")
	logger.Event("process_start", "started", "token", "secret")
	logger.Header("keel-dev", "v0.0.0")
	logger.Section("Summary")
	logger.Field("Result", "pass")
	logger.With("request_id", "abc").WithGroup("group").Debug("child")

	if out := console.String(); !strings.Contains(out, "hello") || strings.Contains(out, "secret") {
		t.Fatalf("console output = %q, want redacted public logger output", out)
	}
	if matches, err := filepath.Glob(filepath.Join(textDir, "*.log")); err != nil || len(matches) != 1 {
		t.Fatalf("text sink matches = %v, err = %v; want one .log file", matches, err)
	}
	if matches, err := filepath.Glob(filepath.Join(jsonlDir, "*.jsonl")); err != nil || len(matches) != 1 {
		t.Fatalf("jsonl sink matches = %v, err = %v; want one .jsonl file", matches, err)
	}
}

// DHF-TEST: keel/requirement-22
func TestNewConfigFansOutToAdditionalHandlers(t *testing.T) {
	var console bytes.Buffer
	extra := newCaptureHandler()

	logger := mustNewLogger(t, logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleJSON,
		Writer:   &console,
		Handlers: []slog.Handler{extra},
	})

	logger.Info("hello", "answer", 42)

	if strings.TrimSpace(console.String()) == "" {
		t.Fatal("console sink was empty; want built-in sink to remain active")
	}
	if len(extra.state.records) != 1 {
		t.Fatalf("extra handler records = %d, want 1", len(extra.state.records))
	}
	if got := extra.state.attrs["service"]; got != "svc" {
		t.Fatalf("extra handler service attr = %#v, want svc", got)
	}
	if got := extra.state.records[0].Message; got != "hello" {
		t.Fatalf("extra handler message = %q, want hello", got)
	}
}

// DHF-TEST: keel/requirement-16, keel/requirement-22
func TestNewConfigRedactsBeforeAdditionalHandlerFanOut(t *testing.T) {
	var console bytes.Buffer
	extra := newCaptureHandler()

	logger := mustNewLogger(t, logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleJSON,
		Writer:   &console,
		Handlers: []slog.Handler{extra},
	})

	secret := "Bearer injected-handler-token"
	logger.Info("login failed "+secret, "token", secret, "detail", "dsn postgres://user:password@db/app")

	if len(extra.state.records) != 1 {
		t.Fatalf("extra handler records = %d, want 1", len(extra.state.records))
	}
	record := extra.state.records[0]
	renderedAttrs := make(map[string]string)
	record.Attrs(func(a slog.Attr) bool {
		renderedAttrs[a.Key] = a.Value.Resolve().String()
		return true
	})
	for name, got := range map[string]string{
		"message": record.Message,
		"token":   renderedAttrs["token"],
		"detail":  renderedAttrs["detail"],
	} {
		if strings.Contains(got, "injected-handler-token") || strings.Contains(got, "password") {
			t.Fatalf("extra handler %s leaked secret: %q", name, got)
		}
	}
	for name, got := range map[string]string{
		"message": record.Message,
		"token":   renderedAttrs["token"],
	} {
		if !strings.Contains(got, "[REDACTED]") {
			t.Fatalf("extra handler %s = %q, want bearer redaction marker", name, got)
		}
	}
}

// DHF-TEST: keel/requirement-19
func TestNewConfigPerRunJSONLSinkUsesInvocationFileAndTracksLines(t *testing.T) {
	jsonlDir := t.TempDir()
	logger := mustNewLogger(t, logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleNone,
		JSONLDir: jsonlDir,
		PerRun:   true,
	})
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	logger.Info("first")
	if got := logger.RunLogLine(); got != 1 {
		t.Fatalf("RunLogLine after first record = %d, want 1", got)
	}
	logger.Info("second")
	if got := logger.RunLogLine(); got != 2 {
		t.Fatalf("RunLogLine after second record = %d, want 2", got)
	}

	path := logger.RunLogPath()
	if path == "" {
		t.Fatal("RunLogPath is empty; want per-run JSONL path")
	}
	if filepath.Dir(path) != jsonlDir {
		t.Fatalf("RunLogPath dir = %q, want %q", filepath.Dir(path), jsonlDir)
	}
	if !strings.HasSuffix(filepath.Base(path), ".jsonl") || strings.Contains(filepath.Base(path), "svc-") {
		t.Fatalf("RunLogPath base = %q, want per-run <timestamp>-<run>.jsonl rather than daily service file", filepath.Base(path))
	}
	if matches, err := filepath.Glob(filepath.Join(jsonlDir, "*.jsonl")); err != nil || len(matches) != 1 || matches[0] != path {
		t.Fatalf("jsonl matches = %v, err = %v; want only %q", matches, err, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("run log lines = %d (%q), want 2 whole-record lines", len(lines), string(data))
	}
}

// DHF-TEST: keel/requirement-56
func TestLevelConvertersUseStrictLowercaseVocabulary(t *testing.T) {
	for name, level := range map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	} {
		got, err := logging.LevelFromString(name)
		if err != nil {
			t.Fatalf("LevelFromString(%q) returned error: %v", name, err)
		}
		if got != level {
			t.Fatalf("LevelFromString(%q) = %s, want %s", name, got, level)
		}
		if roundTrip := logging.LevelToString(level); roundTrip != name {
			t.Fatalf("LevelToString(%s) = %q, want %q", level, roundTrip, name)
		}
	}

	got, err := logging.LevelFromString("")
	if err != nil {
		t.Fatalf("LevelFromString(empty) returned error: %v", err)
	}
	if got != slog.LevelInfo {
		t.Fatalf("LevelFromString(empty) = %s, want info", got)
	}
	if _, err := logging.LevelFromString("warning"); err == nil {
		t.Fatal("LevelFromString(\"warning\") returned nil error; want strict lowercase vocabulary only")
	}
	if _, err := logging.LevelFromString("bogus"); err == nil {
		t.Fatal("LevelFromString(\"bogus\") returned nil error; want unknown non-empty input rejected")
	}
}

// DHF-TEST: keel/requirement-56
func TestConfigVerbositySplitsConsoleAndFileSinks(t *testing.T) {
	var console bytes.Buffer
	textDir := t.TempDir()
	jsonlDir := t.TempDir()

	logger := mustNewLogger(t, logging.Config{
		Service:          "svc",
		ConsoleVerbosity: slog.LevelWarn,
		FileVerbosity:    slog.LevelError,
		Console:          logging.ConsolePlain,
		Writer:           &console,
		TextDir:          textDir,
		JSONLDir:         jsonlDir,
	})
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	logger.Info("info-record")
	logger.Warn("warn-record")
	logger.Error("error-record")

	if out := console.String(); strings.Contains(out, "info-record") || !strings.Contains(out, "warn-record") || !strings.Contains(out, "error-record") {
		t.Fatalf("console output = %q, want warn+error only", out)
	}

	textBytes, err := os.ReadFile(logger.TextLogPath())
	if err != nil {
		t.Fatalf("read text sink: %v", err)
	}
	text := string(textBytes)
	if strings.Contains(text, "info-record") || strings.Contains(text, "warn-record") || !strings.Contains(text, "error-record") {
		t.Fatalf("text sink = %q, want error only", text)
	}

	jsonlBytes, err := os.ReadFile(logger.JSONLLogPath())
	if err != nil {
		t.Fatalf("read jsonl sink: %v", err)
	}
	jsonl := string(jsonlBytes)
	if strings.Contains(jsonl, "info-record") || strings.Contains(jsonl, "warn-record") || !strings.Contains(jsonl, "error-record") {
		t.Fatalf("jsonl sink = %q, want error only", jsonl)
	}
}

// DHF-TEST: keel/requirement-56
func TestConfigVerbosityDefaultsPreserveConsoleInfoAndFileDebug(t *testing.T) {
	var console bytes.Buffer
	textDir := t.TempDir()
	jsonlDir := t.TempDir()

	logger := mustNewLogger(t, logging.Config{
		Service:  "svc",
		Console:  logging.ConsolePlain,
		Writer:   &console,
		TextDir:  textDir,
		JSONLDir: jsonlDir,
	})
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	logger.Debug("debug-record")
	logger.Info("info-record")

	if out := console.String(); strings.Contains(out, "debug-record") || !strings.Contains(out, "info-record") {
		t.Fatalf("console output = %q, want default info threshold", out)
	}
	textBytes, err := os.ReadFile(logger.TextLogPath())
	if err != nil {
		t.Fatalf("read text sink: %v", err)
	}
	if text := string(textBytes); !strings.Contains(text, "debug-record") || !strings.Contains(text, "info-record") {
		t.Fatalf("text sink = %q, want default debug threshold", text)
	}
	jsonlBytes, err := os.ReadFile(logger.JSONLLogPath())
	if err != nil {
		t.Fatalf("read jsonl sink: %v", err)
	}
	if jsonl := string(jsonlBytes); !strings.Contains(jsonl, "debug-record") || !strings.Contains(jsonl, "info-record") {
		t.Fatalf("jsonl sink = %q, want default debug threshold", jsonl)
	}
}

// DHF-TEST: keel/requirement-56
func TestLoggerDailySinkPathAccessorsReturnOpenedPaths(t *testing.T) {
	textDir := t.TempDir()
	jsonlDir := t.TempDir()
	logger := mustNewLogger(t, logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleNone,
		TextDir:  textDir,
		JSONLDir: jsonlDir,
	})
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	if got, want := logger.TextLogPath(), textLogPath(textDir, "svc"); got != want {
		t.Fatalf("TextLogPath = %q, want opened daily text path %q", got, want)
	}
	if got, want := logger.JSONLLogPath(), jsonLogPath(jsonlDir, "svc"); got != want {
		t.Fatalf("JSONLLogPath = %q, want opened daily JSONL path %q", got, want)
	}

	noFiles := mustNewLogger(t, logging.Config{Console: logging.ConsoleNone})
	if got := noFiles.TextLogPath(); got != "" {
		t.Fatalf("TextLogPath without TextDir = %q, want empty", got)
	}
	if got := noFiles.JSONLLogPath(); got != "" {
		t.Fatalf("JSONLLogPath without JSONLDir = %q, want empty", got)
	}
}

// DHF-TEST: keel/requirement-56
func TestBuildIdentityPublicReExposure(t *testing.T) {
	logger := mustNewLogger(t, logging.Config{Console: logging.ConsoleNone})
	ctx, cancel := context.WithCancel(context.Background())
	logger.StartDailyBuildIdentity(ctx, "1.2.3", "abc1234")
	cancel()

	if got := logging.ResolveGitCommit("abc1234"); got != "abc1234" {
		t.Fatalf("ResolveGitCommit explicit = %q, want abc1234", got)
	}
	if got := logging.ResolveGitCommit(""); got == "" {
		t.Fatal("ResolveGitCommit empty returned empty string")
	}
}

// DHF-TEST: keel/requirement-17, keel/requirement-20
func TestSparseAIConsoleEmitsCuratedEventsAndKeepsDebugChildOutputInFiles(t *testing.T) {
	var console bytes.Buffer
	textDir := t.TempDir()
	jsonlDir := t.TempDir()

	logger := mustNewLogger(t, logging.Config{
		Service:          "svc",
		ConsoleVerbosity: slog.LevelInfo,
		FileVerbosity:    slog.LevelDebug,
		Console:          logging.ConsoleSparseAI,
		Writer:           &console,
		TextDir:          textDir,
		JSONLDir:         jsonlDir,
	})
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	logger.Event("step", "running gate", "step", "test", "attempt", 1)
	logger.Debug("child stdout", "event_type", "process_output", "stream", "stdout", "data", "noise")

	lines := strings.Split(strings.TrimSpace(console.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("sparse console lines = %#v, want one curated info event and no debug child output", lines)
	}
	var sparse map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &sparse); err != nil {
		t.Fatalf("sparse console line is not JSON: %q: %v", lines[0], err)
	}
	if got, _ := sparse["level"].(string); got != "INFO" {
		t.Fatalf("sparse level = %q, want INFO", got)
	}
	if got, _ := sparse["event"].(string); got != "step" {
		t.Fatalf("sparse event = %q, want step", got)
	}
	if got, _ := sparse["message"].(string); got != "running gate" {
		t.Fatalf("sparse message = %q, want running gate", got)
	}
	fields, ok := sparse["fields"].(map[string]any)
	if !ok {
		t.Fatalf("sparse fields = %#v, want object", sparse["fields"])
	}
	if got, _ := fields["step"].(string); got != "test" {
		t.Fatalf("sparse fields.step = %#v, want test", fields["step"])
	}
	if _, ok := sparse["ts"]; ok {
		t.Fatalf("sparse console exposed verbose ts field: %#v", sparse)
	}
	if _, ok := sparse["msg"]; ok {
		t.Fatalf("sparse console exposed verbose msg field: %#v", sparse)
	}
	if strings.Contains(console.String(), "child stdout") || strings.Contains(console.String(), "noise") {
		t.Fatalf("sparse console included debug child output: %q", console.String())
	}

	textLogs, err := filepath.Glob(filepath.Join(textDir, "*.log"))
	if err != nil || len(textLogs) != 1 {
		t.Fatalf("text sink matches = %v, err = %v; want one .log file", textLogs, err)
	}
	textBytes, err := os.ReadFile(textLogs[0])
	if err != nil {
		t.Fatalf("read text sink: %v", err)
	}
	if !strings.Contains(string(textBytes), "child stdout") || !strings.Contains(string(textBytes), "noise") {
		t.Fatalf("text sink = %q, want debug child output", string(textBytes))
	}
	jsonLogs, err := filepath.Glob(filepath.Join(jsonlDir, "*.jsonl"))
	if err != nil || len(jsonLogs) != 1 {
		t.Fatalf("jsonl sink matches = %v, err = %v; want one .jsonl file", jsonLogs, err)
	}
	jsonBytes, err := os.ReadFile(jsonLogs[0])
	if err != nil {
		t.Fatalf("read jsonl sink: %v", err)
	}
	if !strings.Contains(string(jsonBytes), `"level":"DEBUG"`) || !strings.Contains(string(jsonBytes), `"data":"noise"`) {
		t.Fatalf("jsonl sink = %q, want debug child output record", string(jsonBytes))
	}
}

// DHF-TEST: keel/requirement-17
func TestConsoleJSONRemainsVerboseRecordRendering(t *testing.T) {
	var console bytes.Buffer
	logger := mustNewLogger(t, logging.Config{
		Service:          "svc",
		ConsoleVerbosity: slog.LevelInfo,
		Console:          logging.ConsoleJSON,
		Writer:           &console,
	})

	logger.Event("step", "running gate", "step", "test")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(console.Bytes()), &record); err != nil {
		t.Fatalf("verbose JSON console line is not JSON: %q: %v", console.String(), err)
	}
	for _, key := range []string{"ts", "level", "msg", "service", "event_type"} {
		if _, ok := record[key]; !ok {
			t.Fatalf("verbose JSON console missing %q in %#v", key, record)
		}
	}
	if _, ok := record["event"]; ok {
		t.Fatalf("verbose JSON console used sparse event field: %#v", record)
	}
	if _, ok := record["fields"]; ok {
		t.Fatalf("verbose JSON console used sparse fields object: %#v", record)
	}
}

// DHF-TEST: keel/requirement-20
func TestJSONOutput_HasG1Fields(t *testing.T) {
	logger, capture := newJSONCaptureLogger(t, "mcp")

	logger.Info("test message")

	got := capture.LastJSON()
	if got == nil {
		t.Fatal("LastJSON returned nil -- no log output captured")
	}

	// ts field must be present and RFC3339Nano-ish.
	ts, ok := got["ts"].(string)
	if !ok || ts == "" {
		t.Error("missing or empty 'ts' field")
	} else if !rfc3339NanoRegex.MatchString(ts) {
		t.Errorf("ts = %q, does not match RFC3339Nano pattern", ts)
	}

	// level must be uppercase.
	level, ok := got["level"].(string)
	if !ok || level == "" {
		t.Error("missing or empty 'level' field")
	} else if level != "INFO" {
		t.Errorf("level = %q, want uppercase 'INFO'", level)
	}

	// msg must be present.
	msg, ok := got["msg"].(string)
	if !ok || msg != "test message" {
		t.Errorf("msg = %q, want 'test message'", msg)
	}

	// service must be present.
	svc, ok := got["service"].(string)
	if !ok || svc != "mcp" {
		t.Errorf("service = %q, want 'mcp'", svc)
	}
}

// DHF-TEST: keel/requirement-20
func TestJSONOutput_LevelIsUppercase(t *testing.T) {
	tests := []struct {
		name  string
		logFn func(*slog.Logger)
		want  string
	}{
		{"debug", func(l *slog.Logger) { l.Debug("d") }, "DEBUG"},
		{"info", func(l *slog.Logger) { l.Info("i") }, "INFO"},
		{"warn", func(l *slog.Logger) { l.Warn("w") }, "WARN"},
		{"error", func(l *slog.Logger) { l.Error("e") }, "ERROR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger, capture := newJSONCaptureLogger(t, "mcp")
			tc.logFn(logger)
			got := capture.LastJSON()
			if got == nil {
				t.Skip("log level below threshold -- not captured")
			}
			if level, _ := got["level"].(string); level != tc.want {
				t.Errorf("level = %q, want %q", level, tc.want)
			}
		})
	}
}

func TestContextualFields_AppearInJSON(t *testing.T) {
	logger, capture := newJSONCaptureLogger(t, "mcp")

	child := logger.With("req_id", "abc-123", "product", "openbrain", "memory_id", 42)
	child.Info("contextual test")

	got := capture.LastJSON()
	if got == nil {
		t.Fatal("no log output captured")
	}

	for _, key := range []string{"req_id", "product", "memory_id"} {
		if _, ok := got[key]; !ok {
			t.Errorf("expected contextual field %q to be present in JSON output", key)
		}
	}
}

func TestLogger_IsTransparentNotAFilter(t *testing.T) {
	// The logger must NOT filter forbidden fields -- that is a call-site
	// discipline enforced by audits, not by the logger. This test proves
	// the logger is transparent: logging "content" as an attribute key
	// MUST appear in the output.
	logger, capture := newJSONCaptureLogger(t, "mcp")

	logger.Info("test", "content", "secret-memory-body")

	got := capture.LastJSON()
	if got == nil {
		t.Fatal("no log output captured")
	}

	val, ok := got["content"].(string)
	if !ok || val != "secret-memory-body" {
		t.Errorf("expected 'content' field to be present and equal 'secret-memory-body', got %v", got["content"])
	}
}

func TestRedactErr_StripsDSNPassword(t *testing.T) {
	original := errors.New("connect postgres://admin:s3cret@db.host:5432/mydb: connection refused")
	redacted := logging.RedactErr(original)

	s := redacted.Error()
	if strings.Contains(s, "s3cret") {
		t.Errorf("RedactErr output still contains password: %s", s)
	}
	if !strings.Contains(s, "connection refused") {
		t.Errorf("RedactErr should preserve the non-sensitive part, got: %s", s)
	}
}

func TestRedactErr_StripsBearerToken(t *testing.T) {
	original := errors.New("auth failed: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig was rejected")
	redacted := logging.RedactErr(original)

	s := redacted.Error()
	if strings.Contains(s, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("RedactErr output still contains bearer token: %s", s)
	}
}

func TestRedactErr_StripsCredentialParams(t *testing.T) {
	// issue-0111: credentials outside the userinfo position — query params and
	// libpq keyword DSNs — must redact; credential-free strings pass unchanged.
	tests := []struct {
		name       string
		input      string
		mustHide   string // secret that must not survive; "" = passthrough case
		wantMarker string // redaction marker expected in output
	}{
		{
			name:       "userinfo form (existing contract)",
			input:      "connect postgres://admin:s3cret@db.host:5432/mydb: refused",
			mustHide:   "s3cret",
			wantMarker: "://***:***@",
		},
		{
			name:       "query-param password",
			input:      "connect postgres://db.host:5432/mydb?password=qp-s3cret&sslmode=require: refused",
			mustHide:   "qp-s3cret",
			wantMarker: "password=***",
		},
		{
			name:       "query-param sslpassword",
			input:      "connect postgres://db.host:5432/mydb?sslmode=require&sslpassword=ssl-s3cret: refused",
			mustHide:   "ssl-s3cret",
			wantMarker: "sslpassword=***",
		},
		{
			name:       "libpq keyword DSN",
			input:      "parse config: host=db.host password=kw-s3cret dbname=mydb: invalid",
			mustHide:   "kw-s3cret",
			wantMarker: "password=***",
		},
		{
			name:  "credential-free URL passes through unchanged",
			input: "GET https://db.host:5432/healthz?timeout=5s returned 503",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := logging.RedactErr(errors.New(tc.input)).Error()
			if tc.mustHide == "" {
				if got != tc.input {
					t.Errorf("credential-free input was altered:\n in:  %s\n out: %s", tc.input, got)
				}
				return
			}
			if strings.Contains(got, tc.mustHide) {
				t.Errorf("output still contains secret %q: %s", tc.mustHide, got)
			}
			if !strings.Contains(got, tc.wantMarker) {
				t.Errorf("output missing redaction marker %q: %s", tc.wantMarker, got)
			}
		})
	}
}

func TestRedactErr_NilIsNil(t *testing.T) {
	if logging.RedactErr(nil) != nil {
		t.Error("RedactErr(nil) should return nil")
	}
}

func TestRecordCapture_Reset(t *testing.T) {
	logger, capture := newJSONCaptureLogger(t, "mcp")

	logger.Info("first")
	capture.Reset()
	got := capture.LastJSON()
	if got != nil {
		t.Errorf("after Reset(), LastJSON() should return nil, got %v", got)
	}

	logger.Info("second")
	got = capture.LastJSON()
	if got == nil {
		t.Fatal("after logging post-Reset, LastJSON() should return data")
	}
	if msg, _ := got["msg"].(string); msg != "second" {
		t.Errorf("msg = %q, want 'second'", msg)
	}
}

// DHF-TEST: keel/requirement-20
func TestJSONOutput_NoTimeField(t *testing.T) {
	// The G1 schema uses "ts", not "time". Verify "time" is absent.
	logger, capture := newJSONCaptureLogger(t, "mcp")
	logger.Info("check time key")

	raw := capture.LastRaw()
	if raw == "" {
		t.Fatal("no raw output captured")
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("failed to parse raw JSON: %v", err)
	}
	if _, ok := m["time"]; ok {
		t.Error("JSON output contains 'time' key -- should be renamed to 'ts'")
	}
	if _, ok := m["ts"]; !ok {
		t.Error("JSON output missing 'ts' key")
	}
}

// DHF-TEST: keel/requirement-20
func TestModuleContextAppearsOnlyInFileSinks(t *testing.T) {
	var console bytes.Buffer
	textDir := t.TempDir()
	jsonlDir := t.TempDir()
	logger := mustNewLogger(t, logging.Config{
		Service:          "svc",
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsolePlain,
		Writer:           &console,
		TextDir:          textDir,
		JSONLDir:         jsonlDir,
	}).With("module", "keel/exec")
	t.Cleanup(func() { _ = logger.Close() })

	logger.Info("module context", "operation", "run")

	if got := console.String(); strings.Contains(got, "module=keel/exec") {
		t.Fatalf("console included module context reserved for file sinks: %q", got)
	}

	textData, err := os.ReadFile(textLogPath(textDir, "svc"))
	if err != nil {
		t.Fatalf("read human file sink: %v", err)
	}
	if got := string(textData); !strings.Contains(got, "module=keel/exec") {
		t.Fatalf("human file sink missing module context: %q", got)
	}

	jsonData, err := os.ReadFile(jsonLogPath(jsonlDir, "svc"))
	if err != nil {
		t.Fatalf("read JSONL file sink: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(jsonData), &record); err != nil {
		t.Fatalf("parse JSONL file sink: %v; body=%q", err, string(jsonData))
	}
	if record["module"] != "keel/exec" {
		t.Fatalf("JSONL file sink module = %#v, want keel/exec; record=%#v", record["module"], record)
	}
}

// DHF-TEST: keel/requirement-20
func TestGroupedModuleContextAppearsOnlyInFileSinks(t *testing.T) {
	var console bytes.Buffer
	textDir := t.TempDir()
	jsonlDir := t.TempDir()
	logger := mustNewLogger(t, logging.Config{
		Service:          "svc",
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsolePlain,
		Writer:           &console,
		TextDir:          textDir,
		JSONLDir:         jsonlDir,
	}).WithGroup("ctx").With("module", "keel/exec")
	t.Cleanup(func() { _ = logger.Close() })

	logger.Info("grouped module context", "operation", "run")

	if got := console.String(); strings.Contains(got, "module=") || strings.Contains(got, "ctx.module") || strings.Contains(got, "keel/exec") {
		t.Fatalf("console included grouped module context reserved for file sinks: %q", got)
	}

	textData, err := os.ReadFile(textLogPath(textDir, "svc"))
	if err != nil {
		t.Fatalf("read human file sink: %v", err)
	}
	if got := string(textData); !strings.Contains(got, "keel/exec") || !strings.Contains(got, "ctx.module=keel/exec") {
		t.Fatalf("human file sink missing grouped module context: %q", got)
	}

	jsonData, err := os.ReadFile(jsonLogPath(jsonlDir, "svc"))
	if err != nil {
		t.Fatalf("read JSONL file sink: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(jsonData), &record); err != nil {
		t.Fatalf("parse JSONL file sink: %v; body=%q", err, string(jsonData))
	}
	group, ok := record["ctx"].(map[string]any)
	if !ok {
		t.Fatalf("JSONL file sink ctx = %#v, want object; record=%#v", record["ctx"], record)
	}
	if group["module"] != "keel/exec" {
		t.Fatalf("JSONL file sink ctx.module = %#v, want keel/exec; record=%#v", group["module"], record)
	}
}

// DHF-TEST: keel/requirement-20
func TestSourceInFilesDefaultsOffAndCanBeEnabled(t *testing.T) {
	defaultDir := t.TempDir()
	defaultLogger := mustNewLogger(t, logging.Config{
		Service: "svc",
		Console: logging.ConsoleNone,
		TextDir: defaultDir,
	})
	defaultLogger.Info("default source")
	if err := defaultLogger.Close(); err != nil {
		t.Fatalf("close default logger: %v", err)
	}
	defaultData, err := os.ReadFile(textLogPath(defaultDir, "svc"))
	if err != nil {
		t.Fatalf("read default human file sink: %v", err)
	}
	if got := string(defaultData); strings.Contains(got, "logging_test.go") {
		t.Fatalf("SourceInFiles default emitted caller source: %q", got)
	}

	sourceDir := t.TempDir()
	sourceLogger := mustNewLogger(t, logging.Config{
		Service:       "svc",
		Console:       logging.ConsoleNone,
		TextDir:       sourceDir,
		SourceInFiles: true,
	})
	sourceLogger.Info("source enabled")
	if err := sourceLogger.Close(); err != nil {
		t.Fatalf("close source logger: %v", err)
	}
	sourceData, err := os.ReadFile(textLogPath(sourceDir, "svc"))
	if err != nil {
		t.Fatalf("read source human file sink: %v", err)
	}
	if got := string(sourceData); !strings.Contains(got, "logging_test.go") {
		t.Fatalf("SourceInFiles=true missing caller source: %q", got)
	}

	jsonlDir := t.TempDir()
	jsonlLogger := mustNewLogger(t, logging.Config{
		Service:       "svc",
		Console:       logging.ConsoleNone,
		JSONLDir:      jsonlDir,
		SourceInFiles: true,
	})
	jsonlLogger.Info("json source enabled")
	if err := jsonlLogger.Close(); err != nil {
		t.Fatalf("close jsonl logger: %v", err)
	}
	jsonlData, err := os.ReadFile(jsonLogPath(jsonlDir, "svc"))
	if err != nil {
		t.Fatalf("read source JSONL file sink: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(jsonlData), &record); err != nil {
		t.Fatalf("parse source JSONL file sink: %v; body=%q", err, string(jsonlData))
	}
	source, ok := record["source"].(string)
	if !ok || !strings.Contains(source, "logging_test.go") {
		t.Fatalf("SourceInFiles=true JSONL source = %#v, want caller file; record=%#v", record["source"], record)
	}
}

// DHF-TEST: openbrain/requirement-602
func TestJSONOutput_FileSinkRetainsDebugWhenConsoleUsesInfo(t *testing.T) {
	var console bytes.Buffer
	dir := t.TempDir()
	logger := mustNewLogger(t, logging.Config{
		Service:          "openbrain-client",
		ConsoleVerbosity: slog.LevelInfo,
		Console:          logging.ConsoleJSON,
		Writer:           &console,
		JSONLDir:         dir,
	})

	logger.Debug("debug detail", "chunk", "stdout payload")
	logger.Info("visible info")
	if err := logger.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}

	consoleRaw := console.String()
	if strings.Contains(consoleRaw, "debug detail") || strings.Contains(consoleRaw, "stdout payload") {
		t.Fatalf("console emitted DEBUG below INFO threshold: %q", consoleRaw)
	}
	if !strings.Contains(consoleRaw, "visible info") {
		t.Fatalf("console did not emit INFO record: %q", consoleRaw)
	}

	fileData, err := os.ReadFile(jsonLogPath(dir, "openbrain-client"))
	if err != nil {
		t.Fatalf("read JSONL file sink: %v", err)
	}
	fileRaw := string(fileData)
	if !strings.Contains(fileRaw, "debug detail") || !strings.Contains(fileRaw, "stdout payload") {
		t.Fatalf("file sink did not retain DEBUG detail: %q", fileRaw)
	}
	if !strings.Contains(fileRaw, "visible info") {
		t.Fatalf("file sink did not retain INFO record: %q", fileRaw)
	}
}

// DHF-TEST: keel/requirement-5, openbrain/requirement-151
func TestConsoleOutput_HumanReadableAndRedacted(t *testing.T) {
	capture := &recordCapture{}
	logger := mustNewLogger(t, logging.Config{Console: logging.ConsolePlain,
		Service:          "cli",
		ConsoleVerbosity: slog.LevelDebug,
		Writer:           capture,
		ConsoleOmitKeys:  []string{"service"},
	})

	logger.Warn("connect failed", "dsn", "postgres://admin:s3cret@db.host:5432/openbrain", "attempt", 2)

	raw := capture.LastRaw()
	if raw == "" {
		t.Fatal("no console output captured")
	}
	if !regexp.MustCompile(`^\d{2}:\d{2}:\d{2} WARN  connect failed`).MatchString(raw) {
		t.Fatalf("console output missing short human severity prefix: %q", raw)
	}
	if !strings.Contains(raw, "connect failed") {
		t.Fatalf("console output missing human-readable message: %q", raw)
	}
	if strings.Contains(raw, "service=cli") {
		t.Fatalf("console output included denylisted service field: %q", raw)
	}
	if strings.Contains(raw, "s3cret") {
		t.Fatalf("console output leaked DSN password: %q", raw)
	}
	if !strings.Contains(raw, "postgres://***:***@db.host:5432/openbrain") {
		t.Fatalf("console output missing redacted DSN: %q", raw)
	}
}

// DHF-TEST: openbrain/requirement-151, openbrain/requirement-32
func TestConsoleOutput_OmitsConfiguredContextKeysButJSONRetainsThem(t *testing.T) {
	var consoleBuf bytes.Buffer
	consoleLogger := mustNewLogger(t, logging.Config{Console: logging.ConsolePlain,
		Service:          "openbrain-client",
		ConsoleVerbosity: slog.LevelDebug,
		Writer:           &consoleBuf,
		ConsoleOmitKeys:  []string{"service", "cr", "verb"},
	}).With("cr", "openbrain/change_request-394", "verb", "dev")
	consoleLogger.Info("disk check", "free_mib", 2048)

	consoleRaw := consoleBuf.String()
	for _, omitted := range []string{"service=openbrain-client", "cr=openbrain/change_request-394", "verb=dev"} {
		if strings.Contains(consoleRaw, omitted) {
			t.Fatalf("console output included denylisted attr %q in %q", omitted, consoleRaw)
		}
	}
	if !strings.Contains(consoleRaw, "free_mib=2048") {
		t.Fatalf("console output dropped per-event attr: %q", consoleRaw)
	}

	jsonLogger, jsonCapture := newJSONCaptureLogger(t, "openbrain-client")
	jsonLogger.With("cr", "openbrain/change_request-394", "verb", "dev").Info("disk check", "free_mib", 2048)
	got := jsonCapture.LastJSON()
	for _, key := range []string{"service", "cr", "verb", "free_mib"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("JSON output missing %q in %#v", key, got)
		}
	}
}

// DHF-TEST: openbrain/requirement-151
func TestConsoleOutput_UsesStructuredHumanRunStyle(t *testing.T) {
	var buf bytes.Buffer
	logger := mustNewLogger(t, logging.Config{Console: logging.ConsolePlain,
		Service:          "openbrain-dev",
		ConsoleVerbosity: slog.LevelDebug,
		Writer:           &buf,
	})

	logger.Header("openbrain-dev", "v0.0.0-dev")
	logger.Fields([]logging.FieldRow{
		{Label: "Directory", Value: "./Product/"},
		{Label: "Files to process", Value: 4},
	})
	logger.Warn("Id hash collision detected within 01:00:00 window.")
	logger.Section("Summary")
	logger.Field("Save result", "./2026-05-29_to_06-08_D.xlsx")
	logger.Info("Done.")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 10 {
		t.Fatalf("captured %d lines, want 10:\n%s", len(lines), buf.String())
	}
	for _, line := range lines {
		if !regexp.MustCompile(`^\d{2}:\d{2}:\d{2} (INFO|WARN)  `).MatchString(line) {
			t.Fatalf("line missing HH:MM:SS fixed-width severity prefix: %q", line)
		}
	}
	if !strings.Contains(lines[0], strings.Repeat("=", 70)) || !strings.Contains(lines[2], strings.Repeat("=", 70)) {
		t.Fatalf("header is not framed by 70-character rules:\n%s", buf.String())
	}
	if !strings.Contains(lines[1], "openbrain-dev v0.0.0-dev") {
		t.Fatalf("header title/version missing from %q", lines[1])
	}
	if !strings.Contains(lines[3], "Directory        : ./Product/") {
		t.Fatalf("shorter field label was not aligned: %q", lines[3])
	}
	if !strings.Contains(lines[4], "Files to process : 4") {
		t.Fatalf("longer field label was not aligned: %q", lines[4])
	}
	if !strings.Contains(lines[5], "WARN  Id hash collision detected within 01:00:00 window.") {
		t.Fatalf("warning was not inline in the same stream: %q", lines[5])
	}
	if !strings.Contains(lines[6], strings.Repeat("-", 70)) || !strings.Contains(lines[7], "Summary") {
		t.Fatalf("section header is not rendered through logger:\n%s", buf.String())
	}
	if !strings.Contains(lines[9], "Done.") {
		t.Fatalf("completion marker missing: %q", lines[9])
	}
}

// DHF-TEST: keel/requirement-24
func TestConsoleOutput_RendersBannersByConsoleMode(t *testing.T) {
	var plain bytes.Buffer
	plainLogger := mustNewLogger(t, logging.Config{
		Console:          logging.ConsolePlain,
		Service:          "keel-dev",
		ConsoleVerbosity: slog.LevelDebug,
		Writer:           &plain,
	})
	plainLogger.Header("keel-dev ci", "v1.2.3")
	plainLogger.Section("Gate")

	plainLines := strings.Split(strings.TrimSpace(plain.String()), "\n")
	if len(plainLines) != 5 {
		t.Fatalf("plain banner line count = %d, want 5:\n%s", len(plainLines), plain.String())
	}
	if !strings.Contains(plainLines[0], strings.Repeat("=", 70)) ||
		!strings.Contains(plainLines[1], "keel-dev ci v1.2.3") ||
		!strings.Contains(plainLines[2], strings.Repeat("=", 70)) {
		t.Fatalf("plain header did not render as human rule/title/rule:\n%s", plain.String())
	}
	if !strings.Contains(plainLines[3], strings.Repeat("-", 70)) ||
		strings.Contains(plainLines[3], "Gate") ||
		!strings.Contains(plainLines[4], "Gate") {
		t.Fatalf("plain section did not render as rule then name:\n%s", plain.String())
	}
	if strings.Contains(plain.String(), "banner=") {
		t.Fatalf("plain console leaked banner attr: %q", plain.String())
	}

	var sparse bytes.Buffer
	sparseLogger := mustNewLogger(t, logging.Config{
		Console: logging.ConsoleSparseAI,
		Service: "keel-dev",
		Writer:  &sparse,
	})
	sparseLogger.Header("keel-dev ci", "v1.2.3")
	sparseLogger.Section("Gate")
	sparseRecords := parseSparseAILogRecords(t, sparse.String())
	if len(sparseRecords) != 2 {
		t.Fatalf("sparse banner records = %d, want 2:\n%s", len(sparseRecords), sparse.String())
	}
	if got := sparseRecords[0]["event"]; got != "header" {
		t.Fatalf("sparse header event = %#v, want header; record=%#v", got, sparseRecords[0])
	}
	if got := sparseRecords[1]["event"]; got != "section" {
		t.Fatalf("sparse section event = %#v, want section; record=%#v", got, sparseRecords[1])
	}
	if strings.Contains(sparse.String(), strings.Repeat("=", 10)) || strings.Contains(sparse.String(), strings.Repeat("-", 10)) {
		t.Fatalf("sparse console emitted human rules: %q", sparse.String())
	}

	var jsonBuf bytes.Buffer
	jsonLogger := mustNewLogger(t, logging.Config{
		Console: logging.ConsoleJSON,
		Service: "keel-dev",
		Writer:  &jsonBuf,
	})
	jsonLogger.Header("keel-dev ci", "v1.2.3")
	jsonLogger.Section("Gate")
	jsonRecords := parseJSONLines(t, jsonBuf.String())
	if len(jsonRecords) != 2 {
		t.Fatalf("json banner records = %d, want 2:\n%s", len(jsonRecords), jsonBuf.String())
	}
	if got, ok := jsonRecords[0]["banner"].(string); !ok || got != "header" {
		t.Fatalf("json header banner = %#v, want header; record=%#v", jsonRecords[0]["banner"], jsonRecords[0])
	}
	if got, ok := jsonRecords[1]["banner"].(string); !ok || got != "section" {
		t.Fatalf("json section banner = %#v, want section; record=%#v", jsonRecords[1]["banner"], jsonRecords[1])
	}
	if strings.Contains(jsonBuf.String(), strings.Repeat("=", 10)) || strings.Contains(jsonBuf.String(), strings.Repeat("-", 10)) {
		t.Fatalf("json console emitted human rules: %q", jsonBuf.String())
	}
}

// DHF-TEST: openbrain/requirement-152
func TestConsoleOutput_LevelThresholdAndColorGating(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	var plain bytes.Buffer
	plainLogger := mustNewLogger(t, logging.Config{Console: logging.ConsolePlain,
		Service:          "cli",
		ConsoleVerbosity: slog.LevelInfo,
		Writer:           &plain,
	})
	plainLogger.Debug("hidden")
	plainLogger.Info("visible")
	got := plain.String()
	if strings.Contains(got, "hidden") {
		t.Fatalf("console emitted DEBUG below INFO threshold: %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-TTY test writer emitted ANSI color: %q", got)
	}
	if !regexp.MustCompile(`^\d{2}:\d{2}:\d{2} INFO  visible`).MatchString(strings.TrimSpace(got)) {
		t.Fatalf("console did not use concrete HH:MM:SS LVL format: %q", got)
	}

	var colored bytes.Buffer
	colorLogger := mustNewLogger(t, logging.Config{Console: logging.ConsolePlain,
		Service:          "cli",
		ConsoleVerbosity: slog.LevelDebug,
		Writer:           &colored,
		ForceColor:       true,
		DisableColor:     false,
	})
	colorLogger.Warn("careful")
	if got := colored.String(); !strings.Contains(got, "\x1b[90m") || !strings.Contains(got, "\x1b[33mWARN\x1b[0m") {
		t.Fatalf("forced color output missing gray timestamp or yellow WARN: %q", got)
	}

	t.Setenv("NO_COLOR", "1")
	var noColor bytes.Buffer
	noColorLogger := mustNewLogger(t, logging.Config{Console: logging.ConsolePlain,
		Service:          "cli",
		ConsoleVerbosity: slog.LevelDebug,
		Writer:           &noColor,
		ForceColor:       true,
	})
	noColorLogger.Error("plain error")
	if got := noColor.String(); strings.Contains(got, "\x1b[") {
		t.Fatalf("NO_COLOR did not disable ANSI color: %q", got)
	}
}

// DHF-TEST: openbrain/requirement-152
func TestConsoleOutput_WritesRollingHumanFileAtDebugAndRetainsTen(t *testing.T) {
	dir := t.TempDir()
	for day := 1; day <= 11; day++ {
		if err := os.WriteFile(filepath.Join(dir, "openbrain-dev-2026-05-"+twoDigit(day)+".log"), []byte("old\n"), 0o600); err != nil {
			t.Fatalf("seed old log: %v", err)
		}
	}

	var console bytes.Buffer
	logger := mustNewLogger(t, logging.Config{Console: logging.ConsolePlain,
		Service:          "openbrain-dev",
		ConsoleVerbosity: slog.LevelInfo,
		Writer:           &console,
		TextDir:          dir,
	})
	t.Cleanup(func() { _ = logger.Close() })
	logger.Debug("debug detail")
	logger.Info("run started")

	if strings.Contains(console.String(), "debug detail") {
		t.Fatalf("console emitted DEBUG despite INFO threshold: %q", console.String())
	}
	matches, err := filepath.Glob(filepath.Join(dir, "openbrain-dev-*.log"))
	if err != nil {
		t.Fatalf("glob human logs: %v", err)
	}
	if len(matches) != 10 {
		t.Fatalf("retained %d human logs, want 10: %v", len(matches), matches)
	}
	if _, err := os.Stat(filepath.Join(dir, "openbrain-dev-2026-05-01.log")); !os.IsNotExist(err) {
		t.Fatalf("oldest daily log was not pruned; stat err=%v", err)
	}

	today := textLogPath(dir, "openbrain-dev")
	body, err := os.ReadFile(today)
	if err != nil {
		t.Fatalf("read current human log %s: %v", today, err)
	}
	got := string(body)
	if !strings.Contains(got, "\tDEBUG\t") || !strings.Contains(got, "debug detail") {
		t.Fatalf("file sink did not capture DEBUG detail: %q", got)
	}
	if !strings.Contains(got, "\tINFO\t") || !strings.Contains(got, "run started") {
		t.Fatalf("file sink did not capture INFO detail: %q", got)
	}
	if !regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}\t(?:DEBUG|INFO)\topenbrain-dev\s+\t`).MatchString(got) {
		t.Fatalf("file sink does not use full timestamp, level, padded source tabs: %q", got)
	}
}

// DHF-TEST: openbrain/change_request-441
func TestLoggerFansOutToConsoleHumanFileAndJSONFile(t *testing.T) {
	dir := t.TempDir()

	var console bytes.Buffer
	logger := mustNewLogger(t, logging.Config{Console: logging.ConsolePlain,
		Service:          "openbrain-client",
		ConsoleVerbosity: slog.LevelInfo,
		Writer:           &console,
		TextDir:          dir,
		JSONLDir:         dir,
	})
	t.Cleanup(func() { _ = logger.Close() })
	logger.Debug("debug detail", "token", "secret-value")
	logger.Info("human visible", "unit", "cr-441")

	if strings.Contains(console.String(), "debug detail") {
		t.Fatalf("console emitted DEBUG despite INFO threshold: %q", console.String())
	}
	if !strings.Contains(console.String(), "human visible") || strings.HasPrefix(strings.TrimSpace(console.String()), "{") {
		t.Fatalf("console output is not human text: %q", console.String())
	}

	humanData, err := os.ReadFile(textLogPath(dir, "openbrain-client"))
	if err != nil {
		t.Fatalf("read human log: %v", err)
	}
	if got := string(humanData); !strings.Contains(got, "debug detail") || !strings.Contains(got, "\tDEBUG\t") {
		t.Fatalf("human file did not capture DEBUG text detail: %q", got)
	}

	jsonData, err := os.ReadFile(jsonLogPath(dir, "openbrain-client"))
	if err != nil {
		t.Fatalf("read JSON log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(jsonData)), "\n")
	if len(lines) != 2 {
		t.Fatalf("JSONL line count = %d, want 2; body=%q", len(lines), string(jsonData))
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("JSONL first line is not JSON: %v; line=%q", err, lines[0])
	}
	if first["msg"] != "debug detail" || first["level"] != "DEBUG" || first["token"] != "[REDACTED]" {
		t.Fatalf("JSON file did not capture DEBUG structured redacted record: %#v", first)
	}
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// DHF-TEST: keel/requirement-5
func TestConsoleAndJSON_RedactAttributeValuesIdentically(t *testing.T) {
	jsonLogger, jsonCapture := newJSONCaptureLogger(t, "cli")
	consoleLogger, consoleCapture := newConsoleCaptureLogger(t, "cli")

	secret := "Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig"
	jsonLogger.Info("auth failed", "authorization", secret)
	consoleLogger.Info("auth failed", "authorization", secret)

	jsonRaw := jsonCapture.LastRaw()
	consoleRaw := consoleCapture.LastRaw()
	for name, raw := range map[string]string{"json": jsonRaw, "console": consoleRaw} {
		if raw == "" {
			t.Fatalf("%s output was empty", name)
		}
		if strings.Contains(raw, "eyJhbGciOiJIUzI1NiJ9") {
			t.Fatalf("%s output leaked bearer token: %q", name, raw)
		}
		if !strings.Contains(raw, "Bearer [REDACTED]") {
			t.Fatalf("%s output missing bearer redaction marker: %q", name, raw)
		}
	}
}

// DHF-TEST: keel/requirement-2
func TestSyntheticAdapterUsesGenericProgressDetailConsoleHook(t *testing.T) {
	logger, capture := newConsoleCaptureLogger(t, "builder-adapter")

	logger.Info("builder progress", "detail", "indexed files", "event_type", "scan")

	raw := capture.LastRaw()
	if !strings.Contains(raw, "builder detail: indexed files") {
		t.Fatalf("synthetic adapter progress was not rendered as detail: %q", raw)
	}
	if strings.Contains(raw, "event_type=") || strings.Contains(raw, "detail=") {
		t.Fatalf("generic progress attrs leaked into console output: %q", raw)
	}
}

// DHF-TEST: openbrain/requirement-104
func TestAllHandlers_RedactSensitiveTokenAttributes(t *testing.T) {
	tests := []struct {
		name      string
		newLogger func() (*slog.Logger, *recordCapture)
	}{
		{"json", func() (*slog.Logger, *recordCapture) { return newJSONCaptureLogger(t, "cli") }},
		{"console", func() (*slog.Logger, *recordCapture) { return newConsoleCaptureLogger(t, "cli") }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger, capture := tc.newLogger()
			logger.Info("token check",
				"mcp_auth_token", "mcp-secret-token",
				"gitea_pat", "gitea-secret-token",
			)

			raw := capture.LastRaw()
			for _, secret := range []string{"mcp-secret-token", "gitea-secret-token"} {
				if strings.Contains(raw, secret) {
					t.Fatalf("%s handler leaked %q in %q", tc.name, secret, raw)
				}
			}
			if !strings.Contains(raw, "[REDACTED]") {
				t.Fatalf("%s handler missing redaction marker in %q", tc.name, raw)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AllJSON tests — BLOCKER 4
// AllJSON() []map[string]any does not exist yet on RecordCapture.
// These tests are RED until the implementation lands in pkg/logging/logging.go.
// ---------------------------------------------------------------------------

// TestRecordCapture_AllJSON_Empty asserts that AllJSON returns an empty (not nil)
// slice when the capture buffer is empty.
func TestRecordCapture_AllJSON_Empty(t *testing.T) {
	_, capture := newJSONCaptureLogger(t, "mcp")
	got := capture.AllJSON()
	// An empty buffer has no lines to return. Allow nil or empty slice.
	if len(got) != 0 {
		t.Errorf("AllJSON on empty buffer returned %d items, want 0", len(got))
	}
}

// TestRecordCapture_AllJSON_SingleLine asserts that after one log call,
// AllJSON returns a slice of length 1 whose only element is the parsed JSON.
func TestRecordCapture_AllJSON_SingleLine(t *testing.T) {
	logger, capture := newJSONCaptureLogger(t, "web-ui")
	logger.Info("hello")

	got := capture.AllJSON()
	if len(got) != 1 {
		t.Fatalf("AllJSON returned %d items, want 1", len(got))
	}
	if got[0]["msg"] != "hello" {
		t.Errorf("AllJSON[0][msg] = %v, want %q", got[0]["msg"], "hello")
	}
}

// TestRecordCapture_AllJSON_MultipleLines asserts that AllJSON returns all
// captured lines in emission order — the key behavior that differentiates it
// from LastJSON (which returns only the final line).
//
// This test fails if AllJSON is implemented as an alias for LastJSON, or if
// it reverses order, or if it drops any intermediate line.
func TestRecordCapture_AllJSON_MultipleLines(t *testing.T) {
	logger, capture := newJSONCaptureLogger(t, "web-ui")
	logger.Info("line-one")
	logger.Warn("line-two")
	logger.Error("line-three")

	got := capture.AllJSON()
	if len(got) != 3 {
		t.Fatalf("AllJSON returned %d items, want 3", len(got))
	}

	// Assert order and content of each line.
	wantMsgs := []string{"line-one", "line-two", "line-three"}
	wantLevels := []string{"INFO", "WARN", "ERROR"}
	for i, want := range wantMsgs {
		if got[i]["msg"] != want {
			t.Errorf("AllJSON[%d][msg] = %v, want %q", i, got[i]["msg"], want)
		}
		if got[i]["level"] != wantLevels[i] {
			t.Errorf("AllJSON[%d][level] = %v, want %q", i, got[i]["level"], wantLevels[i])
		}
	}
}

// TestRecordCapture_AllJSON_AfterReset asserts that after Reset(), AllJSON
// returns only lines logged after the reset.
func TestRecordCapture_AllJSON_AfterReset(t *testing.T) {
	logger, capture := newJSONCaptureLogger(t, "web-ui")
	logger.Info("before-reset")
	capture.Reset()
	logger.Warn("after-reset")

	got := capture.AllJSON()
	if len(got) != 1 {
		t.Fatalf("AllJSON after Reset returned %d items, want 1", len(got))
	}
	if got[0]["msg"] != "after-reset" {
		t.Errorf("AllJSON[0][msg] = %v, want %q", got[0]["msg"], "after-reset")
	}
}

// TestRecordCapture_AllJSON_LastJSONAgreement asserts that when there is at
// least one line, AllJSON()[last] and LastJSON() decode to the same content.
// This verifies the two methods are reading the same buffer and not diverging.
func TestRecordCapture_AllJSON_LastJSONAgreement(t *testing.T) {
	logger, capture := newJSONCaptureLogger(t, "web-ui")
	logger.Info("first")
	logger.Error("second")

	all := capture.AllJSON()
	last := capture.LastJSON()
	if len(all) == 0 {
		t.Fatal("AllJSON returned empty slice")
	}
	if last == nil {
		t.Fatal("LastJSON returned nil")
	}
	// The last element of AllJSON must equal LastJSON.
	finalAllJSON := all[len(all)-1]
	if finalAllJSON["msg"] != last["msg"] {
		t.Errorf("AllJSON[last][msg] = %v, LastJSON[msg] = %v — disagreement",
			finalAllJSON["msg"], last["msg"])
	}
}

func parseJSONLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("unmarshal log line %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func parseSparseAILogRecords(t *testing.T, raw string) []map[string]any {
	t.Helper()
	return parseJSONLines(t, raw)
}
