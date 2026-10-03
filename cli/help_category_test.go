package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-105
func TestRenderRootHelpCategorizesCommandsInDeclarationOrder(t *testing.T) {
	root := categorizedHelpTestRoot()

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
func TestRenderAllHelpAndHelpJSONReflectHelpCategoriesDeterministically(t *testing.T) {
	root := categorizedHelpTestRoot()

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
			t.Fatalf("all help missing help-category heading %q:\n%s", want, allFirst.String())
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
		Path         string `json:"path"`
		HelpCategory string `json:"help_category"`
	}
	if err := json.Unmarshal(jsonFirst.Bytes(), &elems); err != nil {
		t.Fatalf("RenderHelpJSON output is invalid JSON: %v\n%s", err, jsonFirst.String())
	}
	gotCategories := map[string]string{}
	for _, elem := range elems {
		gotCategories[elem.Path] = elem.HelpCategory
	}
	for path, want := range map[string]string{
		"first":  "Alpha",
		"second": "Beta",
		"third":  "Alpha",
		"plain":  "Other",
	} {
		if gotCategories[path] != want {
			t.Fatalf("help_category for %q = %q, want %q\n%s", path, gotCategories[path], want, jsonFirst.String())
		}
	}
}

func categorizedHelpTestRoot() *CommandSpec {
	return &CommandSpec{
		Name: "tool",
		Config: Config{
			Program: "tool",
			Usage:   "tool <command>",
		},
		Subcommands: []*CommandSpec{
			{Name: "first", Use: "first", HelpCategory: "Alpha", Short: "First."},
			{Name: "second", Use: "second", HelpCategory: "Beta", Short: "Second."},
			{Name: "third", Use: "third", HelpCategory: "Alpha", Short: "Third."},
			{Name: "plain", Use: "plain", Short: "Plain."},
		},
	}
}

// DHF-TEST: keel/ac-619
func TestUncategorizedCommandListRendersWithoutHeading(t *testing.T) {
	root := uncategorizedHelpTestRoot()

	var rootHelp bytes.Buffer
	root.RenderRootHelp(&rootHelp)
	if got := rootHelp.String(); !strings.Contains(got, "Commands:\n  plain     Plain.\n") {
		t.Fatalf("root help does not list commands directly under Commands:\n%s", got)
	}
	if got := rootHelp.String(); strings.Contains(got, "Other:") {
		t.Fatalf("root help contains a synthesized help-category heading:\n%s", got)
	}

	var topicHelp bytes.Buffer
	if err := root.RenderTopicHelp(&topicHelp, []string{"workflow"}); err != nil {
		t.Fatalf("RenderTopicHelp: %v", err)
	}
	if got := topicHelp.String(); !strings.Contains(got, "Subcommands:\n  inspect  Inspect.\n") {
		t.Fatalf("topic help does not list subcommands directly under Subcommands:\n%s", got)
	}
	if got := topicHelp.String(); strings.Contains(got, "Other:") {
		t.Fatalf("topic help contains a synthesized help-category heading:\n%s", got)
	}
}

// DHF-TEST: keel/ac-620
func TestDeclaredHelpCategoriesKeepEveryHeading(t *testing.T) {
	twoCategories := helpTestRootWithSubcommands(
		&CommandSpec{Name: "first", Use: "first", HelpCategory: "Alpha", Short: "First."},
		&CommandSpec{Name: "second", Use: "second", HelpCategory: "Beta", Short: "Second."},
	)
	var twoHelp bytes.Buffer
	twoCategories.RenderRootHelp(&twoHelp)
	for _, want := range []string{"Alpha:\n  first ", "Beta:\n  second "} {
		if !strings.Contains(twoHelp.String(), want) {
			t.Fatalf("two-category help missing %q:\n%s", want, twoHelp.String())
		}
	}

	oneCategory := helpTestRootWithSubcommands(
		&CommandSpec{Name: "first", Use: "first", HelpCategory: "Alpha", Short: "First."},
	)
	var oneHelp bytes.Buffer
	oneCategory.RenderRootHelp(&oneHelp)
	if !strings.Contains(oneHelp.String(), "Alpha:\n  first ") {
		t.Fatalf("single declared help category lost its heading:\n%s", oneHelp.String())
	}
}

