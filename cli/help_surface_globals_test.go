package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
)

// globalSpellings returns every command-line spelling GlobalFlagSpecs
// advertises: "--<name>" for each flag and "-<alias>" for each short alias.
func globalSpellings() []string {
	var out []string
	for _, spec := range cli.GlobalFlagSpecs() {
		out = append(out, "--"+spec.Name)
		if spec.Alias != "" {
			out = append(out, "-"+spec.Alias)
		}
	}
	return out
}

// globalProbeArgv returns argv that exercises one spelling: a value flag gets
// the first member of its value set.
func globalProbeArgv(spec cli.FlagSpec, spelling string) []string {
	if spec.Value == "" {
		return []string{spelling}
	}
	values := spec.Enum
	if len(values) == 0 {
		values = strings.Split(spec.Value, "|")
	}
	return []string{spelling, values[0]}
}

// DHF-TEST: keel/requirement-101 (keel/ac-716)
func TestGlobalFlagSpecsNameEverySpellingParseGlobalConfigAccepts(t *testing.T) {
	advertised := map[string]bool{}
	for _, spec := range cli.GlobalFlagSpecs() {
		spellings := []string{"--" + spec.Name}
		if spec.Alias != "" {
			spellings = append(spellings, "-"+spec.Alias)
		}
		for _, spelling := range spellings {
			advertised[spelling] = true
			argv := globalProbeArgv(spec, spelling)
			if _, words, err := cli.ParseGlobalConfig(argv); err != nil || len(words) != 0 {
				t.Fatalf("ParseGlobalConfig(%q) = words %q, err %v; want the advertised spelling consumed", argv, words, err)
			}
		}
	}
	// Reverse direction: a short spelling the parser consumes must be
	// advertised. Every single-letter form is probed, so an accepted but
	// unlisted alias (the -q/-v class of keel/issue-244) fails here.
	for _, letter := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		spelling := "-" + string(letter)
		_, words, err := cli.ParseGlobalConfig([]string{spelling})
		if err == nil && len(words) == 0 && !advertised[spelling] {
			t.Fatalf("ParseGlobalConfig accepts %q but GlobalFlagSpecs does not advertise it", spelling)
		}
	}
}

// usageBlock returns the rendered lines of the first "Usage:" block, with the
// two-space block indent removed.
func usageBlock(help string) []string {
	var lines []string
	in := false
	for _, line := range strings.Split(help, "\n") {
		if line == "Usage:" {
			in = true
			continue
		}
		if !in {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			break
		}
		lines = append(lines, strings.TrimPrefix(line, "  "))
	}
	return lines
}

// rootSynopsis returns the root synopsis from rendered root help: the first
// usage entry plus its indented continuation lines, joined with spaces.
func rootSynopsis(t *testing.T, help string) string {
	t.Helper()
	block := usageBlock(help)
	if len(block) == 0 {
		t.Fatalf("root help has no Usage: block:\n%s", help)
	}
	parts := []string{block[0]}
	for _, line := range block[1:] {
		if !strings.HasPrefix(line, " ") {
			break
		}
		parts = append(parts, strings.TrimSpace(line))
	}
	return strings.Join(parts, " ")
}

// synopsisSpellings splits a synopsis into its flag spellings.
func synopsisSpellings(synopsis string) map[string]bool {
	out := map[string]bool{}
	for _, token := range strings.FieldsFunc(synopsis, func(r rune) bool {
		return r == ' ' || r == '[' || r == ']' || r == '|'
	}) {
		if strings.HasPrefix(token, "-") {
			out[token] = true
		}
	}
	return out
}

