package worktree_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/worktree"
)

// Example_abandonedBranch tears down a checkout whose work will never be
// merged — the case a merge-gated tear-down cannot express. The branch carries
// committed work, no merge is in prospect, and the checkout is still reclaimed
// because the only question asked is whether removing the directory would
// destroy anything. The branch survives; deleting it is a separate, deliberate
// call.
//
// DHF-TEST: keel/requirement-113 (keel/ac-402)
func Example_abandonedBranch() {
	repo := newExampleRepo()
	defer os.RemoveAll(repo)

	m, err := worktree.New(worktree.Config{
		RepoRoot: repo,
		Base:     "main",
		Env:      exampleEnv,
		Logger:   logging.Discard(),
	})
	if err != nil {
		panic(err)
	}
	ctx := context.Background()

	wt, err := m.Up(ctx, "spike-1")
	if err != nil {
		panic(err)
	}
	fmt.Println("up:", wt.Outcome, "on", wt.Branch)

	// Work that turns out to be a dead end, committed and then abandoned.
	if err := os.WriteFile(filepath.Join(wt.Path, "spike.txt"), []byte("dead end\n"), 0o644); err != nil {
		panic(err)
	}
	exampleGit(wt.Path, "add", "spike.txt")
	exampleGit(wt.Path, "commit", "-m", "spike")

	res, err := m.Down(ctx, "spike-1", worktree.DownOptions{Force: true})
	if err != nil {
		panic(err)
	}
	fmt.Println("down:", res.Outcome)

	// Tearing the checkout down did not touch the branch, so the abandoned work
	// is still there until someone decides otherwise.
	state, err := m.State(ctx, "spike-1")
	if err != nil {
		panic(err)
	}
	fmt.Println("checkout present:", state.Exists)

	// A second tear-down converges instead of failing.
	res, err = m.Down(ctx, "spike-1", worktree.DownOptions{})
	if err != nil {
		panic(err)
	}
	fmt.Println("down again:", res.Outcome)

	// Output:
	// up: created on spike-1
	// down: removed
	// checkout present: false
	// down again: noop
}

var exampleEnv = []string{
	"HOME=/nonexistent",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_CONFIG_SYSTEM=/dev/null",
	"GIT_AUTHOR_NAME=keel example",
	"GIT_AUTHOR_EMAIL=example@example.invalid",
	"GIT_COMMITTER_NAME=keel example",
	"GIT_COMMITTER_EMAIL=example@example.invalid",
	"PATH=" + os.Getenv("PATH"),
	"LC_ALL=C",
}

func exampleGit(dir string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = exampleEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("git %v: %v\n%s", args, err, out))
	}
}

func newExampleRepo() string {
	dir, err := os.MkdirTemp("", "keel-worktree-example")
	if err != nil {
		panic(err)
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	exampleGit(dir, "init", "-b", "main", ".")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("seed\n"), 0o644); err != nil {
		panic(err)
	}
	exampleGit(dir, "add", "README.md")
	exampleGit(dir, "commit", "-m", "seed")
	return dir
}
