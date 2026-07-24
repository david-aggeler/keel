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
