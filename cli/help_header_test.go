package cli

import (
	"bytes"
	"strings"
	"testing"
)

// helpHeaderTree builds one command tree holding a root, a group node with a
// subcommand, and a leaf node with none, so every help-header assertion below
// reads the same three shapes out of a single declaration.
func helpHeaderTree(version string) *CommandSpec {
	return &CommandSpec{
		Name: "keel-demo",
		Config: Config{
			Program:     "keel-demo",
			Version:     version,
			RootSummary: "keel-demo runs the log and exec showcase.",
			Usage:       "keel-demo <command> [args]",
		},
		Subcommands: []*CommandSpec{
			{
				Name:  "workflow",
				Short: "Parent command with nested help.",
				Subcommands: []*CommandSpec{
					{Name: "inspect", Short: "Preview a captured run tree."},
				},
			},
		},
	}
}

func renderHelpPage(t *testing.T, root *CommandSpec, path ...string) string {
	t.Helper()
	var help bytes.Buffer
	root.RenderTopicHelp(&help, path)
	return help.String()
}

// DHF-TEST: keel/requirement-111
func TestHelpPagesEmitProgramVersionIdentityLineOnEveryTopic(t *testing.T) {
	root := helpHeaderTree("1.2.3")
	for _, tc := range []struct {
		name string
		path []string
	}{
		{name: "root", path: nil},
		{name: "group", path: []string{"workflow"}},
		{name: "leaf", path: []string{"workflow", "inspect"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderHelpPage(t, root, tc.path...)
			first, _, _ := strings.Cut(got, "\n")
			if first != "keel-demo v1.2.3" {
				t.Fatalf("first help line = %q, want %q\n%s", first, "keel-demo v1.2.3", got)
			}
		})
	}
}

// DHF-TEST: keel/requirement-111
func TestHelpPagesOmitIdentityLineWhenConfigVersionIsEmpty(t *testing.T) {
	root := helpHeaderTree("")
	for _, tc := range []struct {
		name string
		path []string
	}{
		{name: "root", path: nil},
		{name: "group", path: []string{"workflow"}},
		{name: "leaf", path: []string{"workflow", "inspect"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderHelpPage(t, root, tc.path...)
			if strings.Contains(got, "keel-demo v") {
				t.Fatalf("help page carries a version identity line with an empty Config.Version:\n%s", got)
			}
		})
	}
}
