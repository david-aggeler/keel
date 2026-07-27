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

// readOnlySubcommands are the git subcommands that only inspect. A worktree
// subcommand is read-only exactly when it is `worktree list`.
var readOnlySubcommands = map[string]bool{
	"status": true, "rev-parse": true, "rev-list": true, "for-each-ref": true,
	"show-ref": true, "log": true, "diff": true, "ls-files": true, "cat-file": true, "config": true,
	"symbolic-ref": true, "branch": false,
}

func (r *commandRecorder) assertReadOnly(t *testing.T) {
	t.Helper()
	for _, cmd := range r.recorded() {
		fields := strings.Fields(cmd)
		if len(fields) < 2 {
			t.Errorf("unparseable recorded command %q", cmd)
			continue
		}
		sub := fields[1]
		if sub == "worktree" {
			if len(fields) < 3 || fields[2] != "list" {
				t.Errorf("mutating git command issued by a read-only path: %q", cmd)
			}
			continue
		}
		if !readOnlySubcommands[sub] {
			t.Errorf("mutating or unexpected git command issued by a read-only path: %q", cmd)
		}
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
