package worktree_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

func reasonKinds(c worktree.Comparison) []worktree.ReasonKind {
	var out []worktree.ReasonKind
	for _, r := range c.Reasons {
		out = append(out, r.Kind)
	}
	return out
}

// TestCompareAccumulatesEveryReasonAndRendersNoVerdict stages three problems at
// once — the checkout is on the base branch, the base ref does not resolve
// locally, and the working tree is dirty — and requires one distinct reason for
// each. The unresolvable base is never collapsed into "no commits ahead", and
// nothing in the returned value says ready or not-ready: that conjunction
// belongs to the caller, together with whatever clauses of its own it adds.
//
// DHF-TEST: keel/requirement-113 (keel/ac-404)
func TestCompareAccumulatesEveryReasonAndRendersNoVerdict(t *testing.T) {
	root := newRepo(t)
	// Park the primary checkout elsewhere so the base-named branch is free.
	git(t, root, "switch", "-c", "scratch")
	git(t, root, "branch", "unit-1", "scratch")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "origin/unit-1"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "README.md"), "edited\n")

	comparison, err := m.Compare(ctx, "unit-1")
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	want := []worktree.ReasonKind{
		worktree.ReasonOnBaseBranch,
		worktree.ReasonBaseUnresolvable,
		worktree.ReasonWorkingTreeDirty,
	}
	if got := reasonKinds(comparison); !reflect.DeepEqual(got, want) {
		t.Errorf("reasons = %v, want %v", got, want)
	}
	if comparison.Has(worktree.ReasonNoCommitsAhead) {
		t.Error("an unresolvable base was collapsed into a no-commits-ahead reason")
	}

	// No verdict: the value must expose no boolean readiness field at all.
	typ := reflect.TypeOf(comparison)
	for i := range typ.NumField() {
		if field := typ.Field(i); field.Type.Kind() == reflect.Bool {
			t.Errorf("Comparison exposes boolean field %q — the readiness verdict is the caller's", field.Name)
		}
	}
}

// TestCompareReportsNoCommitsAheadSeparately keeps the resolvable-base,
// nothing-committed case distinguishable from an unresolvable base, and reports
// a clean feature branch with commits as having no reasons at all.
//
// DHF-TEST: keel/requirement-113 (keel/ac-404)
func TestCompareReportsNoCommitsAheadSeparately(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}

	comparison, err := m.Compare(ctx, "unit-1")
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if got := reasonKinds(comparison); !reflect.DeepEqual(got, []worktree.ReasonKind{worktree.ReasonNoCommitsAhead}) {
		t.Fatalf("reasons = %v, want exactly a no-commits-ahead reason", got)
	}
	if comparison.Has(worktree.ReasonBaseUnresolvable) {
		t.Error("a resolvable base was reported unresolvable")
	}

	writeFile(t, filepath.Join(wt.Path, "work.txt"), "work\n")
	git(t, wt.Path, "add", "work.txt")
	git(t, wt.Path, "commit", "-m", "work")

	comparison, err = m.Compare(ctx, "unit-1")
	if err != nil {
		t.Fatalf("second compare: %v", err)
	}
	if len(comparison.Reasons) != 0 {
		t.Errorf("reasons = %+v, want none", comparison.Reasons)
	}
	if comparison.Ahead != 1 || comparison.Behind != 0 {
		t.Errorf("ahead/behind = %d/%d, want 1/0", comparison.Ahead, comparison.Behind)
	}
}

// TestStateReportsRegistrationHealthAndCounts is the read-only state view a
// caller consults before deciding anything.
//
// DHF-TEST: keel/requirement-113 (keel/ac-407)
func TestStateReportsRegistrationHealthAndCounts(t *testing.T) {
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
	writeFile(t, filepath.Join(wt.Path, "scratch.txt"), "scratch\n")

	state, err := m.State(ctx, "unit-1")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if !state.Registered || state.Branch != "unit-1" || state.Path != wt.Path {
		t.Errorf("state = %+v, want a registered unit-1 at %s", state, wt.Path)
	}
	if state.Ahead != 1 {
		t.Errorf("ahead = %d, want 1", state.Ahead)
	}
	if got := blockerPaths(state.Stale, worktree.BlockerUntrackedFile); len(got) != 1 || got[0] != "scratch.txt" {
		t.Errorf("untracked blockers = %v, want [scratch.txt]", got)
	}

	// A path that was never brought up is reported, not an error.
	absent, err := m.State(ctx, "unit-2")
	if err != nil {
		t.Fatalf("state of an absent worktree: %v", err)
	}
	if absent.Registered || absent.Exists {
		t.Errorf("absent state = %+v, want neither registered nor present", absent)
	}
}

// TestReportsIssueNoMutatingGitCommand is the verifier-safe property: a role
// forbidden from remediation must be able to call the reports on a troubled
// repository — a dirty checkout beside a prunable sibling registration — any
// number of times without becoming a writer. In particular the stale
// registration is reported with its remediation, never silently pruned.
//
// DHF-TEST: keel/requirement-113 (keel/ac-407)
func TestReportsIssueNoMutatingGitCommand(t *testing.T) {
	root := newRepo(t)
	rec := &commandRecorder{}
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main", Logger: rec})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	sibling, err := m.Up(ctx, "unit-2")
	if err != nil {
		t.Fatalf("up sibling: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "dirt.txt"), "dirt\n")
	// Delete the sibling's directory behind git's back: its registration is now
	// prunable, which is exactly the state a reporting role must not repair.
	if err := removeAll(sibling.Path); err != nil {
		t.Fatalf("drop sibling directory: %v", err)
	}

	before := repoFingerprint(t, root)
	rec.reset()
	for range 2 {
		if _, err := m.State(ctx, "unit-1"); err != nil {
			t.Fatalf("state: %v", err)
		}
		if _, err := m.Compare(ctx, "unit-1"); err != nil {
			t.Fatalf("compare: %v", err)
		}
		if _, err := m.State(ctx, "unit-2"); err != nil {
			t.Fatalf("sibling state: %v", err)
		}
	}
	rec.assertReadOnly(t)
	for _, cmd := range rec.recorded() {
		if strings.Contains(cmd, "worktree prune") {
			t.Errorf("a report pruned a stale registration: %q", cmd)
		}
	}
	if after := repoFingerprint(t, root); after != before {
		t.Errorf("reports changed the repository:\nbefore %s\nafter  %s", before, after)
	}

	siblingState, err := m.State(ctx, "unit-2")
	if err != nil {
		t.Fatalf("sibling state: %v", err)
	}
	if !siblingState.Stale.Has(worktree.BlockerStaleRegistration) {
		t.Errorf("sibling blockers = %+v, want a stale-registration blocker", siblingState.Stale.Blockers)
	}
}

// repoFingerprint captures everything a mutating git command would change:
// registrations, refs, the index, and the working tree.
func repoFingerprint(t *testing.T, root string) string {
	t.Helper()
	return strings.Join([]string{
		git(t, root, "worktree", "list", "--porcelain"),
		git(t, root, "for-each-ref", "--format=%(refname) %(objectname)"),
		git(t, root, "status", "--porcelain"),
	}, "\n")
}