// DHF-TEST: keel/ac-620
func TestDeclaredHelpCategoryNamedOtherKeepsItsHeading(t *testing.T) {
	root := helpTestRootWithSubcommands(
		&CommandSpec{Name: "first", Use: "first", HelpCategory: "Other", Short: "First."},
		&CommandSpec{Name: "second", Use: "second", HelpCategory: "Other", Short: "Second."},
	)

	var help bytes.Buffer
	root.RenderRootHelp(&help)
	if !strings.Contains(help.String(), "Other:\n  first ") {
		t.Fatalf("declared help category named Other lost its heading:\n%s", help.String())
	}
}

// DHF-TEST: keel/ac-620
func TestDeclaredHelpCategoryNamedOtherKeepsItsHeadingWhenAnUncategorizedCommandLeads(t *testing.T) {
	root := helpTestRootWithSubcommands(
		&CommandSpec{Name: "plain", Use: "plain", Short: "Plain."},
		&CommandSpec{Name: "first", Use: "first", HelpCategory: "Other", Short: "First."},
	)

	var help bytes.Buffer
	root.RenderRootHelp(&help)
	if !strings.Contains(help.String(), "Other:\n  plain ") {
		t.Fatalf("declared help category named Other lost its heading to a leading uncategorized command:\n%s", help.String())
	}
}

func uncategorizedHelpTestRoot() *CommandSpec {
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

// DHF-TEST: keel/ac-771
func TestTopicHelpAndHelpJSONCarryDeclaredHelpCategories(t *testing.T) {
	root := helpTestRootWithSubcommands(
		&CommandSpec{Name: "workflow", Use: "workflow <command>", Short: "Workflow.", Subcommands: []*CommandSpec{
			{Name: "compile", Use: "compile", HelpCategory: "Build", Short: "Compile."},
			{Name: "unit", Use: "unit", HelpCategory: "Test", Short: "Unit."},
			{Name: "link", Use: "link", HelpCategory: "Build", Short: "Link."},
		}},
	)

	var topicHelp bytes.Buffer
	if err := root.RenderTopicHelp(&topicHelp, []string{"workflow"}); err != nil {
		t.Fatalf("RenderTopicHelp: %v", err)
	}
	got := topicHelp.String()
	for _, want := range []string{"Build:\n  compile  Compile.\n  link     Link.\n", "Test:\n  unit     Unit.\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("topic help missing %q:\n%s", want, got)
		}
	}
	assertBefore(t, got, "Build:", "Test:")

	var helpJSON bytes.Buffer
	if err := root.RenderHelpJSON(&helpJSON); err != nil {
		t.Fatalf("RenderHelpJSON: %v", err)
	}
	var elems []map[string]any
	if err := json.Unmarshal(helpJSON.Bytes(), &elems); err != nil {
		t.Fatalf("RenderHelpJSON output is invalid JSON: %v\n%s", err, helpJSON.String())
	}
	want := map[string]string{"workflow compile": "Build", "workflow unit": "Test", "workflow link": "Build"}
	for _, elem := range elems {
		path, _ := elem["path"].(string)
		if category, ok := want[path]; ok {
			if elem["help_category"] != category {
				t.Fatalf("help_category for %q = %v, want %q\n%s", path, elem["help_category"], category, helpJSON.String())
			}
			delete(want, path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("--help-json lacks entries %v:\n%s", want, helpJSON.String())
	}
}

// DHF-TEST: keel/ac-772
func TestHelpCategoryHasNoGroupAlias(t *testing.T) {
	if _, ok := reflect.TypeOf(CommandSpec{}).FieldByName("Group"); ok {
		t.Fatal("CommandSpec still declares a Group field; HelpCategory is the only help-heading label")
	}

	var helpJSON bytes.Buffer
	if err := categorizedHelpTestRoot().RenderHelpJSON(&helpJSON); err != nil {
		t.Fatalf("RenderHelpJSON: %v", err)
	}
	var elems []map[string]any
	if err := json.Unmarshal(helpJSON.Bytes(), &elems); err != nil {
		t.Fatalf("RenderHelpJSON output is invalid JSON: %v\n%s", err, helpJSON.String())
	}
	sawCategory := false
	for _, elem := range elems {
		if _, ok := elem["group"]; ok {
			t.Fatalf("--help-json entry %v carries a group key:\n%s", elem["path"], helpJSON.String())
		}
		if _, ok := elem["help_category"]; ok {
			sawCategory = true
		}
	}
	if !sawCategory {
		t.Fatalf("--help-json carries no help_category key at all; the group-key check proves nothing:\n%s", helpJSON.String())
	}
}
