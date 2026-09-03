package worktree_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

// DHF-TEST: keel/requirement-157 (keel/ac-646)
func TestUpReplicatesDeclaredIgnoredFile(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "local.secret\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore local secret")
	writeFile(t, filepath.Join(root, "local.secret"), "token\n")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: "local.secret"}},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if got := readFile(t, filepath.Join(wt.Path, "local.secret")); got != "token\n" {
		t.Fatalf("replicated content = %q, want primary content", got)
	}
	assertReplication(t, wt.Replication, "local.secret", worktree.ReplicateOutcomeCopied)
}

// DHF-TEST: keel/requirement-157 (keel/ac-647, keel/ac-648)
func TestUpSkipsTrackedAndNotIgnoredMatches(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, "tracked.txt"), "tracked\n")
	git(t, root, "add", "tracked.txt")
	git(t, root, "commit", "-m", "track file")
	writeFile(t, filepath.Join(root, "untracked.txt"), "untracked\n")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{
			{Pattern: "tracked.txt"},
			{Pattern: "untracked.txt"},
		},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	assertReplication(t, wt.Replication, "tracked.txt", worktree.ReplicateOutcomeSkippedTracked)
	assertReplication(t, wt.Replication, "untracked.txt", worktree.ReplicateOutcomeSkippedNotIgnored)
	if got := readFile(t, filepath.Join(wt.Path, "tracked.txt")); got != "tracked\n" {
		t.Fatalf("tracked checkout content = %q, want git checkout content", got)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("untracked-not-ignored destination exists: %v", err)
	}
}

// DHF-TEST: keel/requirement-157 (keel/ac-646, keel/ac-647)
func TestUpCopiesOnlyIgnoredCandidatesFromMixedPattern(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), ".claude/materialized/\n")
	writeFile(t, filepath.Join(root, ".claude", "local.md"), "committed\n")
	git(t, root, "add", ".gitignore", ".claude/local.md")
	git(t, root, "commit", "-m", "track local config and ignore materialized")
	writeFile(t, filepath.Join(root, ".claude", "local.md"), "dirty primary\n")
	writeFile(t, filepath.Join(root, ".claude", "materialized", "skill.md"), "materialized\n")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: ".claude/**"}},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if got := readFile(t, filepath.Join(wt.Path, ".claude", "local.md")); got != "committed\n" {
		t.Fatalf("tracked path was copied from primary: %q", got)
	}
	if got := readFile(t, filepath.Join(wt.Path, ".claude", "materialized", "skill.md")); got != "materialized\n" {
		t.Fatalf("ignored candidate content = %q, want copied materialized content", got)
	}
	assertReplication(t, wt.Replication, ".claude/**", worktree.ReplicateOutcomeCopied)
}

// The criterion is about the declared directory, so it holds for every spelling
// of that directory: keel/requirement-160 makes the pattern suffix carry no
// meaning, and the bare spelling is the one keel v0.9.0 got wrong.
//
// DHF-TEST: keel/requirement-157 (keel/ac-651), keel/requirement-160 (keel/ac-671)
func TestUpReplicatesDeclaredIgnoredDirectoryAsLink(t *testing.T) {
	for _, pattern := range []string{".devtools/", ".devtools", ".devtools/**"} {
		t.Run(pattern, func(t *testing.T) {
			root := newRepo(t)
			writeFile(t, filepath.Join(root, ".gitignore"), ".devtools/\n")
			git(t, root, "add", ".gitignore")
			git(t, root, "commit", "-m", "ignore devtools")
			writeFile(t, filepath.Join(root, ".devtools", "index.txt"), "reference\n")

			m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
			item := worktree.ReplicateItem{Pattern: pattern, Mode: worktree.ReplicateLink}
			wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
				Replicate: []worktree.ReplicateItem{item},
			})
			if err != nil {
				t.Fatalf("up: %v", err)
			}
			link := filepath.Join(wt.Path, ".devtools")
			info, err := os.Lstat(link)
			if err != nil {
				t.Fatalf("stat link: %v", err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("%s mode = %v, want symlink", link, info.Mode())
			}
			target, err := os.Readlink(link)
			if err != nil {
				t.Fatalf("readlink: %v", err)
			}
			if target != filepath.Join(root, ".devtools") {
				t.Fatalf("link target = %q, want primary directory", target)
			}
			assertReplication(t, wt.Replication, pattern, worktree.ReplicateOutcomeLinked)

			reused, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
				Replicate: []worktree.ReplicateItem{item},
			})
			if err != nil {
				t.Fatalf("reuse linked up: %v", err)
			}
			assertReplication(t, reused.Replication, pattern, worktree.ReplicateOutcomeLinked)
		})
	}
}

