package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type recordCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (rc *recordCapture) Write(p []byte) (int, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.buf.Write(p)
}

func (rc *recordCapture) LastJSON() map[string]any {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(rc.buf.String()), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		return nil
	}
	return m
}

func (rc *recordCapture) AllJSON() []map[string]any {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	raw := strings.TrimSpace(rc.buf.String())
	out := make([]map[string]any, 0)
	if raw == "" {
		return out
	}
	for _, line := range strings.Split(raw, "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

func (rc *recordCapture) LastRaw() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(rc.buf.String()), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func testLogger(service string) (*slog.Logger, *recordCapture) {
	cap := &recordCapture{}
	logger, err := logging.New(logging.Config{
		Service: service,
		Level:   slog.LevelDebug,
		Console: logging.ConsoleJSON,
		Writer:  cap,
	})
	if err != nil {
		panic(err)
	}
	return logger.Slog(), cap
}

// requireTool skips the test when a required external binary is absent, keeping
// the suite hermetic-but-honest on minimal environments.
func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH", name)
	}
}

func TestValidateVersion(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"v0.1.0", true},
		{"v1.2.3", true},
		{"v0.1.0-rc.1", true},
		{"v10.20.30", true},
		{"0.1.0", false},    // missing leading v
		{"v1.2", false},     // not three components
		{"v1.2.3.4", false}, // too many components
		{"vX.Y.Z", false},   // placeholders
		{"v01.2.3", false},  // leading zero
		{"", false},
		{"latest", false},
	}
	for _, c := range cases {
		err := validateVersion(c.in)
		if c.ok && err != nil {
			t.Errorf("validateVersion(%q) = %v, want nil", c.in, err)
		}
		if !c.ok && err == nil {
			t.Errorf("validateVersion(%q) = nil, want error", c.in)
		}
	}
}

