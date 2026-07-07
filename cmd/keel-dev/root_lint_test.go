package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindModuleRoot(t *testing.T) {
	// From a subdirectory of a keel-shaped module, the root is found.
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	sub := filepath.Join(root, "cmd", "keel-dev")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findModuleRoot(sub)
	if err != nil {
		t.Fatalf("findModuleRoot(%s) = %v", sub, err)
	}
	if got != root {
		t.Fatalf("findModuleRoot = %q, want %q", got, root)
	}

	// A foreign module is refused, not silently gated.
	foreign := t.TempDir()
	writeFile(t, foreign, "go.mod", "module example.com/other\n\ngo 1.25\n")
	if _, err := findModuleRoot(foreign); err == nil {
		t.Fatal("foreign module should be refused, got nil")
	}
}

func TestLintNoStdlibLog(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, dir, "bad.go", "package p\n\nimport \"log\"\n\nvar _ = log.Default\n")
	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-stdlib-log") {
		t.Fatalf("stdlib log import should fail lint, got %v", err)
	}

	// log/slog is allowed.
	writeFile(t, dir, "bad.go", "package p\n\nimport \"log/slog\"\n\nvar _ = slog.Default\n")
	if err := runLint(dir); err != nil {
		t.Fatalf("log/slog should pass lint, got %v", err)
	}
}

func TestLintNoRawFmtOutput(t *testing.T) {
	dir := t.TempDir()
	keeldev := filepath.Join(dir, "cmd", "keel-dev")
	if err := os.MkdirAll(keeldev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, keeldev, "out.go",
		"package main\n\nimport \"fmt\"\n\nfunc x() { fmt.Println(\"run output\") }\n")
	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-raw-fmt-output") {
		t.Fatalf("raw fmt output in keel-dev should fail lint, got %v", err)
	}

	// fmt.Sprintf constructs a value — allowed.
	writeFile(t, keeldev, "out.go",
		"package main\n\nimport \"fmt\"\n\nvar _ = fmt.Sprintf(\"x\")\n")
	if err := runLint(dir); err != nil {
		t.Fatalf("fmt.Sprintf should pass lint, got %v", err)
	}
}

// TestLintSelf holds keel's own tree to its own lint policies.
func TestLintSelf(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	if err := runLint(root); err != nil {
		t.Fatalf("keel fails its own lint:\n%v", err)
	}
}

func TestRunRejectsUnknownFlagAndExtraArgs(t *testing.T) {
	if code := run([]string{"ci", "--jsn"}); code != 2 {
		t.Fatalf("unknown flag should exit 2, got %d", code)
	}
	if code := run([]string{"ci", "extra"}); code != 2 {
		t.Fatalf("ci with an argument should exit 2, got %d", code)
	}
}
