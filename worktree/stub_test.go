package worktree_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

// stubGit writes a git stand-in that succeeds only for the subcommands named in
// ok, so a test can put any individual git call into failure without staging a
// broken repository. Every path in this package treats a failed inspection as a
// finding rather than as silence, and this is how that is exercised.
func stubGit(t *testing.T, ok ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the git stand-in is a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "git-stub")
	script := `#!/bin/sh
case " $KEEL_STUB_OK " in
  *" $1 "*) ;;
  *) echo "stub refuses: $*" >&2; exit 3 ;;
esac
case "$1" in
  worktree) printf '%s' "$KEEL_STUB_LIST" ;;
  rev-parse) echo 0123456789abcdef0123456789abcdef01234567 ;;
  rev-list) printf '%s\n' "$KEEL_STUB_REV_OUT" ;;
esac
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write git stand-in: %v", err)
	}
	t.Setenv("KEEL_STUB_OK", strings.Join(ok, " "))
	return path
}

func stubEnv(extra ...string) []string {
	return append(append([]string(nil), gitEnv...), extra...)
}

// TestGitFailuresPropagateAsTypedErrors: a git that refuses everything must not
// leave any operation reporting success.
//
// DHF-TEST: keel/requirement-113 (keel/ac-399)
func TestGitFailuresPropagateAsTypedErrors(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main", GitBin: stubGit(t), Env: stubEnv("KEEL_STUB_OK=")})
	ctx := context.Background()

	if _, err := m.Up(ctx, "unit-1"); !isCode(err, worktree.CodeGit) {
		t.Errorf("up = %v, want CodeGit", err)
	}
	if _, err := m.Down(ctx, "unit-1", worktree.DownOptions{}); !isCode(err, worktree.CodeGit) {
		t.Errorf("down = %v, want CodeGit", err)
	}
	if _, err := m.State(ctx, "unit-1"); !isCode(err, worktree.CodeGit) {
		t.Errorf("state = %v, want CodeGit", err)
	}
	if _, err := m.Compare(ctx, "unit-1"); !isCode(err, worktree.CodeGit) {
		t.Errorf("compare = %v, want CodeGit", err)
	}
	if _, err := m.ResetFresh(ctx, "unit-1"); !isCode(err, worktree.CodeGit) {
		t.Errorf("reset = %v, want CodeGit", err)
	}

	missing := newManager(t, worktree.Config{RepoRoot: root, Base: "main", GitBin: filepath.Join(t.TempDir(), "absent-git")})
	if _, err := missing.Up(ctx, "unit-1"); !isCode(err, worktree.CodeGit) {
		t.Errorf("up with a git binary that cannot start = %v, want CodeGit", err)
	}
}

// TestInspectionFailuresBecomeBlockers: an uninspectable checkout is treated as
// holding work, never as clean, and the failure is named.
//
// DHF-TEST: keel/requirement-113 (keel/ac-401)
func TestInspectionFailuresBecomeBlockers(t *testing.T) {
	root := newRepo(t)
	path := filepath.Join(root, "worktrees", "unit-1")
	writeFile(t, filepath.Join(path, "keep.txt"), "keep\n")
	list := "worktree " + path + "\ndetached\n\n"

	m := newManager(t, worktree.Config{
		RepoRoot: root,
		Base:     "main",
		GitBin:   stubGit(t, "worktree"),
		Env:      stubEnv("KEEL_STUB_OK=worktree", "KEEL_STUB_LIST="+list),
	})

	_, err := m.Down(context.Background(), "unit-1", worktree.DownOptions{})
	report := blockedReport(t, err)
	failures := report.OfKind(worktree.BlockerInspectionFailed)
	if len(failures) < 2 {
		t.Fatalf("inspection failures = %+v, want the status and commit checks to both report", failures)
	}
	if report.HoldsWork() {
		t.Error("an inspection failure was miscounted as work held")
	}
	// Forcing does not dismiss a check that never ran.
	if _, err := m.Down(context.Background(), "unit-1", worktree.DownOptions{Force: true}); !isCode(err, worktree.CodeBlocked) {
		t.Errorf("forced down over an uninspectable checkout = %v, want CodeBlocked", err)
	}
}

// TestCompareFailsClosed: every check the comparison could not evaluate becomes
// its own reason, and a checkout on an unexpected branch is called out rather
// than silently compared.
//
// DHF-TEST: keel/requirement-113 (keel/ac-404)
func TestCompareFailsClosed(t *testing.T) {
	root := newRepo(t)
	path := filepath.Join(root, "worktrees", "unit-1")
	writeFile(t, filepath.Join(path, "keep.txt"), "keep\n")
	list := "worktree " + path + "\nbranch refs/heads/other\n\n"
	ctx := context.Background()

	t.Run("base unresolvable and status unreadable", func(t *testing.T) {
		m := newManager(t, worktree.Config{
			RepoRoot: root, Base: "main", GitBin: stubGit(t, "worktree"),
			Env: stubEnv("KEEL_STUB_OK=worktree", "KEEL_STUB_LIST="+list),
		})
		comparison, err := m.Compare(ctx, "unit-1")
		if err != nil {
			t.Fatalf("compare: %v", err)
		}
		for _, want := range []worktree.ReasonKind{worktree.ReasonBaseUnresolvable, worktree.ReasonInspectionFailed} {
			if !comparison.Has(want) {
				t.Errorf("reasons %v lack %q", reasonKinds(comparison), want)
			}
		}
	})

	t.Run("counts unreadable", func(t *testing.T) {
		m := newManager(t, worktree.Config{
			RepoRoot: root, Base: "main", GitBin: stubGit(t, "worktree", "rev-parse", "rev-list"),
			Env: stubEnv("KEEL_STUB_OK=worktree rev-parse rev-list", "KEEL_STUB_LIST="+list, "KEEL_STUB_REV_OUT=not-a-count"),
		})
		comparison, err := m.Compare(ctx, "unit-1")
		if err != nil {
			t.Fatalf("compare: %v", err)
		}
		if !comparison.Has(worktree.ReasonInspectionFailed) {
			t.Errorf("reasons %v lack an inspection failure for the unreadable counts", reasonKinds(comparison))
		}
	})

	t.Run("unregistered path", func(t *testing.T) {
		m := newManager(t, worktree.Config{
			RepoRoot: root, Base: "main", GitBin: stubGit(t, "worktree"),
			Env: stubEnv("KEEL_STUB_OK=worktree", "KEEL_STUB_LIST="),
		})
		if _, err := m.Compare(ctx, "unit-1"); !isCode(err, worktree.CodeConflict) {
			t.Errorf("compare of an unregistered path = %v, want CodeConflict", err)
		}
	})
}

// TestResetFreshSurfacesGitFailures keeps a half-run reset from reading as a
// clean one.
//
// DHF-TEST: keel/requirement-113 (keel/ac-400)
func TestResetFreshSurfacesGitFailures(t *testing.T) {
	root := newRepo(t)
	path := filepath.Join(root, "worktrees", "unit-1")
	writeFile(t, filepath.Join(path, "keep.txt"), "keep\n")
	list := "worktree " + path + "\nbranch refs/heads/unit-1\n\n"
	ctx := context.Background()

	unreadableCounts := newManager(t, worktree.Config{
		RepoRoot: root, Base: "main", GitBin: stubGit(t, "worktree", "rev-parse", "rev-list"),
		Env: stubEnv("KEEL_STUB_OK=worktree rev-parse rev-list", "KEEL_STUB_LIST="+list, "KEEL_STUB_REV_OUT=not-a-count"),
	})
	if _, err := unreadableCounts.ResetFresh(ctx, "unit-1"); !isCode(err, worktree.CodeGit) {
		t.Errorf("reset with unreadable counts = %v, want CodeGit", err)
	}

	resetFails := newManager(t, worktree.Config{
		RepoRoot: root, Base: "main", GitBin: stubGit(t, "worktree", "rev-parse", "rev-list"),
		Env: stubEnv("KEEL_STUB_OK=worktree rev-parse rev-list", "KEEL_STUB_LIST="+list, "KEEL_STUB_REV_OUT=0"),
	})
	if _, err := resetFails.ResetFresh(ctx, "unit-1"); !isCode(err, worktree.CodeGit) {
		t.Errorf("reset whose git reset fails = %v, want CodeGit", err)
	}

	branchMismatch := newManager(t, worktree.Config{
		RepoRoot: root, Base: "main", GitBin: stubGit(t, "worktree"),
		Env: stubEnv("KEEL_STUB_OK=worktree", "KEEL_STUB_LIST=worktree "+path+"\nbranch refs/heads/other\n\n"),
	})
	if _, err := branchMismatch.ResetFresh(ctx, "unit-1"); !isCode(err, worktree.CodeConflict) {
		t.Errorf("reset of a checkout on another branch = %v, want CodeConflict", err)
	}
}

// TestDetachedCheckoutReportsOrphanCommits: commits reachable from no ref at all
// are exactly the ones removal would destroy, so they block by commit id.
//
// DHF-TEST: keel/requirement-113 (keel/ac-401)
func TestDetachedCheckoutReportsOrphanCommits(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "orphan.txt"), "orphan\n")
	git(t, wt.Path, "add", "orphan.txt")
	git(t, wt.Path, "commit", "-m", "orphan")
	head := strings.TrimSpace(git(t, wt.Path, "rev-parse", "HEAD"))
	// Detach, then move the branch back: the commit is now reachable from the
	// checkout and from nothing else.
	git(t, wt.Path, "checkout", "--detach")
	git(t, root, "branch", "-f", "unit-1", "main")

	_, err = m.Down(ctx, "unit-1", worktree.DownOptions{})
	report := blockedReport(t, err)
	unpushed := report.OfKind(worktree.BlockerUnpushedCommit)
	if len(unpushed) != 1 || unpushed[0].Commit != head {
		t.Fatalf("unpushed blockers = %+v, want the orphaned commit %s", unpushed, head)
	}

	state, err := m.State(ctx, "unit-1")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if !state.Detached {
		t.Error("state does not report the checkout as detached")
	}

	comparison, err := m.Compare(ctx, "unit-1")
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if !comparison.Has(worktree.ReasonOnBaseBranch) {
		t.Errorf("reasons %v lack the nothing-to-compare finding for a detached checkout", reasonKinds(comparison))
	}
}
