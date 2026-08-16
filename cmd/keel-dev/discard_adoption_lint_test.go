package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inlineDiscardSource spells one inline discard-logger construction. It is
// assembled from fragments so this test file is not itself an occurrence of
// what the policy forbids — the scan reads test files too.
func inlineDiscardSource(pkg string) string {
	construction := "slog." + "New(slog.NewTextHandler(io." + "Discard" + ", nil))"
	return "package " + pkg + `

import (
	"io"
	"log/slog"
)

// Quiet returns a logger that drops every record.
func Quiet() *slog.Logger { return ` + construction + " }\n"
}

// discardFixture writes a go.mod plus the named files, and returns the tree
// root. Paths are slash-separated and relative to that root.
func discardFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	for name, src := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLintRejectsInlineDiscardConstruction proves ac-493 still fails the gate
// on a hand-rolled discard logger outside log/discard.go, now that the
// criterion is enforced as a lint policy rather than a log/ test.
//
// DHF-TEST: keel/requirement-122 (keel/ac-493)
func TestLintRejectsInlineDiscardConstruction(t *testing.T) {
	dir := discardFixture(t, map[string]string{
		"log/discard.go":     inlineDiscardSource("log"),
		"exec/claude/cli.go": inlineDiscardSource("claude"),
	})
	err := runLint(dir, lintFixtureFiles(t, dir))
	if err == nil {
		t.Fatal("an inline discard construction outside log/discard.go should fail lint, got nil")
	}
	got := err.Error()
	for _, want := range []string{"no-inline-discard-construction", filepath.Join("exec", "claude", "cli.go"), "log.Discard()"} {
		if !strings.Contains(got, want) {
			t.Fatalf("inline-discard violation missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, filepath.Join("log", "discard.go")+":") {
		t.Fatalf("the canonical site must not be reported as a violation:\n%s", got)
	}
}

// TestLintAllowsTheCanonicalDiscardSite proves the policy's whole point: the
// one permitted construction site passes.
//
// DHF-TEST: keel/requirement-122 (keel/ac-493)
func TestLintAllowsTheCanonicalDiscardSite(t *testing.T) {
	dir := discardFixture(t, map[string]string{"log/discard.go": inlineDiscardSource("log")})
	if err := runLint(dir, lintFixtureFiles(t, dir)); err != nil && strings.Contains(err.Error(), "no-inline-discard-construction") {
		t.Fatalf("log/discard.go is the sanctioned construction site, got:\n%v", err)
	}
}

// TestLintDiscardScanIsSelfControlling proves the property that makes a clean
// result mean anything: if the needles stop matching the canonical site, they
// have drifted away from the construction they name, and every other file's
// "clean" verdict is vacuous. The scan must fail rather than pass silently.
//
// DHF-TEST: keel/requirement-122 (keel/ac-493)
func TestLintDiscardScanIsSelfControlling(t *testing.T) {
	dir := discardFixture(t, map[string]string{"log/discard.go": `package log

import "log/slog"

// Discard no longer builds what the needles describe.
func Discard() *slog.Logger { return slog.Default() }
`})
	err := runLint(dir, lintFixtureFiles(t, dir))
	if err == nil || !strings.Contains(err.Error(), "no-inline-discard-construction") {
		t.Fatalf("a canonical site matching no needle should fail the scan, got %v", err)
	}
	if !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("the self-control failure should name needle drift:\n%v", err)
	}
}

// TestLintDiscardScanReadsTestFiles proves the evaluated set includes test
// files. The duplication log.Discard retires is not a production-only habit —
// the original openbrain sites keel/issue-159 found included test helpers.
//
// DHF-TEST: keel/requirement-122 (keel/ac-493)
func TestLintDiscardScanReadsTestFiles(t *testing.T) {
	dir := discardFixture(t, map[string]string{
		"log/discard.go":       inlineDiscardSource("log"),
		"exec/helpers_test.go": inlineDiscardSource("exec"),
	})
	err := runLint(dir, lintFixtureFiles(t, dir))
	if err == nil || !strings.Contains(err.Error(), filepath.Join("exec", "helpers_test.go")) {
		t.Fatalf("a test file building a discard logger inline should be reported, got %v", err)
	}
}
