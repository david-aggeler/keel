package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// worktreeScriptDir is the change-request skill's script directory, relative to
// this package.
const worktreeScriptDir = "../../.claude/skills/change-request/scripts"

// worktreeScriptEnv is one throwaway repository the wrapper scripts are driven
// against, plus the keel-dev binary they delegate to.
type worktreeScriptEnv struct {
	repo      string
	worktrees string
	keelDev   string
}

// path returns the worktree path the scripts report for a work item.
func (e worktreeScriptEnv) path(name string) string {
	return filepath.Join(e.worktrees, name)
}

type worktreeScriptResult struct {
	exitCode int
	stdout   string
	stderr   string
}

// newWorktreeScriptEnv builds a repository with a `main` branch and a go.mod
// declaring the keel module path, so a delegated wrapper's keel-dev invocation
// anchors on the throwaway repository instead of refusing it as foreign.
func newWorktreeScriptEnv(t *testing.T, keelDev string) worktreeScriptEnv {
	t.Helper()
	repo := resolvedTempDir(t)
	mustRun(t, repo, "git", "init", "-b", "main")
	mustRun(t, repo, "git", "config", "user.email", "keel-test@example.invalid")
	mustRun(t, repo, "git", "config", "user.name", "Keel Test")
	writeFile(t, repo, "go.mod", "module github.com/david-aggeler/keel\n\ngo 1.25\n")
	mustRun(t, repo, "git", "add", "go.mod")
	mustRun(t, repo, "git", "commit", "-m", "base")
	return worktreeScriptEnv{repo: repo, worktrees: filepath.Join(repo, "worktrees"), keelDev: keelDev}
}

func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

// buildKeelDev compiles the CLI once so the wrapper scripts can delegate to a
// real binary rather than a `go run` of sources the throwaway repository does
// not contain.
func buildKeelDev(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "keel-dev")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/keel-dev")
	cmd.Dir = filepath.Join("..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build keel-dev: %v\n%s", err, out)
	}
	return bin
}

func runWorktreeScript(t *testing.T, env worktreeScriptEnv, dir, script string, args ...string) worktreeScriptResult {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(worktreeScriptDir, script))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{path}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "KEEL_DEV_BIN="+env.keelDev, "LC_ALL=C")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		exitErr, ok := runErr.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %s %v: %v", script, args, runErr)
		}
		code = exitErr.ExitCode()
	}
	return worktreeScriptResult{exitCode: code, stdout: stdout.String(), stderr: stderr.String()}
}

// worktreeResultTokens are the leading tokens of every result line the scripts'
// documented contract defines. Anything else on stdout — `git worktree add`
// writes its own "Preparing worktree"/"HEAD is now at" chatter there — is not
// part of the contract and is deliberately not pinned.
var worktreeResultTokens = []string{"up-noop", "up", "down-noop", "down", "resume-noop", "resume", "status"}

// worktreeResultLines extracts the contract's result lines from a script's
// stdout, in order.
func worktreeResultLines(stdout string) []string {
	var lines []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimRight(line, "\r")
		for _, token := range worktreeResultTokens {
			if strings.HasPrefix(line, token+" ") {
				lines = append(lines, line)
				break
			}
		}
	}
	return lines
}

// assertScript checks one scenario against the exit-code taxonomy, the exact
// result lines, and any stderr tokens the contract names.
func assertScript(t *testing.T, label string, got worktreeScriptResult, wantExit int, wantLines []string, wantStderr ...string) {
	t.Helper()
	if got.exitCode != wantExit {
		t.Errorf("%s: exit %d, want %d\nstdout: %q\nstderr: %s", label, got.exitCode, wantExit, got.stdout, got.stderr)
	}
	gotLines := worktreeResultLines(got.stdout)
	if len(gotLines) != len(wantLines) {
		t.Errorf("%s: %d result lines, want %d\ngot: %q\nwant: %q\nstderr: %s", label, len(gotLines), len(wantLines), gotLines, wantLines, got.stderr)
	} else {
		for i, want := range wantLines {
			if gotLines[i] != want {
				t.Errorf("%s: result line %d is %q, want %q\nstderr: %s", label, i, gotLines[i], want, got.stderr)
			}
		}
	}
	for _, token := range wantStderr {
		if !strings.Contains(got.stderr, token) {
			t.Errorf("%s: stderr does not carry %q\nstderr: %s", label, token, got.stderr)
		}
	}
}

// results is shorthand for a scenario's expected result lines.
func results(lines ...string) []string { return lines }

