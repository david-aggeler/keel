package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-30
func TestCLIPackageDocsMeetNarrativeBar(t *testing.T) {
	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{
		"command tree",
		"Dispatch",
		"generated help",
		"Mode",
		"RuntimeConfig",
		"UsageError",
		"exit code 2",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("package docs missing %q", want)
		}
	}
	if !regexp.MustCompile(`DHF-REQ:.*keel/requirement-30`).MatchString(text) {
		t.Fatal("package docs missing DHF-REQ trace for keel/requirement-30")
	}
	exampleSource, err := os.ReadFile("example_test.go")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^func Example`).MatchString(text) && !regexp.MustCompile(`(?m)^func Example`).MatchString(string(exampleSource)) {
		t.Fatal("cli package must include a runnable go doc Example")
	}
}

// DHF-TEST: keel/requirement-21
func TestCommandModelDispatchHelpAndUsageErrors(t *testing.T) {
	var called []string
	root := &CommandSpec{
		Name: "keel-dev",
		Config: Config{
			Program:      "keel-dev",
			RootSummary:  "keel-dev is keel's development CLI.",
			Usage:        "keel-dev [--mode human|ai|json] [-v|--verbose] <command> [args]",
			HelpUsage:    "keel-dev help [command]",
			CommandUsage: "keel-dev <command> --help",
			GlobalFlags: []FlagSpec{
				{Name: "mode", Value: "human|ai|json", Default: "human", Short: "Console mode."},
				{Name: "verbose", Short: "Include debug-level detail."},
			},
			Trailing: "Run keel-dev help <command> for command details.",
		},
		Subcommands: []*CommandSpec{
			{
				Name:  "ci",
				Use:   "ci",
				Short: "Run the verification gate.",
				Handler: func(_ context.Context, args []string) error {
					called = append(called, args...)
					return nil
				},
			},
			{
				Name:  "release",
				Use:   "release vX.Y.Z",
				Short: "Cut a release.",
			},
		},
	}
	root.InheritConfig()

	if child, ok := root.Child("ci"); !ok || child.Name != "ci" {
		t.Fatalf("Child(ci) = %v, %v; want ci, true", child, ok)
	}
	if node, rest, ok := root.Find([]string{"ci"}); !ok || node.Name != "ci" || len(rest) != 0 {
		t.Fatalf("Find(ci) = %v, %v, %v; want ci, empty, true", node, rest, ok)
	}
	if got := root.Usage(nil); got != "usage: keel-dev [--mode human|ai|json] [-v|--verbose] <command> [args]" {
		t.Fatalf("root Usage = %q", got)
	}
	if got := root.Subcommands[1].Usage([]string{"release"}); got != "usage: keel-dev release vX.Y.Z" {
		t.Fatalf("command Usage = %q", got)
	}

	if err := root.Dispatch(context.Background(), []string{"ci", "extra"}); err != nil {
		t.Fatalf("Dispatch(ci): %v", err)
	}
	if strings.Join(called, " ") != "extra" {
		t.Fatalf("handler args = %q, want extra", strings.Join(called, " "))
	}

	// An unrecognized flag-shaped argument is a usage error (exit 2), never
	// coerced into a positional handler argument. DHF-REQ: keel/requirement-21
	flagErr := root.Dispatch(context.Background(), []string{"ci", "--fast"})
	var flagUsage UsageError
	if !errors.As(flagErr, &flagUsage) {
		t.Fatalf("Dispatch(ci --fast) error = %T, want UsageError", flagErr)
	}
	if flagUsage.ExitCode() != 2 {
		t.Fatalf("unknown-flag exit = %d, want 2", flagUsage.ExitCode())
	}

	var help bytes.Buffer
	root.RenderRootHelp(&help)
	for _, want := range []string{
		"keel-dev is keel's development CLI.",
		"keel-dev [--mode human|ai|json] [-v|--verbose] <command> [args]",
		"--mode human|ai|json",
		"ci       Run the verification gate.",
	} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("root help missing %q:\n%s", want, help.String())
		}
	}

	help.Reset()
	if err := root.RenderTopicHelp(&help, []string{"release"}); err != nil {
		t.Fatalf("RenderTopicHelp(release): %v", err)
	}
	for _, want := range []string{
		"release:",
		"keel-dev release vX.Y.Z",
		"Cut a release.",
	} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("topic help missing %q:\n%s", want, help.String())
		}
	}

	err := root.Dispatch(context.Background(), []string{"unknown"})
	var usageErr UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("Dispatch unknown error = %T, want UsageError", err)
	}
	if usageErr.ExitCode() != 2 {
		t.Fatalf("UsageError exit = %d, want 2", usageErr.ExitCode())
	}
}

// DHF-TEST: keel/requirement-111 (keel/ac-392, keel/ac-393)
func TestRenderRootHelpRendersOptionalProgramVersionHeader(t *testing.T) {
	root := &CommandSpec{
		Name: "keel-demo",
		Config: Config{
			Program:      "keel-demo",
			Version:      "1.2.3",
			RootSummary:  "keel-demo runs the showcase.",
			Usage:        "keel-demo [--mode human|ai|json]",
			HelpUsage:    "keel-demo help [command]",
			CommandUsage: "keel-demo <command> --help",
		},
	}

	var help bytes.Buffer
	root.RenderRootHelp(&help)
	lines := strings.Split(help.String(), "\n")
	if got, want := lines[0], "keel-demo v1.2.3"; got != want {
		t.Fatalf("first root-help line = %q, want %q\n%s", got, want, help.String())
	}
	if got, want := lines[2], "keel-demo runs the showcase."; got != want {
		t.Fatalf("root summary line = %q, want %q after version header\n%s", got, want, help.String())
	}

	root.Config.Version = ""
	help.Reset()
	root.RenderRootHelp(&help)
	lines = strings.Split(help.String(), "\n")
	if got, want := lines[0], "keel-demo runs the showcase."; got != want {
		t.Fatalf("first root-help line with empty version = %q, want %q\n%s", got, want, help.String())
	}
	if strings.Contains(help.String(), "keel-demo v") {
		t.Fatalf("root help with empty version includes header:\n%s", help.String())
	}
}

// DHF-TEST: keel/requirement-21
func TestParseGlobalConfigTreatsModeAndNoHeaderAsSharedCore(t *testing.T) {
	cfg, rest, err := ParseGlobalConfig([]string{"--mode", "ai", "--no-header", "ci"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeAI || !cfg.NoHeader {
		t.Fatalf("config = %+v, want mode ai and no-header", cfg)
	}
	if strings.Join(rest, " ") != "ci" {
		t.Fatalf("rest = %q, want ci", strings.Join(rest, " "))
	}
}

// DHF-TEST: keel/requirement-57
func TestParseGlobalConfigRecognizesHelpAllPositionIndependently(t *testing.T) {
	cfg, rest, err := ParseGlobalConfig([]string{"ci", "--help-all", "--mode", "ai", "extra"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HelpAll {
		t.Fatalf("HelpAll = false, want true")
	}
	if cfg.Mode != ModeAI {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, ModeAI)
	}
	if strings.Join(rest, " ") != "ci extra" {
		t.Fatalf("rest = %q, want ci extra", strings.Join(rest, " "))
	}

	cfg, rest, err = ParseGlobalConfig([]string{"--help-all=true"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HelpAll || strings.Join(rest, " ") != "--help-all=true" {
		t.Fatalf("--help-all=true parsed as cfg=%+v rest=%q, want unconsumed unknown flag", cfg, strings.Join(rest, " "))
	}
}

// DHF-TEST: keel/requirement-57
func TestRenderAllHelpEmitsRootAndEveryCommandOnceInTreeOrder(t *testing.T) {
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Usage:        "tool <command>",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
		},
		Subcommands: []*CommandSpec{
			{
				Name:  "parent",
				Short: "Parent command.",
				Subcommands: []*CommandSpec{
					{Name: "beta", Use: "parent beta", Short: "Second."},
					{Name: "alpha", Use: "parent alpha", Short: "First."},
				},
			},
			{Name: "status", Use: "status", Short: "Show status."},
		},
	}

	var first bytes.Buffer
	root.RenderAllHelp(&first)
	var second bytes.Buffer
	root.RenderAllHelp(&second)
	if first.String() != second.String() {
		t.Fatalf("RenderAllHelp is not deterministic:\nfirst:\n%s\nsecond:\n%s", first.String(), second.String())
	}

	got := first.String()
	for _, want := range []string{
		"Usage:\n  tool <command>",
		"parent commands:",
		"parent beta:",
		"parent alpha:",
		"status:",
	} {
		if strings.Count(got, want) != 1 {
			t.Fatalf("RenderAllHelp count(%q) = %d, want 1\n%s", want, strings.Count(got, want), got)
		}
	}
	assertBefore(t, got, "Usage:\n  tool <command>", "parent commands:")
	assertBefore(t, got, "parent commands:", "parent beta:")
	assertBefore(t, got, "parent beta:", "parent alpha:")
	assertBefore(t, got, "parent alpha:", "status:")
}

func assertBefore(t *testing.T, text, earlier, later string) {
	t.Helper()
	earlierAt := strings.Index(text, earlier)
	laterAt := strings.Index(text, later)
	if earlierAt < 0 || laterAt < 0 || earlierAt >= laterAt {
		t.Fatalf("expected %q before %q in:\n%s", earlier, later, text)
	}
}

// DHF-TEST: keel/requirement-100
func TestParseGlobalConfigRecognizesHelpJSONPositionIndependently(t *testing.T) {
	cfg, rest, err := ParseGlobalConfig([]string{"ci", "--help-json", "--mode", "ai", "extra"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HelpJSON {
		t.Fatalf("HelpJSON = false, want true")
	}
	if cfg.Mode != ModeAI {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, ModeAI)
	}
	if strings.Join(rest, " ") != "ci extra" {
		t.Fatalf("rest = %q, want ci extra (--help-json consumed, not left in command words)", strings.Join(rest, " "))
	}
}

// helpJSONElement mirrors the documented element shape of RenderHelpJSON output
// so tests can decode and assert against it without inspecting the renderer's
// internals.
type helpJSONElement struct {
	Path    string   `json:"path"`
	Kind    string   `json:"kind"`
	Summary string   `json:"summary"`
	Usage   string   `json:"usage"`
	Lines   []string `json:"lines"`
	Flags   []struct {
		Name        string `json:"name"`
		Value       string `json:"value"`
		Default     string `json:"default"`
		Description string `json:"description"`
	} `json:"flags"`
}

// DHF-TEST: keel/requirement-100, keel/requirement-101
func TestRenderHelpJSONEmitsFlatArrayOneElementPerCommand(t *testing.T) {
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Usage:        "tool <command>",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
		},
		Subcommands: []*CommandSpec{
			{
				Name:  "parent",
				Short: "Parent command.",
				Subcommands: []*CommandSpec{
					{Name: "beta", Use: "parent beta", Short: "Second.", Flags: []FlagSpec{{Name: "flag", Value: "n", Default: "1", Short: "Declared flag."}}},
					{Name: "alpha", Use: "parent alpha", Short: "First."},
				},
			},
			{Name: "status", Use: "status", Short: "Show status."},
		},
	}
	// Commands across all depths plus the keel-owned help-only mode topic: parent,
	// parent beta, parent alpha, status, mode => 5.
	const wantCount = 5

	var buf bytes.Buffer
	if err := root.RenderHelpJSON(&buf); err != nil {
		t.Fatalf("RenderHelpJSON: %v", err)
	}

	var elems []helpJSONElement
	if err := json.Unmarshal(buf.Bytes(), &elems); err != nil {
		t.Fatalf("output is not a valid JSON array: %v\n%s", err, buf.String())
	}
	if len(elems) != wantCount {
		t.Fatalf("element count = %d, want %d\n%s", len(elems), wantCount, buf.String())
	}

	// Also assert the raw shape has the flags key as an array for every element
	// (a nil slice would marshal to null, not []).
	var rawElems []map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &rawElems); err != nil {
		t.Fatalf("output is not an array of objects: %v", err)
	}
	seen := map[string]bool{}
	for i, e := range elems {
		if e.Path == "" {
			t.Fatalf("element %d has empty path\n%s", i, buf.String())
		}
		if seen[e.Path] {
			t.Fatalf("path %q appears more than once", e.Path)
		}
		seen[e.Path] = true
		for _, key := range []string{"path", "kind", "summary", "usage", "flags"} {
			if _, ok := rawElems[i][key]; !ok {
				t.Fatalf("element %d missing key %q\n%s", i, key, buf.String())
			}
		}
		if !strings.HasPrefix(strings.TrimSpace(string(rawElems[i]["flags"])), "[") {
			t.Fatalf("element %d flags is not a JSON array: %s", i, rawElems[i]["flags"])
		}
		if e.Path == "mode" {
			if e.Kind != "topic" {
				t.Fatalf("mode inventory kind = %q, want topic", e.Kind)
			}
			if e.Summary == "" || e.Usage != "tool help mode" {
				t.Fatalf("mode inventory summary/usage = %q/%q, want topic summary and help usage", e.Summary, e.Usage)
			}
			for _, want := range ModeHelpLines() {
				if !stringSliceContains(e.Lines, want) {
					t.Fatalf("mode inventory lines missing %q: %+v", want, e.Lines)
				}
			}
			continue
		}
		if e.Kind != "command" {
			t.Fatalf("command %q inventory kind = %q, want command", e.Path, e.Kind)
		}
		if len(e.Lines) != 0 {
			t.Fatalf("command %q unexpectedly has topic lines: %+v", e.Path, e.Lines)
		}
	}

	// One element carries the declared flag with all four flag subfields.
	var beta *helpJSONElement
	for i := range elems {
		if elems[i].Path == "parent beta" {
			beta = &elems[i]
		}
	}
	if beta == nil {
		t.Fatalf("expected an element with path %q\n%s", "parent beta", buf.String())
	}
	if len(beta.Flags) != 1 || beta.Flags[0].Name != "flag" || beta.Flags[0].Value != "n" ||
		beta.Flags[0].Default != "1" || beta.Flags[0].Description != "Declared flag." {
		t.Fatalf("parent beta flags = %+v, want one {flag,n,1,Declared flag.}", beta.Flags)
	}

	// Determinism: byte-identical across repeated renders.
	var second bytes.Buffer
	if err := root.RenderHelpJSON(&second); err != nil {
		t.Fatalf("RenderHelpJSON (second): %v", err)
	}
	if buf.String() != second.String() {
		t.Fatalf("RenderHelpJSON not deterministic:\nfirst:\n%s\nsecond:\n%s", buf.String(), second.String())
	}
}

func stringSliceContains(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

// DHF-TEST: keel/requirement-21
func TestCommandHelpersCoverNestedAndErrorPaths(t *testing.T) {
	var called []string
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Usage:        "tool <command>",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
		},
		Subcommands: []*CommandSpec{
			{
				Name:  "parent",
				Short: "Parent command.",
				Subcommands: []*CommandSpec{
					{Name: "beta", Short: "Second.", Flags: []FlagSpec{{Name: "flag", Short: "Declared flag."}}, Handler: func(_ context.Context, args []string) error {
						called = append(called, args...)
						return nil
					}},
					{Name: "alpha", Short: "First."},
				},
			},
		},
	}
	root.InheritConfig()

	if got := root.Subcommands[0].Usage([]string{"parent"}); got != "usage: tool parent beta|alpha" {
		t.Fatalf("nested usage = %q", got)
	}
	if got := SubcommandAlternates(root.Subcommands[0].Subcommands); got != "beta|alpha" {
		t.Fatalf("alternates preserve command-tree order = %q", got)
	}

	var help bytes.Buffer
	if err := root.RenderTopicHelp(&help, []string{"parent"}); err != nil {
		t.Fatalf("RenderTopicHelp(parent): %v", err)
	}
	for _, want := range []string{
		"parent commands:",
		"alpha",
		"beta",
	} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("nested help missing %q:\n%s", want, help.String())
		}
	}

	help.Reset()
	var missing UsageError
	if err := root.RenderTopicHelp(&help, []string{"missing"}); !errors.As(err, &missing) {
		t.Fatalf("RenderTopicHelp(missing) = %v (%T), want UsageError", err, err)
	}
	if !strings.Contains(help.String(), `unknown help topic "missing"`) {
		t.Fatalf("unknown help did not render diagnostic:\n%s", help.String())
	}

	if err := root.Dispatch(context.Background(), nil); err == nil {
		t.Fatal("empty dispatch should return UsageError")
	}
	if err := root.Dispatch(context.Background(), []string{"parent"}); err == nil {
		t.Fatal("command without handler should return UsageError")
	}
	if err := root.Dispatch(context.Background(), []string{"parent", "beta", "--flag"}); err != nil {
		t.Fatalf("nested leaf dispatch: %v", err)
	}
	if strings.Join(called, " ") != "--flag" {
		t.Fatalf("nested handler args = %q, want --flag", strings.Join(called, " "))
	}

	// A declared flag is accepted; an undeclared flag-shaped token is rejected
	// with exit 2 rather than coerced. DHF-REQ: keel/requirement-21
	nope := root.Dispatch(context.Background(), []string{"parent", "beta", "--nope"})
	var nopeUsage UsageError
	if !errors.As(nope, &nopeUsage) || nopeUsage.ExitCode() != 2 {
		t.Fatalf("Dispatch(parent beta --nope) = %v (%T), want UsageError exit 2", nope, nope)
	}

	// The --name=value form is resolved against declared flags the same way:
	// a declared flag is accepted, an undeclared one is rejected with exit 2.
	if err := root.Dispatch(context.Background(), []string{"parent", "beta", "--flag=x"}); err != nil {
		t.Fatalf("declared --flag=x should be accepted: %v", err)
	}
	if got := called[len(called)-1]; got != "--flag=x" {
		t.Fatalf("handler last arg = %q, want --flag=x", got)
	}
	badEq := root.Dispatch(context.Background(), []string{"parent", "beta", "--nope=1"})
	var badEqUsage UsageError
	if !errors.As(badEq, &badEqUsage) || badEqUsage.ExitCode() != 2 {
		t.Fatalf("Dispatch(parent beta --nope=1) = %v (%T), want UsageError exit 2", badEq, badEq)
	}

	specs := SimpleSpecs("tool group", map[string]string{"b": "Bee.", "a": "Aye."})
	if len(specs) != 2 || specs[0].Name != "a" || specs[1].Use != "tool group b" {
		t.Fatalf("SimpleSpecs = %#v", specs)
	}
}

// DHF-TEST: keel/requirement-21
func TestUsageErrorAndGlobalParseErrors(t *testing.T) {
	err := NewUsageError("bad %s", "args")
	if err.Error() != "bad args" {
		t.Fatalf("Error = %q", err.Error())
	}
	if (UsageError{}).Error() != "" {
		t.Fatalf("zero UsageError should render empty text")
	}
	if !errors.Is(fmt.Errorf("wrap: %w", err), err.Err) {
		t.Fatalf("UsageError should unwrap to underlying diagnostic")
	}

	if _, _, err := ParseGlobalConfig([]string{"--mode"}); err == nil {
		t.Fatal("missing --mode value should fail")
	}
	if _, _, err := ParseGlobalConfig([]string{"--mode", "bogus"}); err == nil {
		t.Fatal("unknown --mode should fail")
	}
	if mode, err := ParseMode("HUMAN"); err != nil || mode != ModeHuman {
		t.Fatalf("ParseMode(HUMAN) = %q, %v", mode, err)
	}
}

// DHF-TEST: keel/requirement-21, keel/requirement-152
func TestLegacyCommandRowHelpersAndDefaultUsage(t *testing.T) {
	// A root with no Use and no Config.Usage falls back to the generic
	// "<command> [args]" suffix, but the program token itself has no fallback:
	// it is the root's Config.Program and nothing else. The former assertion
	// here pinned the literal "command" that rootProgram returned for a nameless
	// node; keel/requirement-152 deleted that function rather than relocating
	// the literal.
	root := &CommandSpec{Name: "binary", Config: Config{Program: "tool"}}
	if got := root.Usage(nil); got != "usage: tool <command> [args]" {
		t.Fatalf("default Usage = %q, want %q", got, "usage: tool <command> [args]")
	}

	commands := []*CommandSpec{
		{Name: "beta", Use: "beta", Short: "Second."},
		{Name: "alpha", Use: "alpha", Short: "First."},
	}
	var rows bytes.Buffer
	PrintCommandRows(&rows, commands)
	for _, want := range []string{
		"  beta   Second.",
		"  alpha  First.",
	} {
		if !strings.Contains(rows.String(), want) {
			t.Fatalf("PrintCommandRows missing %q:\n%s", want, rows.String())
		}
	}

	parent := []string{"parent"}
	var nested bytes.Buffer
	RenderSubcommandHelp(&nested, parent, commands, 0)
	got := nested.String()
	assertBefore(t, got, "alpha", "beta")
	for _, want := range []string{
		"  alpha",
		"      First.",
		"  beta",
		"      Second.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderSubcommandHelp missing %q:\n%s", want, got)
		}
	}
}

// globalFlagsSection returns the text of the "Global flags:" paragraph of
// rendered root help, so tests can assert on the flag table without matching
// flag names that also appear in the usage line or output-mode prose. It reads
// only public RenderRootHelp output.
func globalFlagsSection(t *testing.T, help string) string {
	t.Helper()
	for _, para := range strings.Split(help, "\n\n") {
		if strings.HasPrefix(para, "Global flags:") {
			return para
		}
	}
	t.Fatalf("root help has no \"Global flags:\" section:\n%s", help)
	return ""
}

// outputModeSection returns the text of the "Output mode:" paragraph of
// rendered root help, or "" when absent.
func outputModeSection(help string) string {
	for _, para := range strings.Split(help, "\n\n") {
		if strings.HasPrefix(para, "Output mode:") {
			return para
		}
	}
	return ""
}

func rootHelpParagraph(help, heading string) string {
	for _, para := range strings.Split(help, "\n\n") {
		if strings.HasPrefix(para, heading) {
			return para
		}
	}
	return ""
}

// DHF-TEST: keel/requirement-101
//
// ac-362: a Config whose GlobalFlags contributes only binary-specific globals
// (no keel-owned flags) still renders every flag ParseGlobalConfig parses
// exactly once, sourced from keel, plus the consumer's own globals.
func TestRenderRootHelpRendersKeelOwnedGlobalFlagsWithoutConsumerRedeclaration(t *testing.T) {
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Usage:        "tool <command> [args]",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
			// Only a binary-specific global; the consumer declares none of
			// keel's parsed flags.
			GlobalFlags: []FlagSpec{
				{Name: "target", Value: "path", Short: "Repository root to operate on."},
			},
		},
		Subcommands: []*CommandSpec{
			{Name: "ci", Use: "ci", Short: "Run the gate."},
		},
	}

	var help bytes.Buffer
	root.RenderRootHelp(&help)
	section := globalFlagsSection(t, help.String())

	// Every global flag ParseGlobalConfig parses appears exactly once, keel-owned.
	for _, name := range []string{"--mode", "--verbose", "--no-header", "--help", "--help-all", "--help-json", "--version"} {
		// A flag row is "  --name\n" (no value) or "  --name <value>\n"; count
		// both forms so name-prefix aliases (--help vs --help-all) don't collide.
		rowPrefix := "  " + name
		if got := strings.Count(section, rowPrefix+" ") + strings.Count(section, rowPrefix+"\n"); got != 1 {
			t.Fatalf("global flag row %q appears %d times, want exactly 1:\n%s", name, got, section)
		}
	}
	// The consumer's own binary-specific global still renders.
	if !strings.Contains(section, "  --target path") {
		t.Fatalf("consumer global --target missing from Global flags section:\n%s", section)
	}
}

// DHF-TEST: keel/requirement-101
//
// ac-363: a Config supplying no ModeHelp still documents --mode exactly once in
// root help, and the mode topic carries the keel-owned canonical text.
func TestRenderRootHelpDocumentsModeOnceFromKeelText(t *testing.T) {
	var dispatchHelp bytes.Buffer
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Usage:        "tool <command> [args]",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
			HelpWriter:   &dispatchHelp,
			// No ModeHelp, no GlobalFlags: everything mode-related must come
			// from keel.
		},
		Subcommands: []*CommandSpec{
			{Name: "ci", Use: "ci", Short: "Run the gate."},
		},
	}

	var help bytes.Buffer
	root.RenderRootHelp(&help)

	section := globalFlagsSection(t, help.String())
	if got := strings.Count(section, "  --mode"); got != 1 {
		t.Fatalf("--mode flag row appears %d times, want exactly 1:\n%s", got, section)
	}
	if mode := outputModeSection(help.String()); mode != "" {
		t.Fatalf("root help still carries per-value output-mode prose:\n%s", mode)
	}
	if !strings.Contains(help.String(), "Topics:\n  mode") {
		t.Fatalf("root help does not list the mode help-only topic:\n%s", help.String())
	}

	help.Reset()
	if err := root.RenderTopicHelp(&help, []string{"mode"}); err != nil {
		t.Fatalf("RenderTopicHelp(mode): %v", err)
	}
	for _, want := range ModeHelpLines() {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("mode topic missing keel-owned text %q:\n%s", want, help.String())
		}
	}

	if err := root.Dispatch(context.Background(), []string{"help", "mode"}); err != nil {
		t.Fatalf("Dispatch(help mode): %v", err)
	}
	if dispatchHelp.String() != help.String() {
		t.Fatalf("Dispatch(help mode) output:\n%s\nwant:\n%s", dispatchHelp.String(), help.String())
	}
}

// DHF-TEST: keel/requirement-101
//
// Back-compat: a consumer that still re-declares a keel-owned flag or a keel
// mode line is de-duped (rendered once, keel-owned), while genuinely additional
// consumer entries are additive.
func TestRenderRootHelpDeDupesConsumerReDeclaredKeelFlagsAndAppendsExtras(t *testing.T) {
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Usage:        "tool <command> [args]",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
			GlobalFlags: []FlagSpec{
				// Legacy re-declaration of a keel-owned flag: must not double-render.
				{Name: "mode", Value: "human|ai|json", Default: "human", Short: "Console mode."},
				// A genuinely binary-specific global: additive.
				{Name: "target", Value: "path", Short: "Repository root to operate on."},
			},
			ModeHelp: []string{
				// Legacy duplicate of a keel canonical mode line: de-duped.
				"ai emits sparse AI-readable records.",
				// A binary-specific fact: additive.
				"Structured logs are written under .logs/.",
			},
		},
		Subcommands: []*CommandSpec{
			{Name: "ci", Use: "ci", Short: "Run the gate."},
		},
	}

	var rootHelp bytes.Buffer
	root.RenderRootHelp(&rootHelp)
	section := globalFlagsSection(t, rootHelp.String())

	if got := strings.Count(section, "  --mode"); got != 1 {
		t.Fatalf("re-declared --mode rendered %d times, want exactly 1:\n%s", got, section)
	}
	if !strings.Contains(section, "  --target path") {
		t.Fatalf("additive consumer global --target missing:\n%s", section)
	}

	if mode := outputModeSection(rootHelp.String()); mode != "" {
		t.Fatalf("root help still renders mode lines:\n%s", mode)
	}

	var mode bytes.Buffer
	if err := root.RenderTopicHelp(&mode, []string{"mode"}); err != nil {
		t.Fatalf("RenderTopicHelp(mode): %v", err)
	}
	if got := strings.Count(mode.String(), "ai emits sparse AI-readable records."); got != 1 {
		t.Fatalf("duplicate mode line rendered %d times, want exactly 1:\n%s", got, mode.String())
	}
	if !strings.Contains(mode.String(), "Structured logs are written under .logs/.") {
		t.Fatalf("additive consumer mode line missing:\n%s", mode.String())
	}
}

// DHF-TEST: keel/requirement-101 (keel/ac-363)
func TestModeHelpTopicDoesNotLeakIntoCommandSurface(t *testing.T) {
	var renderedHelp bytes.Buffer
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Usage:        "tool <command> [args]",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
			HelpWriter:   &renderedHelp,
		},
		Subcommands: []*CommandSpec{
			{Name: "ci", Use: "ci", Short: "Run the gate.", Handler: noopHandler},
		},
	}

	var rootHelp bytes.Buffer
	root.RenderRootHelp(&rootHelp)
	if commands := rootHelpParagraph(rootHelp.String(), "Commands:"); strings.Contains(commands, "\n  mode  ") {
		t.Fatalf("mode topic rendered under Commands instead of Topics:\n%s", commands)
	}

	err := root.Dispatch(context.Background(), []string{"mode"})
	var usage UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("Dispatch(mode) = %v (%T), want UsageError", err, err)
	}
	if renderedHelp.String() != "" {
		t.Fatalf("Dispatch(mode) rendered help-only topic as a command:\n%s", renderedHelp.String())
	}
}

// DHF-TEST: keel/requirement-101 (keel/ac-363)
func TestRenderAllHelpIncludesModeTopicAfterCommands(t *testing.T) {
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Usage:        "tool <command> [args]",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
		},
		Subcommands: []*CommandSpec{
			{Name: "ci", Use: "ci", Short: "Run the gate."},
		},
	}

	var all bytes.Buffer
	root.RenderAllHelp(&all)
	got := all.String()
	assertBefore(t, got, "ci:", "mode:")
	for _, want := range ModeHelpLines() {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderAllHelp missing mode topic line %q:\n%s", want, got)
		}
	}
}
