package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func diagnosticsTree() *CommandSpec {
	noop := func(context.Context, []string) error { return nil }
	return &CommandSpec{
		Config: Config{Program: "tool", Usage: "tool <command> [args]"},
		Subcommands: []*CommandSpec{
			{Name: "worktree", Short: "Manage worktrees.", Handler: noop},
			{Name: "release", Short: "Cut a release.", Handler: noop},
			{
				Name:  "gate",
				Short: "Run one gate stage.",
				Subcommands: []*CommandSpec{
					{Name: "gofmt", Short: "Check formatting.", Handler: noop},
					{Name: "vet", Short: "Run go vet.", Handler: noop},
				},
			},
		},
	}
}

func dispatchUsageError(t *testing.T, args ...string) string {
	t.Helper()
	err := diagnosticsTree().Dispatch(context.Background(), args)
	var usage UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("Dispatch(%q) err = %v, want UsageError", args, err)
	}
	if usage.ExitCode() != 2 {
		t.Fatalf("Dispatch(%q) exit = %d, want 2", args, usage.ExitCode())
	}
	return err.Error()
}

// DHF-TEST: keel/requirement-169 (keel/ac-719)
func TestDispatchReportsUnconsumedDashTokenAsUnknownFlag(t *testing.T) {
	for _, args := range [][]string{{"--frob"}, {"-x"}, {"gate", "--frob"}} {
		msg := dispatchUsageError(t, args...)
		flag := args[len(args)-1]
		if !strings.Contains(msg, `unknown flag "`+flag+`"`) {
			t.Fatalf("Dispatch(%q) = %q, want unknown flag %q", args, msg, flag)
		}
		if strings.Contains(msg, "unknown command") {
			t.Fatalf("Dispatch(%q) = %q, must not say unknown command", args, msg)
		}
	}
}

// DHF-TEST: keel/requirement-169 (keel/ac-720)
func TestDispatchSuggestsNearestSiblingWithinBound(t *testing.T) {
	cases := []struct {
		args    []string
		suggest string
	}{
		// distance 1
		{args: []string{"worktre"}, suggest: "worktree"},
		// distance 2: the bound, inclusive
		{args: []string{"rolase"}, suggest: "release"},
		{args: []string{"wroktree"}, suggest: "worktree"},
		// suggestion among a group's children
		{args: []string{"gate", "gofnt"}, suggest: "gofmt"},
	}
	for _, tc := range cases {
		msg := dispatchUsageError(t, tc.args...)
		if !strings.Contains(msg, `unknown command "`+tc.args[len(tc.args)-1]+`"`) {
			t.Fatalf("Dispatch(%q) = %q, want unknown command", tc.args, msg)
		}
		if !strings.Contains(msg, `did you mean "`+tc.suggest+`"?`) {
			t.Fatalf("Dispatch(%q) = %q, want suggestion %q", tc.args, msg, tc.suggest)
		}
	}
	// distance 3: one past the bound, and a word far from every sibling.
	for _, args := range [][]string{{"relxxxe"}, {"frobnicate"}, {"gate", "frobnicate"}} {
		msg := dispatchUsageError(t, args...)
		if strings.Contains(msg, "did you mean") {
			t.Fatalf("Dispatch(%q) = %q, want no suggestion", args, msg)
		}
	}
}

// DHF-TEST: keel/requirement-169 (keel/ac-720)
func TestNearestCommandBound(t *testing.T) {
	siblings := []*CommandSpec{{Name: "ci"}, {Name: "release"}}
	if got, ok := nearestCommand("rel", siblings); ok {
		t.Fatalf("nearestCommand(rel) = %q, want none: distance 4 exceeds the bound", got)
	}
	if got, ok := nearestCommand("x", siblings); ok {
		t.Fatalf("nearestCommand(x) = %q, want none: the edit rewrites the whole candidate", got)
	}
	if got, ok := nearestCommand("releas", siblings); !ok || got != "release" {
		t.Fatalf("nearestCommand(releas) = %q, %v, want release", got, ok)
	}
	if got := suggestionMaxDistance; got != 2 {
		t.Fatalf("suggestionMaxDistance = %d, want 2", got)
	}
}

// DHF-TEST: keel/requirement-169 (keel/ac-721)
func TestDispatchBareGroupShowsConciseHelp(t *testing.T) {
	msg := dispatchUsageError(t, "gate")
	for _, want := range []string{"usage: tool gate gofmt|vet", "Subcommands:", "gofmt  Check formatting.", "vet    Run go vet."} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Dispatch(gate) = %q, missing %q", msg, want)
		}
	}
}

// DHF-TEST: keel/requirement-169 (keel/ac-721)
func TestDispatchBareGroupExplicitHelpUnchanged(t *testing.T) {
	var help strings.Builder
	tree := diagnosticsTree()
	tree.Config.HelpWriter = &help
	if err := tree.Dispatch(context.Background(), []string{"gate", "--help"}); err != nil {
		t.Fatalf("Dispatch(gate --help) err = %v, want nil", err)
	}
	if !strings.Contains(help.String(), "Subcommands:") {
		t.Fatalf("gate --help = %q, want command help", help.String())
	}
}
