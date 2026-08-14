package worktree

import (
	"errors"
	"testing"
)

// TestSummarizeCountsEachKind keeps the one-line refusal message faithful to the
// report behind it.
//
// DHF-TEST: keel/requirement-113 (keel/ac-401)
func TestSummarizeCountsEachKind(t *testing.T) {
	report := StaleReport{Blockers: []Blocker{
		{Kind: BlockerUntrackedFile, Path: "a"},
		{Kind: BlockerUncommittedChange, Path: "b"},
		{Kind: BlockerUntrackedFile, Path: "c"},
	}}
	if got, want := summarize(report), "untracked_file x2, uncommitted_change x1"; got != want {
		t.Errorf("summarize = %q, want %q", got, want)
	}
}

// TestBlockingItemsForceScope pins what a force may and may not clear: held
// work yields to it, and the conditions no permission can change do not.
//
// DHF-TEST: keel/requirement-113 (keel/ac-403)
func TestBlockingItemsForceScope(t *testing.T) {
	report := StaleReport{Blockers: []Blocker{
		{Kind: BlockerUncommittedChange},
		{Kind: BlockerUntrackedFile},
		{Kind: BlockerUnpushedCommit},
		{Kind: BlockerCurrentDirectory},
		{Kind: BlockerUndeletableContent},
		{Kind: BlockerLockedRegistration},
		{Kind: BlockerStaleRegistration},
		{Kind: BlockerInspectionFailed},
	}}
	if got := blockingItems(report, DownPolicyDefault, false); len(got.Blockers) != len(report.Blockers) {
		t.Errorf("unforced blockers = %d, want all %d", len(got.Blockers), len(report.Blockers))
	}
	forced := blockingItems(report, DownPolicyDefault, true)
	if forced.HoldsWork() {
		t.Error("a force left a held-work blocker standing")
	}
	for _, kind := range []BlockerKind{BlockerUndeletableContent, BlockerLockedRegistration, BlockerStaleRegistration, BlockerInspectionFailed} {
		if !forced.Has(kind) {
			t.Errorf("a force wrongly cleared %q", kind)
		}
	}
	if forced.Has(BlockerCurrentDirectory) {
		t.Error("a force did not clear the caller's own working directory")
	}
	keptBranchCommits := blockingItems(report, DownPolicyKeepBranchCommits, false)
	if keptBranchCommits.Has(BlockerUnpushedCommit) {
		t.Error("the keep-branch-commits policy left unpushed commits blocking")
	}
	for _, kind := range []BlockerKind{BlockerUncommittedChange, BlockerUntrackedFile, BlockerCurrentDirectory} {
		if !keptBranchCommits.Has(kind) {
			t.Errorf("the keep-branch-commits policy wrongly cleared %q without force", kind)
		}
	}
	var empty StaleReport
	if !empty.Empty() {
		t.Error("an empty report does not report itself empty")
	}
}

// TestSameBranchMatchesRefSpellings keeps a base declared as a remote-tracking
// ref matching the local branch it names, without hardcoding any branch name.
//
// DHF-TEST: keel/requirement-113 (keel/ac-404)
func TestSameBranchMatchesRefSpellings(t *testing.T) {
	cases := map[[2]string]bool{
		{"main", "main"}:                          true,
		{"main", "refs/heads/main"}:               true,
		{"main", "origin/main"}:                   true,
		{"feature/x", "feature/x"}:                true,
		{"main", "origin/master"}:                 false,
		{"main", "trunk"}:                         false,
		{"topic", "refs/heads/topic-2"}:           false,
		{"x", "upstream/release/x"}:               false,
		{"release/x", "up/release/x"}:             true,
		{"main", "refs/remotes/o/main"}:           false,
		{"remotes/o/main", "refs/remotes/o/main"}: true,
	}
	for input, want := range cases {
		if got := sameBranch(input[0], input[1]); got != want {
			t.Errorf("sameBranch(%q, %q) = %v, want %v", input[0], input[1], got, want)
		}
	}
}

// TestParseCountRejectsNonCounts keeps a garbled count from reading as zero.
//
// DHF-TEST: keel/requirement-113 (keel/ac-404)
func TestParseCountRejectsNonCounts(t *testing.T) {
	for _, bad := range []string{"", "x", "-1", "1 2"} {
		if _, ok := parseCount(bad); ok {
			t.Errorf("parseCount(%q) accepted", bad)
		}
	}
	if n, ok := parseCount(" 7\n"); !ok || n != 7 {
		t.Errorf("parseCount(\" 7\\n\") = %d, %v", n, ok)
	}
}

// TestErrorUnwrap keeps the wrapped cause visible to errors.Is.
//
// DHF-TEST: keel/requirement-113 (keel/ac-399)
func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("underlying")
	err := wrapError("down", CodeGit, "/tmp/x", cause, "inspect %s", "/tmp/x")
	if !errors.Is(err, cause) {
		t.Error("wrapped cause is not visible to errors.Is")
	}
	if got := err.Error(); got != "keel/worktree: down: inspect /tmp/x: underlying" {
		t.Errorf("Error() = %q", got)
	}
	plain := newError("up", CodeConflict, "", "no cause here")
	if plain.Unwrap() != nil {
		t.Error("an unwrapped error reports a cause")
	}
	if got := plain.Error(); got != "keel/worktree: up: no cause here" {
		t.Errorf("Error() = %q", got)
	}
	bare := &Error{Message: "no operation"}
	if got := bare.Error(); got != "keel/worktree: no operation" {
		t.Errorf("Error() = %q", got)
	}
}
