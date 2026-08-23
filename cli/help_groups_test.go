package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-105
func TestRenderRootHelpGroupsCommandsInDeclarationOrder(t *testing.T) {
	root := groupedHelpTestRoot()

	var help bytes.Buffer
	root.RenderRootHelp(&help)
	got := help.String()

	for _, want := range []string{
		"Alpha:",
		"  first   First.",
		"  third   Third.",
		"Beta:",
		"  second  Second.",
		"Other:",
		"  plain   Plain.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("root help missing %q:\n%s", want, got)
		}
	}
	assertBefore(t, got, "Alpha:", "Beta:")
	assertBefore(t, got, "Beta:", "Other:")
	assertBefore(t, got, "first   First.", "third   Third.")
}

// DHF-TEST: keel/requirement-105
func TestRenderAllHelpAndHelpJSONReflectGroupsDeterministically(t *testing.T) {
	root := groupedHelpTestRoot()

	var allFirst bytes.Buffer
	root.RenderAllHelp(&allFirst)
	var allSecond bytes.Buffer
	root.RenderAllHelp(&allSecond)
	if allFirst.String() != allSecond.String() {
		t.Fatalf("RenderAllHelp is not deterministic:\nfirst:\n%s\nsecond:\n%s", allFirst.String(), allSecond.String())
	}
	for _, want := range []string{
		"Alpha:",
		"Beta:",
		"Other:",
	} {
		if !strings.Contains(allFirst.String(), want) {
			t.Fatalf("all help missing group heading %q:\n%s", want, allFirst.String())
		}
	}

	var jsonFirst bytes.Buffer
	if err := root.RenderHelpJSON(&jsonFirst); err != nil {
		t.Fatalf("RenderHelpJSON: %v", err)
	}
	var jsonSecond bytes.Buffer
	if err := root.RenderHelpJSON(&jsonSecond); err != nil {
		t.Fatalf("RenderHelpJSON second: %v", err)
	}
	if jsonFirst.String() != jsonSecond.String() {
		t.Fatalf("RenderHelpJSON is not deterministic:\nfirst:\n%s\nsecond:\n%s", jsonFirst.String(), jsonSecond.String())
	}

	var elems []struct {
		Path  string `json:"path"`
		Group string `json:"group"`
	}
	if err := json.Unmarshal(jsonFirst.Bytes(), &elems); err != nil {
		t.Fatalf("RenderHelpJSON output is invalid JSON: %v\n%s", err, jsonFirst.String())
	}
	gotGroups := map[string]string{}
	for _, elem := range elems {
		gotGroups[elem.Path] = elem.Group
	}
	for path, want := range map[string]string{
		"first":  "Alpha",
		"second": "Beta",
		"third":  "Alpha",
		"plain":  "Other",
	} {
		if gotGroups[path] != want {
			t.Fatalf("group for %q = %q, want %q\n%s", path, gotGroups[path], want, jsonFirst.String())
		}
	}
}

func groupedHelpTestRoot() *CommandSpec {
	return &CommandSpec{
		Name: "tool",
		Config: Config{
			Program: "tool",
			Usage:   "tool <command>",
		},
		Subcommands: []*CommandSpec{
			{Name: "first", Use: "first", Group: "Alpha", Short: "First."},
			{Name: "second", Use: "second", Group: "Beta", Short: "Second."},
			{Name: "third", Use: "third", Group: "Alpha", Short: "Third."},
			{Name: "plain", Use: "plain", Short: "Plain."},
		},
	}
}

