package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// stubTools builds a bin directory of fake git/gh/go/gofmt executables and
// prepends it to PATH for the test. Each stub appends its argv to calls.log and
// obeys per-command behavior baked into the script, so the whole release loop
// runs hermetically — no network, no real tags, no gh.
func stubTools(t *testing.T, dirtyTree bool, tagExists bool) (callsFile string) {
	t.Helper()
	bin := t.TempDir()
	callsFile = filepath.Join(bin, "calls.log")

	gitStatus := ""
	if dirtyTree {
		gitStatus = ` M dirty.go`
	}
	gitTagList := ""
	if tagExists {
		gitTagList = "v9.9.9"
	}

	stub(t, bin, callsFile, "git", `
case "$1 $2" in
  "status --porcelain") printf '%s' '`+gitStatus+`' ;;
  "tag --list") printf '%s' '`+gitTagList+`' ;;
esac
exit 0`)
	stub(t, bin, callsFile, "gh", "exit 0")
	stub(t, bin, callsFile, "gofmt", "exit 0")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "tool cover") echo "total:	(statements)	92.0%" ;;
esac
exit 0`)

	// Static-tool battery stubs: the version gate (keel/ac-42) probes each
	// tool's --version, so the stubs must echo the pinned version substring and
	// otherwise exit 0. deadcode is presence-only (no --version probe).
	stub(t, bin, callsFile, "golangci-lint", `
case "$1" in
  --version) echo "golangci-lint has version v2.0.2" ;;
esac
exit 0`)
	stub(t, bin, callsFile, "govulncheck", `
case "$1" in
  --version) echo "Scanner: govulncheck@v1.3.0" ;;
esac
exit 0`)
	stub(t, bin, callsFile, "cspell", `
case "$1" in
  --version) echo "10.0.1" ;;
