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

func blockedReport(t *testing.T, err error) worktree.StaleReport {
	t.Helper()
	var e *worktree.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *worktree.Error", err)
	}
	if e.Code != worktree.CodeBlocked {
		t.Fatalf("err code = %v, want CodeBlocked (err %v)", e.Code, err)
	}
	if e.Report == nil {
		t.Fatalf("blocked err carries no report: %v", err)
	}
	return *e.Report
}

func blockerPaths(report worktree.StaleReport, kind worktree.BlockerKind) []string {
	var out []string
	for _, b := range report.OfKind(kind) {
		out = append(out, b.Path)
	}
	return out
}

// TestDownRefusesUncommittedAndUntrackedWork names each offending path rather
// than reporting a bare "dirty", and leaves the checkout in place. Ignored
// files and caller-excluded bookkeeping never count.
//
// DHF-TEST: keel/requirement-113 (keel/ac-401)
func TestDownRefusesUncommittedAndUntrackedWork(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "build/\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore build output")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main", ExcludePaths: []string{".stamp"}})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}

	writeFile(t, filepath.Join(wt.Path, "README.md"), "edited\n")      // tracked, modified
	writeFile(t, filepath.Join(wt.Path, "scratch.txt"), "scratch\n")   // untracked
	writeFile(t, filepath.Join(wt.Path, "build", "out.o"), "junk\n")   // ignored
	writeFile(t, filepath.Join(wt.Path, ".stamp"), "bookkeeping\n")    // caller-excluded
	writeFile(t, filepath.Join(wt.Path, "nested", "new.txt"), "new\n") // untracked, nested

	_, err = m.Down(ctx, "unit-1", worktree.DownOptions{})
	report := blockedReport(t, err)

	if got := blockerPaths(report, worktree.BlockerUncommittedChange); len(got) != 1 || got[0] != "README.md" {
		t.Errorf("uncommitted blockers = %v, want [README.md]", got)
	}
	untracked := blockerPaths(report, worktree.BlockerUntrackedFile)
	if len(untracked) != 2 {
		t.Fatalf("untracked blockers = %v, want two entries", untracked)
	}
	joined := strings.Join(untracked, " ")
	for _, want := range []string{"scratch.txt", "nested/"} {
		if !strings.Contains(joined, want) {
			t.Errorf("untracked blockers %v do not name %q", untracked, want)
		}
	}
	for _, unwanted := range []string{"build/out.o", ".stamp"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("untracked blockers %v wrongly include %q", untracked, unwanted)
		}
	}
	if !report.HoldsWork() {
		t.Error("report does not classify the checkout as holding work")
	}
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Errorf("refused tear-down removed the checkout anyway: %v", statErr)
	}

	// The same tear-down with a deliberate force gets through.
	res, err := m.Down(ctx, "unit-1", worktree.DownOptions{Force: true})
	if err != nil {
		t.Fatalf("forced down: %v", err)
	}
	if res.Outcome != worktree.DownRemoved {
		t.Errorf("forced down outcome = %q, want %q", res.Outcome, worktree.DownRemoved)
	}
	if _, statErr := os.Stat(wt.Path); !os.IsNotExist(statErr) {
		t.Errorf("forced down left the checkout behind: %v", statErr)
	}
}

// TestDownRefusesUnpushedCommits names the commit ids that exist on no remote.
//
// DHF-TEST: keel/requirement-113 (keel/ac-401)
func TestDownRefusesUnpushedCommits(t *testing.T) {
	root, _ := newRepoWithRemote(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "work.txt"), "work\n")
	git(t, wt.Path, "add", "work.txt")
	git(t, wt.Path, "commit", "-m", "local work")
	head := strings.TrimSpace(git(t, wt.Path, "rev-parse", "HEAD"))

	_, err = m.Down(ctx, "unit-1", worktree.DownOptions{})
	report := blockedReport(t, err)
	unpushed := report.OfKind(worktree.BlockerUnpushedCommit)
	if len(unpushed) != 1 || unpushed[0].Commit != head {
		t.Fatalf("unpushed blockers = %+v, want the single commit %s", unpushed, head)
	}

	// Pushing it clears the blocker: the work now exists somewhere else.
	git(t, wt.Path, "push", "origin", "unit-1")
	if _, err := m.Down(ctx, "unit-1", worktree.DownOptions{}); err != nil {
		t.Fatalf("down after push: %v", err)
	}
}

