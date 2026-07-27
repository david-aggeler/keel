package worktree_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

// TestDeleteBranchIsSafeAndSeparateFromTearDown pins the two halves of the
// branch-removal contract: it is never a side effect of tear-down, and it
// carries git's own safe-delete semantics — an unmerged branch survives unless
// the force call is used deliberately.
//
// DHF-TEST: keel/requirement-113 (keel/ac-402)
func TestDeleteBranchIsSafeAndSeparateFromTearDown(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "work.txt"), "unmerged work\n")
	git(t, wt.Path, "add", "work.txt")
	git(t, wt.Path, "commit", "-m", "unmerged")

	if _, err := m.Down(ctx, "unit-1", worktree.DownOptions{}); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, err := gitTry(root, "show-ref", "--verify", "--quiet", "refs/heads/unit-1"); err != nil {
		t.Fatal("tear-down deleted the branch")
	}

	// Safe delete refuses work that exists only here.
	if err := m.DeleteBranch(ctx, "unit-1"); err == nil {
		t.Fatal("safe delete accepted an unmerged branch")
	}
	if _, err := gitTry(root, "show-ref", "--verify", "--quiet", "refs/heads/unit-1"); err != nil {
		t.Fatal("refused safe delete destroyed the branch anyway")
	}

	if err := m.ForceDeleteBranch(ctx, "unit-1"); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	if _, err := gitTry(root, "show-ref", "--verify", "--quiet", "refs/heads/unit-1"); err == nil {
		t.Error("force delete left the branch behind")
	}

	// A branch that is not there names its own condition.
	if err := m.DeleteBranch(ctx, "unit-1"); !isCode(err, worktree.CodeBranchMissing) {
		t.Errorf("delete of an absent branch = %v, want CodeBranchMissing", err)
	}
}

// TestDeleteBranchAcceptsMergedWork is the ordinary close: the work landed on
// the base, so the safe delete goes through untouched.
//
// DHF-TEST: keel/requirement-113 (keel/ac-402)
func TestDeleteBranchAcceptsMergedWork(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "work.txt"), "work\n")
	git(t, wt.Path, "add", "work.txt")
	git(t, wt.Path, "commit", "-m", "work")
	git(t, root, "merge", "--no-ff", "-m", "land unit-1", "unit-1")

	if _, err := m.Down(ctx, "unit-1", worktree.DownOptions{}); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := m.DeleteBranch(ctx, "unit-1"); err != nil {
		t.Fatalf("safe delete of merged work: %v", err)
	}
}

// TestResetFreshReturnsTheCheckoutToBase covers the fresh-pickup path and its
// refusal to discard commits the branch already carries.
//
// DHF-TEST: keel/requirement-113 (keel/ac-400)
func TestResetFreshReturnsTheCheckoutToBase(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "README.md"), "edited\n")
	writeFile(t, filepath.Join(wt.Path, "scratch.txt"), "scratch\n")

	if _, err := m.ResetFresh(ctx, "unit-1"); err != nil {
		t.Fatalf("reset fresh: %v", err)
	}
	if out := strings.TrimSpace(git(t, wt.Path, "status", "--porcelain")); out != "" {
		t.Errorf("checkout still dirty after reset: %q", out)
	}

	// Committed work is not something a reset may silently discard.
	writeFile(t, filepath.Join(wt.Path, "work.txt"), "work\n")
	git(t, wt.Path, "add", "work.txt")
	git(t, wt.Path, "commit", "-m", "work")
	if _, err := m.ResetFresh(ctx, "unit-1"); !isCode(err, worktree.CodeBlocked) {
		t.Errorf("reset over committed work = %v, want CodeBlocked", err)
	}

	// An unregistered path is a conflict, not a silent creation.
	if _, err := m.ResetFresh(ctx, "unit-2"); !isCode(err, worktree.CodeConflict) {
		t.Errorf("reset of an unregistered path = %v, want CodeConflict", err)
	}
}

// TestBaseResolutionFallsBackAndRefuses walks the fallback chain rather than
// hardcoding a default branch, and fails typed when nothing resolves.
//
// DHF-TEST: keel/requirement-113 (keel/ac-400)
func TestBaseResolutionFallsBackAndRefuses(t *testing.T) {
	ctx := context.Background()

	t.Run("falls back to an existing default", func(t *testing.T) {
		root := newRepo(t)
		m := newManager(t, worktree.Config{RepoRoot: root})
		wt, err := m.Up(ctx, "unit-1")
		if err != nil {
			t.Fatalf("up: %v", err)
		}
		if wt.Base != "main" {
			t.Errorf("base = %q, want main from the fallback chain", wt.Base)
		}
	})

	t.Run("refuses when nothing resolves", func(t *testing.T) {
		root := newRepo(t)
		git(t, root, "branch", "-m", "main", "custom")
		m := newManager(t, worktree.Config{RepoRoot: root})
		if _, err := m.Up(ctx, "unit-1"); !isCode(err, worktree.CodeBranchMissing) {
			t.Errorf("up without a resolvable base = %v, want CodeBranchMissing", err)
		}
	})
}

