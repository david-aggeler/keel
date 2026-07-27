package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
	"github.com/david-aggeler/keel/worktree"
)

// worktreeLeafVerbs are the leaves keel/ac-408 requires the namespace to carry:
// bring-up (up, resume), tear-down (down), and the two read-only reports
// (status, compare).
var worktreeLeafVerbs = []string{"up", "down", "resume", "status", "compare"}

// DHF-TEST: keel/requirement-114 (keel/ac-413)
func TestWorktreeHelpSurfacesExitCodeTaxonomy(t *testing.T) {
	wantRows := worktree.ExitCodeTaxonomy()
	tree := commandTree()

	var topic strings.Builder
	tree.RenderTopicHelp(&topic, []string{"worktree"})
	assertHelpContainsExitCodes(t, "topic help", topic.String(), wantRows)
	if !strings.Contains(topic.String(), "Exit codes:") {
		t.Fatalf("topic help does not render an exit-code section:\n%s", topic.String())
	}

	var all strings.Builder
	tree.RenderAllHelp(&all)
	assertHelpContainsExitCodes(t, "--help-all", all.String(), wantRows)

	var rawJSON strings.Builder
	if err := tree.RenderHelpJSON(&rawJSON); err != nil {
		t.Fatalf("RenderHelpJSON: %v", err)
	}
	var inventory []struct {
		Path      string `json:"path"`
		ExitCodes []struct {
			Code    int    `json:"code"`
			Meaning string `json:"meaning"`
		} `json:"exit_codes"`
	}
	if err := json.Unmarshal([]byte(rawJSON.String()), &inventory); err != nil {
		t.Fatalf("--help-json did not render a JSON command inventory: %v\n%s", err, rawJSON.String())
	}
	wantJSONRows := make([]cli.ExitCodeSpec, 0, len(wantRows))
	for _, row := range wantRows {
		wantJSONRows = append(wantJSONRows, cli.ExitCodeSpec{Code: int(row.Code), Meaning: row.Meaning})
	}
	wantPaths := append([]string{"worktree"}, func() []string {
		paths := make([]string, 0, len(worktreeLeafVerbs))
		for _, verb := range worktreeLeafVerbs {
			paths = append(paths, "worktree "+verb)
		}
		return paths
	}()...)
	seenPaths := map[string]bool{}
	for _, command := range inventory {
		for _, wantPath := range wantPaths {
			if command.Path == wantPath {
				seenPaths[wantPath] = true
				assertExitCodeJSON(t, command.Path, command.ExitCodes, wantJSONRows)
			}
		}
	}
	for _, wantPath := range wantPaths {
		if !seenPaths[wantPath] {
			t.Fatalf("--help-json has no %q command object:\n%s", wantPath, rawJSON.String())
		}
	}
}

func assertHelpContainsExitCodes(t *testing.T, label, help string, wantRows []worktree.ExitCodeDoc) {
	t.Helper()
	for _, row := range wantRows {
		prefix := fmt.Sprintf("%d", row.Code)
		found := false
		for _, line := range strings.Split(help, "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == prefix && strings.Contains(line, row.Meaning) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s missing exit-code row for %d / %q:\n%s", label, row.Code, row.Meaning, help)
		}
	}
}

