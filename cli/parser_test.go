package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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
func TestValidateTreeRejectsCommandGlobalFlagCollisions(t *testing.T) {
	root := parserTestRoot(&CommandSpec{
		Name: "run",
		Use:  "run",
		Flags: []FlagSpec{
			{Name: "mode", StringTarget: new(string)},
		},
		Handler: func(context.Context, []string) error { return nil },
	})

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted command flag colliding with global --mode")
	}
	if !strings.Contains(err.Error(), "run") || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("collision error = %q, want command and flag named", err.Error())
	}

	root.Subcommands[0].Flags = []FlagSpec{{Name: "custom", Alias: "h", BoolTarget: new(bool)}}
	err = root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted command alias colliding with global -h")
	}
	if !strings.Contains(err.Error(), "run") || !strings.Contains(err.Error(), "h") {
		t.Fatalf("alias collision error = %q, want command and alias named", err.Error())
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
