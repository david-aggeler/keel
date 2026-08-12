package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-89
func TestMergeBranchScriptMergesReportsAndRunsNoGate(t *testing.T) {
	repo, mainBefore := mergeBranchScriptRepo(t)
	callsFile := installUnexpectedMergeBranchGoStub(t)

	out, err := runMergeBranchScript(t, repo, "unit")
	if err != nil {
		t.Fatalf("merge-branch.sh should pass without invoking gates: %v\n%s", err, out)
	}
	mainAfter := gitOutput(t, repo, "rev-parse", "HEAD")
	if mainAfter == mainBefore {
		t.Fatalf("main did not advance; output:\n%s", out)
	}
	if !strings.Contains(out, "MERGE_SHA="+mainAfter) {
		t.Fatalf("output should report the merge commit SHA %s:\n%s", mainAfter, out)
	}
	if calls := readOptionalFile(t, callsFile); calls != "" {
		t.Fatalf("merge command must not invoke go gate commands; calls:\n%s", calls)
	}
	if branch := gitOutput(t, repo, "rev-parse", "--verify", "refs/heads/unit"); branch == "" {
		t.Fatal("unit branch was deleted")
	}
}

// DHF-TEST: keel/requirement-89
func TestMergeBranchScriptReportsAlreadyMergedBranchWithoutSecondMergeCommit(t *testing.T) {
	repo, _ := mergeBranchScriptRepo(t)
	mustRun(t, repo, "git", "merge", "--no-ff", "--no-edit", "unit")
	existingMerge := gitOutput(t, repo, "rev-parse", "HEAD")
	countBefore := gitOutput(t, repo, "rev-list", "--count", "HEAD")
	callsFile := installUnexpectedMergeBranchGoStub(t)

	out, err := runMergeBranchScript(t, repo, "unit")
	if err != nil {
		t.Fatalf("merge-branch.sh should report an already-merged branch: %v\n%s", err, out)
	}
	countAfter := gitOutput(t, repo, "rev-list", "--count", "HEAD")
	if countAfter != countBefore {
		t.Fatalf("already-merged branch created another commit: before=%s after=%s\n%s", countBefore, countAfter, out)
	}
	if !strings.Contains(out, "MERGE_SHA="+existingMerge) {
		t.Fatalf("output should report the existing merge commit SHA %s:\n%s", existingMerge, out)
	}
	if calls := readOptionalFile(t, callsFile); calls != "" {
		t.Fatalf("already-merged guard must not invoke go gate commands; calls:\n%s", calls)
	}
}

// DHF-TEST: keel/requirement-89
func TestMergeBranchScriptRejectsBranchWithNoCommitsAhead(t *testing.T) {
	repo, mainBefore := mergeBranchScriptRepo(t)
	mustRun(t, repo, "git", "branch", "empty", "main")
	callsFile := installUnexpectedMergeBranchGoStub(t)

	out, err := runMergeBranchScript(t, repo, "empty")
	if err == nil {
		t.Fatalf("merge-branch.sh should reject a branch with no commits ahead of main; output:\n%s", out)
	}
	mainAfter := gitOutput(t, repo, "rev-parse", "HEAD")
	if mainAfter != mainBefore {
		t.Fatalf("main changed for empty branch: before=%s after=%s\n%s", mainBefore, mainAfter, out)
	}
	if !strings.Contains(out, "no commits ahead") {
		t.Fatalf("empty branch refusal should name the reason:\n%s", out)
	}
	if calls := readOptionalFile(t, callsFile); calls != "" {
		t.Fatalf("empty branch refusal must not invoke go gate commands; calls:\n%s", calls)
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

func installUnexpectedMergeBranchGoStub(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellSingleQuote(callsFile) + "\n" +
		"echo \"unexpected go args: $*\" >&2\n" +
		"exit 97\n"
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

func readOptionalFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