func TestUpReplicatesDeclaredIgnoredDirectoryByCopy(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), ".cache/\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore cache")
	writeFile(t, filepath.Join(root, ".cache", "nested", "item.txt"), "cache\n")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: ".cache/"}},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if got := readFile(t, filepath.Join(wt.Path, ".cache", "nested", "item.txt")); got != "cache\n" {
		t.Fatalf("copied directory file = %q, want primary content", got)
	}
	assertReplication(t, wt.Replication, ".cache/", worktree.ReplicateOutcomeCopied)
}

// DHF-TEST: keel/requirement-157 (keel/ac-653, keel/ac-654)
func TestUpReplicationPoliciesFillMissingAndRefreshDrift(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "one.secret\ntwo.secret\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore secrets")
	writeFile(t, filepath.Join(root, "one.secret"), "one-primary\n")
	writeFile(t, filepath.Join(root, "two.secret"), "two-primary\n")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{
			{Pattern: "one.secret"},
			{Pattern: "two.secret"},
		},
	})
	if err != nil {
		t.Fatalf("first up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "one.secret"), "one-local\n")
	if err := os.Remove(filepath.Join(wt.Path, "two.secret")); err != nil {
		t.Fatalf("remove replicated file: %v", err)
	}

	if _, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{
			{Pattern: "one.secret"},
			{Pattern: "two.secret"},
		},
	}); err != nil {
		t.Fatalf("missing-only up: %v", err)
	}
	if got := readFile(t, filepath.Join(wt.Path, "one.secret")); got != "one-local\n" {
		t.Fatalf("missing-only overwrote drifted copy: %q", got)
	}
	if got := readFile(t, filepath.Join(wt.Path, "two.secret")); got != "two-primary\n" {
		t.Fatalf("missing-only did not fill absent copy: %q", got)
	}

	refreshed, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Policy: worktree.ReplicateRefresh,
		Replicate: []worktree.ReplicateItem{
			{Pattern: "one.secret"},
		},
	})
	if err != nil {
		t.Fatalf("refresh up: %v", err)
	}
	if got := readFile(t, filepath.Join(refreshed.Path, "one.secret")); got != "one-primary\n" {
		t.Fatalf("refresh content = %q, want primary", got)
	}
	assertReplication(t, refreshed.Replication, "one.secret", worktree.ReplicateOutcomeCopied)
}

// DHF-TEST: keel/requirement-157 (keel/ac-655)
func TestDownRemovesWorktreeWhoseOnlyExtraContentIsReplicatedCopy(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "local.secret\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore local secret")
	writeFile(t, filepath.Join(root, "local.secret"), "token\n")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: "local.secret"}},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
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
}

// DHF-TEST: keel/requirement-157 (keel/ac-656)
func TestUpRefusesHazardReplicatePatternBeforeCopy(t *testing.T) {
	for _, pattern := range []string{"worktrees/nested.secret", "../outside.secret"} {
		t.Run(pattern, func(t *testing.T) {
			root := newRepo(t)
			writeFile(t, filepath.Join(root, ".gitignore"), "local.secret\n")
			git(t, root, "add", ".gitignore")
			git(t, root, "commit", "-m", "ignore local secret")
			writeFile(t, filepath.Join(root, "local.secret"), "token\n")
			rec := &replicateLogRecorder{}
			m := newManager(t, worktree.Config{RepoRoot: root, Base: "main", Logger: rec})
			path, _, err := m.Resolve("unit-1")
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}

			_, err = m.Up(context.Background(), "unit-1", worktree.UpOptions{
				Replicate: []worktree.ReplicateItem{
					{Pattern: pattern},
					{Pattern: "local.secret"},
				},
			})
			if !isCode(err, worktree.CodeInvalidArgument) {
				t.Fatalf("up err = %v, want CodeInvalidArgument", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("worktree path exists after hazard refusal: %v", err)
			}
			rec.assertOutcome(t, pattern, worktree.ReplicateOutcomeRefusedHazard)
		})
	}
}

func TestUpReplicateOffSkipsReplicationValidation(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})

	wt, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Policy:    worktree.ReplicateOff,
		Replicate: []worktree.ReplicateItem{{Pattern: "../outside.secret"}},
	})
	if err != nil {
		t.Fatalf("up with replicate off: %v", err)
	}
	if len(wt.Replication) != 0 {
		t.Fatalf("replication results = %v, want none", wt.Replication)
	}
}