// TestDownPolicyKeepsBranchCommitsReachable proves callers can name the
// tear-down policy that treats branch-reachable commits as preserved by the
// surviving branch instead of reassembling blocker sets or forcing every held
// work gate.
//
// DHF-TEST: keel/requirement-114 (keel/ac-439)
func TestDownPolicyKeepsBranchCommitsReachable(t *testing.T) {
	root, _ := newRepoWithRemote(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	keepBranchCommits := worktree.DownOptions{Policy: worktree.DownPolicyKeepBranchCommits}

	for _, name := range []string{"unit-1", "unit-2"} {
		wt, err := m.Up(ctx, name)
		if err != nil {
			t.Fatalf("%s up: %v", name, err)
		}
		writeFile(t, filepath.Join(wt.Path, "work.txt"), name+"\n")
		git(t, wt.Path, "add", "work.txt")
		git(t, wt.Path, "commit", "-m", name+" local work")

		res, err := m.Down(ctx, name, keepBranchCommits)
		if err != nil {
			t.Fatalf("%s down with named policy: %v", name, err)
		}
		if res.Outcome != worktree.DownRemoved {
			t.Errorf("%s outcome = %q, want %q", name, res.Outcome, worktree.DownRemoved)
		}
		if _, statErr := os.Stat(wt.Path); !os.IsNotExist(statErr) {
			t.Errorf("%s checkout still present after named-policy down: %v", name, statErr)
		}
		if out, err := gitTry(root, "show-ref", "--verify", "--quiet", "refs/heads/"+name); err != nil {
			t.Errorf("%s branch was destroyed by tear-down: %v\n%s", name, err, out)
		}
	}

	wt, err := m.Up(ctx, "unit-3")
	if err != nil {
		t.Fatalf("unit-3 up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "README.md"), "dirty\n")
	_, err = m.Down(ctx, "unit-3", keepBranchCommits)
	report := blockedReport(t, err)
	if !report.Has(worktree.BlockerUncommittedChange) {
		t.Fatalf("named policy blockers = %+v, want uncommitted change to remain blocking", report.Blockers)
	}
}

// TestDownCleanNeverMergedSucceedsWithoutMergeQuery is the abandoned-branch
// case: nothing was ever merged, nothing ever will be, and the checkout is
// still reclaimable — with no merge-status or ahead/behind query anywhere in
// the removal decision, and the branch left intact.
//
// DHF-TEST: keel/requirement-113 (keel/ac-402)
func TestDownCleanNeverMergedSucceedsWithoutMergeQuery(t *testing.T) {
	root := newRepo(t)
	rec := &commandRecorder{}
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main", Logger: rec})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "work.txt"), "abandoned\n")
	git(t, wt.Path, "add", "work.txt")
	git(t, wt.Path, "commit", "-m", "work nobody will merge")

	rec.reset()
	res, err := m.Down(ctx, "unit-1", worktree.DownOptions{})
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	if res.Outcome != worktree.DownRemoved {
		t.Errorf("outcome = %q, want %q", res.Outcome, worktree.DownRemoved)
	}
	if _, statErr := os.Stat(wt.Path); !os.IsNotExist(statErr) {
		t.Errorf("checkout still present: %v", statErr)
	}
	rec.assertNoMergeQuery(t)

	// The branch outlives its checkout — removal is not a branch delete.
	if out, err := gitTry(root, "show-ref", "--verify", "--quiet", "refs/heads/unit-1"); err != nil {
		t.Errorf("branch unit-1 was destroyed by tear-down: %v\n%s", err, out)
	}
}

// TestDownAlreadyGoneIsADistinctNoop lets a desired-state caller re-invoke
// tear-down after a crash — or after a peer already tore the same worktree
// down — and converge instead of erroring, while still being able to tell the
// two outcomes apart. No mutating git command runs.
//
// DHF-TEST: keel/requirement-113 (keel/ac-405)
func TestDownAlreadyGoneIsADistinctNoop(t *testing.T) {
	root := newRepo(t)
	rec := &commandRecorder{}
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main", Logger: rec})
	ctx := context.Background()
	if _, err := m.Up(ctx, "unit-1"); err != nil {
		t.Fatalf("up: %v", err)
	}
	first, err := m.Down(ctx, "unit-1", worktree.DownOptions{})
	if err != nil {
		t.Fatalf("first down: %v", err)
	}
	if first.Outcome != worktree.DownRemoved {
		t.Fatalf("first outcome = %q, want %q", first.Outcome, worktree.DownRemoved)
	}

	rec.reset()
	second, err := m.Down(ctx, "unit-1", worktree.DownOptions{})
	if err != nil {
		t.Fatalf("second down: %v", err)
	}
	if second.Outcome != worktree.DownNoop {
		t.Errorf("second outcome = %q, want %q", second.Outcome, worktree.DownNoop)
	}
	if second.Path != first.Path {
		t.Errorf("no-op path = %q, want %q", second.Path, first.Path)
	}
	rec.assertReadOnly(t)

	// The branch is still there, so the no-op is about the checkout only.
	if out, err := gitTry(root, "show-ref", "--verify", "--quiet", "refs/heads/unit-1"); err != nil {
		t.Errorf("branch unit-1 missing after no-op: %v\n%s", err, out)
	}
}

// TestDownPrunesARegistrationWhoseDirectoryIsGone covers the inverse of the
// no-op: the checkout was deleted out of band but git still lists it. There is
// nothing left to destroy, so tear-down drops the stale registration itself and
// says so distinctly, rather than refusing over a directory that is not there.
// A second call then converges on the plain no-op.
//
// DHF-TEST: keel/requirement-113 (keel/ac-405)
func TestDownPrunesARegistrationWhoseDirectoryIsGone(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	// Delete the directory behind git's back; the registration survives.
	if err := removeAll(wt.Path); err != nil {
		t.Fatalf("remove checkout: %v", err)
	}

	res, err := m.Down(ctx, "unit-1", worktree.DownOptions{})
	if err != nil {
		t.Fatalf("down of a registered-but-absent checkout: %v", err)
	}
	if res.Outcome != worktree.DownPruned {
		t.Errorf("outcome = %q, want %q", res.Outcome, worktree.DownPruned)
	}

	state, err := m.State(ctx, "unit-1")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.Registered {
		t.Error("the stale registration outlived the tear-down")
	}
	if !state.Stale.Empty() {
		t.Errorf("state still reports blockers after the prune: %+v", state.Stale)
	}

	// The branch outlives the registration, and a re-run is the plain no-op.
	if out, err := gitTry(root, "show-ref", "--verify", "--quiet", "refs/heads/unit-1"); err != nil {
		t.Errorf("branch unit-1 was destroyed by the prune: %v\n%s", err, out)
	}
	second, err := m.Down(ctx, "unit-1", worktree.DownOptions{})
	if err != nil {
		t.Fatalf("second down: %v", err)
	}
	if second.Outcome != worktree.DownNoop {
		t.Errorf("second outcome = %q, want %q", second.Outcome, worktree.DownNoop)
	}
}

// TestDownReportsARegistrationAPruneCannotClear keeps the pruning branch honest:
// git refuses to prune a locked registration, so the absent directory stays a
// reported condition with the unlock remediation instead of a silent success.
//
// DHF-TEST: keel/requirement-113 (keel/ac-401)
func TestDownReportsARegistrationAPruneCannotClear(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	git(t, root, "worktree", "lock", wt.Path)
	if err := removeAll(wt.Path); err != nil {
		t.Fatalf("remove checkout: %v", err)
	}

	// A force covers held work; it does not unlock a registration for you.
	_, err = m.Down(ctx, "unit-1", worktree.DownOptions{Force: true})
	report := blockedReport(t, err)
	stale := report.OfKind(worktree.BlockerStaleRegistration)
	if len(stale) != 1 {
		t.Fatalf("stale-registration blockers = %+v, want one", stale)
	}
	if !strings.Contains(stale[0].Remediation, "worktree unlock") {
		t.Errorf("remediation %q does not name the unlock", stale[0].Remediation)
	}
	if report.HoldsWork() {
		t.Error("a surviving registration was classified as work held")
	}
}

// TestDownReportsLostRegistration catches the directory-present, registration-
// lost state and hands back the prune remediation rather than a raw git error.
//
// DHF-TEST: keel/requirement-113 (keel/ac-401)
func TestDownReportsLostRegistration(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".git", "worktrees", "unit-1")); err != nil {
		t.Fatalf("drop registration: %v", err)
	}

	_, err = m.Down(ctx, "unit-1", worktree.DownOptions{})
	report := blockedReport(t, err)
	stale := report.OfKind(worktree.BlockerStaleRegistration)
	if len(stale) != 1 {
		t.Fatalf("stale-registration blockers = %+v, want one", stale)
	}
	if !strings.Contains(stale[0].Remediation, "worktree prune") {
		t.Errorf("remediation %q does not name the prune", stale[0].Remediation)
	}
	if report.HoldsWork() {
		t.Error("a lost registration was classified as work held")
	}
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Errorf("refused tear-down removed the directory anyway: %v", statErr)
	}
}

