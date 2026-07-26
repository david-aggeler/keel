package worktree_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

// TestUndeletableEmptyAndUnreadableDirectories covers the two shapes the
// unlinkability walk meets besides a populated unwritable directory: one with
// nothing in it (the directory itself is what cannot be removed) and one whose
// entries cannot even be listed (an inspection failure, never silence).
//
// DHF-TEST: keel/requirement-113 (keel/ac-403)
func TestUndeletableEmptyAndUnreadableDirectories(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: every path is unlinkable, so the condition cannot be staged")
	}
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}

	empty := filepath.Join(wt.Path, "empty")
	unreadable := filepath.Join(wt.Path, "unreadable")
	for _, dir := range []string{empty, unreadable} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.Chmod(empty, 0o500); err != nil { // readable, not writable
		t.Fatalf("chmod empty: %v", err)
	}
	if err := os.Chmod(unreadable, 0o300); err != nil { // writable, not readable
		t.Fatalf("chmod unreadable: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(empty, 0o755)
		_ = os.Chmod(unreadable, 0o755)
	})

	_, err = m.Down(ctx, "unit-1", worktree.DownOptions{Force: true})
	report := blockedReport(t, err)
	if got := blockerPaths(report, worktree.BlockerUndeletableContent); len(got) != 1 || got[0] != empty {
		t.Errorf("undeletable blockers = %v, want the empty unwritable directory %s", got, empty)
	}
	failures := blockerPaths(report, worktree.BlockerInspectionFailed)
	if len(failures) != 1 || failures[0] != unreadable {
		t.Errorf("inspection failures = %v, want the unlistable directory %s", failures, unreadable)
	}
}

// TestPathsThatFailToStat keeps an unusable worktrees directory from
// reading as "nothing there yet".
//
// DHF-TEST: keel/requirement-113 (keel/ac-400)
func TestPathsThatFailToStat(t *testing.T) {
	root := newRepo(t)
	// A file where the worktrees directory belongs: every path under it fails to
	// stat with something other than not-exist.
	blocked := filepath.Join(root, "blocked")
	writeFile(t, blocked, "not a directory\n")
	m := newManager(t, worktree.Config{RepoRoot: root, WorktreesDir: blocked, Base: "main"})
	ctx := context.Background()

	if _, err := m.Up(ctx, "unit-1"); !isCode(err, worktree.CodeGit) {
		t.Errorf("up under an unusable parent = %v, want CodeGit", err)
	}
	if _, err := m.State(ctx, "unit-1"); !isCode(err, worktree.CodeGit) {
		t.Errorf("state under an unusable parent = %v, want CodeGit", err)
	}
	if _, err := m.Down(ctx, "unit-1", worktree.DownOptions{}); !isCode(err, worktree.CodeGit) {
		t.Errorf("down under an unusable parent = %v, want CodeGit", err)
	}
}

// TestUnsafeNamesRefusedByEveryOperation keeps every entry point behind the same
// validation, so no operation can act on a name the others reject.
//
// DHF-TEST: keel/requirement-113 (keel/ac-400)
func TestUnsafeNamesRefusedByEveryOperation(t *testing.T) {
	m := newManager(t, worktree.Config{RepoRoot: newRepo(t), Base: "main"})
	ctx := context.Background()
	const bad = "bad name"

	if _, err := m.Up(ctx, bad); !isCode(err, worktree.CodeInvalidArgument) {
		t.Errorf("up = %v, want CodeInvalidArgument", err)
	}
	if _, err := m.Down(ctx, bad, worktree.DownOptions{}); !isCode(err, worktree.CodeInvalidArgument) {
		t.Errorf("down = %v, want CodeInvalidArgument", err)
	}
	if _, err := m.State(ctx, bad); !isCode(err, worktree.CodeInvalidArgument) {
		t.Errorf("state = %v, want CodeInvalidArgument", err)
	}
	if _, err := m.Compare(ctx, bad); !isCode(err, worktree.CodeInvalidArgument) {
		t.Errorf("compare = %v, want CodeInvalidArgument", err)
	}
	if _, err := m.ResetFresh(ctx, bad); !isCode(err, worktree.CodeInvalidArgument) {
		t.Errorf("reset = %v, want CodeInvalidArgument", err)
	}
	if err := m.DeleteBranch(ctx, bad); !isCode(err, worktree.CodeInvalidArgument) {
		t.Errorf("delete branch = %v, want CodeInvalidArgument", err)
	}
	if err := m.ForceDeleteBranch(ctx, bad); !isCode(err, worktree.CodeInvalidArgument) {
		t.Errorf("force delete branch = %v, want CodeInvalidArgument", err)
	}
}

// TestGitFailureCarriesStdoutWhenStderrIsSilent keeps a git that reports on
// stdout from producing an empty diagnosis.
//
// DHF-TEST: keel/requirement-113 (keel/ac-399)
func TestGitFailureCarriesStdoutWhenStderrIsSilent(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "git-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'diagnosis on stdout'\nexit 4\n"), 0o755); err != nil {
		t.Fatalf("write git stand-in: %v", err)
	}
	m := newManager(t, worktree.Config{RepoRoot: newRepo(t), Base: "main", GitBin: stub})

	_, err := m.State(context.Background(), "unit-1")
	if !isCode(err, worktree.CodeGit) {
		t.Fatalf("state = %v, want CodeGit", err)
	}
	if !strings.Contains(err.Error(), "diagnosis on stdout") {
		t.Errorf("error %q drops the only diagnosis git gave", err)
	}
}

// TestStateOfANameNeverBroughtUpIsQuiet keeps the read-only report usable as a
// probe: nothing there is a fact, not a failure.
//
// DHF-TEST: keel/requirement-113 (keel/ac-407)
func TestStateOfANameNeverBroughtUpIsQuiet(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	state, err := m.State(context.Background(), "never-created")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.Exists || state.Registered || !state.Stale.Empty() {
		t.Errorf("state = %+v, want an empty report for a name never brought up", state)
	}
	if state.Base != "" || state.Ahead != 0 {
		t.Errorf("state = %+v, want no counts without a branch to count", state)
	}
}
