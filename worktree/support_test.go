package worktree_test

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain silences the process-wide default logger: keel/exec falls back to it
// whenever a test builds a Manager without one, and the START/END pair for every
// git command would otherwise bury the test output.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// gitEnv is the fixed environment every fixture git command runs under, so a
// developer's global git config cannot change what a test observes.
var gitEnv = []string{
	"HOME=/nonexistent",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_CONFIG_SYSTEM=/dev/null",
	"GIT_AUTHOR_NAME=keel test",
	"GIT_AUTHOR_EMAIL=test@example.invalid",
	"GIT_COMMITTER_NAME=keel test",
	"GIT_COMMITTER_EMAIL=test@example.invalid",
	"PATH=" + os.Getenv("PATH"),
	"LC_ALL=C",
}

// git runs one git command in dir and fails the test on a non-zero exit.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitTry(dir, args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

func gitTry(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// newRepo creates a temporary repository with one commit on branch main and
// returns its root. The repository has no remotes.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// macOS temp dirs are symlinked; git reports the resolved path in
	// `worktree list`, so canonicalize up front and compare like for like.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	git(t, root, "init", "-b", "main", ".")
	writeFile(t, filepath.Join(root, "README.md"), "seed\n")
	git(t, root, "add", "README.md")
	git(t, root, "commit", "-m", "seed")
	return root
}

// newRepoWithRemote creates a repository with one commit on branch main whose
// "origin" is a bare repository that already carries that commit.
func newRepoWithRemote(t *testing.T) (root, remote string) {
	t.Helper()
	root = newRepo(t)
	remote = filepath.Join(filepath.Dir(root), "remote.git")
	git(t, filepath.Dir(root), "init", "--bare", "-b", "main", remote)
	git(t, root, "remote", "add", "origin", remote)
	git(t, root, "push", "-u", "origin", "main")
	return root, remote
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// removeAll deletes a directory tree, used to stage a registration that git
// still lists but whose directory is gone.
func removeAll(path string) error { return os.RemoveAll(path) }
