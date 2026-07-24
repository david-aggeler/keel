package cli

import (
	"context"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-106
func TestValidateTreeRejectsDepthBeyondTwoCommandTokens(t *testing.T) {
	root := &CommandSpec{
		Name: "tool",
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
		Name: "tool",
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

// DHF-TEST: keel/requirement-106
func TestValidateTreeRejectsSingleChildNamespace(t *testing.T) {
	root := &CommandSpec{
		Name: "tool",
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

// DHF-TEST: keel/requirement-106
func TestValidateTreeRejectsMixedHandlerAndChildren(t *testing.T) {
	root := &CommandSpec{
		Name: "tool",
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

func noopHandler(context.Context, []string) error { return nil }
