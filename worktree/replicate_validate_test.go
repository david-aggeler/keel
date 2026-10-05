package worktree_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

// validateRow is one replicate item judged against one worktrees placement.
// wantMsg is the exact refusal message; empty means the item is accepted.
type validateRow struct {
	placement string
	pattern   string
	wantMsg   string
}

// worktreesPlacements returns the worktrees directory for each placement the
// refusal rule distinguishes: below the repository root, the root itself, and
// a sibling outside it.
func worktreesPlacements(root string) map[string]string {
	return map[string]string{
		"below":   filepath.Join(root, "worktrees"),
		"root":    root,
		"outside": filepath.Join(filepath.Dir(root), "elsewhere"),
	}
}

func validateRows() []validateRow {
	escapes := func(p string) string { return fmt.Sprintf("replicate pattern %q escapes the repository root", p) }
	absolute := func(p string) string {
		return fmt.Sprintf("replicate pattern %q must be relative to the repository root", p)
	}
	reaches := func(p, rel string) string {
		return fmt.Sprintf("replicate pattern %q reaches the worktrees parent %s", p, rel)
	}
	const empty = "replicate pattern must not be empty"
	outsideRel := filepath.Join("..", "elsewhere")

	rows := []validateRow{
		{"below", ".claude/**", ""},
		{"below", "*.local", ""},
		{"below", "**", ""},
		{"below", "worktreesX/y", ""},
		{"below", "local.secret", ""},
		{"below", "worktrees/**", reaches("worktrees/**", "worktrees")},
		{"below", "worktrees", reaches("worktrees", "worktrees")},
		{"below", "worktrees/a/b.secret", reaches("worktrees/a/b.secret", "worktrees")},
		{"below", "./worktrees/x", reaches("./worktrees/x", "worktrees")},
		{"below", "", empty},
		{"below", "   ", empty},
		{"below", "/etc/passwd", absolute("/etc/passwd")},
		{"below", ".", escapes(".")},
		{"below", "./", escapes("./")},
		{"below", "..", escapes("..")},
		{"below", "../outside.secret", escapes("../outside.secret")},
		{"below", "a/../../x", escapes("a/../../x")},
		{"below", " ../x ", escapes(" ../x ")},
		{"root", "", empty},
		{"root", "/abs", absolute("/abs")},
		{"root", "..", escapes("..")},
		{"outside", "../x", escapes("../x")},
	}
	for _, pattern := range []string{"**", "*.local", ".claude/**", "worktrees/**"} {
		rows = append(rows,
			validateRow{"root", pattern, reaches(pattern, ".")},
			validateRow{"outside", pattern, reaches(pattern, outsideRel)},
		)
	}
	return rows
}

func assertRefusal(t *testing.T, label string, err error, wantMsg string) {
	t.Helper()
	if wantMsg == "" {
		if err != nil {
			t.Fatalf("%s: err = %v, want accepted", label, err)
		}
		return
	}
	var e *worktree.Error
	if !errors.As(err, &e) {
		t.Fatalf("%s: err = %v, want *worktree.Error", label, err)
	}
	if e.Op != "up" || e.Code != worktree.CodeInvalidArgument || e.Message != wantMsg {
		t.Fatalf("%s: got op=%q code=%v message=%q, want op=up code=CodeInvalidArgument message=%q", label, e.Op, e.Code, e.Message, wantMsg)
	}
}

// DHF-TEST: keel/requirement-181 (keel/ac-781)
func TestValidateReplicateItemRefusesEveryHazardBranch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	placements := worktreesPlacements(root)
	for _, row := range validateRows() {
		label := fmt.Sprintf("%s/%q", row.placement, row.pattern)
		item := worktree.ReplicateItem{Pattern: row.pattern}
		wtDir := placements[row.placement]

		assertRefusal(t, label+" single", worktree.ValidateReplicateItem(root, wtDir, item), row.wantMsg)
		// The list form stops at the first refusal, so a refused item behind an
		// accepted one still yields the item's own error.
		list := []worktree.ReplicateItem{{Pattern: "local.secret"}, item}
		if row.placement != "below" {
			list = []worktree.ReplicateItem{item}
		}
		assertRefusal(t, label+" list", worktree.ValidateReplicateItems(root, wtDir, list), row.wantMsg)
	}
	if err := worktree.ValidateReplicateItems(root, placements["below"], nil); err != nil {
		t.Fatalf("empty item list: err = %v, want nil", err)
	}
}

// DHF-TEST: keel/requirement-181 (keel/ac-781)
func TestValidateReplicateItemCanonicalizesRelativeRoots(t *testing.T) {
	// A relative spelling of the same paths must yield the verdict New-built
	// managers give, because New canonicalizes both directories.
	t.Chdir(t.TempDir())
	err := worktree.ValidateReplicateItem("repo", "repo/worktrees/../worktrees", worktree.ReplicateItem{Pattern: "worktrees/**"})
	assertRefusal(t, "relative", err, `replicate pattern "worktrees/**" reaches the worktrees parent worktrees`)
	if err := worktree.ValidateReplicateItem("repo", "repo/worktrees", worktree.ReplicateItem{Pattern: ".claude/**"}); err != nil {
		t.Fatalf("relative accepted item: err = %v", err)
	}
}

// DHF-TEST: keel/requirement-181 (keel/ac-782)
func TestUpVerdictEqualsValidateReplicateItems(t *testing.T) {
	root := newRepo(t)
	placements := worktreesPlacements(root)
	managers := map[string]*worktree.Manager{}
	for placement, dir := range placements {
		managers[placement] = newManager(t, worktree.Config{RepoRoot: root, WorktreesDir: dir, Base: "main"})
	}
	for i, row := range validateRows() {
		label := fmt.Sprintf("%s/%q", row.placement, row.pattern)
		items := []worktree.ReplicateItem{{Pattern: row.pattern}}

		_, upErr := managers[row.placement].Up(context.Background(), fmt.Sprintf("unit-%d", i), worktree.UpOptions{Replicate: items})
		valErr := worktree.ValidateReplicateItems(root, placements[row.placement], items)

		var upE, valE *worktree.Error
		upRefused := errors.As(upErr, &upE) && upE.Code == worktree.CodeInvalidArgument
		valRefused := errors.As(valErr, &valE)
		switch {
		case upErr != nil && !upRefused:
			t.Fatalf("%s: up failed for a reason other than refusal: %v", label, upErr)
		case upRefused != valRefused:
			t.Fatalf("%s: up refused=%v (%v), ValidateReplicateItems refused=%v (%v)", label, upRefused, upErr, valRefused, valErr)
		case upRefused && (upE.Op != valE.Op || upE.Code != valE.Code || upE.Message != valE.Message):
			t.Fatalf("%s: up error %+v differs from ValidateReplicateItems error %+v", label, *upE, *valE)
		}
		assertRefusal(t, label+" table", valErr, row.wantMsg)
	}
}
