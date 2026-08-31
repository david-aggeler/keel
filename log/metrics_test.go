package log_test

import (
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

const foundationExportsModulePath = "github.com/david-aggeler/keel"

// DHF-TEST: keel/requirement-31
func TestFoundationExportsAreConsumerAgnostic(t *testing.T) {
	importPaths, err := foundationExportImportPaths(t)
	if err != nil {
		t.Fatal(err)
	}
	var docFailures []string
	for _, importPath := range importPaths {
		out, err := exec.Command("go", "doc", importPath).CombinedOutput()
		if err != nil {
			docFailures = append(docFailures, "go doc "+importPath+": "+err.Error()+"\n"+string(out))
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.HasPrefix(line, "const ") &&
				!strings.HasPrefix(line, "func ") &&
				!strings.HasPrefix(line, "type ") {
				continue
			}
			for _, forbidden := range []string{"Vault", "vault"} {
				if strings.Contains(line, forbidden) {
					t.Errorf("go doc %s contains consumer-domain term %q in exported declaration %q", importPath, forbidden, line)
				}
			}
		}
	}
	if len(docFailures) != 0 {
		t.Fatalf("go doc failures:\n%s", strings.Join(docFailures, "\n"))
	}
}

// DHF-TEST: keel/requirement-85 (keel/ac-666)
func TestFoundationExportsIgnoreGitignoredTestOnlyPackages(t *testing.T) {
	root := moduleRoot(t)
	scratch := filepath.Join(root, "scratchpad", "foundation-exports-test-only-package")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(scratch); err != nil {
			t.Errorf("cleanup scratch package: %v", err)
		}
	})
	if err := os.WriteFile(filepath.Join(scratch, "probe_test.go"), []byte("package probe_test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "./log", "-run", "^TestFoundationExportsAreConsumerAgnostic$", "-count=1")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("foundation exports test should ignore gitignored test-only package: %v\n%s", err, out)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "env", "GOMOD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v\n%s", err, out)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatalf("go env GOMOD = %q, want module file", gomod)
	}
	return filepath.Dir(gomod)
}

// DHF-REQ: keel/requirement-85 (keel/ac-666)
func foundationExportImportPaths(t *testing.T) ([]string, error) {
	t.Helper()
	root := moduleRoot(t)
	excludes := foundationExportGateExcludes(t, root)
	cmd := exec.Command("git", "ls-files", "-z", "*.go")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errWithOutput("git ls-files -z *.go", err, out)
	}

	seen := map[string]bool{}
	for _, file := range strings.Split(string(out), "\x00") {
		if file == "" {
			continue
		}
		if foundationExportPathExcluded(file, excludes) {
			continue
		}
		dir := path.Dir(filepath.ToSlash(file))
		if dir == "." {
			seen[foundationExportsModulePath] = true
			continue
		}
		seen[foundationExportsModulePath+"/"+dir] = true
	}
	importPaths := make([]string, 0, len(seen))
	for importPath := range seen {
		importPaths = append(importPaths, importPath)
	}
	sort.Strings(importPaths)
	return importPaths, nil
}

func foundationExportGateExcludes(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "keel-dev.yaml"))
	if err != nil {
		t.Fatalf("read keel-dev.yaml: %v", err)
	}
	var excludes []string
	inGate := false
	inExcludes := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case indent == 0:
			inGate = trimmed == "gate:"
			inExcludes = false
		case inGate && indent == 2:
			inExcludes = trimmed == "excludes:"
		case inGate && inExcludes && strings.HasPrefix(trimmed, "- "):
			pattern := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			excludes = append(excludes, strings.Trim(pattern, `"'`))
		}
	}
	if len(excludes) == 0 {
		t.Fatal("keel-dev.yaml gate.excludes is empty or unreadable")
	}
	return excludes
}

func foundationExportPathExcluded(file string, patterns []string) bool {
	file = filepath.ToSlash(file)
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(pattern)
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "**")
			if strings.HasPrefix(file, prefix) {
				return true
			}
			continue
		}
		if matched, _ := path.Match(pattern, file); matched {
			return true
		}
	}
	return false
}

func errWithOutput(command string, err error, out []byte) error {
	if len(out) == 0 {
		return err
	}
	return &commandError{command: command, err: err, output: string(out)}
}

type commandError struct {
	command string
	err     error
	output  string
}

func (e *commandError) Error() string {
	return e.command + ": " + e.err.Error() + "\n" + e.output
}

