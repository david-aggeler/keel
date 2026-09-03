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
// its link target. It calls lstat on every entry, so a symlink is never
// confused with the tree it points at.
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

// writeSymlink creates a symbolic link at path with the literal target string,
// creating parent directories as needed.
func writeSymlink(t *testing.T, path, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("symlink %s -> %s: %v", path, target, err)
	}
}

// DHF-TEST: keel/requirement-160 (keel/ac-672)
func TestUpCopyPreservesSymlinkInsideReplicatedTree(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "deps/\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore deps")
	writeFile(t, filepath.Join(root, "deps", "real", "index.js"), "module\n")
	writeSymlink(t, filepath.Join(root, "deps", "alias"), "real")
	writeSymlink(t, filepath.Join(root, "deps", "nested", "up"), "../real")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: "deps/", Mode: worktree.ReplicateCopy}},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	for rel, want := range map[string]string{
		"deps/alias":     "real",
		"deps/nested/up": "../real",
	} {
		replica := filepath.Join(wt.Path, filepath.FromSlash(rel))
		info, statErr := os.Lstat(replica)
		if statErr != nil {
			t.Fatalf("stat %s: %v", rel, statErr)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s mode = %v, want symlink", rel, info.Mode())
		}
		target, linkErr := os.Readlink(replica)
		if linkErr != nil {
			t.Fatalf("readlink %s: %v", rel, linkErr)
		}
		if target != want {
			t.Fatalf("%s target = %q, want %q", rel, target, want)
		}
	}
	assertReplication(t, wt.Replication, "deps/", worktree.ReplicateOutcomeCopied)
}

// DHF-TEST: keel/requirement-160 (keel/ac-673)
func TestUpReportsPartialWhenMaterializationCoversFewerThanEveryCandidate(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), ".claude/generated/\n")
	writeFile(t, filepath.Join(root, ".claude", "local.md"), "committed\n")
	git(t, root, "add", ".gitignore", ".claude/local.md")
	git(t, root, "commit", "-m", "track local config and ignore generated")
	writeFile(t, filepath.Join(root, ".claude", "generated", "one.md"), "one\n")
	writeFile(t, filepath.Join(root, ".claude", "generated", "two.md"), "two\n")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: ".claude", Mode: worktree.ReplicateLink}},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if len(wt.Replication) != 1 {
		t.Fatalf("replication = %v, want one result", wt.Replication)
	}
	result := wt.Replication[0]
	if result.Pattern != ".claude" {
		t.Fatalf("result pattern = %q, want the declared item", result.Pattern)
	}
	if result.Outcome == worktree.ReplicateOutcomeCopied || result.Outcome == worktree.ReplicateOutcomeLinked {
		t.Fatalf("partial materialization outcome = %q, want neither copied nor linked", result.Outcome)
	}
	if result.Eligible != 2 {
		t.Fatalf("eligible candidates = %d, want 2", result.Eligible)
	}
	if result.Materialized >= result.Eligible {
		t.Fatalf("materialized = %d of %d, want fewer than every candidate", result.Materialized, result.Eligible)
	}
}

// DHF-TEST: keel/requirement-160 (keel/ac-674)
func TestDownRemovesWorktreeWhoseOnlyExtraContentIsReplicatedLink(t *testing.T) {
	root := newRepo(t)
	// The trailing slash matches directories only, so the ignore rule that hides
	// the primary directory does not hide the symlink standing in for it.
	writeFile(t, filepath.Join(root, ".gitignore"), "deps/\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore deps")
	writeFile(t, filepath.Join(root, "deps", "one.txt"), "one\n")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: "deps/", Mode: worktree.ReplicateLink}},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	state, err := m.State(context.Background(), "unit-1")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	for _, blocker := range state.Stale.OfKind(worktree.BlockerUntrackedFile) {
		t.Fatalf("replicated link reported as untracked work: %+v", blocker)
	}

	removed, err := m.Down(context.Background(), "unit-1", worktree.DownOptions{})
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	if removed.Outcome != worktree.DownRemoved {
		t.Fatalf("down outcome = %q, want %q", removed.Outcome, worktree.DownRemoved)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after down: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "deps", "one.txt")); err != nil {
		t.Fatalf("tear-down reached through the link into the primary checkout: %v", err)
	}
}
