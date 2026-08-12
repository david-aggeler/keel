package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// worktreeTestEnv is one throwaway repository the worktree verbs are driven
// against, plus the keel-dev binary a test may delegate to.
//
// This scaffolding outlived the change-request skill's `worktree-*.sh` wrappers
// it was originally written for. The catalog deleted those wrappers when it cut
// its change-request skill over to `openbrain-client worktree`; the gate exclude
// list now keeps catalog-materialized paths out of file-selecting steps. The
// wrapper tests went with the wrappers (keel/issue-129, keel/change_request-169).
// The helpers stay because cmd/keel-dev's own verb tests use them.
type worktreeTestEnv struct {
	repo      string
	worktrees string
	keelDev   string
}

// newWorktreeScriptEnv builds a repository with a `main` branch and a go.mod
// declaring the keel module path, so a keel-dev invocation anchors on the
// throwaway repository instead of refusing it as foreign.
func newWorktreeScriptEnv(t *testing.T, keelDev string) worktreeTestEnv {
	t.Helper()
	repo := resolvedTempDir(t)
	mustRun(t, repo, "git", "init", "-b", "main")
	mustRun(t, repo, "git", "config", "user.email", "keel-test@example.invalid")
	mustRun(t, repo, "git", "config", "user.name", "Keel Test")
	writeFile(t, repo, "go.mod", "module github.com/david-aggeler/keel\n\ngo 1.25\n")
	mustRun(t, repo, "git", "add", "go.mod")
	mustRun(t, repo, "git", "commit", "-m", "base")
	return worktreeTestEnv{repo: repo, worktrees: filepath.Join(repo, "worktrees"), keelDev: keelDev}
}

// resolvedTempDir returns t.TempDir() with symlinks resolved, so a path a test
// computes compares equal to the one git reports back.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

// buildKeelDev compiles the CLI once so a test can drive a real binary rather
// than a `go run` of sources the throwaway repository does not contain.
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