// TestWorktreeScriptsPreserveArgumentRejection pins the bad-argument leg of the
// taxonomy: every script rejects a bad kind, slug, or sequence with exit 64 and
// no result line, before any lifecycle work is attempted.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeScriptsPreserveArgumentRejection(t *testing.T) {
	env := newWorktreeScriptEnv(t, buildKeelDev(t))
	scripts := []string{"worktree-up.sh", "worktree-down.sh", "worktree-status.sh", "worktree-resume.sh"}
	cases := []struct {
		label string
		args  []string
		token string
	}{
		{"bad kind", []string{"nope", "1", "alpha"}, "invalid kind"},
		{"bad slug", []string{"cr", "1", "Alpha"}, "invalid slug"},
		{"bad seq", []string{"cr", "x", "alpha"}, "invalid seq"},
		{"slug too long", []string{"cr", "1", strings.Repeat("a", 101)}, "slug too long"},
	}
	for _, script := range scripts {
		for _, tc := range cases {
			got := runWorktreeScript(t, env, env.repo, script, tc.args...)
			assertScript(t, script+" "+tc.label, got, 64, nil, tc.token)
		}
	}
}

// TestWorktreeScriptsRefuseOutsideRepository pins the not-in-repo leg: exit 2
// with no result line, from a directory that is not a git repository.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeScriptsRefuseOutsideRepository(t *testing.T) {
	env := newWorktreeScriptEnv(t, buildKeelDev(t))
	outside := resolvedTempDir(t)
	for _, script := range []string{"worktree-up.sh", "worktree-down.sh", "worktree-status.sh", "worktree-resume.sh"} {
		got := runWorktreeScript(t, env, outside, script, "cr", "1", "alpha")
		assertScript(t, script+" outside a repository", got, 2, nil, "not in a git repo")
	}
}

// TestWorktreeUpScriptLifecycle pins worktree-up.sh's success, no-op, and
// conflict legs.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeUpScriptLifecycle(t *testing.T) {
	env := newWorktreeScriptEnv(t, buildKeelDev(t))

	created := runWorktreeScript(t, env, env.repo, "worktree-up.sh", "cr", "1", "alpha")
	assertScript(t, "up creates", created, 0, results("up cr-1-alpha "+env.path("cr-1-alpha")))

	repeated := runWorktreeScript(t, env, env.repo, "worktree-up.sh", "cr", "1", "alpha")
	assertScript(t, "up repeats as a no-op", repeated, 0, results("up-noop cr-1-alpha "+env.path("cr-1-alpha")))

	// A directory in the way that this repository has never registered.
	if err := os.MkdirAll(env.path("cr-2-beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	occupied := runWorktreeScript(t, env, env.repo, "worktree-up.sh", "cr", "2", "beta")
	assertScript(t, "up refuses an unregistered path", occupied, 65, nil)

	// A branch with no checkout: bring-up refuses and points at resume.
	mustRun(t, env.repo, "git", "branch", "cr-3-gamma")
	orphaned := runWorktreeScript(t, env, env.repo, "worktree-up.sh", "cr", "3", "gamma")
	assertScript(t, "up refuses an existing branch", orphaned, 65, nil, "cr-3-gamma")
}

// TestWorktreeDownScriptLifecycle pins worktree-down.sh's removal, no-op, and
// refusal legs — including the stale-registration remediation hint.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeDownScriptLifecycle(t *testing.T) {
	env := newWorktreeScriptEnv(t, buildKeelDev(t))
	runWorktreeScript(t, env, env.repo, "worktree-up.sh", "cr", "1", "alpha")

	absent := runWorktreeScript(t, env, env.repo, "worktree-down.sh", "cr", "9", "absent")
	assertScript(t, "down on an absent checkout", absent, 0, results("down-noop cr-9-absent "+env.path("cr-9-absent")))

	writeFile(t, env.path("cr-1-alpha"), "scratch.txt", "work\n")
	dirty := runWorktreeScript(t, env, env.repo, "worktree-down.sh", "cr", "1", "alpha")
	assertScript(t, "down refuses a dirty checkout", dirty, 66, nil)

	if err := os.Remove(filepath.Join(env.path("cr-1-alpha"), "scratch.txt")); err != nil {
		t.Fatal(err)
	}
	removed := runWorktreeScript(t, env, env.repo, "worktree-down.sh", "cr", "1", "alpha")
	assertScript(t, "down removes a clean checkout", removed, 0, results("down cr-1-alpha "+env.path("cr-1-alpha")))

	// A directory this repository has no registration for: refused, naming the
	// prune that would clear it.
	if err := os.MkdirAll(env.path("cr-4-delta"), 0o755); err != nil {
		t.Fatal(err)
	}
	unregistered := runWorktreeScript(t, env, env.repo, "worktree-down.sh", "cr", "4", "delta")
	assertScript(t, "down refuses an unregistered path", unregistered, 66, nil, "git worktree prune")
}