func assertExitCodeJSON(t *testing.T, path string, got []struct {
	Code    int    `json:"code"`
	Meaning string `json:"meaning"`
}, wantRows []cli.ExitCodeSpec) {
	t.Helper()
	seen := make(map[int]string, len(got))
	for _, row := range got {
		seen[row.Code] = row.Meaning
	}
	for _, row := range wantRows {
		if seen[row.Code] != row.Meaning {
			t.Fatalf("--help-json %s exit_codes[%d] = %q, want %q: %+v", path, row.Code, seen[row.Code], row.Meaning, got)
		}
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-413)
func TestWorktreeWrapperHeadersPointAtGeneratedHelpForExitCodes(t *testing.T) {
	for _, script := range []string{"worktree-up.sh", "worktree-down.sh", "worktree-status.sh", "worktree-resume.sh"} {
		body, err := os.ReadFile(filepath.Join("..", "..", ".claude", "skills", "change-request", "scripts", script))
		if err != nil {
			t.Fatalf("read %s: %v", script, err)
		}
		text := string(body)
		if strings.Contains(text, "# Exit codes:") {
			t.Fatalf("%s still carries a hand-maintained exit-code taxonomy", script)
		}
		if !strings.Contains(text, "keel-dev help worktree") {
			t.Fatalf("%s does not point operators at generated worktree help for exit codes", script)
		}
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-408)
func TestCommandTreeExposesWorktreeNamespace(t *testing.T) {
	tree := commandTree()
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected keel-dev's command tree: %v", err)
	}

	namespace, ok := tree.Child("worktree")
	if !ok {
		t.Fatal("keel-dev's command tree has no worktree namespace")
	}
	if namespace.Handler != nil {
		t.Error("the worktree namespace declares a handler; it must be a pure namespace")
	}
	if len(namespace.Subcommands) < 2 {
		t.Errorf("the worktree namespace has %d children, want at least 2", len(namespace.Subcommands))
	}

	for _, verb := range worktreeLeafVerbs {
		leaf, ok := namespace.Child(verb)
		if !ok {
			t.Errorf("the worktree namespace has no %q leaf", verb)
			continue
		}
		if leaf.Handler == nil {
			t.Errorf("worktree %s declares no handler", verb)
		}
		if len(leaf.Subcommands) > 0 {
			t.Errorf("worktree %s declares children; the tree must stay depth 2", verb)
		}
		if leaf.Short == "" {
			t.Errorf("worktree %s declares no summary", verb)
		}
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-408)
func TestWorktreeVerbsResolveThroughDispatchPaths(t *testing.T) {
	tree := commandTree()
	for _, verb := range worktreeLeafVerbs {
		node, remaining, ok := tree.Find([]string{"worktree", verb})
		if !ok {
			t.Errorf("keel-dev worktree %s does not resolve as a command path", verb)
			continue
		}
		if len(remaining) != 0 {
			t.Errorf("keel-dev worktree %s left %v unconsumed", verb, remaining)
		}
		if node.Name != verb {
			t.Errorf("keel-dev worktree %s resolved to %q", verb, node.Name)
		}
	}

	var deepest int
	walk(tree, 0, &deepest)
	if deepest > 2 {
		t.Errorf("keel-dev's command tree reaches depth %d, want at most 2", deepest)
	}
}

func walk(node *cli.CommandSpec, depth int, deepest *int) {
	if depth > *deepest {
		*deepest = depth
	}
	for _, child := range node.Subcommands {
		walk(child, depth+1, deepest)
	}
}

// worktreeVerbEnv drives the verbs in process against a throwaway repository,
// capturing the protocol stream and the exit status the CLI would return.
type worktreeVerbEnv struct {
	repo      string
	worktrees string
}

func newWorktreeRepo(t *testing.T) string {
	t.Helper()
	repo := resolvedTempDir(t)
	mustRun(t, repo, "git", "init", "-b", "main")
	mustRun(t, repo, "git", "config", "user.email", "keel-test@example.invalid")
	mustRun(t, repo, "git", "config", "user.name", "Keel Test")
	writeFile(t, repo, "base.txt", "base\n")
	mustRun(t, repo, "git", "add", "base.txt")
	mustRun(t, repo, "git", "commit", "-m", "base")
	return repo
}

func newWorktreeVerbEnv(t *testing.T) worktreeVerbEnv {
	t.Helper()
	repo := newWorktreeRepo(t)
	return worktreeVerbEnv{repo: repo, worktrees: filepath.Join(repo, "worktrees")}
}

func (e worktreeVerbEnv) path(name string) string { return filepath.Join(e.worktrees, name) }

// run dispatches one verb and returns its protocol output and exit status.
func (e worktreeVerbEnv) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	out, _, code := e.runWithLogs(t, args...)
	return out, code
}

func (e worktreeVerbEnv) runWithLogs(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	var out strings.Builder
	ctx := withRunStateProtocol(context.Background(), logger, nil, e.repo, &out)
	code := exitFor(logger, commandTree().Dispatch(ctx, args))
	return out.String(), logs.String(), code
}

func assertVerb(t *testing.T, label, gotOut string, gotCode int, wantOut string, wantCode int) {
	t.Helper()
	if gotCode != wantCode {
		t.Errorf("%s: exit %d, want %d (output %q)", label, gotCode, wantCode, gotOut)
	}
	if gotOut != wantOut {
		t.Errorf("%s: output %q, want %q", label, gotOut, wantOut)
	}
}

func assertLogHas(t *testing.T, label, logs, token string) {
	t.Helper()
	if !strings.Contains(logs, token) {
		t.Errorf("%s: logs do not contain %q\nlogs: %s", label, token, logs)
	}
}

func assertWorktreeBranchAbsent(t *testing.T, repo, path, branch string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree path %s exists after refusal: %v", path, err)
	}
	if testBranchExists(t, repo, branch) {
		t.Fatalf("branch %s exists after refusal", branch)
	}
}

func testBranchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repo
	err := cmd.Run()
	if err == nil {
		return true
	}
	exitErr, ok := err.(*exec.ExitError)
	if ok && exitErr.ExitCode() == 1 {
		return false
	}
	t.Fatalf("git show-ref for %s: %v", branch, err)
	return false
}

// DHF-TEST: keel/requirement-113, keel/requirement-114 (keel/ac-408, keel/ac-409, keel/ac-411)
func TestWorktreeUpVerb(t *testing.T) {
	env := newWorktreeVerbEnv(t)

	out, logs, code := env.runWithLogs(t, "worktree", "up", "cr-1-alpha")
	assertVerb(t, "up creates", out, code, "up cr-1-alpha "+env.path("cr-1-alpha")+"\n", 0)
	assertLogHas(t, "up creates", logs, "outcome=created")

	out, logs, code = env.runWithLogs(t, "worktree", "up", "cr-1-alpha")
	assertVerb(t, "up reuses", out, code, "up-noop cr-1-alpha "+env.path("cr-1-alpha")+"\n", 0)
	assertLogHas(t, "up reuses", logs, "outcome=reused")

	if err := os.MkdirAll(env.path("cr-2-beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, code = env.run(t, "worktree", "up", "cr-2-beta")
	assertVerb(t, "up refuses an unregistered path", out, code, "", 65)

	mustRun(t, env.repo, "git", "branch", "cr-3-gamma")
	out, logs, code = env.runWithLogs(t, "worktree", "up", "cr-3-gamma")
	assertVerb(t, "up attaches an existing branch", out, code, "up cr-3-gamma "+env.path("cr-3-gamma")+"\n", 0)
	assertLogHas(t, "up attaches an existing branch", logs, "outcome=attached")

	out, code = env.run(t, "worktree", "up", "../escape")
	assertVerb(t, "up refuses an unsafe name", out, code, "", 64)
}

// TestWorktreeUpVerbRefusesForeignBranch covers the path that is registered for
// this work item but checked out on some other branch.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeUpVerbRefusesForeignBranch(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	mustRun(t, env.repo, "git", "branch", "other")
	if err := os.MkdirAll(env.worktrees, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, env.repo, "git", "worktree", "add", env.path("cr-1-alpha"), "other")

	out, code := env.run(t, "worktree", "up", "cr-1-alpha")
	assertVerb(t, "up refuses a checkout on another branch", out, code, "", 65)
}

// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeDownVerb(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	env.run(t, "worktree", "up", "cr-1-alpha")

	out, code := env.run(t, "worktree", "down", "cr-9-absent")
	assertVerb(t, "down on an absent checkout", out, code, "down-noop cr-9-absent "+env.path("cr-9-absent")+"\n", 0)

	writeFile(t, env.path("cr-1-alpha"), "scratch.txt", "work\n")
	out, code = env.run(t, "worktree", "down", "cr-1-alpha")
	assertVerb(t, "down refuses a dirty checkout", out, code, "", 66)

	out, code = env.run(t, "worktree", "down", "--force", "cr-1-alpha")
	assertVerb(t, "down --force removes a dirty checkout", out, code, "down cr-1-alpha "+env.path("cr-1-alpha")+"\n", 0)

	if gitOutput(t, env.repo, "rev-parse", "--verify", "refs/heads/cr-1-alpha") == "" {
		t.Error("tear-down did not preserve the branch")
	}
}

