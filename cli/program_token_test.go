package cli

import (
	"bytes"
	"strings"
	"testing"
)

// programTokenTree builds one otherwise-valid three-deep command tree whose
// every node Name differs from the root program token. The differing names are
// what make the assertions below discriminating: a tree whose nodes happened to
// share the root program would render the same token even with a per-node
// fallback in place.
func programTokenTree() *CommandSpec {
	return &CommandSpec{
		Name: "binary",
		Config: Config{
			Program:     "tool",
			Version:     "1.2.3",
			RootSummary: "tool runs the showcase.",
			Usage:       "tool <command> [args]",
		},
		Subcommands: []*CommandSpec{
			{
				Name:  "grp",
				Short: "Parent command with nested help.",
				Subcommands: []*CommandSpec{
					{Name: "leaf", Use: "grp leaf <arg>", Short: "Leaf verb.", Handler: noopHandler},
					{Name: "other", Short: "Second leaf.", Handler: noopHandler},
				},
			},
			{Name: "run", Short: "Root-level verb.", Handler: noopHandler},
		},
	}
}

// DHF-TEST: keel/requirement-152 (keel/ac-628)
func TestValidateTreeRejectsRootWithoutConfigProgram(t *testing.T) {
	root := programTokenTree()
	root.Config.Program = ""

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a root whose Config.Program is empty")
	}
	if !strings.Contains(err.Error(), "Config.Program") {
		t.Fatalf("empty-program error = %q, want the message to name Config.Program", err.Error())
	}

	// Positive control: the same tree with the field set still passes, so the
	// rejection above is attributable to Config.Program and not to some other
	// invariant this fixture happens to violate.
	if err := programTokenTree().ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected a tree whose root sets Config.Program: %v", err)
	}
}

// DHF-TEST: keel/requirement-152 (keel/ac-631)
func TestDescendantConfigProgramDoesNotOverrideTheRoot(t *testing.T) {
	root := programTokenTree()
	root.Subcommands[0].Config.Program = "usurper"

	var help bytes.Buffer
	root.RenderTopicHelp(&help, []string{"grp"})
	got := help.String()

	if strings.Contains(got, "usurper") {
		t.Fatalf("child help page carries the child's own program token\n%s", got)
	}
	first, _, _ := strings.Cut(got, "\n")
	if first != "tool v1.2.3" {
		t.Fatalf("child identity line = %q, want %q\n%s", first, "tool v1.2.3", got)
	}
	if root.Subcommands[0].Config.Program != "tool" {
		t.Fatalf("child Config.Program after inheritance = %q, want %q",
			root.Subcommands[0].Config.Program, "tool")
	}
}

// DHF-TEST: keel/requirement-152 (keel/ac-629)
func TestIdentityLineNamesTheRootProgramOnEveryPage(t *testing.T) {
	root := programTokenTree()
	for _, tc := range []struct {
		name string
		path []string
	}{
		{name: "root", path: nil},
		{name: "group", path: []string{"grp"}},
		{name: "leaf", path: []string{"grp", "leaf"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var help bytes.Buffer
			root.RenderTopicHelp(&help, tc.path)
			got := help.String()
			first, _, _ := strings.Cut(got, "\n")
			if first != "tool v1.2.3" {
				t.Fatalf("identity line = %q, want %q\n%s", first, "tool v1.2.3", got)
			}
		})
	}
}

// DHF-TEST: keel/requirement-152 (keel/ac-630)
func TestLeafUsageLineOpensWithTheRootProgram(t *testing.T) {
	root := programTokenTree()

	var help bytes.Buffer
	root.RenderTopicHelp(&help, []string{"grp", "leaf"})
	got := help.String()

	_, after, found := strings.Cut(got, "Usage:\n")
	if !found {
		t.Fatalf("leaf help page has no Usage: block\n%s", got)
	}
	usage := strings.TrimSpace(strings.SplitN(after, "\n", 2)[0])
	if !strings.HasPrefix(usage, "tool ") {
		t.Fatalf("leaf usage line = %q, want it to open with the root program %q\n%s",
			usage, "tool", got)
	}
	if usage != "tool grp leaf <arg>" {
		t.Fatalf("leaf usage line = %q, want %q\n%s", usage, "tool grp leaf <arg>", got)
	}
}
