package worktree_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

func newManager(t *testing.T, cfg worktree.Config) *worktree.Manager {
	t.Helper()
	if cfg.Env == nil {
		cfg.Env = gitEnv
	}
	m, err := worktree.New(cfg)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return m
}

// TestUpCreatesAttachesAndReuses walks bring-up's three success outcomes: a
// fresh branch off the base ref, a re-attach to an existing branch whose
// checkout is gone, and an in-place reuse of a live checkout.
//
// DHF-TEST: keel/requirement-113 (keel/ac-400)
func TestUpCreatesAttachesAndReuses(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()

	created, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("first up: %v", err)
	}
	if created.Outcome != worktree.OutcomeCreated {
		t.Errorf("first up outcome = %q, want %q", created.Outcome, worktree.OutcomeCreated)
	}
	if created.Branch != "unit-1" {
		t.Errorf("branch = %q, want %q", created.Branch, "unit-1")
	}
	if _, err := os.Stat(filepath.Join(created.Path, "README.md")); err != nil {
		t.Errorf("worktree not populated: %v", err)
	}

	reused, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("second up: %v", err)
	}
	if reused.Outcome != worktree.OutcomeReused {
		t.Errorf("second up outcome = %q, want %q", reused.Outcome, worktree.OutcomeReused)
	}

	// Drop the checkout but keep the branch: the next bring-up re-attaches.
	git(t, root, "worktree", "remove", created.Path)
	attached, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("third up: %v", err)
	}
	if attached.Outcome != worktree.OutcomeAttached {
		t.Errorf("third up outcome = %q, want %q", attached.Outcome, worktree.OutcomeAttached)
	}
}

// TestUpRefusesUnownedPath covers bring-up's three path-side refusals: the path
// exists but is not a directory, is not a worktree registered with this
// repository, or is registered on another branch. Each names its condition and
// leaves the existing contents untouched.
//
// DHF-TEST: keel/requirement-113 (keel/ac-400)
func TestUpRefusesUnownedPath(t *testing.T) {
	ctx := context.Background()

	t.Run("not a directory", func(t *testing.T) {
		root := newRepo(t)
		m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
		path, _, err := m.Resolve("unit-1")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		writeFile(t, path, "not a directory\n")

		if _, err := m.Up(ctx, "unit-1"); !isCode(err, worktree.CodeConflict) {
			t.Fatalf("up err = %v, want CodeConflict", err)
		} else if !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("err %q does not name the condition", err)
		}
		if body, readErr := os.ReadFile(path); readErr != nil || string(body) != "not a directory\n" {
			t.Errorf("existing file modified: %q, %v", body, readErr)
		}
	})

	t.Run("not registered with this repository", func(t *testing.T) {
		root := newRepo(t)
		m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
		path, _, err := m.Resolve("unit-1")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		writeFile(t, filepath.Join(path, "stray.txt"), "stray\n")

		if _, err := m.Up(ctx, "unit-1"); !isCode(err, worktree.CodeConflict) {
			t.Fatalf("up err = %v, want CodeConflict", err)
		} else if !strings.Contains(err.Error(), "not a worktree registered with") {
			t.Errorf("err %q does not name the condition", err)
		}
		if _, statErr := os.Stat(filepath.Join(path, "stray.txt")); statErr != nil {
			t.Errorf("existing contents disturbed: %v", statErr)
		}
	})

	t.Run("registered on another branch", func(t *testing.T) {
		root := newRepo(t)
		m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
		path, _, err := m.Resolve("unit-1")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		git(t, root, "worktree", "add", "-b", "other", path, "main")

		if _, err := m.Up(ctx, "unit-1"); !isCode(err, worktree.CodeConflict) {
			t.Fatalf("up err = %v, want CodeConflict", err)
		} else if !strings.Contains(err.Error(), `registered on branch "other"`) {
			t.Errorf("err %q does not name the occupying branch", err)
		}
	})
}

// TestUpRefusesBranchCheckedOutElsewhere is the double-root guard: the target
// path is fresh, but the requested branch is already rooted in another
// registered worktree. The refusal names that checkout and changes nothing.
//
// DHF-TEST: keel/requirement-113 (keel/ac-406)
func TestUpRefusesBranchCheckedOutElsewhere(t *testing.T) {
	root := newRepo(t)
	elsewhere := filepath.Join(root, "elsewhere")
	git(t, root, "worktree", "add", "-b", "unit-1", elsewhere, "main")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	path, _, err := m.Resolve("unit-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	before := git(t, root, "worktree", "list", "--porcelain")

	_, err = m.Up(context.Background(), "unit-1")
	if !isCode(err, worktree.CodeConflict) {
		t.Fatalf("up err = %v, want CodeConflict", err)
	}
	if !strings.Contains(err.Error(), elsewhere) {
		t.Errorf("err %q does not name the existing checkout %q", err, elsewhere)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("target path was created despite the refusal: %v", statErr)
	}
	if after := git(t, root, "worktree", "list", "--porcelain"); after != before {
		t.Errorf("worktree registrations changed:\nbefore %s\nafter  %s", before, after)
	}
}

// TestResolveRejectsUnsafeNames keeps every unsafe work-item name in front of
// the first git side effect.
//
// DHF-TEST: keel/requirement-113 (keel/ac-400)
func TestResolveRejectsUnsafeNames(t *testing.T) {
	m := newManager(t, worktree.Config{RepoRoot: newRepo(t), Base: "main"})
	for _, name := range []string{"", " ", "..", "a/b", `a\b`, "-flag", "unit 1", ".hidden", "trailing.", "x.lock", "HEAD", " padded"} {
		if _, _, err := m.Resolve(name); !isCode(err, worktree.CodeInvalidArgument) {
			t.Errorf("Resolve(%q) err = %v, want CodeInvalidArgument", name, err)
		}
	}
	path, branch, err := m.Resolve("cr-148")
	if err != nil {
		t.Fatalf("Resolve(cr-148): %v", err)
	}
	if branch != "cr-148" || filepath.Base(path) != "cr-148" {
		t.Errorf("Resolve(cr-148) = %q, %q", path, branch)
	}
}

func isCode(err error, want worktree.ErrorCode) bool {
	var e *worktree.Error
	return errors.As(err, &e) && e.Code == want
}