esac
exit 0`)
	stub(t, bin, callsFile, "deadcode", "exit 0")
	// gitleaks is presence-only (no --version probe); a clean scan exits 0.
	stub(t, bin, callsFile, "gitleaks", "exit 0")

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return callsFile
}

func stub(t *testing.T, bin, callsFile, name, body string) {
	t.Helper()
	script := "#!/bin/sh\necho \"" + name + " $*\" >> " + callsFile + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func calls(t *testing.T, callsFile string) string {
	t.Helper()
	data, err := os.ReadFile(callsFile)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// moduleFixture writes a minimal keel-shaped module that passes the in-process
// gate steps (lint scans, coverage parse is stubbed via the fake go).
func moduleFixture(t *testing.T) string {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, dir, "p.go", "package p\n\nfunc One() int {\n\treturn 1\n}\n")
	return dir
}

// TestRunReleaseHappyPath drives the full release loop against stubs: preflight
// -> annotated tag -> push -> gh release -> anonymous-fetch verification, and
// asserts the mutating commands ran in order after the preflight (keel/ac-22).
func TestRunReleaseHappyPath(t *testing.T) {
	callsFile := stubTools(t, false, false)
	dir := moduleFixture(t)

	if err := runRelease(context.Background(), discardLogger(), dir, "v9.9.9"); err != nil {
		t.Fatalf("happy-path release failed: %v", err)
	}

	got := calls(t, callsFile)
	for _, want := range []string{
		"git status --porcelain",
		"git tag --list v9.9.9",
		"git tag -a v9.9.9 -m keel v9.9.9",
		"git push origin v9.9.9",
		"gh release create v9.9.9 --title keel v9.9.9 --generate-notes",
		"go get github.com/david-aggeler/keel@v9.9.9",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected call %q; calls:\n%s", want, got)
		}
	}
	if strings.Index(got, "git tag -a") < strings.Index(got, "git status --porcelain") {
		t.Error("tag created before preflight")
	}
}

// TestRunReleaseRefusesDirtyTree proves preflight aborts before any tag when
// the tree is dirty (keel/ac-21).
func TestRunReleaseRefusesDirtyTree(t *testing.T) {
	callsFile := stubTools(t, true, false)
	dir := moduleFixture(t)

	err := runRelease(context.Background(), discardLogger(), dir, "v9.9.9")
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("want dirty-tree refusal, got %v", err)
	}
	if got := calls(t, callsFile); strings.Contains(got, "git tag -a") || strings.Contains(got, "gh release") {
		t.Fatalf("mutating call after failed preflight:\n%s", got)
	}
}

// TestRunReleaseRefusesExistingTag proves an already-existing tag aborts the
// release before any mutation.
func TestRunReleaseRefusesExistingTag(t *testing.T) {
	callsFile := stubTools(t, false, true)
	dir := moduleFixture(t)

	err := runRelease(context.Background(), discardLogger(), dir, "v9.9.9")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want existing-tag refusal, got %v", err)
	}
	if got := calls(t, callsFile); strings.Contains(got, "git tag -a") {
		t.Fatalf("tag created despite existing tag:\n%s", got)
	}
}

// TestRunVerifySucceeds covers the tag-CI entrypoint against the stub go.
func TestRunVerifySucceeds(t *testing.T) {
	stubTools(t, false, false)
	if err := runVerify(context.Background(), discardLogger(), "v9.9.9"); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

// TestRunVerifyRetriesThenFails exercises the retry loop with a failing go stub
// and short delays.
func TestRunVerifyRetriesThenFails(t *testing.T) {
	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "exit 1")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldAttempts, oldDelay := fetchAttempts, fetchDelay
	fetchAttempts, fetchDelay = 2, 0
	defer func() { fetchAttempts, fetchDelay = oldAttempts, oldDelay }()

	err := runVerify(context.Background(), discardLogger(), "v9.9.9")
	if err == nil {
		t.Fatal("verify should fail when go get keeps failing")
	}
	if n := strings.Count(calls(t, callsFile), "go get "); n != 2 {
		t.Fatalf("want 2 fetch attempts, got %d", n)
	}
}

// TestRunCmdRoutesChildOutputThroughLogger is the ac-35 proof: child stdout
// arrives as keel/log records (redacted), not raw passthrough.
func TestRunCmdRoutesChildOutputThroughLogger(t *testing.T) {
	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "chatty", `echo "hello from child"
echo "dsn postgres://user:hunter2@db/x"`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	logger, cap := logging.NewForTesting("keel-dev")
	if err := runCmd(context.Background(), logger, ".", "chatty"); err != nil {
		t.Fatalf("runCmd: %v", err)
	}

	var sawHello, sawRedacted, sawSecret bool
	for _, rec := range cap.AllJSON() {
		msg, _ := rec["msg"].(string)
		if msg == "hello from child" {
			sawHello = true
		}
		if strings.Contains(msg, "***:***@") {
			sawRedacted = true
		}
		if strings.Contains(msg, "hunter2") {
			sawSecret = true
		}
	}
	if !sawHello {
		t.Error("child stdout line not surfaced as a log record")
	}
	if !sawRedacted || sawSecret {
		t.Errorf("redaction not applied to child output: redacted=%v secretLeaked=%v", sawRedacted, sawSecret)
	}
}

// TestLineLogWriterFlushAndCR covers the unterminated-line and CRLF paths.
func TestLineLogWriterFlushAndCR(t *testing.T) {
	logger, cap := logging.NewForTesting("keel-dev")
	w := newLineLogWriter(logger, "step", "stdout")
	if _, err := w.Write([]byte("one\r\npartial")); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	w.Flush() // second flush is a no-op

	var msgs []string
	for _, rec := range cap.AllJSON() {
		if m, ok := rec["msg"].(string); ok {
			msgs = append(msgs, m)
		}
	}
	if len(msgs) != 2 || msgs[0] != "one" || msgs[1] != "partial" {
		t.Fatalf("want [one partial], got %v", msgs)
	}
}
