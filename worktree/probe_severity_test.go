package worktree_test

import (
	"context"
	"sync"
	"testing"

	"github.com/david-aggeler/keel/worktree"
)

// errorRecorder is a worktree.Logger that keeps every Error record.
type errorRecorder struct {
	mu     sync.Mutex
	errors []string
}

func (r *errorRecorder) Debug(string, ...any) {}
func (r *errorRecorder) Info(string, ...any)  {}
func (r *errorRecorder) InfoContext(context.Context, string, ...any) {
}
func (r *errorRecorder) Error(msg string, _ ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, msg)
}

// A probe whose non-zero exit is the answer — here "origin/HEAD does not
// resolve" in a repository with no remote — records no Error: the exit status
// decides severity, and for a probe the caller has said a miss is not a
// failure.
//
// DHF-TEST: keel/requirement-24
func TestProbeMissIsNotRecordedAsAnError(t *testing.T) {
	root := newRepo(t)
	rec := &errorRecorder{}
	m := newManager(t, worktree.Config{RepoRoot: root, Logger: rec})
	if _, err := m.Up(context.Background(), "unit-1"); err != nil {
		t.Fatalf("up: %v", err)
	}
	if len(rec.errors) != 0 {
		t.Fatalf("Error records = %v, want none: the origin/HEAD probe miss is an answer", rec.errors)
	}
}
