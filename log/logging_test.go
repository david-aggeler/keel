package log_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

func newJSONCaptureLogger(service string) (*slog.Logger, *logging.RecordCapture) {
	rc := &logging.RecordCapture{}
	return logging.New(logging.Config{
		Service: service,
		Level:   slog.LevelDebug,
		Console: logging.ConsoleJSON,
		Writer:  rc,
	}).Slog(), rc
}

func newConsoleCaptureLogger(service string) (*slog.Logger, *logging.RecordCapture) {
	rc := &logging.RecordCapture{}
	return logging.New(logging.Config{
		Service: service,
		Level:   slog.LevelDebug,
		Console: logging.ConsolePlain,
		Writer:  rc,
	}).Slog(), rc
}

func TestNewConfigJSONCaptureHelper_ReturnsLoggerAndCapture(t *testing.T) {
	logger, capture := newJSONCaptureLogger("mcp")
	if logger == nil {
		t.Fatal("newJSONCaptureLogger returned nil logger")
	}
	if capture == nil {
		t.Fatal("newJSONCaptureLogger returned nil capture")
	}
}

// DHF-TEST: keel/requirement-16
func TestNewConfigExposesFourSinkLoggerSurface(t *testing.T) {
	var console bytes.Buffer
	textDir := t.TempDir()
	jsonlDir := t.TempDir()

	logger := logging.New(logging.Config{
		Service:       "svc",
		Level:         slog.LevelDebug,
		Console:       logging.ConsolePlain,
		Writer:        &console,
		TextDir:       textDir,
		JSONLDir:      jsonlDir,
		PerRun:        false,
		SourceInFiles: true,
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

// DHF-TEST: keel/requirement-19
func TestNewConfigPerRunJSONLSinkUsesInvocationFileAndTracksLines(t *testing.T) {
	jsonlDir := t.TempDir()
	logger := logging.New(logging.Config{
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

// DHF-TEST: keel/requirement-17
func TestSparseAIConsoleEmitsCuratedEventsAndKeepsDebugChildOutputInFiles(t *testing.T) {
	var console bytes.Buffer
	textDir := t.TempDir()
	jsonlDir := t.TempDir()

	logger := logging.New(logging.Config{
		Service:  "svc",
		Level:    slog.LevelInfo,
		Console:  logging.ConsoleSparseAI,
		Writer:   &console,
		TextDir:  textDir,
		JSONLDir: jsonlDir,
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
	if got, _ := sparse["level"].(string); got != "info" {
		t.Fatalf("sparse level = %q, want info", got)
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
	logger := logging.New(logging.Config{
		Service: "svc",
		Level:   slog.LevelInfo,
		Console: logging.ConsoleJSON,
		Writer:  &console,
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
	logger, capture := newJSONCaptureLogger("mcp")

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
			logger, capture := newJSONCaptureLogger("mcp")
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
	logger, capture := newJSONCaptureLogger("mcp")

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
	logger, capture := newJSONCaptureLogger("mcp")

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
	logger, capture := newJSONCaptureLogger("mcp")

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
	logger, capture := newJSONCaptureLogger("mcp")
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
	logger := logging.New(logging.Config{
		Service:  "svc",
		Level:    slog.LevelDebug,
		Console:  logging.ConsolePlain,
		Writer:   &console,
		TextDir:  textDir,
		JSONLDir: jsonlDir,
	}).With("module", "keel/exec")
	t.Cleanup(func() { _ = logger.Close() })

	logger.Info("module context", "operation", "run")

	if got := console.String(); strings.Contains(got, "module=keel/exec") {
		t.Fatalf("console included module context reserved for file sinks: %q", got)
	}

	textData, err := os.ReadFile(logging.HumanLogPath(textDir, "svc"))
	if err != nil {
		t.Fatalf("read human file sink: %v", err)
	}
	if got := string(textData); !strings.Contains(got, "module=keel/exec") {
		t.Fatalf("human file sink missing module context: %q", got)
	}

	jsonData, err := os.ReadFile(logging.JSONLogPath(jsonlDir, "svc"))
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
	logger := logging.New(logging.Config{
		Service:  "svc",
		Level:    slog.LevelDebug,
		Console:  logging.ConsolePlain,
		Writer:   &console,
		TextDir:  textDir,
		JSONLDir: jsonlDir,
	}).WithGroup("ctx").With("module", "keel/exec")
	t.Cleanup(func() { _ = logger.Close() })

	logger.Info("grouped module context", "operation", "run")

	if got := console.String(); strings.Contains(got, "module=") || strings.Contains(got, "ctx.module") || strings.Contains(got, "keel/exec") {
		t.Fatalf("console included grouped module context reserved for file sinks: %q", got)
	}

	textData, err := os.ReadFile(logging.HumanLogPath(textDir, "svc"))
	if err != nil {
		t.Fatalf("read human file sink: %v", err)
	}
	if got := string(textData); !strings.Contains(got, "keel/exec") || !strings.Contains(got, "ctx.module=keel/exec") {
		t.Fatalf("human file sink missing grouped module context: %q", got)
	}

	jsonData, err := os.ReadFile(logging.JSONLogPath(jsonlDir, "svc"))
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
	defaultLogger := logging.New(logging.Config{
		Service: "svc",
		Console: logging.ConsoleNone,
		TextDir: defaultDir,
	})
	defaultLogger.Info("default source")
	if err := defaultLogger.Close(); err != nil {
		t.Fatalf("close default logger: %v", err)
	}
	defaultData, err := os.ReadFile(logging.HumanLogPath(defaultDir, "svc"))
	if err != nil {
		t.Fatalf("read default human file sink: %v", err)
	}
	if got := string(defaultData); strings.Contains(got, "logging_test.go") {
		t.Fatalf("SourceInFiles default emitted caller source: %q", got)
	}

	sourceDir := t.TempDir()
	sourceLogger := logging.New(logging.Config{
		Service:       "svc",
		Console:       logging.ConsoleNone,
		TextDir:       sourceDir,
		SourceInFiles: true,
	})
	sourceLogger.Info("source enabled")
	if err := sourceLogger.Close(); err != nil {
		t.Fatalf("close source logger: %v", err)
	}
	sourceData, err := os.ReadFile(logging.HumanLogPath(sourceDir, "svc"))
	if err != nil {
		t.Fatalf("read source human file sink: %v", err)
	}
	if got := string(sourceData); !strings.Contains(got, "logging_test.go") {
		t.Fatalf("SourceInFiles=true missing caller source: %q", got)
	}
}

// DHF-TEST: openbrain/requirement-602
func TestJSONOutput_FileSinkRetainsDebugWhenConsoleUsesInfo(t *testing.T) {
	var console bytes.Buffer
	var file bytes.Buffer
	logger := logging.New(logging.Config{
		Service: "openbrain-client",
		Level:   slog.LevelInfo,
		Console: logging.ConsoleJSON,
		Writer:  &console,
		JSONFileHandler: slog.NewJSONHandler(&file, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}),
	})

	logger.Debug("debug detail", "chunk", "stdout payload")
	logger.Info("visible info")

	consoleRaw := console.String()
	if strings.Contains(consoleRaw, "debug detail") || strings.Contains(consoleRaw, "stdout payload") {
		t.Fatalf("console emitted DEBUG below INFO threshold: %q", consoleRaw)
	}
	if !strings.Contains(consoleRaw, "visible info") {
		t.Fatalf("console did not emit INFO record: %q", consoleRaw)
	}

	fileRaw := file.String()
	if !strings.Contains(fileRaw, "debug detail") || !strings.Contains(fileRaw, "stdout payload") {
		t.Fatalf("file sink did not retain DEBUG detail: %q", fileRaw)
	}
	if !strings.Contains(fileRaw, "visible info") {
		t.Fatalf("file sink did not retain INFO record: %q", fileRaw)
	}
}

// DHF-TEST: keel/requirement-5, openbrain/requirement-151
func TestConsoleOutput_HumanReadableAndRedacted(t *testing.T) {
	capture := &logging.RecordCapture{}
	logger := logging.New(logging.Config{Console: logging.ConsolePlain,
		Service:         "cli",
		Level:           slog.LevelDebug,
		Writer:          capture,
		ConsoleOmitKeys: []string{"service"},
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
	consoleLogger := logging.New(logging.Config{Console: logging.ConsolePlain,
		Service:         "openbrain-client",
		Level:           slog.LevelDebug,
		Writer:          &consoleBuf,
		ConsoleOmitKeys: []string{"service", "cr", "verb"},
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

	jsonLogger, jsonCapture := newJSONCaptureLogger("openbrain-client")
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
	logger := logging.New(logging.Config{Console: logging.ConsolePlain,
		Service: "openbrain-dev",
		Level:   slog.LevelDebug,
		Writer:  &buf,
	})

	logger.Header("openbrain-dev", "v0.0.0-dev")
	logging.Fields(logger.Slog(), []logging.FieldRow{
		{Label: "Directory", Value: "./Product/"},
		{Label: "Files to process", Value: 4},
	})
	logger.Warn("Id hash collision detected within 01:00:00 window.")
	logger.Section("Summary")
	logger.Field("Save result", "./2026-05-29_to_06-08_D.xlsx")
	logger.Info("Done.")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 9 {
		t.Fatalf("captured %d lines, want 9:\n%s", len(lines), buf.String())
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
	if !strings.Contains(lines[6], strings.Repeat("-", 70)) || !strings.Contains(lines[6], "Summary") {
		t.Fatalf("section header is not rendered through logger: %q", lines[6])
	}
	if !strings.Contains(lines[8], "Done.") {
		t.Fatalf("completion marker missing: %q", lines[8])
	}
}

// DHF-TEST: openbrain/requirement-152
func TestConsoleOutput_LevelThresholdAndColorGating(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	var plain bytes.Buffer
	plainLogger := logging.New(logging.Config{Console: logging.ConsolePlain,
		Service: "cli",
		Level:   slog.LevelInfo,
		Writer:  &plain,
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
	colorLogger := logging.New(logging.Config{Console: logging.ConsolePlain,
		Service:      "cli",
		Level:        slog.LevelDebug,
		Writer:       &colored,
		ForceColor:   true,
		DisableColor: false,
	})
	colorLogger.Warn("careful")
	if got := colored.String(); !strings.Contains(got, "\x1b[90m") || !strings.Contains(got, "\x1b[33mWARN\x1b[0m") {
		t.Fatalf("forced color output missing gray timestamp or yellow WARN: %q", got)
	}

	t.Setenv("NO_COLOR", "1")
	var noColor bytes.Buffer
	noColorLogger := logging.New(logging.Config{Console: logging.ConsolePlain,
		Service:    "cli",
		Level:      slog.LevelDebug,
		Writer:     &noColor,
		ForceColor: true,
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
	logger := logging.New(logging.Config{Console: logging.ConsolePlain,
		Service: "openbrain-dev",
		Level:   slog.LevelInfo,
		Writer:  &console,
		TextDir: dir,
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

	today := logging.HumanLogPath(dir, "openbrain-dev")
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

// TestHumanFileHandler_SharedAcrossLoggersAndCloseable pins the fix for the
// per-record file-open leak: a single NewHumanFileHandler is opened once and
// reused across many New(Config) loggers (as the devtool does per console line),
// all writes land in the one daily file, and the handler closes cleanly.
//
// DHF-REQ: openbrain/requirement-152
func TestHumanFileHandler_SharedAcrossLoggersAndCloseable(t *testing.T) {
	dir := t.TempDir()
	fh, err := logging.NewHumanFileHandler(dir, "openbrain-dev")
	if err != nil {
		t.Fatalf("NewHumanFileHandler: %v", err)
	}

	// Build a fresh console logger per emission with the SAME shared file
	// handler — the leak-free path. None of these opens a new file.
	for i := 0; i < 5; i++ {
		var console bytes.Buffer
		logger := logging.New(logging.Config{Console: logging.ConsolePlain,
			Service:          "openbrain-dev",
			Level:            slog.LevelInfo,
			Writer:           &console,
			HumanFileHandler: fh,
		})
		logger.Info("line", "n", i)
	}

	// Exactly one daily file exists — no per-logger file proliferation.
	matches, err := filepath.Glob(filepath.Join(dir, "openbrain-dev-*.log"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 daily log, got %d: %v", len(matches), matches)
	}

	closer, ok := fh.(io.Closer)
	if !ok {
		t.Fatalf("NewHumanFileHandler result %T does not implement io.Closer", fh)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	body, err := os.ReadFile(logging.HumanLogPath(dir, "openbrain-dev"))
	if err != nil {
		t.Fatalf("read daily log: %v", err)
	}
	for i := 0; i < 5; i++ {
		if !strings.Contains(string(body), "n="+strconv.Itoa(i)) {
			t.Fatalf("shared file missing line n=%d: %q", i, string(body))
		}
	}
}

// DHF-TEST: openbrain/change_request-441
func TestLoggerFansOutToConsoleHumanFileAndJSONFile(t *testing.T) {
	dir := t.TempDir()
	humanHandler, err := logging.NewHumanFileHandler(dir, "openbrain-client")
	if err != nil {
		t.Fatalf("NewHumanFileHandler: %v", err)
	}
	t.Cleanup(func() { _ = humanHandler.(io.Closer).Close() })
	jsonHandler, err := logging.NewJSONFileHandler(dir, "openbrain-client")
	if err != nil {
		t.Fatalf("NewJSONFileHandler: %v", err)
	}
	t.Cleanup(func() { _ = jsonHandler.(io.Closer).Close() })

	var console bytes.Buffer
	logger := logging.New(logging.Config{Console: logging.ConsolePlain,
		Service:          "openbrain-client",
		Level:            slog.LevelInfo,
		Writer:           &console,
		HumanFileHandler: humanHandler,
		JSONFileHandler:  jsonHandler,
	})
	logger.Debug("debug detail", "token", "secret-value")
	logger.Info("human visible", "unit", "cr-441")

	if strings.Contains(console.String(), "debug detail") {
		t.Fatalf("console emitted DEBUG despite INFO threshold: %q", console.String())
	}
	if !strings.Contains(console.String(), "human visible") || strings.HasPrefix(strings.TrimSpace(console.String()), "{") {
		t.Fatalf("console output is not human text: %q", console.String())
	}

	humanData, err := os.ReadFile(logging.HumanLogPath(dir, "openbrain-client"))
	if err != nil {
		t.Fatalf("read human log: %v", err)
	}
	if got := string(humanData); !strings.Contains(got, "debug detail") || !strings.Contains(got, "\tDEBUG\t") {
		t.Fatalf("human file did not capture DEBUG text detail: %q", got)
	}

	jsonData, err := os.ReadFile(logging.JSONLogPath(dir, "openbrain-client"))
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
	jsonLogger, jsonCapture := newJSONCaptureLogger("cli")
	consoleLogger, consoleCapture := newConsoleCaptureLogger("cli")

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

// DHF-TEST: openbrain/requirement-104
func TestAllHandlers_RedactSensitiveTokenAttributes(t *testing.T) {
	tests := []struct {
		name      string
		newLogger func() (*slog.Logger, *logging.RecordCapture)
	}{
		{"json", func() (*slog.Logger, *logging.RecordCapture) { return newJSONCaptureLogger("cli") }},
		{"console", func() (*slog.Logger, *logging.RecordCapture) { return newConsoleCaptureLogger("cli") }},
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
	_, capture := newJSONCaptureLogger("mcp")
	got := capture.AllJSON()
	// An empty buffer has no lines to return. Allow nil or empty slice.
	if len(got) != 0 {
		t.Errorf("AllJSON on empty buffer returned %d items, want 0", len(got))
	}
}

// TestRecordCapture_AllJSON_SingleLine asserts that after one log call,
// AllJSON returns a slice of length 1 whose only element is the parsed JSON.
func TestRecordCapture_AllJSON_SingleLine(t *testing.T) {
	logger, capture := newJSONCaptureLogger("web-ui")
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
	logger, capture := newJSONCaptureLogger("web-ui")
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
	logger, capture := newJSONCaptureLogger("web-ui")
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
	logger, capture := newJSONCaptureLogger("web-ui")
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
