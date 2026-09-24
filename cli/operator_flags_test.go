package cli_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
	"github.com/david-aggeler/keel/term"
)

// terminalProbe answers "terminal" on every stream, so color, animation and
// prompting would all be permitted without an explicit policy.
type terminalProbe struct{}

func (terminalProbe) IsTerminal(term.Stream) bool { return true }

func (terminalProbe) Size(term.Stream) (term.Size, bool) { return term.Size{Rows: 40, Cols: 120}, true }

func colorTermEnv(k string) string {
	if k == "TERM" {
		return "xterm-256color"
	}
	return ""
}

func parseGlobals(t *testing.T, argv ...string) cli.RuntimeConfig {
	t.Helper()
	cfg, rest, err := cli.ParseGlobalConfig(argv)
	if err != nil {
		t.Fatalf("ParseGlobalConfig(%q) error = %v", argv, err)
	}
	if len(rest) != 0 {
		t.Fatalf("ParseGlobalConfig(%q) left words %q, want none", argv, rest)
	}
	return cfg
}

func resolveOnTerminal(cfg cli.RuntimeConfig) term.Capability {
	tc := cfg.TermConfig(term.Stderr)
	tc.Probe = terminalProbe{}
	tc.Getenv = colorTermEnv
	return term.New(tc)
}

// DHF-TEST: keel/requirement-166
func TestGlobalFlagSpecsAdvertiseOperatorControls(t *testing.T) {
	names := map[string]cli.FlagSpec{}
	for _, spec := range cli.GlobalFlagSpecs() {
		names[spec.Name] = spec
	}
	for _, want := range []string{"quiet", "color", "no-input", "plain"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("GlobalFlagSpecs missing %q", want)
		}
	}
	if names["quiet"].Alias != "q" {
		t.Fatalf("quiet alias = %q, want q", names["quiet"].Alias)
	}
	if got := strings.Join(names["color"].Enum, ","); got != "auto,always,never" {
		t.Fatalf("color enum = %q, want auto,always,never", got)
	}
}

// DHF-TEST: keel/requirement-166 (keel/ac-706)
func TestQuietRaisesConsoleFloorToWarn(t *testing.T) {
	for _, argv := range [][]string{{"-q"}, {"--quiet"}} {
		cfg := parseGlobals(t, argv...)
		if !cfg.Quiet {
			t.Fatalf("%q: Quiet = false", argv)
		}
		for _, base := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn} {
			if got := cfg.ConsoleLevel(base); got != slog.LevelWarn {
				t.Fatalf("%q: ConsoleLevel(%v) = %v, want WARN", argv, base, got)
			}
		}
	}
	if got := parseGlobals(t).ConsoleLevel(slog.LevelInfo); got != slog.LevelInfo {
		t.Fatalf("no flag: ConsoleLevel(INFO) = %v, want INFO", got)
	}
	if got := parseGlobals(t, "-v").ConsoleLevel(slog.LevelInfo); got != slog.LevelDebug {
		t.Fatalf("-v: ConsoleLevel(INFO) = %v, want DEBUG", got)
	}
}

// DHF-TEST: keel/requirement-166
func TestQuietAndVerboseTogetherAreAUsageError(t *testing.T) {
	_, _, err := cli.ParseGlobalConfig([]string{"-q", "--verbose"})
	var usage cli.UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("-q --verbose error = %v, want UsageError", err)
	}
}

// DHF-TEST: keel/requirement-166
func TestColorPolicyParsesEveryValueAndRejectsOthers(t *testing.T) {
	cases := map[string]term.ColorPolicy{"auto": term.ColorAuto, "always": term.ColorAlways, "never": term.ColorNever}
	for value, want := range cases {
		for _, argv := range [][]string{{"--color=" + value}, {"--color", value}} {
			if got := parseGlobals(t, argv...).Color; got != want {
				t.Fatalf("%q: Color = %v, want %v", argv, got, want)
			}
		}
	}
	if got := parseGlobals(t).Color; got != term.ColorAuto {
		t.Fatalf("default Color = %v, want ColorAuto", got)
	}
	for _, argv := range [][]string{{"--color=sometimes"}, {"--color"}, {"--color="}} {
		_, _, err := cli.ParseGlobalConfig(argv)
		var usage cli.UsageError
		if !errors.As(err, &usage) || usage.ExitCode() != 2 {
			t.Fatalf("%q error = %v, want UsageError exit 2", argv, err)
		}
	}
}

