package main

import (
	"testing"

	"github.com/david-aggeler/keel/cli"
)

// worktreeLeafVerbs are the leaves keel/ac-408 requires the namespace to carry:
// bring-up (up, resume), tear-down (down), and the two read-only reports
// (status, compare).
var worktreeLeafVerbs = []string{"up", "down", "resume", "status", "compare"}

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