// globalFlagSectionSpellings returns the flag spellings named on the flag rows
// of the rendered "Global flags:" section.
func globalFlagSectionSpellings(t *testing.T, help string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	in := false
	for _, line := range strings.Split(help, "\n") {
		if line == "Global flags:" {
			in = true
			continue
		}
		if !in {
			continue
		}
		if line == "" {
			break
		}
		if !strings.HasPrefix(line, "  -") {
			continue
		}
		for _, token := range strings.Fields(strings.ReplaceAll(line, ",", " ")) {
			if strings.HasPrefix(token, "-") {
				out[token] = true
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("root help has no Global flags: rows:\n%s", help)
	}
	return out
}

func globalSurfaceTree(width int) *cli.CommandSpec {
	tree := &cli.CommandSpec{
		Name: "tool",
		Config: cli.Config{
			Program:      "tool",
			Usage:        "tool <command>",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
		},
		Subcommands: []*cli.CommandSpec{
			{Name: "status", Use: "status", Short: "Show status.", Handler: func(context.Context, []string) error { return nil }},
		},
	}
	tree.SetHelpWidth(width)
	return tree
}

// DHF-TEST: keel/requirement-101 (keel/ac-716)
func TestRootHelpSynopsisAndGlobalFlagsAdvertiseEveryAcceptedSpelling(t *testing.T) {
	for _, width := range []int{0, 40} {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			var help bytes.Buffer
			globalSurfaceTree(width).RenderRootHelp(&help)
			synopsis := rootSynopsis(t, help.String())
			if !strings.HasPrefix(synopsis, "tool ") || !strings.HasSuffix(synopsis, "<command> [args]") {
				t.Fatalf("root synopsis = %q, want \"tool [global flags] <command> [args]\"\n%s", synopsis, help.String())
			}
			inSynopsis := synopsisSpellings(synopsis)
			inSection := globalFlagSectionSpellings(t, help.String())
			for _, spelling := range globalSpellings() {
				if !inSynopsis[spelling] {
					t.Fatalf("root Usage synopsis does not list %q: %q\n%s", spelling, synopsis, help.String())
				}
				if !inSection[spelling] {
					t.Fatalf("Global flags section does not list %q\n%s", spelling, help.String())
				}
			}
		})
	}
}

// DHF-TEST: keel/requirement-101 (keel/ac-724)
// The raw rendered lines are asserted, never a re-joined synopsis: joining
// continuation lines hides the width.
func TestRootHelpSynopsisWrapsToHelpWidthWithoutSplittingFlagGroups(t *testing.T) {
	const width = 40
	var help bytes.Buffer
	globalSurfaceTree(width).RenderRootHelp(&help)
	var synopsis []string
	in := false
	for _, line := range strings.Split(help.String(), "\n") {
		if line == "Usage:" {
			in = true
			continue
		}
		if !in {
			continue
		}
		if len(synopsis) > 0 && !strings.HasPrefix(line, "   ") {
			break
		}
		synopsis = append(synopsis, line)
	}
	if len(synopsis) < 2 {
		t.Fatalf("width-%d root synopsis spans %d line(s), want more than one\n%s", width, len(synopsis), help.String())
	}
	if !strings.HasPrefix(synopsis[0], "  tool ") {
		t.Fatalf("root synopsis first line = %q, want the block indent then the program name\n%s", synopsis[0], help.String())
	}
	continuation := "  " + strings.Repeat(" ", len("tool")+1)
	for i, line := range synopsis {
		if len(line) > width {
			t.Fatalf("root synopsis line %d = %q is %d columns including its indent, want at most %d\n%s", i, line, len(line), width, help.String())
		}
		if i > 0 && (!strings.HasPrefix(line, continuation) || strings.HasPrefix(line, continuation+" ")) {
			t.Fatalf("root synopsis continuation line %d = %q, want it indented past the program name (%d columns)\n%s", i, line, len(continuation), help.String())
		}
		depth := 0
		for _, r := range line {
			switch r {
			case '[':
				depth++
			case ']':
				depth--
			}
			if depth < 0 {
				break
			}
		}
		if depth != 0 {
			t.Fatalf("root synopsis line %d = %q splits a bracketed flag group across lines\n%s", i, line, help.String())
		}
	}
}

// DHF-TEST: keel/requirement-101 (keel/ac-716)
func TestRootHelpSynopsisListsConsumerGlobalsAndOptionalCommandForInvocableRoot(t *testing.T) {
	tree := globalSurfaceTree(0)
	tree.Config.GlobalFlags = []cli.FlagSpec{{Name: "profile", Value: "name", Short: "Binary-specific profile."}}
	tree.Config.RootHandler = func(context.Context, []string) error { return nil }
	var help bytes.Buffer
	tree.RenderRootHelp(&help)
	synopsis := rootSynopsis(t, help.String())
	if !strings.Contains(synopsis, "[--profile name]") {
		t.Fatalf("root synopsis = %q, want the consumer global [--profile name]", synopsis)
	}
	if !strings.HasSuffix(synopsis, "[<command> [args]]") {
		t.Fatalf("root synopsis = %q, want an optional [<command> [args]] for an invocable root", synopsis)
	}
}

