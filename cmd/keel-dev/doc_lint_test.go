package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLintNoUndocumentedExports proves the ac-49 gate flags an exported
// identifier in a library package (log/exec) that lacks a doc comment, naming
// the identifier and its location, and passes once the comment is present. It
// also covers exported struct fields (ac-46's field clause) and confirms
// cmd/keel-dev is out of scope for the doc check.
//
// DHF-TEST: keel/user_need-1 (keel/ac-49)
func TestLintNoUndocumentedExports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	logdir := filepath.Join(dir, "log")
	if err := os.MkdirAll(logdir, 0o755); err != nil {
		t.Fatal(err)
	}

	// An exported func with no doc comment fails, naming the identifier.
	writeFile(t, logdir, "widget.go", "package log\n\nfunc Widget() {}\n")
	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-undocumented-exports") || !strings.Contains(err.Error(), "Widget") {
		t.Fatalf("undocumented exported func should fail lint naming it, got %v", err)
	}

	// With a doc comment it passes.
	writeFile(t, logdir, "widget.go", "package log\n\n// Widget does a thing.\nfunc Widget() {}\n")
	if err := runLint(dir); err != nil {
		t.Fatalf("documented exported func should pass lint, got %v", err)
	}

	// The check also covers exported struct fields (ac-46's field clause).
	writeFile(t, logdir, "widget.go", "package log\n\n// Widget holds config.\ntype Widget struct {\n\tName string\n}\n")
	err = runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-undocumented-exports") || !strings.Contains(err.Error(), "Name") {
		t.Fatalf("undocumented exported struct field should fail lint naming it, got %v", err)
	}

	// cmd/keel-dev is out of scope for the doc check (ac-49 permits excluding it).
	keeldev := filepath.Join(dir, "cmd", "keel-dev")
	if err := os.MkdirAll(keeldev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, logdir, "widget.go", "package log\n\n// Widget holds config.\ntype Widget struct {\n\t// Name is the widget name.\n\tName string\n}\n")
	writeFile(t, keeldev, "undoc.go", "package main\n\nfunc Exported() {}\n")
	if err := runLint(dir); err != nil {
		t.Fatalf("cmd/keel-dev undocumented export must not trip the library doc check, got %v", err)
	}
}
