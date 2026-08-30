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
	if err := root.RenderTopicHelp(&help, path); err != nil {
		t.Fatalf("RenderTopicHelp(%q): %v", strings.Join(path, " "), err)
	}
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

// DHF-TEST: keel/requirement-149
func TestHelpPagesShareOneHeaderOrderingAcrossRootGroupAndLeaf(t *testing.T) {
	root := helpHeaderTree("1.2.3")
	for _, tc := range []struct {
		name    string
		path    []string
		summary string
	}{
		{name: "root", path: nil, summary: "keel-demo runs the log and exec showcase."},
		{name: "group", path: []string{"workflow"}, summary: "Parent command with nested help."},
		{name: "leaf", path: []string{"workflow", "inspect"}, summary: "Preview a captured run tree."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderHelpPage(t, root, tc.path...)
			identity := strings.Index(got, "keel-demo v1.2.3")
			summary := strings.Index(got, tc.summary)
			usage := strings.Index(got, "Usage:")
			if identity != 0 {
				t.Fatalf("identity line index = %d, want 0\n%s", identity, got)
			}
			if summary < 0 || usage < 0 {
				t.Fatalf("summary index = %d, usage index = %d, want both present\n%s", summary, usage, got)
			}
			if !(identity < summary && summary < usage) {
				t.Fatalf("header order = identity %d, summary %d, usage %d; want identity < summary < usage\n%s",
					identity, summary, usage, got)
			}
		})
	}
}

// DHF-TEST: keel/requirement-149
func TestLeafHelpTitleCarriesNoCommandsWord(t *testing.T) {
	root := helpHeaderTree("1.2.3")
	got := renderHelpPage(t, root, "workflow", "inspect")
	header, _, _ := strings.Cut(got, "Usage:")
	if strings.Contains(header, "commands") {
		t.Fatalf("leaf help header names commands on a node that has none:\n%s", got)
	}
	if !strings.Contains(header, "workflow inspect") {
		t.Fatalf("leaf help header does not name the command path:\n%s", got)
	}
}

// DHF-TEST: keel/requirement-149
func TestGroupHelpStillNamesTheCommandsItCarries(t *testing.T) {
	root := helpHeaderTree("1.2.3")
	got := renderHelpPage(t, root, "workflow")
	for _, want := range []string{"workflow commands:", "Subcommands:", "inspect"} {
		if !strings.Contains(got, want) {
			t.Fatalf("group help missing %q:\n%s", want, got)
		}
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