// TestWorktreeDownVerbPrunesAnAbsentDirectory covers the half-finished case: the
// directory is gone but the registration survives.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeDownVerbPrunesAnAbsentDirectory(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	env.run(t, "worktree", "up", "cr-1-alpha")
	if err := os.RemoveAll(env.path("cr-1-alpha")); err != nil {
		t.Fatal(err)
	}

	out, code := env.run(t, "worktree", "down", "cr-1-alpha")
	assertVerb(t, "down prunes a stale registration", out, code, "down-noop cr-1-alpha "+env.path("cr-1-alpha")+"\n", 0)

	if strings.Contains(gitOutput(t, env.repo, "worktree", "list", "--porcelain"), env.path("cr-1-alpha")) {
		t.Error("the stale registration survived tear-down")
	}
}

// TestWorktreeDownVerbRefusesUnregisteredDirectory covers the stale-registration
// refusal and the remediation the report carries.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeDownVerbRefusesUnregisteredDirectory(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	if err := os.MkdirAll(env.path("cr-4-delta"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, code := env.run(t, "worktree", "down", "cr-4-delta")
	assertVerb(t, "down refuses an unregistered directory", out, code, "", 66)
}

// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeResumeVerb(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	env.run(t, "worktree", "up", "cr-1-alpha")

	out, code := env.run(t, "worktree", "resume", "cr-1-alpha")
	assertVerb(t, "resume on a registered checkout", out, code, "resume-noop cr-1-alpha "+env.path("cr-1-alpha")+"\n", 0)

	out, code = env.run(t, "worktree", "resume", "cr-9-absent")
	assertVerb(t, "resume without a branch", out, code, "", 67)
	assertWorktreeBranchAbsent(t, env.repo, env.path("cr-9-absent"), "cr-9-absent")

	env.run(t, "worktree", "down", "cr-1-alpha")
	out, code = env.run(t, "worktree", "resume", "cr-1-alpha")
	assertVerb(t, "resume re-attaches", out, code, "resume cr-1-alpha "+env.path("cr-1-alpha")+"\n", 0)

	out, code = env.run(t, "worktree", "resume", "../escape")
	assertVerb(t, "resume refuses an unsafe name", out, code, "", 64)
}

// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeStatusVerb(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	env.run(t, "worktree", "up", "cr-1-alpha")

	out, code := env.run(t, "worktree", "status", "cr-1-alpha")
	assertVerb(t, "status of a live checkout", out, code,
		"status cr-1-alpha "+env.path("cr-1-alpha")+" branch=true worktree=true\n", 0)

	out, code = env.run(t, "worktree", "status", "cr-9-absent")
	assertVerb(t, "status of an absent checkout", out, code,
		"status cr-9-absent "+env.path("cr-9-absent")+" branch=false worktree=false\n", 0)

	out, code = env.run(t, "worktree", "status", "--glob", "cr*")
	assertVerb(t, "glob status", out, code,
		"status cr-1-alpha "+env.path("cr-1-alpha")+" branch=true worktree=true\n", 0)

	out, code = env.run(t, "worktree", "status", "--glob", "epic*")
	assertVerb(t, "glob status with no matches", out, code, "", 0)

	out, code = env.run(t, "worktree", "status", "--glob", "CR*")
	assertVerb(t, "glob status rejects a bad pattern", out, code, "", 64)

	out, code = env.run(t, "worktree", "status")
	assertVerb(t, "status needs a name or a pattern", out, code, "", 2)

	out, code = env.run(t, "worktree", "status", "--glob", "cr*", "cr-1-alpha")
	assertVerb(t, "status refuses both forms", out, code, "", 2)
}

// TestWorktreeStatusVerbWithoutWorktreesDirectory covers the report path used
// when the worktrees parent has never existed.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeStatusVerbWithoutWorktreesDirectory(t *testing.T) {
	env := newWorktreeVerbEnv(t)

	out, code := env.run(t, "worktree", "status", "cr-1-alpha")
	assertVerb(t, "status without a worktrees directory", out, code,
		"status cr-1-alpha "+filepath.Join(filepath.Dir(env.repo), "cr-1-alpha")+" branch=false worktree=false\n", 0)

	out, code = env.run(t, "worktree", "status", "--glob", "cr*")
	assertVerb(t, "glob status without a worktrees directory", out, code, "", 0)
}

// DHF-TEST: keel/requirement-114 (keel/ac-408)
func TestWorktreeCompareVerb(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	env.run(t, "worktree", "up", "cr-1-alpha")

	out, code := env.run(t, "worktree", "compare", "cr-1-alpha")
	if code != 0 {
		t.Fatalf("compare on a fresh branch exited %d (%q)", code, out)
	}
	if !strings.HasPrefix(out, "compare cr-1-alpha cr-1-alpha base=main ahead=0 behind=0 ") {
		t.Errorf("compare on a fresh branch reported %q", out)
	}

	unit := env.path("cr-1-alpha")
	writeFile(t, unit, "slice.txt", "slice\n")
	mustRun(t, unit, "git", "add", "slice.txt")
	mustRun(t, unit, "git", "commit", "-m", "slice")

	out, code = env.run(t, "worktree", "compare", "cr-1-alpha")
	if code != 0 {
		t.Fatalf("compare after a commit exited %d (%q)", code, out)
	}
	if !strings.Contains(out, "ahead=1") {
		t.Errorf("compare after a commit reported %q, want ahead=1", out)
	}

	out, code = env.run(t, "worktree", "compare", "cr-9-absent")
	assertVerb(t, "compare on an unregistered checkout", out, code, "", 65)
}

