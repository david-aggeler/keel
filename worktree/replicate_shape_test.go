package worktree_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

// replicaSnapshot renders what a replicated item actually materialized to, as a
// comparable string: one line per entry with its shape and either its bytes or
// its link target. It lstats every entry, so a symlink is never confused with
// the tree it points at.
func replicaSnapshot(t *testing.T, worktreePath, rel string) string {
	t.Helper()
	base := filepath.Join(worktreePath, filepath.FromSlash(rel))
	var lines []string
	err := filepath.WalkDir(base, func(current string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name, relErr := filepath.Rel(base, current)
		if relErr != nil {
			return relErr
		}
		info, infoErr := os.Lstat(current)
		if infoErr != nil {
			return infoErr
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, linkErr := os.Readlink(current)
			if linkErr != nil {
				return linkErr
			}
			lines = append(lines, filepath.ToSlash(name)+" symlink "+target)
		case info.IsDir():
			lines = append(lines, filepath.ToSlash(name)+" dir")
		default:
			body, readErr := os.ReadFile(current)
			if readErr != nil {
				return readErr
			}
			lines = append(lines, filepath.ToSlash(name)+" file "+string(body))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", base, err)
	}
	return strings.Join(lines, "\n")
}

// DHF-TEST: keel/requirement-160 (keel/ac-671)
func TestUpReplicatesEverySpellingOfOneDirectoryIdentically(t *testing.T) {
	for _, mode := range []worktree.ReplicateMode{worktree.ReplicateCopy, worktree.ReplicateLink} {
		t.Run(string(mode), func(t *testing.T) {
			root := newRepo(t)
			writeFile(t, filepath.Join(root, ".gitignore"), "d/\n")
			git(t, root, "add", ".gitignore")
			git(t, root, "commit", "-m", "ignore d")
			writeFile(t, filepath.Join(root, "d", "one.txt"), "one\n")
			writeFile(t, filepath.Join(root, "d", "nested", "two.txt"), "two\n")

			m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
			spellings := []string{"d", "d/", "d/**"}
			snapshots := make([]string, len(spellings))
			outcomes := make([]worktree.ReplicateOutcome, len(spellings))
			for i, pattern := range spellings {
				wt, err := m.Up(context.Background(), fmt.Sprintf("unit-%d", i), worktree.UpOptions{
					Replicate: []worktree.ReplicateItem{{Pattern: pattern, Mode: mode}},
				})
				if err != nil {
					t.Fatalf("up %q: %v", pattern, err)
				}
				if len(wt.Replication) != 1 {
					t.Fatalf("up %q replication = %v, want one result", pattern, wt.Replication)
				}
				snapshots[i] = replicaSnapshot(t, wt.Path, "d")
				outcomes[i] = wt.Replication[0].Outcome
			}
			for i := 1; i < len(spellings); i++ {
				if snapshots[i] != snapshots[0] {
					t.Fatalf("spelling %q materialized\n%s\nspelling %q materialized\n%s",
						spellings[i], snapshots[i], spellings[0], snapshots[0])
				}
				if outcomes[i] != outcomes[0] {
					t.Fatalf("spelling %q outcome = %q, spelling %q outcome = %q",
						spellings[i], outcomes[i], spellings[0], outcomes[0])
				}
			}
		})
	}
}
