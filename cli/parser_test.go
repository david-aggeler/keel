package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-153 (keel/ac-633)
func TestDispatchRendersHelpForDeepestNamedNode(t *testing.T) {
	var help bytes.Buffer
	handlerCalled := false
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Usage:        "tool <command>",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
			HelpWriter:   &help,
		},
		Subcommands: []*CommandSpec{{
			Name:  "group",
			Short: "Grouped commands.",
			Subcommands: []*CommandSpec{{
				Name:  "leaf",
				Short: "Leaf command.",
				Handler: func(context.Context, []string) error {
					handlerCalled = true
					return nil
				},
			}},
		}},
	}

	tests := []struct {
		name string
		args []string
		path []string
	}{
		{name: "root long", args: []string{"--help"}},
		{name: "root short", args: []string{"-h"}},
		{name: "group long", args: []string{"group", "--help"}, path: []string{"group"}},
		{name: "group short", args: []string{"group", "-h"}, path: []string{"group"}},
		{name: "leaf long", args: []string{"group", "leaf", "--help"}, path: []string{"group", "leaf"}},
		{name: "leaf short", args: []string{"group", "leaf", "-h"}, path: []string{"group", "leaf"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			help.Reset()
			handlerCalled = false
			var want bytes.Buffer
			if len(tt.path) == 0 {
				root.RenderRootHelp(&want)
			} else {
				node, _, ok := root.Find(tt.path)
				if !ok {
					t.Fatalf("fixture path %q did not resolve", strings.Join(tt.path, " "))
				}
				node.RenderCommandHelp(&want, tt.path)
			}

			if err := root.Dispatch(context.Background(), tt.args); err != nil {
				t.Fatalf("Dispatch(%q): %v", strings.Join(tt.args, " "), err)
			}
			if handlerCalled {
				t.Fatalf("Dispatch(%q) invoked the handler", strings.Join(tt.args, " "))
			}
			if help.String() != want.String() {
				t.Fatalf("Dispatch(%q) help:\n%s\nwant:\n%s", strings.Join(tt.args, " "), help.String(), want.String())
			}
		})
	}
}

