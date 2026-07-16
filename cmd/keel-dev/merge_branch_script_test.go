package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-89
func TestMergeBranchScriptRunsCoreThenVSIXGateAndPrintsMergeSHA(t *testing.T) {
	repo, mainBefore := mergeBranchScriptRepo(t)
	callsFile := installMergeBranchGoStub(t, 0, 0)

	out, err := runMergeBranchScript(t, repo, "unit")
	if err != nil {
		t.Fatalf("merge-branch.sh should pass when core and VSIX gates pass: %v\n%s", err, out)
	}
	mainAfter := gitOutput(t, repo, "rev-parse", "HEAD")
	if mainAfter == mainBefore {
		t.Fatalf("main did not advance on green gates; output:\n%s", out)
	}
	if !strings.Contains(out, "MERGE_SHA="+mainAfter) {
		t.Fatalf("output should report the merge commit SHA %s:\n%s", mainAfter, out)
	}

	calls := readFile(t, callsFile)
	core := strings.Index(calls, "run ./cmd/keel-dev ci\n")
	vsix := strings.Index(calls, "run ./cmd/keel-dev vsix ci\n")
	if core == -1 || vsix == -1 {
		t.Fatalf("expected core and VSIX gates; calls:\n%s", calls)
	}
	if core > vsix {
		t.Fatalf("core gate should run before VSIX gate; calls:\n%s", calls)
	}
}

// DHF-TEST: keel/requirement-89
func TestMergeBranchScriptRevertsWhenVSIXGateFails(t *testing.T) {
	repo, mainBefore := mergeBranchScriptRepo(t)
	callsFile := installMergeBranchGoStub(t, 0, 7)

	out, err := runMergeBranchScript(t, repo, "unit")
	if err == nil {
		t.Fatalf("merge-branch.sh should fail when VSIX gate fails; output:\n%s", out)
	}
	mainAfter := gitOutput(t, repo, "rev-parse", "HEAD")
	if mainAfter != mainBefore {
		t.Fatalf("main advanced despite red VSIX gate: before=%s after=%s\n%s", mainBefore, mainAfter, out)
	}
	if !strings.Contains(out, "post-merge VSIX gate red") {
		t.Fatalf("red VSIX gate should be named in the failure output:\n%s", out)
	}
	calls := readFile(t, callsFile)
	if !strings.Contains(calls, "run ./cmd/keel-dev vsix ci\n") {
		t.Fatalf("VSIX gate was not invoked; calls:\n%s", calls)
	}
}

func mergeBranchScriptRepo(t *testing.T) (repo string, mainBefore string) {
	t.Helper()
	repo = t.TempDir()
	mustRun(t, repo, "git", "init")
	mustRun(t, repo, "git", "config", "user.email", "keel-test@example.invalid")
	mustRun(t, repo, "git", "config", "user.name", "Keel Test")
	mustRun(t, repo, "git", "branch", "-M", "main")
	writeFile(t, repo, "tracked.txt", "base\n")
	mustRun(t, repo, "git", "add", "tracked.txt")
	mustRun(t, repo, "git", "commit", "-m", "base")
	mainBefore = gitOutput(t, repo, "rev-parse", "HEAD")

	mustRun(t, repo, "git", "checkout", "-b", "unit")
	writeFile(t, repo, "tracked.txt", "unit\n")
	mustRun(t, repo, "git", "commit", "-am", "unit")
	mustRun(t, repo, "git", "checkout", "main")
	return repo, mainBefore
}

func installMergeBranchGoStub(t *testing.T, coreExit, vsixExit int) string {
	t.Helper()
	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellSingleQuote(callsFile) + "\n" +
		"case \"$*\" in\n" +
		"  'run ./cmd/keel-dev ci') exit " + itoaStub(coreExit) + " ;;\n" +
		"  'run ./cmd/keel-dev vsix ci') exit " + itoaStub(vsixExit) + " ;;\n" +
		"  *) echo \"unexpected go args: $*\" >&2; exit 97 ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return callsFile
}

func runMergeBranchScript(t *testing.T, repo, branch string) (string, error) {
	t.Helper()
	script := filepath.Join("..", "..", ".claude", "skills", "merge", "scripts", "merge-branch.sh")
	script, err := filepath.Abs(script)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, branch)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