// TestWorktreeDownScriptRemovesUnpushedWork pins the leg that keel/worktree's
// own tear-down would refuse: a checkout whose commits are on no remote is
// removable, because tear-down never deletes the branch that keeps them.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeDownScriptRemovesUnpushedWork(t *testing.T) {
	env := newWorktreeScriptEnv(t, buildKeelDev(t))
	remote := resolvedTempDir(t)
	mustRun(t, remote, "git", "init", "--bare", "-b", "main")
	mustRun(t, env.repo, "git", "remote", "add", "origin", remote)
	mustRun(t, env.repo, "git", "push", "-u", "origin", "main")

	runWorktreeScript(t, env, env.repo, "worktree-up.sh", "cr", "1", "alpha")
	unit := env.path("cr-1-alpha")
	writeFile(t, unit, "slice.txt", "slice\n")
	mustRun(t, unit, "git", "add", "slice.txt")
	mustRun(t, unit, "git", "commit", "-m", "slice")

	removed := runWorktreeScript(t, env, env.repo, "worktree-down.sh", "cr", "1", "alpha")
	assertScript(t, "down removes a checkout holding unpushed commits", removed, 0, results("down cr-1-alpha "+unit))

	if gitOutput(t, env.repo, "rev-parse", "--verify", "refs/heads/cr-1-alpha") == "" {
		t.Error("tear-down did not preserve the branch")
	}
}

// TestWorktreeResumeScriptLifecycle pins worktree-resume.sh's re-attach, no-op,
// and branch-missing legs.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeResumeScriptLifecycle(t *testing.T) {
	env := newWorktreeScriptEnv(t, buildKeelDev(t))
	runWorktreeScript(t, env, env.repo, "worktree-up.sh", "cr", "1", "alpha")

	registered := runWorktreeScript(t, env, env.repo, "worktree-resume.sh", "cr", "1", "alpha")
	assertScript(t, "resume on a registered checkout", registered, 0, results("resume-noop cr-1-alpha "+env.path("cr-1-alpha")))

	missing := runWorktreeScript(t, env, env.repo, "worktree-resume.sh", "cr", "9", "absent")
	assertScript(t, "resume without a branch", missing, 67, nil, "cr-9-absent")

	// Tear the checkout down, keeping the branch, then re-attach it.
	runWorktreeScript(t, env, env.repo, "worktree-down.sh", "cr", "1", "alpha")
	reattached := runWorktreeScript(t, env, env.repo, "worktree-resume.sh", "cr", "1", "alpha")
	assertScript(t, "resume re-attaches an existing branch", reattached, 0, results("resume cr-1-alpha "+env.path("cr-1-alpha")))
}

// TestWorktreeStatusScriptReports pins worktree-status.sh's three-argument and
// glob forms, including the bad-pattern rejection.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeStatusScriptReports(t *testing.T) {
	env := newWorktreeScriptEnv(t, buildKeelDev(t))
	runWorktreeScript(t, env, env.repo, "worktree-up.sh", "cr", "1", "alpha")

	live := runWorktreeScript(t, env, env.repo, "worktree-status.sh", "cr", "1", "alpha")
	assertScript(t, "status of a live checkout", live, 0,
		results("status cr-1-alpha "+env.path("cr-1-alpha")+" branch=true worktree=true"))

	absent := runWorktreeScript(t, env, env.repo, "worktree-status.sh", "cr", "9", "absent")
	assertScript(t, "status of an absent checkout", absent, 0,
		results("status cr-9-absent "+env.path("cr-9-absent")+" branch=false worktree=false"))

	matched := runWorktreeScript(t, env, env.repo, "worktree-status.sh", "--glob", "cr*")
	assertScript(t, "glob status", matched, 0,
		results("status cr-1-alpha "+env.path("cr-1-alpha")+" branch=true worktree=true"))

	none := runWorktreeScript(t, env, env.repo, "worktree-status.sh", "--glob", "epic*")
	assertScript(t, "glob status with no matches", none, 0, nil)

	bad := runWorktreeScript(t, env, env.repo, "worktree-status.sh", "--glob", "CR*")
	assertScript(t, "glob status rejects a bad pattern", bad, 64, nil, "invalid glob pattern")

	arity := runWorktreeScript(t, env, env.repo, "worktree-status.sh", "--glob")
	assertScript(t, "glob status rejects a missing pattern", arity, 64, nil, "usage:")
}