// TestEnsureCleanTree exercises the release preflight's clean-tree guard against
// a real temp git repo: empty repo is clean, an untracked file makes it dirty.
func TestEnsureCleanTree(t *testing.T) {
	requireTool(t, "git")
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")

	if err := ensureCleanTree(context.Background(), discardLogger(), dir); err != nil {
		t.Fatalf("fresh repo should be clean, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureCleanTree(context.Background(), discardLogger(), dir); err == nil {
		t.Fatal("dirty repo should be rejected, got nil")
	}
}

// TestRunCIGofmtGate proves the gate fails fast on unformatted code and passes
// on formatted code — the canonical `keel-dev ci` sequence over a tiny module.
func TestRunCIGofmtGate(t *testing.T) {
	requireTool(t, "gofmt")
	requireTool(t, "go")

	dir := t.TempDir()
	writeModule(t, dir)

	// Formatted, compiling, fully covered source: whole gate should pass,
	// including the coverage floor.
	writeFile(t, dir, "keel_test_pkg.go", "package p\n\nfunc One() int {\n\treturn 1\n}\n")
	writeFile(t, dir, "keel_test_pkg_test.go", "package p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"one\")\n\t}\n}\n")
	writeFile(t, dir, "cspell.json", "{\"version\":\"0.2\",\"language\":\"en-US\",\"words\":[\"keel\"]}\n")
	if err := runCI(context.Background(), discardLogger(), dir); err != nil {
		t.Fatalf("clean module should pass ci, got %v", err)
	}

	// Unformatted source (leading spaces): gofmt gate must fail before build.
	writeFile(t, dir, "keel_test_pkg.go", "package p\n\nvar    Y = 2\n")
	if err := runCI(context.Background(), discardLogger(), dir); err == nil {
		t.Fatal("unformatted module should fail ci, got nil")
	}
}

// DHF-TEST: keel/requirement-18
func TestRunCIFailureCarriesStructuredOperationalError(t *testing.T) {
	requireTool(t, "gofmt")

	dir := t.TempDir()
	writeModule(t, dir)
	writeFile(t, dir, "bad.go", "package p\n\nvar    Y = 2\n")

	logDir := t.TempDir()
	logger, err := logging.New(logging.Config{
		Service:  "keel-dev",
		Console:  logging.ConsoleNone,
		JSONLDir: logDir,
		PerRun:   true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	err = runCIWithRunLog(context.Background(), logger.Slog(), logger, dir)
	if err == nil {
		t.Fatal("unformatted module should fail ci, got nil")
	}
	var opErr *logging.OperationalError
	if !errors.As(err, &opErr) {
		t.Fatalf("runCI error type = %T, want OperationalError: %v", err, err)
	}
	if opErr.Task != "ci:gofmt" {
		t.Fatalf("OperationalError task = %q, want ci:gofmt", opErr.Task)
	}
	if opErr.LogFile == "" || opErr.LogFile != logger.RunLogPath() {
		t.Fatalf("OperationalError log file = %q, want %q", opErr.LogFile, logger.RunLogPath())
	}
	if opErr.StartLine <= 0 {
		t.Fatalf("OperationalError start line = %d, want positive line number", opErr.StartLine)
	}
	if opErr.ExitCode != 1 {
		t.Fatalf("OperationalError exit code = %d, want 1", opErr.ExitCode)
	}
	if !strings.Contains(opErr.Hint, opErr.LogFile) || !strings.Contains(opErr.Hint, "line") {
		t.Fatalf("OperationalError hint = %q, want log-file line coordinate", opErr.Hint)
	}
}

// TestRunStepLogsThroughKeelLog proves every subprocess flows through keel/exec
// and thus emits the START/END lifecycle through a keel/log handler.
func TestRunStepLogsThroughKeelLog(t *testing.T) {
	requireTool(t, "go")

	logger, cap := testLogger("keel-dev")
	err := runStep(context.Background(), logger, ".", step{
		name: "probe", program: "go", args: []string{"env", "GOOS"},
	})
	if err != nil {
		t.Fatalf("probe step failed: %v", err)
	}

	var sawStart, sawEnd bool
	for _, rec := range cap.AllJSON() {
		switch rec["event_type"] {
		case "process_start":
			sawStart = true
		case "process_end":
			sawEnd = true
		}
	}
	if !sawStart || !sawEnd {
		t.Fatalf("expected process_start and process_end through keel/log; start=%v end=%v", sawStart, sawEnd)
	}
}

// DHF-TEST: keel/requirement-17, keel/requirement-24
func TestQuietStderrLoggerPromotesRealStderrAndFiltersKnownBenignLines(t *testing.T) {
	logger, cap := testLogger("keel-dev")
	wrapped := quietStderrLogger{Logger: logger}

	wrapped.Error("process output",
		"event_type", "process_output",
		"stream", "stderr",
		"step", "gitleaks",
		"data", "real leak detected",
	)
	wrapped.Error("process output",
		"event_type", "process_output",
		"stream", "stderr",
		"step", "gitleaks",
		"data", "\x1b[90m3:33AM\x1b[0m \x1b[32mINF\x1b[0m scan completed in 42ms",
	)

	records := cap.AllJSON()
	if len(records) != 2 {
		t.Fatalf("records = %#v, want real stderr plus reclassified benign stderr", records)
	}
	if records[0]["level"] != "ERROR" || records[0]["data"] != "real leak detected" {
		t.Fatalf("real stderr record = %#v, want ERROR", records[0])
	}
	if records[1]["level"] != "DEBUG" || records[1]["data"] != "\x1b[90m3:33AM\x1b[0m \x1b[32mINF\x1b[0m scan completed in 42ms" {
		t.Fatalf("known-benign stderr record = %#v, want DEBUG", records[1])
	}
}

// DHF-TEST: keel/requirement-17, keel/requirement-24
func TestLineLogWriterRoutesStderrAtError(t *testing.T) {
	logger, cap := testLogger("keel-dev")
	lines := newLineLogWriter(logger, "probe", "stderr")
	if _, err := lines.Write([]byte("failure\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	rec := cap.LastJSON()
	if rec["level"] != "ERROR" || rec["stream"] != "stderr" || rec["msg"] != "failure" {
		t.Fatalf("stderr line record = %#v, want ERROR process output", rec)
	}
}

// DHF-TEST: keel/requirement-25
func TestRunCISectionNamesPhaseOnly(t *testing.T) {
	logger, cap := testLogger("keel-dev")
	err := runCI(context.Background(), logger, t.TempDir())
	if err == nil {
		t.Fatal("empty directory should fail after emitting the ci section")
	}

	records := cap.AllJSON()
	if len(records) == 0 {
		t.Fatal("no log records captured")
	}
	first := records[0]
	if first["banner"] != "section" {
		t.Fatalf("first record banner = %#v, want section; record=%#v", first["banner"], first)
	}
	if first["msg"] != "ci" {
		t.Fatalf("ci section = %#v, want phase-only section", first["msg"])
	}
}

// TestConsoleSuppressesServiceAttr proves the human console omits the
// redundant service=keel-dev attr while JSON mode keeps it (keel/issue-3).
func TestConsoleSuppressesServiceAttr(t *testing.T) {
	cfg := loggerConfig(nil)

	rc := &recordCapture{}
	cfg.Writer = rc
	cfg.Console = logging.ConsolePlain
	logger, err := logging.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info("hello", "k", "v")
	if out := rc.LastRaw(); strings.Contains(out, "service=") {
		t.Errorf("console line should omit service attr: %q", out)
	} else if !strings.Contains(out, "k=v") {
		t.Errorf("non-suppressed attrs must survive: %q", out)
	}

	rcJSON := &recordCapture{}
	cfg.Writer = rcJSON
	cfg.Console = logging.ConsoleJSON
	logger, err = logging.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info("hello")
	if rec := rcJSON.LastJSON(); rec["service"] != "keel-dev" {
		t.Errorf("JSON mode must keep the service field, got %v", rec)
	}
}

// DHF-TEST: keel/requirement-25
func TestConsoleModeSelectsRendering(t *testing.T) {
	cases := []struct {
		mode string
		want logging.Console
	}{
		{"human", logging.ConsolePlain},
		{"ai", logging.ConsoleSparseAI},
		{"json", logging.ConsoleJSON},
	}
	for _, c := range cases {
		got, err := consoleForMode(c.mode)
		if err != nil {
			t.Fatalf("consoleForMode(%q) error = %v", c.mode, err)
		}
		if got != c.want {
			t.Fatalf("consoleForMode(%q) = %q, want %q", c.mode, got, c.want)
		}
	}

	if _, err := consoleForMode("bogus"); err == nil || !strings.Contains(err.Error(), "unknown --mode") {
		t.Fatalf("unknown mode should fail loud, got %v", err)
	}
}

// DHF-TEST: keel/requirement-25
func TestLoggerConfigUsesConsoleMode(t *testing.T) {
	rc := &recordCapture{}
	logger := newLogger("ai", slog.LevelInfo, rc)
	logger.Info("gate started", "gate", "probe")

	rec := rc.LastJSON()
	if rec["event"] != "log" || rec["message"] != "gate started" {
		t.Fatalf("ai mode should use sparse-AI console records, got %#v", rec)
	}

	jsonCap := &recordCapture{}
	jsonLogger := newLogger("json", slog.LevelInfo, jsonCap)
	jsonLogger.Info("gate started", "gate", "probe")
	if rec := jsonCap.LastJSON(); rec["service"] != "keel-dev" || rec["msg"] != "gate started" {
		t.Fatalf("json mode should use verbose JSON records, got %#v", rec)
	}
}

// --- helpers ---

func writeModule(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "go.mod", "module keeldevtest\n\ngo 1.25\n")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRun(t *testing.T, dir, program string, args ...string) {
	t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", program, args, err, out)
	}
}