// DHF-TEST: keel/requirement-104
func TestDispatchParsesTypedFlagsWithDefaultsBeforeHandler(t *testing.T) {
	var name string
	var verbose bool
	var capturedName string
	var capturedVerbose bool

	root := parserTestRoot(&CommandSpec{
		Name: "run",
		Use:  "run",
		Flags: []FlagSpec{
			{Name: "name", Value: "text", Default: "guest", StringTarget: &name},
			{Name: "verbose", BoolTarget: &verbose},
		},
		Handler: func(_ context.Context, args []string) error {
			capturedName = name
			capturedVerbose = verbose
			if len(args) != 0 {
				t.Fatalf("handler args = %q, want no flag args", strings.Join(args, " "))
			}
			return nil
		},
	})

	if err := root.Dispatch(context.Background(), []string{"run", "--verbose"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if capturedName != "guest" || !capturedVerbose {
		t.Fatalf("bound values before handler = %q, %v; want guest, true", capturedName, capturedVerbose)
	}
}

// DHF-TEST: keel/requirement-104
func TestDispatchAcceptsGNUFlagSpellingsAndEndOfOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "long equals", args: []string{"run", "--mode=ai"}, want: "mode=ai alpha=false beta=false args="},
		{name: "long value", args: []string{"run", "--mode", "ai"}, want: "mode=ai alpha=false beta=false args="},
		{name: "short value", args: []string{"run", "-m", "ai"}, want: "mode=ai alpha=false beta=false args="},
		{name: "bundled bools", args: []string{"run", "-ab"}, want: "mode=human alpha=true beta=true args="},
		{name: "interspersed", args: []string{"run", "operand", "--mode", "ai"}, want: "mode=ai alpha=false beta=false args=operand"},
		{name: "end of options", args: []string{"run", "--", "--mode", "-ab"}, want: "mode=human alpha=false beta=false args=--mode -ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mode string
			var alpha bool
			var beta bool
			var got string
			root := parserTestRoot(&CommandSpec{
				Name: "run",
				Use:  "run [args]",
				Flags: []FlagSpec{
					{Name: "mode", Alias: "m", Default: "human", StringTarget: &mode},
					{Name: "alpha", Alias: "a", BoolTarget: &alpha},
					{Name: "beta", Alias: "b", BoolTarget: &beta},
				},
				Handler: func(_ context.Context, args []string) error {
					got = "mode=" + mode + " alpha=" + boolText(alpha) + " beta=" + boolText(beta) + " args=" + strings.Join(args, " ")
					return nil
				},
			})

			if err := root.Dispatch(context.Background(), tt.args); err != nil {
				t.Fatalf("Dispatch(%q): %v", strings.Join(tt.args, " "), err)
			}
			if got != tt.want {
				t.Fatalf("captured = %q, want %q", got, tt.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-104
func TestDispatchValidatesPositionalArityBeforeHandler(t *testing.T) {
	var called bool
	root := parserTestRoot(&CommandSpec{
		Name:        "show",
		Use:         "show <id>",
		Positionals: []PositionalSpec{{Name: "id", Min: 1, Max: 1}},
		Handler: func(_ context.Context, args []string) error {
			called = true
			if strings.Join(args, " ") != "abc" {
				t.Fatalf("handler args = %q, want abc", strings.Join(args, " "))
			}
			return nil
		},
	})

	for _, args := range [][]string{{"show"}, {"show", "abc", "extra"}} {
		called = false
		err := root.Dispatch(context.Background(), args)
		var usage UsageError
		if !errors.As(err, &usage) || usage.ExitCode() != 2 {
			t.Fatalf("Dispatch(%q) = %v (%T), want UsageError exit 2", strings.Join(args, " "), err, err)
		}
		if called {
			t.Fatalf("handler called for invalid args %q", strings.Join(args, " "))
		}
		if !strings.Contains(err.Error(), "usage: tool show <id>") {
			t.Fatalf("arity error missing usage line: %v", err)
		}
	}

	if err := root.Dispatch(context.Background(), []string{"show", "abc"}); err != nil {
		t.Fatalf("Dispatch valid positional: %v", err)
	}
	if !called {
		t.Fatal("handler was not called for valid positional")
	}
}

// DHF-TEST: keel/requirement-104
func TestDispatchRejectsEnumAndRequiredFlagViolations(t *testing.T) {
	var mode string
	root := parserTestRoot(&CommandSpec{
		Name: "run",
		Use:  "run --mode human|ai",
		Flags: []FlagSpec{
			{Name: "mode", Value: "human|ai", Enum: []string{"human", "ai"}, Required: true, StringTarget: &mode},
		},
		Handler: func(context.Context, []string) error {
			t.Fatal("handler should not run for invalid flag input")
			return nil
		},
	})

	for _, tt := range []struct {
		args []string
		want string
	}{
		{args: []string{"run", "--mode", "json"}, want: "human, ai"},
		{args: []string{"run"}, want: "required flag --mode"},
	} {
		err := root.Dispatch(context.Background(), tt.args)
		var usage UsageError
		if !errors.As(err, &usage) || usage.ExitCode() != 2 {
			t.Fatalf("Dispatch(%q) = %v (%T), want UsageError exit 2", strings.Join(tt.args, " "), err, err)
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("error %q missing %q", err.Error(), tt.want)
		}
	}
}

// DHF-TEST: keel/requirement-104
func TestDispatchParsesBoolValuesAndRepeatableStringLists(t *testing.T) {
	var dryRun bool
	var tags []string
	var got string
	root := parserTestRoot(&CommandSpec{
		Name: "run",
		Use:  "run",
		Flags: []FlagSpec{
			{Name: "dry-run", BoolTarget: &dryRun},
			{Name: "tag", Value: "name", Repeatable: true, StringSliceTarget: &tags},
		},
		Handler: func(_ context.Context, args []string) error {
			got = "dry-run=" + boolText(dryRun) + " tags=" + strings.Join(tags, ",") + " args=" + strings.Join(args, " ")
			return nil
		},
	})

	if err := root.Dispatch(context.Background(), []string{"run", "--dry-run=false", "--tag", "one", "--tag=two", "operand"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got != "dry-run=false tags=one,two args=operand" {
		t.Fatalf("captured = %q", got)
	}
}

// DHF-TEST: keel/requirement-104
func TestDispatchReportsFlagParserEdgeCases(t *testing.T) {
	var mode string
	root := parserTestRoot(&CommandSpec{
		Name: "run",
		Use:  "run",
		Flags: []FlagSpec{
			{Name: "mode", Alias: "m", StringTarget: &mode},
			{Name: "debug", Alias: "d", BoolTarget: new(bool)},
			{Name: "legacy"},
		},
		Handler: func(context.Context, []string) error { return nil },
	})

	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing long value", args: []string{"run", "--mode"}, want: "--mode requires a value"},
		{name: "missing short value", args: []string{"run", "-m"}, want: "-m requires a value"},
		{name: "string flag inside bundle", args: []string{"run", "-dm"}, want: "flag -m requires a separate value"},
		{name: "unknown short", args: []string{"run", "-x"}, want: `unknown flag "-x"`},
		{name: "invalid bool value", args: []string{"run", "--debug=maybe"}, want: `invalid bool flag value "maybe"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := root.Dispatch(context.Background(), tt.args)
			var usage UsageError
			if !errors.As(err, &usage) {
				t.Fatalf("Dispatch(%q) = %v (%T), want UsageError", strings.Join(tt.args, " "), err, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q missing %q", err.Error(), tt.want)
			}
		})
	}

	var got []string
	root.Subcommands[0].Handler = func(_ context.Context, args []string) error {
		got = append([]string{}, args...)
		return nil
	}
	if err := root.Dispatch(context.Background(), []string{"run", "--legacy", "operand"}); err != nil {
		t.Fatalf("help-only flag passthrough: %v", err)
	}
	if strings.Join(got, " ") != "--legacy operand" {
		t.Fatalf("handler args = %q, want help-only flag passthrough", strings.Join(got, " "))
	}
}

// DHF-TEST: keel/requirement-104
func TestDispatchValidatesMinimumAndRangePositionals(t *testing.T) {
	root := parserTestRoot(&CommandSpec{
		Name:        "copy",
		Use:         "copy <src> [dst]",
		Positionals: []PositionalSpec{{Name: "path", Min: 1, Max: 2}},
		Handler:     func(context.Context, []string) error { return nil },
	})
	if err := root.Dispatch(context.Background(), []string{"copy", "a", "b"}); err != nil {
		t.Fatalf("range positionals: %v", err)
	}
	err := root.Dispatch(context.Background(), []string{"copy", "a", "b", "c"})
	if err == nil || !strings.Contains(err.Error(), "1-2 path") {
		t.Fatalf("range arity error = %v, want range text", err)
	}

	root.Subcommands[0].Positionals = []PositionalSpec{{Name: "path", Min: 1, Max: -1}}
	err = root.Dispatch(context.Background(), []string{"copy"})
	if err == nil || !strings.Contains(err.Error(), "at least 1 path") {
		t.Fatalf("minimum arity error = %v, want minimum text", err)
	}
}

// DHF-TEST: keel/requirement-153 (keel/ac-632)
func TestDeclaredGlobalFlagNamesReachCommandHandler(t *testing.T) {
	tests := []struct {
		name  string
		flag  FlagSpec
		forms [][]string
	}{
		{name: "mode", flag: FlagSpec{Name: "mode"}, forms: [][]string{{"--mode", "13"}, {"--mode=13"}}},
		{name: "verbose", flag: FlagSpec{Name: "verbose"}, forms: [][]string{{"--verbose", "13"}, {"--verbose=13"}}},
		{name: "verbose alias", flag: FlagSpec{Name: "verbose", Alias: "v"}, forms: [][]string{{"-v", "13"}}},
		{name: "no-header", flag: FlagSpec{Name: "no-header"}, forms: [][]string{{"--no-header", "13"}, {"--no-header=13"}}},
		{name: "help", flag: FlagSpec{Name: "help"}, forms: [][]string{{"--help", "13"}, {"--help=13"}}},
		{name: "help alias", flag: FlagSpec{Name: "help", Alias: "h"}, forms: [][]string{{"-h", "13"}}},
		{name: "help-all", flag: FlagSpec{Name: "help-all"}, forms: [][]string{{"--help-all", "13"}, {"--help-all=13"}}},
		{name: "help-json", flag: FlagSpec{Name: "help-json"}, forms: [][]string{{"--help-json", "13"}, {"--help-json=13"}}},
		{name: "version", flag: FlagSpec{Name: "version"}, forms: [][]string{{"--version", "13"}, {"--version=13"}}},
	}
	for _, tt := range tests {
		for _, form := range tt.forms {
			t.Run(tt.name+" "+strings.Join(form, " "), func(t *testing.T) {
				var parsed string
				var handled string
				flag := tt.flag
				flag.Value = "value"
				flag.StringTarget = &parsed
				root := parserTestRoot(&CommandSpec{
					Name:  "run",
					Use:   "run",
					Flags: []FlagSpec{flag},
					Handler: func(context.Context, []string) error {
						handled = parsed
						return nil
					},
				})
				if err := root.ValidateTree(); err != nil {
					t.Fatalf("ValidateTree rejected declared global name: %v", err)
				}

				argv := append([]string{"run"}, form...)
				cfg, words, err := root.ParseGlobalConfig(argv)
				if err != nil {
					t.Fatalf("ParseGlobalConfig(%q): %v", strings.Join(argv, " "), err)
				}
				if cfg != (RuntimeConfig{Mode: ModeHuman}) {
					t.Fatalf("ParseGlobalConfig(%q) cfg = %+v, want globals unset", strings.Join(argv, " "), cfg)
				}
				if err := root.Dispatch(context.Background(), words); err != nil {
					t.Fatalf("Dispatch(%q): %v", strings.Join(words, " "), err)
				}
				if handled != "13" {
					t.Fatalf("handler parsed %q, want 13", handled)
				}
			})
		}
	}
}

// DHF-TEST: keel/requirement-104
// A second Dispatch on the same tree must not inherit typed flag values bound by
// an earlier Dispatch: defaults are re-applied and repeatable slices are reset
// before each parse, so bound targets carry only the current invocation's values.
func TestDispatchDoesNotLeakFlagValuesAcrossCalls(t *testing.T) {
	var name string
	var dry bool
	var ids []string
	var gotName string
	var gotDry bool
	var gotIDs []string

	root := parserTestRoot(&CommandSpec{
		Name: "run",
		Use:  "run",
		Flags: []FlagSpec{
			{Name: "name", Value: "text", Default: "guest", StringTarget: &name},
			{Name: "dry", BoolTarget: &dry},
			{Name: "id", Value: "id", Repeatable: true, StringSliceTarget: &ids},
		},
		Handler: func(_ context.Context, _ []string) error {
			gotName, gotDry, gotIDs = name, dry, append([]string(nil), ids...)
			return nil
		},
	})

	if err := root.Dispatch(context.Background(), []string{"run", "--name", "alice", "--dry", "--id", "a", "--id", "b"}); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if gotName != "alice" || !gotDry || strings.Join(gotIDs, ",") != "a,b" {
		t.Fatalf("first call bound values = %q, %v, %v; want alice, true, [a b]", gotName, gotDry, gotIDs)
	}

	// Second call sets none of the flags: every target must revert to its default,
	// not retain the first call's values.
	if err := root.Dispatch(context.Background(), []string{"run", "--id", "c"}); err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}
	if gotName != "guest" {
		t.Fatalf("second call leaked string flag: name = %q, want default %q", gotName, "guest")
	}
	if gotDry {
		t.Fatal("second call leaked bool flag: dry = true, want reset to false")
	}
	if strings.Join(gotIDs, ",") != "c" {
		t.Fatalf("second call leaked repeatable flag: id = %v, want [c]", gotIDs)
	}
}

func parserTestRoot(child *CommandSpec) *CommandSpec {
	return &CommandSpec{
		Name: "tool",
		Config: Config{
			Program: "tool",
			Usage:   "tool <command> [args]",
		},
		Subcommands: []*CommandSpec{child},
	}
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