// TestWorktreeVerbsRefuseOutsideRepository pins the not-in-repo status the
// wrappers' contract carries.
//
// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeVerbsRefuseOutsideRepository(t *testing.T) {
	env := worktreeVerbEnv{repo: resolvedTempDir(t)}
	for _, verb := range []string{"up", "down", "resume", "status", "compare"} {
		out, code := env.run(t, "worktree", verb, "cr-1-alpha")
		assertVerb(t, "worktree "+verb+" outside a repository", out, code, "", 2)
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestMarkerWorktreeBase(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"absent", "version: 1\n", ""},
		{"plain", "placeholders:\n  worktree_base: trees/\n", "trees/"},
		{"quoted", "placeholders:\n  worktree_base: \"trees/\"\n", "trees/"},
		{"single quoted", "placeholders:\n  worktree_base: 'trees/'\n", "trees/"},
		{"trailing comment", "placeholders:\n  worktree_base: trees/ # here\n", "trees/"},
		{"carriage returns", "placeholders:\r\n  worktree_base: trees/\r\n", "trees/"},
		{"outside the block", "worktree_base: trees/\n", ""},
		{"after the block ends", "placeholders:\n  other: 1\nvalues:\n  worktree_base: trees/\n", ""},
		{"later key wins nothing", "placeholders:\n  worktree_base: first/\n  worktree_base: second/\n", "first/"},
	}
	for _, tc := range cases {
		if got := markerWorktreeBase(tc.body); got != tc.want {
			t.Errorf("%s: markerWorktreeBase = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeVerbsHonorTheDeclaredWorktreeBase(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	writeFile(t, env.repo, "openbrain-client.local.yaml", "placeholders:\n  worktree_base: trees/\n")

	out, code := env.run(t, "worktree", "up", "cr-1-alpha")
	assertVerb(t, "up under a declared base", out, code,
		"up cr-1-alpha "+filepath.Join(env.repo, "trees", "cr-1-alpha")+"\n", 0)
}

// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeVerbsRefuseAnAbsoluteWorktreeBase(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	writeFile(t, env.repo, "openbrain-client.local.yaml", "placeholders:\n  worktree_base: /tmp/elsewhere\n")

	out, code := env.run(t, "worktree", "status", "cr-1-alpha")
	assertVerb(t, "an absolute base is refused", out, code, "", 65)
}

// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeDownBlockersFilterByPolicy(t *testing.T) {
	report := worktree.StaleReport{Blockers: []worktree.Blocker{
		{Kind: worktree.BlockerUncommittedChange, Path: "a.txt"},
		{Kind: worktree.BlockerUntrackedFile, Path: "b.txt"},
		{Kind: worktree.BlockerUnpushedCommit, Commit: "deadbeef"},
		{Kind: worktree.BlockerUndeletableContent, Path: "c.txt"},
	}}

	blocking := worktreeDownBlockers(report, false)
	if len(blocking) != 3 {
		t.Fatalf("unforced tear-down kept %d blockers, want 3: %v", len(blocking), blocking)
	}
	for _, blocker := range blocking {
		if blocker.Kind == worktree.BlockerUnpushedCommit {
			t.Error("commits absent from every remote must not block tear-down")
		}
	}
	if got := summarizeBlockers(blocking); got != "uncommitted_change x1, untracked_file x1, undeletable_content x1" {
		t.Errorf("summary = %q", got)
	}

	forced := worktreeDownBlockers(report, true)
	if len(forced) != 1 || forced[0].Kind != worktree.BlockerUndeletableContent {
		t.Errorf("forced tear-down kept %v, want only undeletable content", forced)
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestValidWorktreeGlob(t *testing.T) {
	valid := []string{"cr*", "cr-1-alpha", "epic_2", "a"}
	invalid := []string{"", "CR*", "1cr", "cr/..", "cr.*", "cr *"}
	for _, pattern := range valid {
		if !validWorktreeGlob(pattern) {
			t.Errorf("%q rejected, want accepted", pattern)
		}
	}
	for _, pattern := range invalid {
		if validWorktreeGlob(pattern) {
			t.Errorf("%q accepted, want rejected", pattern)
		}
	}
}

// DHF-TEST: keel/requirement-114 (keel/ac-409)
func TestWorktreeExitMapsTheTaxonomy(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"conflict", &worktree.Error{Op: "up", Code: worktree.CodeConflict, Message: "occupied"}, 65},
		{"blocked", &worktree.Error{Op: "down", Code: worktree.CodeBlocked, Message: "held"}, 66},
		{"branch missing", &worktree.Error{Op: "resume", Code: worktree.CodeBranchMissing, Message: "gone"}, 67},
		{"unclassified", errors.New("boom"), 1},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range cases {
		if got := exitFor(logger, worktreeExit("verb", tc.err)); got != tc.want {
			t.Errorf("%s: exit %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := exitFor(logger, worktreeFailure("up", worktree.CodeInvalidArgument, "bad %s", "name")); got != 64 {
		t.Errorf("worktreeFailure exit %d, want 64", got)
	}
}