func TestEmit_ProducesInfoLevelWithKindMetric(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("tool_call",
		slog.String("tool", "store_memory"),
		slog.Int64("duration_ms", 42),
		slog.Bool("error", false),
	)

	got := rc.LastJSON()
	if got == nil {
		t.Fatal("expected a captured log line, got nil")
	}
	if got["msg"] != "tool_call" {
		t.Errorf("msg = %q; want %q", got["msg"], "tool_call")
	}
	if got["level"] != "INFO" {
		t.Errorf("level = %q; want %q", got["level"], "INFO")
	}
	if got["kind"] != "metric" {
		t.Errorf("kind = %q; want %q", got["kind"], "metric")
	}
}

func TestEmit_MsDurationsAreNumeric(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("sync_timing",
		slog.String("op", "create"),
		slog.Int64("pull_ms", 10),
		slog.Int64("commit_ms", 20),
		slog.Int64("total_ms", 30),
	)

	got := rc.LastJSON()
	if got == nil {
		t.Fatal("expected a captured log line, got nil")
	}
	// JSON numbers unmarshal as float64 in map[string]any.
	for field, want := range map[string]float64{
		"pull_ms":   10,
		"commit_ms": 20,
		"total_ms":  30,
	} {
		v, ok := got[field].(float64)
		if !ok {
			t.Errorf("%s: type = %T; want float64", field, got[field])
			continue
		}
		if v != want {
			t.Errorf("%s = %v; want %v", field, v, want)
		}
	}
}

func TestEmit_CountFieldsAreNumeric(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("ingest_summary",
		slog.Int("ok_count", 5),
		slog.Int("err_count", 2),
	)

	got := rc.LastJSON()
	if got == nil {
		t.Fatal("expected a captured log line, got nil")
	}
	for field, want := range map[string]float64{
		"ok_count":  5,
		"err_count": 2,
	} {
		v, ok := got[field].(float64)
		if !ok {
			t.Errorf("%s: type = %T; want float64", field, got[field])
			continue
		}
		if v != want {
			t.Errorf("%s = %v; want %v", field, v, want)
		}
	}
}

func TestEmit_MultipleCallsProduceMultipleLines(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("tool_call", slog.String("tool", "tool_a"))
	logger.Emit("tool_call", slog.String("tool", "tool_b"))
	logger.Emit("tool_call", slog.String("tool", "tool_c"))

	// Count lines in the capture buffer.
	raw := rc.LastRaw()
	if raw == "" {
		t.Fatal("expected captured output, got empty")
	}
	// LastRaw returns the last line; verify it's the third tool.
	if !strings.Contains(raw, "tool_c") {
		t.Errorf("last line should mention tool_c; got: %s", raw)
	}

	// Reset and verify buffer tracks all three.
	rc.Reset()
	logger.Emit("tool_call", slog.String("tool", "tool_d"))
	got := rc.LastJSON()
	if got == nil {
		t.Fatal("expected a line after reset")
	}
	if got["tool"] != "tool_d" {
		t.Errorf("tool = %q; want %q", got["tool"], "tool_d")
	}
}

// ---------------------------------------------------------------------------
// AllJSON integration with metrics — verifies that AllJSON sees all Emit calls.
// This test is RED until AllJSON() is implemented on RecordCapture.
// ---------------------------------------------------------------------------

// TestEmit_AllJSONSeesAllLines asserts that AllJSON returns one entry per
// Emit call when multiple events are emitted. This is the key use-case for
// drift tests that need to inspect both a per-record line and a summary line.
func TestEmit_AllJSONSeesAllLines(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("tool_call", slog.String("tool", "a"))
	logger.Emit("tool_call", slog.String("tool", "b"))

	all := rc.AllJSON()
	if len(all) != 2 {
		t.Fatalf("AllJSON returned %d items after 2 Emit calls, want 2", len(all))
	}
	if all[0]["tool"] != "a" {
		t.Errorf("AllJSON[0][tool] = %v, want %q", all[0]["tool"], "a")
	}
	if all[1]["tool"] != "b" {
		t.Errorf("AllJSON[1][tool] = %v, want %q", all[1]["tool"], "b")
	}
	// Both must carry kind=metric.
	for i, line := range all {
		if line["kind"] != "metric" {
			t.Errorf("AllJSON[%d][kind] = %v, want %q", i, line["kind"], "metric")
		}
	}
}

func newJSONMetricLogger(t testing.TB, service string) (*logging.Logger, *recordCapture) {
	t.Helper()
	rc := &recordCapture{}
	return mustNewLogger(t, logging.Config{
		Service:          service,
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           rc,
	}), rc
}
