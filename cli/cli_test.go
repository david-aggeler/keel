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

	if err := root.Dispatch(context.Background(), []string{"ci", "--fast"}); err != nil {
		t.Fatalf("Dispatch(ci): %v", err)
	}
	if strings.Join(called, " ") != "--fast" {
		t.Fatalf("handler args = %q, want --fast", strings.Join(called, " "))
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
					{Name: "beta", Short: "Second.", Handler: func(_ context.Context, args []string) error {
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