// TestDefaultBaseResolutionUsesLocalDefaultBranch pins the owner's 2026-07-27
// default-base decision: a remote default may name the branch, but the commit
// comes from the local branch.
//
// DHF-TEST: keel/requirement-113 (keel/ac-417)
func TestDefaultBaseResolutionUsesLocalDefaultBranch(t *testing.T) {
	root, _ := newRepoWithRemote(t)
	git(t, root, "remote", "set-head", "origin", "main")
	remoteHead := strings.TrimSpace(git(t, root, "rev-parse", "origin/HEAD"))
	writeFile(t, filepath.Join(root, "local.txt"), "local\n")
	git(t, root, "add", "local.txt")
	git(t, root, "commit", "-m", "local only")
	localHead := strings.TrimSpace(git(t, root, "rev-parse", "main"))
	if localHead == remoteHead {
		t.Fatal("fixture did not leave local main ahead of origin/HEAD")
	}

	m := newManager(t, worktree.Config{RepoRoot: root})
	wt, err := m.Up(context.Background(), "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	branchHead := strings.TrimSpace(git(t, root, "rev-parse", wt.Branch))
	if wt.Base != "main" {
		t.Errorf("base = %q, want local default branch main", wt.Base)
	}
	if branchHead != localHead {
		t.Errorf("branch head = %s, want local main %s, not remote %s", branchHead, localHead, remoteHead)
	}
}

// TestNewValidatesConfig keeps option-injection and a bad root in front of the
// first git side effect, and resolves the repository from the working directory
// when the caller names none.
//
// DHF-TEST: keel/requirement-113 (keel/ac-399)
func TestNewValidatesConfig(t *testing.T) {
	root := newRepo(t)

	for _, cfg := range []worktree.Config{
		{RepoRoot: root, Base: "--upload-pack=touch"},
		{RepoRoot: root, BranchPrefix: "-x"},
	} {
		if _, err := worktree.New(cfg); !isCode(err, worktree.CodeInvalidArgument) {
			t.Errorf("New(%+v) err = %v, want CodeInvalidArgument", cfg, err)
		}
	}

	t.Chdir(root)
	m, err := worktree.New(worktree.Config{Env: gitEnv})
	if err != nil {
		t.Fatalf("New from inside a repository: %v", err)
	}
	if m.RepoRoot() != root {
		t.Errorf("RepoRoot() = %q, want %q", m.RepoRoot(), root)
	}

	t.Chdir(t.TempDir())
	if _, err := worktree.New(worktree.Config{Env: gitEnv}); !isCode(err, worktree.CodeNotInRepository) {
		t.Errorf("New outside a repository = %v, want CodeNotInRepository", err)
	}
}

// TestWorktreesDirDefaults keeps sibling checkouts under one shared root when
// the manager is driven from inside a linked worktree.
//
// DHF-TEST: keel/requirement-113 (keel/ac-400)
func TestWorktreesDirDefaults(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	first, err := m.Up(context.Background(), "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}

	sibling := newManager(t, worktree.Config{RepoRoot: first.Path, Base: "main"})
	path, _, err := sibling.Resolve("unit-2")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if want := filepath.Join(root, "worktrees", "unit-2"); path != want {
		t.Errorf("sibling path = %q, want %q — worktrees must not nest", path, want)
	}
}

// TestErrorSurface pins the shape a command wrapper maps onto an exit status.
//
// DHF-TEST: keel/requirement-113 (keel/ac-399)
func TestErrorSurface(t *testing.T) {
	m := newManager(t, worktree.Config{RepoRoot: newRepo(t), Base: "main"})
	_, _, err := m.Resolve("bad name")
	if err == nil {
		t.Fatal("Resolve accepted a name with a space")
	}
	if !strings.HasPrefix(err.Error(), "keel/worktree: ") {
		t.Errorf("error %q does not carry the package-path prefix", err)
	}
	if got := worktree.CodeOf(err); got != worktree.CodeInvalidArgument {
		t.Errorf("CodeOf = %v, want CodeInvalidArgument", got)
	}
	if got := worktree.CodeOf(os.ErrNotExist); got != 0 {
		t.Errorf("CodeOf(foreign) = %v, want 0", got)
	}
	if got := worktree.CodeOf(nil); got != 0 {
		t.Errorf("CodeOf(nil) = %v, want 0", got)
	}
	var typed *worktree.Error
	if !errorAs(err, &typed) || typed.ExitCode() != 64 {
		t.Errorf("ExitCode = %v, want 64", err)
	}
	for code, want := range map[worktree.ErrorCode]string{
		worktree.CodeGit:             "git",
		worktree.CodeNotInRepository: "not_in_repository",
		worktree.CodeInvalidArgument: "invalid_argument",
		worktree.CodeConflict:        "conflict",
		worktree.CodeBlocked:         "blocked",
		worktree.CodeBranchMissing:   "branch_missing",
		worktree.ErrorCode(99):       "unknown",
	} {
		if got := code.String(); got != want {
			t.Errorf("ErrorCode(%d).String() = %q, want %q", int(code), got, want)
		}
	}
}
