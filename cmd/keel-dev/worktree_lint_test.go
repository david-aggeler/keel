package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-114 (keel/ac-410)
func TestNoShellWorktreeLifecycleAcceptsDelegatingWrappers(t *testing.T) {
	violations, err := scanNoShellWorktreeLifecycle(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Errorf("the delegated worktree scripts still carry git lifecycle commands:\n%s", strings.Join(violations, "\n"))
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-410)
func TestNoShellWorktreeLifecycleFlagsLifecycleCommands(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"worktree add", `git -C "$PRIMARY" worktree add -b "$BRANCH" "$PATH" main`, true},
		{"worktree list", `git -C "$PRIMARY" worktree list --porcelain`, true},
		{"branch delete", `git branch -D "$BRANCH"`, true},
		{"checkout", `git checkout -b "$BRANCH"`, true},
		{"switch", `git switch -c "$BRANCH"`, true},
		{"reset", `git -C "$WT" reset --hard main`, true},
		{"clean", `git -C "$WT" clean -ffd`, true},
		{"read-only toplevel", `git rev-parse --show-toplevel >/dev/null 2>&1`, false},
		{"read-only show-ref", `git show-ref --verify --quiet "refs/heads/$BRANCH"`, false},
		{"read-only status", `git status --porcelain`, false},
		{"comment mentioning worktree add", `# run 'git worktree add' by hand if this refuses`, false},
		{"prose without git", `echo "run git worktree prune to clean up" >&2`, true},
	}
	for _, tc := range cases {
		root := t.TempDir()
		dir := filepath.Join(root, ".claude", "skills", "change-request", "scripts")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "worktree-up.sh"), []byte("#!/usr/bin/env bash\n"+tc.line+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		violations, err := scanNoShellWorktreeLifecycle(root)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(violations) > 0; got != tc.want {
			t.Errorf("%s: flagged=%v, want %v (violations: %v)", tc.name, got, tc.want, violations)
		}
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-412)
func TestNoShellWorktreeLifecycleRequiresExecutableWrappers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "skills", "change-request", "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "worktree-up.sh")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\nexec keel-dev worktree up \"$@\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := scanNoShellWorktreeLifecycle(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) == 0 {
		t.Fatal("non-executable worktree wrapper produced no lint violation")
	}
	if got := strings.Join(violations, "\n"); !strings.Contains(got, "mode 0644") {
		t.Fatalf("non-executable wrapper violation did not name its mode:\n%s", got)
	}

	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	violations, err = scanNoShellWorktreeLifecycle(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("executable worktree wrapper produced violations:\n%s", strings.Join(violations, "\n"))
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-410)
func TestNoShellWorktreeLifecycleIgnoresMissingScripts(t *testing.T) {
	violations, err := scanNoShellWorktreeLifecycle(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Errorf("a tree without the skill scripts produced violations: %v", violations)
	}
}