// DHF-TEST: keel/ac-619
func TestUngroupedCommandListRendersWithoutGroupHeading(t *testing.T) {
	root := ungroupedHelpTestRoot()

	var rootHelp bytes.Buffer
	root.RenderRootHelp(&rootHelp)
	if got := rootHelp.String(); !strings.Contains(got, "Commands:\n  plain     Plain.\n") {
		t.Fatalf("root help does not list commands directly under Commands:\n%s", got)
	}
	if got := rootHelp.String(); strings.Contains(got, "Other:") {
		t.Fatalf("root help contains a synthesized group heading:\n%s", got)
	}

	var topicHelp bytes.Buffer
	root.RenderTopicHelp(&topicHelp, []string{"workflow"})
	if got := topicHelp.String(); !strings.Contains(got, "Subcommands:\n  inspect  Inspect.\n") {
		t.Fatalf("topic help does not list subcommands directly under Subcommands:\n%s", got)
	}
	if got := topicHelp.String(); strings.Contains(got, "Other:") {
		t.Fatalf("topic help contains a synthesized group heading:\n%s", got)
	}
}

// DHF-TEST: keel/ac-620
func TestDeclaredGroupsKeepEveryHeading(t *testing.T) {
	twoGroups := helpTestRootWithSubcommands(
		&CommandSpec{Name: "first", Use: "first", Group: "Alpha", Short: "First."},
		&CommandSpec{Name: "second", Use: "second", Group: "Beta", Short: "Second."},
	)
	var twoHelp bytes.Buffer
	twoGroups.RenderRootHelp(&twoHelp)
	for _, want := range []string{"Alpha:\n  first ", "Beta:\n  second "} {
		if !strings.Contains(twoHelp.String(), want) {
			t.Fatalf("two-group help missing %q:\n%s", want, twoHelp.String())
		}
	}

	oneGroup := helpTestRootWithSubcommands(
		&CommandSpec{Name: "first", Use: "first", Group: "Alpha", Short: "First."},
	)
	var oneHelp bytes.Buffer
	oneGroup.RenderRootHelp(&oneHelp)
	if !strings.Contains(oneHelp.String(), "Alpha:\n  first ") {
		t.Fatalf("single declared group lost its heading:\n%s", oneHelp.String())
	}
}

// DHF-TEST: keel/ac-620
func TestDeclaredGroupNamedOtherKeepsItsHeading(t *testing.T) {
	root := helpTestRootWithSubcommands(
		&CommandSpec{Name: "first", Use: "first", Group: "Other", Short: "First."},
		&CommandSpec{Name: "second", Use: "second", Group: "Other", Short: "Second."},
	)

	var help bytes.Buffer
	root.RenderRootHelp(&help)
	if !strings.Contains(help.String(), "Other:\n  first ") {
		t.Fatalf("declared group named Other lost its heading:\n%s", help.String())
	}
}

// DHF-TEST: keel/ac-620
func TestDeclaredGroupNamedOtherKeepsItsHeadingWhenAnUngroupedCommandLeads(t *testing.T) {
	root := helpTestRootWithSubcommands(
		&CommandSpec{Name: "plain", Use: "plain", Short: "Plain."},
		&CommandSpec{Name: "first", Use: "first", Group: "Other", Short: "First."},
	)

	var help bytes.Buffer
	root.RenderRootHelp(&help)
	if !strings.Contains(help.String(), "Other:\n  plain ") {
		t.Fatalf("declared group named Other lost its heading to a leading ungrouped command:\n%s", help.String())
	}
}

func ungroupedHelpTestRoot() *CommandSpec {
	return &CommandSpec{
		Name: "tool",
		Config: Config{
			Program: "tool",
			Usage:   "tool <command>",
		},
		Subcommands: []*CommandSpec{
			{Name: "plain", Use: "plain", Short: "Plain."},
			{Name: "workflow", Use: "workflow <command>", Short: "Workflow.", Subcommands: []*CommandSpec{
				{Name: "inspect", Use: "inspect", Short: "Inspect."},
				{Name: "replay", Use: "replay", Short: "Replay."},
			}},
		},
	}
}

func helpTestRootWithSubcommands(subcommands ...*CommandSpec) *CommandSpec {
	return &CommandSpec{
		Name: "tool",
		Config: Config{
			Program: "tool",
			Usage:   "tool <command>",
		},
		Subcommands: subcommands,
	}
}
