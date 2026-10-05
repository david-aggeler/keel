package worktree_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

// DHF-TEST: keel/requirement-157 (keel/ac-783)
func TestUpReplicatesNamesGitQuotes(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "cache/\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore cache")

	// Without -z, git C-quotes the first four names under core.quotePath, splits
	// the newline name in two, and the old splitter trimmed the edge spaces.
	names := []string{"café.env", `a"b`, `back\slash`, "new\nline", " lead", "trail ", "plain"}
	var created []string
	for _, name := range names {
		path := filepath.Join(root, "cache", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("body of "+name), 0o600); err != nil {
			// A filesystem that cannot hold the name cannot exhibit the defect.
			t.Logf("skip name %q: %v", name, err)
			continue
		}
		created = append(created, name)
	}

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: "cache/**"}},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	for _, name := range created {
		if got := readFile(t, filepath.Join(wt.Path, "cache", name)); got != "body of "+name {
			t.Fatalf("replicated %q content = %q", name, got)
		}
	}
	assertReplication(t, wt.Replication, "cache/**", worktree.ReplicateOutcomeCopied)
	for _, result := range wt.Replication {
		if result.Pattern == "cache/**" && (result.Eligible != len(created) || result.Materialized != len(created)) {
			t.Fatalf("eligible=%d materialized=%d, want both %d (%q)", result.Eligible, result.Materialized, len(created), created)
		}
	}
}
