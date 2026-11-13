//go:build unix

package worktree_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/david-aggeler/keel/worktree"
)

// TestHungGitDescendantDoesNotOutliveTheDeadline: git's descendant holds the
// capture pipes open for two seconds; a 50 ms deadline must still bound the
// call. This is the keel-side copy of the consumer repro in openbrain
// issue-1817.
//
// DHF-TEST: keel/requirement-190 (keel/ac-836)
func TestHungGitDescendantDoesNotOutliveTheDeadline(t *testing.T) {
	root := newRepo(t)
	shim := filepath.Join(t.TempDir(), "git-hang")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nsleep 2\n"), 0o755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main", GitBin: shim})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := m.State(ctx, "unit-1")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("state against a hung git returned no error")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("state took %v; want < 500ms after a 50ms deadline", elapsed)
	}
}
