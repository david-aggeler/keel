package cli

import (
	"context"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-106
func TestValidateTreeRejectsDepthBeyondTwoCommandTokens(t *testing.T) {
	root := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{
				Name: "admin",
				Subcommands: []*CommandSpec{
					{
						Name: "config",
						Subcommands: []*CommandSpec{
							{Name: "set", Handler: noopHandler},
							{Name: "get", Handler: noopHandler},
						},
					},
					{Name: "status", Handler: noopHandler},
				},
			},
			{Name: "run", Handler: noopHandler},
		},
	}

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a path deeper than two command tokens")
	}
	if !strings.Contains(err.Error(), "admin config set") {
		t.Fatalf("depth error = %q, want offending path named", err.Error())
	}

	valid := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{
				Name: "admin",
				Subcommands: []*CommandSpec{
					{Name: "status", Handler: noopHandler},
					{Name: "repair", Handler: noopHandler},
				},
			},
			{Name: "run", Handler: noopHandler},
		},
	}
	if err := valid.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected a depth-2 tree: %v", err)
	}
}

// DHF-TEST: keel/requirement-154 (keel/ac-635)
func TestValidateTreeAcceptsSingleChildNamespace(t *testing.T) {
	root := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{
				Name: "admin",
				Subcommands: []*CommandSpec{
					{Name: "status", Handler: noopHandler},
				},
			},
			{Name: "run", Handler: noopHandler},
		},
	}

	if err := root.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected a namespace with one child: %v", err)
	}

	root.Subcommands[0].Subcommands = append(root.Subcommands[0].Subcommands, &CommandSpec{Name: "repair", Handler: noopHandler})
	if err := root.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected namespace with two children: %v", err)
	}
}

// TestValidateTreeAcceptsRootWithSingleChildNamespace pins that a root may hold
// exactly one namespace subcommand, the carrier-tree shape a library package
// exports before a consumer grafts it into its own root.
//
// DHF-TEST: keel/requirement-154 (keel/ac-635)
func TestValidateTreeAcceptsRootWithSingleChildNamespace(t *testing.T) {
	carrier := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{
				Name: "admin",
				Subcommands: []*CommandSpec{
					{Name: "status", Handler: noopHandler},
					{Name: "repair", Handler: noopHandler},
				},
			},
		},
	}

	if err := carrier.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected a root holding exactly one subcommand: %v", err)
	}
}

// DHF-TEST: keel/requirement-106
func TestValidateTreeRejectsMixedHandlerAndChildren(t *testing.T) {
	root := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{
				Name:    "admin",
				Handler: noopHandler,
				Subcommands: []*CommandSpec{
					{Name: "status", Handler: noopHandler},
					{Name: "repair", Handler: noopHandler},
				},
			},
			{Name: "run", Handler: noopHandler},
		},
	}

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a node with both handler and children")
	}
	if !strings.Contains(err.Error(), "admin") {
		t.Fatalf("mixed node error = %q, want node named", err.Error())
	}

	root.Subcommands[0].Handler = nil
	if err := root.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected pure namespace and leaf nodes: %v", err)
	}
}

// DHF-TEST: keel/requirement-106
func TestValidateTreeRejectsNilChildWithoutPanicking(t *testing.T) {
	root := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{
				Name: "admin",
				Subcommands: []*CommandSpec{
					{Name: "status", Handler: noopHandler},
					nil,
				},
			},
			{Name: "run", Handler: noopHandler},
		},
	}

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a tree with a nil child command")
	}
	if !strings.Contains(err.Error(), "nil child") {
		t.Fatalf("nil-child error = %q, want it to name the nil child", err.Error())
	}
	if !strings.Contains(err.Error(), "admin") {
		t.Fatalf("nil-child error = %q, want the offending parent named", err.Error())
	}
}

// DHF-TEST: keel/requirement-154 (keel/ac-635)
func TestValidateTreeRejectsNilChildBeforeUseStringVerbResolution(t *testing.T) {
	root := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{
				Name: "admin",
				Use:  "admin missing",
				Subcommands: []*CommandSpec{
					{Name: "status", Handler: noopHandler},
					nil,
				},
			},
		},
	}

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a tree with a nil child command")
	}
	if !strings.Contains(err.Error(), "nil child") {
		t.Fatalf("nil-child error = %q, want the retained nil-child diagnostic", err.Error())
	}
	if !strings.Contains(err.Error(), "admin") {
		t.Fatalf("nil-child error = %q, want the offending parent named", err.Error())
	}
}

// DHF-TEST: keel/requirement-154 (keel/ac-638)
func TestValidateTreeRejectsUseStringEnumeratedVerbWithoutChildNode(t *testing.T) {
	root := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{
				Name:  "admin",
				Use:   "admin missing",
				Short: "Administrative commands.",
				Subcommands: []*CommandSpec{
					{Name: "status", Handler: noopHandler},
					{Name: "repair", Handler: noopHandler},
				},
			},
			{Name: "run", Handler: noopHandler},
		},
	}

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a Use string that enumerates a non-node verb")
	}
	if !strings.Contains(err.Error(), "admin") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("enumerated verb error = %q, want offending path and verb", err.Error())
	}
}

// DHF-TEST: keel/requirement-101 (keel/ac-363)
func TestValidateTreeRejectsCommandThatShadowsHelpOnlyTopic(t *testing.T) {
	root := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{Name: "mode", Use: "mode", Short: "Shadow the help-only topic.", Handler: noopHandler},
		},
	}

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a command that shadows the mode help-only topic")
	}
	if !strings.Contains(err.Error(), "mode") || !strings.Contains(err.Error(), "help-only topic") {
		t.Fatalf("shadowing error = %q, want topic name and collision kind", err.Error())
	}
}

// DHF-TEST: keel/requirement-154 (keel/ac-638)
func TestValidateTreeAllowsResolvedAlternatesAndPlaceholderUseText(t *testing.T) {
	root := &CommandSpec{
		Name:   "tool",
		Config: Config{Program: "tool"},
		Subcommands: []*CommandSpec{
			{
				Name:  "workflow",
				Use:   "workflow inspect|replay",
				Short: "Workflow commands.",
				Subcommands: []*CommandSpec{
					{Name: "inspect", Handler: noopHandler},
					{Name: "replay", Handler: noopHandler},
				},
			},
			{
				Name: "worktree",
				Subcommands: []*CommandSpec{
					{Name: "status", Use: "worktree status <name> | worktree status --glob <pattern>", Handler: noopHandler},
					{Name: "resume", Use: "worktree resume <name>", Handler: noopHandler},
				},
			},
		},
	}

	if err := root.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected resolved alternates or placeholder use text: %v", err)
	}
}

func noopHandler(context.Context, []string) error { return nil }
