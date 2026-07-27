package worktree_test

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// commandRecorder is a worktree.Logger that keeps the command line of every git
// invocation keel/exec starts. It is how the read-only and no-merge-query
// contracts are observed: the package's whole git footprint travels the
// START/END lifecycle, so the recorded command set is the evidence.
type commandRecorder struct {
	mu       sync.Mutex
	commands []string
}

func (r *commandRecorder) Debug(string, ...any) {}
func (r *commandRecorder) Error(string, ...any) {}

func (r *commandRecorder) Info(msg string, args ...any) { r.record(msg, args) }

func (r *commandRecorder) InfoContext(_ context.Context, msg string, args ...any) {
	r.record(msg, args)
}

func (r *commandRecorder) record(msg string, args []any) {
	if msg != "process start" {
		return
	}
	for i := 0; i+1 < len(args); i += 2 {
		if key, ok := args[i].(string); ok && key == "command_line" {
			if line, ok := args[i+1].(string); ok {
				r.mu.Lock()
				r.commands = append(r.commands, line)
				r.mu.Unlock()
			}
		}
	}
}

func (r *commandRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = nil
}

func (r *commandRecorder) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

// readOnlySubcommands decides the git subcommands whose every form either only
// inspects (true) or is known to mutate (false). Subcommands carrying both a read
// and a write form are NOT in this map — `worktree` and `config` are decided by
// argv shape in classifyGitCommand, which is the sole classifier.
var readOnlySubcommands = map[string]bool{
	"status": true, "rev-parse": true, "rev-list": true, "for-each-ref": true,
	"show-ref": true, "log": true, "diff": true, "ls-files": true, "cat-file": true,
	"symbolic-ref": true, "branch": false,
}

// readOnlyConfigForms are the `git config` flags that select a read form. Every
// other form — a bare `key value` write, --unset, --add, --replace-all, --edit —
// changes the repository's config.
var readOnlyConfigForms = map[string]bool{
	"--get": true, "--get-all": true, "--get-regexp": true, "--list": true,
}

// gitCommandVerdict is the guard's judgement of one recorded command line.
type gitCommandVerdict int

const (
	gitReadOnly gitCommandVerdict = iota
	gitMutating
	gitUnparseable
)

func (v gitCommandVerdict) String() string {
	switch v {
	case gitReadOnly:
		return "read-only"
	case gitMutating:
		return "mutating"
	default:
		return "unparseable"
	}
}

// classifyGitCommand is the single place that decides whether a recorded git
// command only inspects the repository. Default-deny: an unknown subcommand, an
// unrecognized `config` form, and any `worktree` form other than `worktree list`
// all count as mutating. A false alarm costs the author one justification; a
// missed write is a silent breach of the contract the guard exists to enforce.
//
// DHF-REQ: keel/requirement-113 (keel/ac-407)
func classifyGitCommand(cmd string) gitCommandVerdict {
	fields := strings.Fields(cmd)
	if len(fields) < 2 {
		return gitUnparseable
	}
	switch sub := fields[1]; sub {
	case "worktree":
		if len(fields) > 2 && fields[2] == "list" {
			return gitReadOnly
		}
		return gitMutating
	case "config":
		for _, arg := range fields[2:] {
			if readOnlyConfigForms[arg] {
				return gitReadOnly
			}
		}
		return gitMutating
	default:
		if readOnlySubcommands[sub] {
			return gitReadOnly
		}
		return gitMutating
	}
}

func (r *commandRecorder) assertReadOnly(t *testing.T) {
	t.Helper()
	for _, cmd := range r.recorded() {
		switch classifyGitCommand(cmd) {
		case gitUnparseable:
			t.Errorf("unparseable recorded command %q", cmd)
		case gitMutating:
			t.Errorf("mutating or unexpected git command issued by a read-only path: %q", cmd)
		}
	}
}

// TestReadOnlyGuardIsFormAwareForConfig drives the guard's own classifier over
// the two `git config` forms the package actually issues: the write that stores
// branch.<name>.keel-worktree-base during bring-up, and the --get read the report
// paths make. Both carry the same subcommand token, so a classifier keyed on the
// token alone waves the write through — the ac-407 hole this unit closes. The
// unrecognized forms are here to pin the default-deny posture: anything the
// classifier does not positively recognize as a read counts as a mutation.
//
// DHF-TEST: keel/requirement-113 (keel/ac-407)
func TestReadOnlyGuardIsFormAwareForConfig(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want gitCommandVerdict
	}{
		{"config read on the report path", `git config --get --default  branch.unit-1.keel-worktree-base`, gitReadOnly},
		{"config write from bring-up", "git config branch.unit-1.keel-worktree-base main", gitMutating},
		{"config unset", "git config --unset branch.unit-1.keel-worktree-base", gitMutating},
		{"config add", "git config --add branch.unit-1.keel-worktree-base main", gitMutating},
		{"config list", "git config --list --local", gitReadOnly},
		{"config get-all", "git config --get-all branch.unit-1.keel-worktree-base", gitReadOnly},
		{"config get-regexp", `git config --get-regexp branch\..*\.keel-worktree-base`, gitReadOnly},
		{"bare config", "git config", gitMutating},
		{"worktree list", "git worktree list --porcelain", gitReadOnly},
		{"worktree add", "git worktree add /tmp/unit-1 unit-1", gitMutating},
		{"worktree prune", "git worktree prune", gitMutating},
		{"status", "git status --porcelain", gitReadOnly},
		{"branch delete", "git branch -D unit-1", gitMutating},
		{"unknown subcommand", "git push origin unit-1", gitMutating},
		{"no subcommand", "git", gitUnparseable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &commandRecorder{}
			rec.Info("process start", "command_line", tc.cmd)
			recorded := rec.recorded()
			if len(recorded) != 1 {
				t.Fatalf("recorded = %q, want the one command line", recorded)
			}
			if got := classifyGitCommand(recorded[0]); got != tc.want {
				t.Errorf("classifyGitCommand(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

// mergeQueryMarkers are the argv shapes that would make a decision depend on
// merge state or on a comparison against a base ref.
var mergeQueryMarkers = []string{"merge-base", "--merged", "--no-merged", "--contains", "cherry", ".."}

func (r *commandRecorder) assertNoMergeQuery(t *testing.T) {
	t.Helper()
	for _, cmd := range r.recorded() {
		for _, marker := range mergeQueryMarkers {
			if strings.Contains(cmd, marker) {
				t.Errorf("removal decision consulted merge state: %q contains %q", cmd, marker)
			}
		}
	}
}
