package main

import (
	"strings"
	"testing"
)

// worktreeNameLeaves are the verb-family leaves that take a work-item name as
// their positional argument.
var worktreeNameLeaves = []string{"up", "resume", "down", "branch-delete", "status", "compare"}

// malformedWorkItemNames are the four grammar violations the wrapper scripts
// used to reject in shell, paired with the diagnostic token each produced. The
// tokens are part of the scripts' contract (keel/ac-409), so whichever layer
// owns the grammar has to keep emitting them.
var malformedWorkItemNames = []struct {
	label string
	name  string
	token string
}{
	{"bad kind", "nope-1-alpha", "invalid kind"},
	{"bad slug", "cr-1-Alpha", "invalid slug"},
	{"bad seq", "cr-x-alpha", "invalid seq"},
	{"slug too long", "cr-1-" + strings.Repeat("a", 101), "slug too long"},
}

// TestWorktreeVerbsRejectMalformedWorkItemName pins the delegate as the decider
// of work-item-name validity: every name-taking leaf rejects a violation of the
// <kind>-<seq>-<slug> grammar with the invalid-argument status 64 and the same
// diagnostic token the wrapper scripts used to print, before any lifecycle work.
//
// DHF-TEST: keel/requirement-114 (keel/ac-421)
func TestWorktreeVerbsRejectMalformedWorkItemName(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	for _, leaf := range worktreeNameLeaves {
		for _, tc := range malformedWorkItemNames {
			out, logs, code := env.runWithLogs(t, "worktree", leaf, tc.name)
			label := "worktree " + leaf + " " + tc.label
			if code != 64 {
				t.Errorf("%s: exit %d, want 64\noutput: %q\nlogs: %s", label, code, out, logs)
			}
			if out != "" {
				t.Errorf("%s: emitted a result line %q, want none", label, out)
			}
			if !strings.Contains(logs, tc.token) {
				t.Errorf("%s: diagnostics do not carry %q\nlogs: %s", label, tc.token, logs)
			}
		}
	}
}

// TestWorktreeVerbsAcceptRunnerWorkItemName pins the slug as optional. The
// autonomous run-queue tail owns worktrees named cr-<seq> with no slug at all
// (CLAUDE.md, the automated-change-request skill), and this repository's own
// checkouts are named that way, so the grammar the wrappers can never produce a
// name for must stay reachable through the binary.
//
// DHF-TEST: keel/requirement-114 (keel/ac-421)
func TestWorktreeVerbsAcceptRunnerWorkItemName(t *testing.T) {
	env := newWorktreeVerbEnv(t)
	out, code := env.run(t, "worktree", "up", "cr-333")
	assertVerb(t, "up on a runner-owned cr-<seq> name", out, code, "up cr-333 "+env.path("cr-333")+"\n", 0)

	for _, name := range []string{"epic-4", "story-71", "cr-1-alpha"} {
		out, code := env.run(t, "worktree", "status", name)
		if code != 0 {
			t.Errorf("status %s: exit %d, want 0 (output %q)", name, code, out)
		}
	}
}