func TestUpRejectsInvalidReplicationOptions(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})

	if _, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{Policy: "overwrite"}); !isCode(err, worktree.CodeInvalidArgument) {
		t.Fatalf("invalid policy err = %v, want CodeInvalidArgument", err)
	}
	if _, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: "local.secret", Mode: "hard-link"}},
	}); !isCode(err, worktree.CodeInvalidArgument) {
		t.Fatalf("invalid mode err = %v, want CodeInvalidArgument", err)
	}
	if _, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{}, worktree.UpOptions{}); !isCode(err, worktree.CodeInvalidArgument) {
		t.Fatalf("multiple options err = %v, want CodeInvalidArgument", err)
	}
}

func TestUpReportsReplicateFailedWhenEligibleItemCannotBeWritten(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "nested/\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore nested")
	writeFile(t, filepath.Join(root, "nested", "item.secret"), "primary\n")

	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main"})
	wt, err := m.Up(context.Background(), "unit-1")
	if err != nil {
		t.Fatalf("initial up: %v", err)
	}
	writeFile(t, filepath.Join(wt.Path, "nested"), "file blocks directory materialization\n")

	_, err = m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{{Pattern: "nested/"}},
	})
	if !isCode(err, worktree.CodeReplicateFailed) {
		t.Fatalf("replicate failure err = %v, want CodeReplicateFailed", err)
	}
}

// DHF-TEST: keel/requirement-157 (keel/ac-657)
func TestUpLogsEveryDeclaredReplicationOutcome(t *testing.T) {
	root := newRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), "local.secret\n")
	git(t, root, "add", ".gitignore")
	git(t, root, "commit", "-m", "ignore local secret")
	writeFile(t, filepath.Join(root, "local.secret"), "token\n")
	writeFile(t, filepath.Join(root, "plain.txt"), "plain\n")
	rec := &replicateLogRecorder{}
	m := newManager(t, worktree.Config{RepoRoot: root, Base: "main", Logger: rec})

	_, err := m.Up(context.Background(), "unit-1", worktree.UpOptions{
		Replicate: []worktree.ReplicateItem{
			{Pattern: "local.secret"},
			{Pattern: "plain.txt"},
			{Pattern: "absent.secret"},
		},
	})
	if err != nil {
		t.Fatalf("up: %v", err)
	}

	rec.assertOutcome(t, "local.secret", worktree.ReplicateOutcomeCopied)
	rec.assertOutcome(t, "plain.txt", worktree.ReplicateOutcomeSkippedNotIgnored)
	rec.assertOutcome(t, "absent.secret", worktree.ReplicateOutcomeSkippedAbsent)
}

type replicateLogRecorder struct {
	commandRecorder
	mu      sync.Mutex
	entries []ReplicateLogEntry
}

type ReplicateLogEntry struct {
	Pattern string
	Outcome worktree.ReplicateOutcome
}

func (r *replicateLogRecorder) Info(msg string, args ...any) {
	r.commandRecorder.Info(msg, args...)
	if msg != "worktree replicate item" {
		return
	}
	var entry ReplicateLogEntry
	for i := 0; i+1 < len(args); i += 2 {
		key, _ := args[i].(string)
		switch key {
		case "pattern":
			entry.Pattern, _ = args[i+1].(string)
		case "outcome":
			if s, ok := args[i+1].(string); ok {
				entry.Outcome = worktree.ReplicateOutcome(s)
			}
		}
	}
	r.mu.Lock()
	r.entries = append(r.entries, entry)
	r.mu.Unlock()
}

func (r *replicateLogRecorder) assertOutcome(t *testing.T, pattern string, outcome worktree.ReplicateOutcome) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.entries {
		if entry.Pattern == pattern && entry.Outcome == outcome {
			return
		}
	}
	t.Fatalf("log entries %v lack pattern %q outcome %q", r.entries, pattern, outcome)
}

func assertReplication(t *testing.T, results []worktree.ReplicateResult, pattern string, outcome worktree.ReplicateOutcome) {
	t.Helper()
	for _, result := range results {
		if result.Pattern == pattern && result.Outcome == outcome {
			return
		}
	}
	t.Fatalf("replication results %v lack pattern %q outcome %q", results, pattern, outcome)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func TestReplicatePublicSurfaceUsesDocumentedOutcomeValues(t *testing.T) {
	got := []string{
		string(worktree.ReplicateOutcomeCopied),
		string(worktree.ReplicateOutcomeLinked),
		string(worktree.ReplicateOutcomeSkippedTracked),
		string(worktree.ReplicateOutcomeSkippedNotIgnored),
		string(worktree.ReplicateOutcomeSkippedAbsent),
		string(worktree.ReplicateOutcomeRefusedHazard),
	}
	want := "copied,linked,skipped_tracked,skipped_not_ignored,skipped_absent,refused_hazard"
	if strings.Join(got, ",") != want {
		t.Fatalf("replicate outcomes = %q, want %q", strings.Join(got, ","), want)
	}
}
