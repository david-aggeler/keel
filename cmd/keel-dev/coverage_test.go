package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/david-aggeler/keel/vscode"
)

// DHF-TEST: keel/requirement-11
func TestCoverageFloorRatchetsToNinetyPercent(t *testing.T) {
	if coverageFloorPercent != 90.0 {
		t.Fatalf("coverageFloorPercent = %.1f, want 90.0", coverageFloorPercent)
	}
}

// DHF-TEST: keel/requirement-11
func TestCoveragePackageLineParsingAndEmission(t *testing.T) {
	for _, tc := range []struct {
		line       string
		wantPkg    string
		wantMillis int64
		wantOK     bool
	}{
		{line: "ok  \tgithub.com/david-aggeler/keel/log\t1.234s", wantPkg: "github.com/david-aggeler/keel/log", wantMillis: 1234, wantOK: true},
		{line: "?   \tgithub.com/david-aggeler/keel/cmd/tool\t[no test files]", wantPkg: "github.com/david-aggeler/keel/cmd/tool", wantOK: true},
		{line: "ok  \tgithub.com/david-aggeler/keel/vscode\tbad-duration", wantPkg: "github.com/david-aggeler/keel/vscode", wantOK: true},
		{line: "FAIL\tgithub.com/david-aggeler/keel/log\t0.1s", wantOK: false},
		{line: "ok github.com/david-aggeler/keel/log", wantOK: false},
	} {
		pkg, millis, ok := parseGoTestPackageLine(tc.line)
		if ok != tc.wantOK || pkg != tc.wantPkg || millis != tc.wantMillis {
			t.Fatalf("parseGoTestPackageLine(%q) = %q,%d,%v; want %q,%d,%v", tc.line, pkg, millis, ok, tc.wantPkg, tc.wantMillis, tc.wantOK)
		}
	}

	var events []vscode.RunEvent
	emitVSCodeCoveragePackages("ok  \tgithub.com/david-aggeler/keel/log\t1.5s\nFAIL\tignored\t0.1s\n", func(event vscode.RunEvent) {
		events = append(events, event)
	})
	if len(events) != 1 || events[0].Event != "passed" || events[0].TestID != "go::package::github.com/david-aggeler/keel/log" || events[0].DurationMS != 1500 {
		t.Fatalf("coverage package events = %+v", events)
	}
}

// DHF-TEST: keel/requirement-11
func TestPruneOldVSCodeCoverageDirsRemovesOnlyExpiredDirectories(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "old")
	newDir := filepath.Join(root, "new")
	file := filepath.Join(root, "cover.out")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "cover.out", "profile")
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(newDir, time.Now(), time.Now()); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}

	pruneOldVSCodeCoverageDirs(root, slog.Default())
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old coverage dir still exists")
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("new coverage dir missing: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("new dir or non-dir file was removed")
	}
}

// DHF-TEST: keel/requirement-11
func TestRunTestWithCoverageReportsCommandAndParseFailures(t *testing.T) {
	root := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./...")
    printf 'unit failure\n' >&2
    exit 7
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin)
	if err := runTestWithCoverage(context.Background(), logger, root); err == nil {
		t.Fatal("failing go test returned nil error, want child failure")
	}

	bin = t.TempDir()
	callsFile = filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./...")
    for arg in "$@"; do
      case "$arg" in
        -coverprofile=*)
          profile=${arg#-coverprofile=}
          mkdir -p "$(dirname "$profile")"
          printf 'mode: atomic\npkg/file.go:1.1,1.10 1 1\n' > "$profile"
          ;;
      esac
    done
    exit 0
    ;;
  "tool cover")
    printf 'cover broke\n' >&2
    exit 4
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin)
	if err := runTestWithCoverage(context.Background(), logger, root); err == nil || !strings.Contains(err.Error(), "go tool cover") || !strings.Contains(err.Error(), "cover broke") {
		t.Fatalf("failing go tool cover err = %v, want stderr surfaced", err)
	}

	bin = t.TempDir()
	callsFile = filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./...")
    exit 0
    ;;
  "tool cover")
    printf 'pkg/file.go:1:\tFunc\t91.2%%\n'
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin)
	if err := runTestWithCoverage(context.Background(), logger, root); err == nil || !strings.Contains(err.Error(), "no total: line") {
		t.Fatalf("missing total err = %v, want parse failure", err)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-39
func TestRunVSCodeTestCoverageReportsFailureBranches(t *testing.T) {
	root := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./...")
    printf 'ok  \tgithub.com/david-aggeler/keel/log\t0.010s\n'
    printf 'suite failed\n' >&2
    exit 1
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin)
	var events []vscode.RunEvent
	err := runVSCodeTestCoverage(context.Background(), logger, root, "run-fail-test", 1024, func(event vscode.RunEvent) {
		events = append(events, event)
	})
	if err == nil || !strings.Contains(err.Error(), "go test coverage") || !strings.Contains(err.Error(), "suite failed") {
		t.Fatalf("failing VS Code go test err = %v, want stderr surfaced", err)
	}
	if !runEventsContain(events, "passed", "go::package::github.com/david-aggeler/keel/log") {
		t.Fatalf("failing coverage lane still should emit package events, got %+v", events)
	}

	bin = t.TempDir()
	callsFile = filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./...")
    for arg in "$@"; do
      case "$arg" in
        -coverprofile=*)
          profile=${arg#-coverprofile=}
          mkdir -p "$(dirname "$profile")"
          printf 'mode: atomic\npkg/file.go:1.1,1.10 1 1\n' > "$profile"
          ;;
      esac
    done
    exit 0
    ;;
  "tool cover")
    printf 'total:\t(statements)\t10.0%%\n'
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin)
	events = nil
	err = runVSCodeTestCoverage(context.Background(), logger, root, "run-below-floor", 1024, func(event vscode.RunEvent) {
		events = append(events, event)
	})
	if err == nil || !strings.Contains(err.Error(), "below the") {
		t.Fatalf("below-floor VS Code coverage err = %v, want floor failure", err)
	}
	if !runEventsContain(events, "artifact", vscodeLaneTestCoverage) {
		t.Fatalf("below-floor coverage lane should emit artifact before failing, got %+v", events)
	}
}