// DHF-TEST: keel/requirement-166
func TestColorPolicyThreadsToTerm(t *testing.T) {
	if resolveOnTerminal(parseGlobals(t, "--color=never")).Color() {
		t.Fatal("--color=never on a terminal: Color() = true")
	}
	if !resolveOnTerminal(parseGlobals(t)).Color() {
		t.Fatal("default on a terminal: Color() = false")
	}
	tc := parseGlobals(t, "--color=always").TermConfig(term.Stdout)
	tc.Probe = nopProbe{}
	tc.Getenv = func(string) string { return "" }
	if !term.New(tc).Color() {
		t.Fatal("--color=always off a terminal: Color() = false")
	}
}

type nopProbe struct{}

func (nopProbe) IsTerminal(term.Stream) bool        { return false }
func (nopProbe) Size(term.Stream) (term.Size, bool) { return term.Size{}, false }

// DHF-TEST: keel/requirement-166
func TestNoInputForbidsPrompting(t *testing.T) {
	cfg := parseGlobals(t, "--no-input")
	if !cfg.NoInput {
		t.Fatal("--no-input: NoInput = false")
	}
	if resolveOnTerminal(cfg).Prompt() {
		t.Fatal("--no-input on a terminal: Prompt() = true")
	}
	if !resolveOnTerminal(parseGlobals(t)).Prompt() {
		t.Fatal("default on a terminal: Prompt() = false")
	}
}

// DHF-TEST: keel/requirement-166 (keel/ac-707)
func TestPlainEqualsColorNeverAndNoInputOnATerminal(t *testing.T) {
	base := resolveOnTerminal(parseGlobals(t))
	if !base.Color() || !base.Animation() || !base.Prompt() {
		t.Fatalf("precondition: terminal baseline color=%v animation=%v prompt=%v, want all true", base.Color(), base.Animation(), base.Prompt())
	}
	plain := resolveOnTerminal(parseGlobals(t, "--plain"))
	if plain.Color() || plain.Animation() || plain.Prompt() {
		t.Fatalf("--plain: color=%v animation=%v prompt=%v, want all false", plain.Color(), plain.Animation(), plain.Prompt())
	}
	primitives := resolveOnTerminal(parseGlobals(t, "--color=never", "--no-input"))
	if primitives.Color() != plain.Color() || primitives.Animation() != plain.Animation() || primitives.Prompt() != plain.Prompt() {
		t.Fatalf("--color=never --no-input: color=%v animation=%v prompt=%v, want equal to --plain", primitives.Color(), primitives.Animation(), primitives.Prompt())
	}
	if got := parseGlobals(t, "--plain").EffectiveColor(); got != term.ColorNever {
		t.Fatalf("--plain EffectiveColor = %v, want ColorNever", got)
	}
}

// DHF-TEST: keel/requirement-166
func TestCommandDeclaredOperatorFlagNamesStayCommandWords(t *testing.T) {
	root := &cli.CommandSpec{
		Name:   "tool",
		Config: cli.Config{Program: "tool"},
		Subcommands: []*cli.CommandSpec{{
			Name:  "fmt",
			Flags: []cli.FlagSpec{{Name: "color", Value: "x"}, {Name: "quiet", Alias: "q"}},
		}},
	}
	cfg, words, err := root.ParseGlobalConfig([]string{"fmt", "--color=red", "-q"})
	if err != nil {
		t.Fatalf("ParseGlobalConfig error = %v", err)
	}
	if cfg.Quiet || cfg.Color != term.ColorAuto {
		t.Fatalf("command-declared flags parsed as globals: %+v", cfg)
	}
	if strings.Join(words, " ") != "fmt --color=red -q" {
		t.Fatalf("words = %q", words)
	}
}

// Help wrapping is in the unit's Scope; no in-focus criterion states it.
func TestHelpWidthWrapsFlagDescriptions(t *testing.T) {
	long := "Select the console output protocol for this very long description that must wrap."
	root := &cli.CommandSpec{
		Name:   "tool",
		Config: cli.Config{Program: "tool", Usage: "tool <command>"},
		Subcommands: []*cli.CommandSpec{{
			Name:    "run",
			Short:   "Run it.",
			Flags:   []cli.FlagSpec{{Name: "long", Short: long}},
			Handler: func(context.Context, []string) error { return nil },
		}},
	}
	var buf bytes.Buffer
	cmd := root.Subcommands[0]
	root.InheritConfig()
	root.SetHelpWidth(40)
	cmd.RenderCommandHelp(&buf, []string{"run"})
	for _, line := range strings.Split(buf.String(), "\n") {
		if len(line) > 40 {
			t.Fatalf("line exceeds HelpWidth 40 (%d): %q\n%s", len(line), line, buf.String())
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(buf.String()), " "), long) {
		t.Fatalf("wrapped help lost words:\n%s", buf.String())
	}
}
