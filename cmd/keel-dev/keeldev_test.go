package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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
	if err := runCI(context.Background(), discardLogger(), dir); err != nil {
		t.Fatalf("clean module should pass ci, got %v", err)
	}

	// Unformatted source (leading spaces): gofmt gate must fail before build.
	writeFile(t, dir, "keel_test_pkg.go", "package p\n\nvar    Y = 2\n")
	if err := runCI(context.Background(), discardLogger(), dir); err == nil {
		t.Fatal("unformatted module should fail ci, got nil")
	}
}

// TestRunStepLogsThroughKeelLog proves every subprocess flows through keel/exec
// and thus emits the START/END lifecycle through a keel/log handler.
func TestRunStepLogsThroughKeelLog(t *testing.T) {
	requireTool(t, "go")

	logger, cap := logging.NewForTesting("keel-dev")
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