// helpJSONInventoryElement is the documented --help-json element shape,
// including the root entry and the flag alias and value-set keys.
type helpJSONInventoryElement struct {
	Path      string                  `json:"path"`
	Kind      string                  `json:"kind"`
	Summary   string                  `json:"summary"`
	Usage     string                  `json:"usage"`
	Invocable *bool                   `json:"invocable"`
	Flags     []helpJSONInventoryFlag `json:"flags"`
}

type helpJSONInventoryFlag struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Value   string   `json:"value"`
	Enum    []string `json:"enum"`
}

func renderInventory(t *testing.T, tree *cli.CommandSpec) []helpJSONInventoryElement {
	t.Helper()
	var buf bytes.Buffer
	if err := tree.RenderHelpJSON(&buf); err != nil {
		t.Fatalf("RenderHelpJSON: %v", err)
	}
	var inventory []helpJSONInventoryElement
	if err := json.Unmarshal(buf.Bytes(), &inventory); err != nil {
		t.Fatalf("parse inventory: %v\n%s", err, buf.String())
	}
	return inventory
}

func inventoryRoot(t *testing.T, inventory []helpJSONInventoryElement) helpJSONInventoryElement {
	t.Helper()
	var roots []helpJSONInventoryElement
	for _, e := range inventory {
		if e.Kind == "root" {
			roots = append(roots, e)
		}
	}
	if len(roots) != 1 {
		t.Fatalf("inventory has %d kind=root elements, want 1: %+v", len(roots), inventory)
	}
	return roots[0]
}

// DHF-TEST: keel/requirement-100 (keel/ac-715)
func TestRenderHelpJSONRootEntryCarriesEveryGlobalFlag(t *testing.T) {
	inventory := renderInventory(t, globalSurfaceTree(0))
	root := inventoryRoot(t, inventory)
	if root.Path != "tool" || root.Usage == "" {
		t.Fatalf("root element path/usage = %q/%q, want program path and synopsis", root.Path, root.Usage)
	}
	if root.Invocable == nil || *root.Invocable {
		t.Fatalf("root invocable = %v, want explicit false for a root without a handler", root.Invocable)
	}
	for _, spec := range cli.GlobalFlagSpecs() {
		idx := slices.IndexFunc(root.Flags, func(f helpJSONInventoryFlag) bool { return f.Name == spec.Name })
		if idx < 0 {
			t.Fatalf("root element has no --%s flag: %+v", spec.Name, root.Flags)
		}
		got := root.Flags[idx]
		var wantAliases []string
		if spec.Alias != "" {
			wantAliases = []string{spec.Alias}
		}
		if !slices.Equal(got.Aliases, wantAliases) {
			t.Fatalf("--%s aliases = %q, want %q", spec.Name, got.Aliases, wantAliases)
		}
		if !slices.Equal(got.Enum, spec.Enum) {
			t.Fatalf("--%s enum = %q, want %q", spec.Name, got.Enum, spec.Enum)
		}
		if spec.Value != "" && len(got.Enum) == 0 {
			t.Fatalf("value flag --%s carries no value set", spec.Name)
		}
	}
	// The root entry is additive: the command elements still number one per
	// command (keel/ac-360).
	commands := 0
	for _, e := range inventory {
		if e.Kind == "command" {
			commands++
		}
	}
	if commands != 1 {
		t.Fatalf("kind=command elements = %d, want 1", commands)
	}
}

// DHF-TEST: keel/requirement-100 (keel/ac-715)
func TestRenderHelpJSONMarksInvocableRootAndDispatchRunsIt(t *testing.T) {
	tree := globalSurfaceTree(0)
	ran := false
	tree.Config.RootHandler = func(context.Context, []string) error {
		ran = true
		return nil
	}
	root := inventoryRoot(t, renderInventory(t, tree))
	if root.Invocable == nil || !*root.Invocable {
		t.Fatalf("root invocable = %v, want true for a root with a handler", root.Invocable)
	}
	if !strings.HasSuffix(root.Usage, "[<command> [args]]") {
		t.Fatalf("root usage = %q, want optional command words", root.Usage)
	}
	if err := tree.Dispatch(context.Background(), nil); err != nil {
		t.Fatalf("Dispatch(no words) with a root handler: %v", err)
	}
	if !ran {
		t.Fatal("Dispatch(no words) did not run the root handler")
	}
}
