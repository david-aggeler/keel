package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

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
	root.RenderTopicHelp(&help, []string{"release"})
	for _, want := range []string{
		"release commands:",
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
	root.RenderTopicHelp(&help, []string{"parent"})
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
	root.RenderTopicHelp(&help, []string{"missing"})
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