// TestDownReportsLockedRegistration surfaces git's own lock as a distinct
// blocker with the unlock remediation.
//
// DHF-TEST: keel/requirement-113 (keel/ac-401)
func TestDownReportsLockedRegistration(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	git(t, root, "worktree", "lock", wt.Path)

	_, err = m.Down(ctx, "unit-1", worktree.DownOptions{})
	report := blockedReport(t, err)
	locked := report.OfKind(worktree.BlockerLockedRegistration)
	if len(locked) != 1 {
		t.Fatalf("locked blockers = %+v, want one", locked)
	}
	if !strings.Contains(locked[0].Remediation, "worktree unlock") {
		t.Errorf("remediation %q does not name the unlock", locked[0].Remediation)
	}
}

// TestDownReportsUndeletableContent is the stale-file class that "commit or
// stash your work" misses entirely: the checkout holds no work at all, but some
// of it cannot be unlinked. It is reported as its own condition with the paths
// named — never as a raw permission error, and never cleared by a force,
// because force addresses the safety gate, not the filesystem.
//
// DHF-TEST: keel/requirement-113 (keel/ac-403)
func TestDownReportsUndeletableContent(t *testing.T) {
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

	// Committed, clean residue inside a directory the caller cannot write: the
	// files are worthless, and no amount of forcing makes them unlinkable.
	residue := filepath.Join(wt.Path, "cache")
	writeFile(t, filepath.Join(residue, "build.o"), "residue\n")
	git(t, wt.Path, "add", "cache/build.o")
	git(t, wt.Path, "commit", "-m", "residue")
	if err := os.Chmod(residue, 0o500); err != nil {
		t.Fatalf("chmod residue: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(residue, 0o755) })

	for _, opts := range []worktree.DownOptions{{}, {Force: true}} {
		_, err := m.Down(ctx, "unit-1", opts)
		report := blockedReport(t, err)
		paths := blockerPaths(report, worktree.BlockerUndeletableContent)
		if len(paths) == 0 {
			t.Fatalf("force=%v: no undeletable-content blocker in %+v", opts.Force, report.Blockers)
		}
		if !strings.Contains(strings.Join(paths, " "), "cache") {
			t.Errorf("force=%v: undeletable paths %v do not name the residue directory", opts.Force, paths)
		}
		if report.HoldsWork() {
			t.Errorf("force=%v: undeletable residue was classified as work held", opts.Force)
		}
		if strings.Contains(err.Error(), "permission denied") {
			t.Errorf("force=%v: raw permission error surfaced: %v", opts.Force, err)
		}
	}
}

// TestDownRefusesTheDirectoryTheCallerIsStandingIn keeps a caller from pulling
// the floor out from under its own process.
//
// DHF-TEST: keel/requirement-113 (keel/ac-401)
func TestDownRefusesTheDirectoryTheCallerIsStandingIn(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	ctx := context.Background()
	wt, err := m.Up(ctx, "unit-1")
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	t.Chdir(wt.Path)

	_, err = m.Down(ctx, "unit-1", worktree.DownOptions{})
	report := blockedReport(t, err)
	if !report.Has(worktree.BlockerCurrentDirectory) {
		t.Fatalf("blockers = %+v, want a current-directory blocker", report.Blockers)
	}
}
