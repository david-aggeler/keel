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

// DHF-TEST: keel/requirement-106 (keel/ac-382)
func TestValidateTreeRejectsSingleChildNamespace(t *testing.T) {
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

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a namespace with one child")
	}
	if !strings.Contains(err.Error(), "admin") {
		t.Fatalf("single-child namespace error = %q, want namespace named", err.Error())
	}

	root.Subcommands[0].Subcommands = append(root.Subcommands[0].Subcommands, &CommandSpec{Name: "repair", Handler: noopHandler})
	if err := root.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected namespace with two children: %v", err)
	}
}

// TestValidateTreeExemptsTheRootFromTheTwoChildRule pins the root carve-out of
// the two-child rule: the carrier-tree shape a library package exports — a root
// holding exactly one namespace subcommand, which a consumer grafts into its own
// root — passes ValidateTree. The opposite direction, a namespace *below* the
// root holding exactly one child, is pinned by
// TestValidateTreeRejectsSingleChildNamespace; the two together are the both-way
// pin the exemption needs, so deleting or widening the !root guard reddens one
// of them.
//
// The single subcommand is itself a well-formed two-child namespace so that the
// two-child rule is the only rule in play: a bare childless, handler-less child
// would trip the neither-namespace-nor-leaf rule instead and prove nothing.
//
// DHF-TEST: keel/requirement-106 (keel/ac-382)
func TestValidateTreeExemptsTheRootFromTheTwoChildRule(t *testing.T) {
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

func noopHandler(context.Context, []string) error { return nil }
