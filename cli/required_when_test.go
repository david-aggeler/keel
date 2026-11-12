package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// requiredWhenFixture is the CR-311 fixture: leaf run declares --b (Enum x, y,
// z) and --a, required when --b is x or y. called reports handler runs.
func requiredWhenFixture(bDefault string, extra ...FlagSpec) (*CommandSpec, *bool) {
	var a, b string
	called := false
	flags := []FlagSpec{
		{Name: "b", Value: "mode", Enum: []string{"x", "y", "z"}, Default: bDefault, StringTarget: &b},
		{Name: "a", Value: "id", StringTarget: &a, RequiredWhen: []FlagCondition{{Flag: "b", In: []string{"x", "y"}}}},
	}
	root := parserTestRoot(&CommandSpec{
		Name:  "run",
		Use:   "run",
		Flags: append(flags, extra...),
		Handler: func(context.Context, []string) error {
			called = true
			return nil
		},
	})
	return root, &called
}

// helpJSONFlagsByName decodes RenderHelpJSON and returns the raw flag elements
// of the command at path, keyed by flag name.
func helpJSONFlagsByName(t *testing.T, root *CommandSpec, path string) map[string]map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := root.RenderHelpJSON(&out); err != nil {
		t.Fatalf("RenderHelpJSON: %v", err)
	}
	var inventory []struct {
		Path  string           `json:"path"`
		Flags []map[string]any `json:"flags"`
	}
	if err := json.Unmarshal(out.Bytes(), &inventory); err != nil {
		t.Fatalf("help-json is not a JSON array: %v\n%s", err, out.String())
	}
	for _, e := range inventory {
		if e.Path != path {
			continue
		}
		flags := map[string]map[string]any{}
		for _, f := range e.Flags {
			name, _ := f["name"].(string)
			flags[name] = f
		}
		return flags
	}
	t.Fatalf("help-json has no element %q:\n%s", path, out.String())
	return nil
}

// DHF-TEST: keel/requirement-186 (keel/ac-808)
func TestHelpJSONFlagCarriesRequired(t *testing.T) {
	var x, y string
	root := parserTestRoot(&CommandSpec{
		Name: "run",
		Use:  "run",
		Flags: []FlagSpec{
			{Name: "x", Value: "v", Required: true, StringTarget: &x},
			{Name: "y", Value: "v", StringTarget: &y},
		},
		Handler: noopHandler,
	})

	flags := helpJSONFlagsByName(t, root, "run")
	if got, ok := flags["x"]["required"].(bool); !ok || !got {
		t.Fatalf("x required = %#v, want true", flags["x"]["required"])
	}
	if got, ok := flags["y"]["required"].(bool); !ok || got {
		t.Fatalf("y required = %#v, want false", flags["y"]["required"])
	}
}

// DHF-TEST: keel/requirement-187 (keel/ac-810)
func TestDispatchRejectsMissingConditionallyRequiredFlag(t *testing.T) {
	root, called := requiredWhenFixture("")

	err := root.Dispatch(context.Background(), []string{"run", "--b", "x"})
	var usage UsageError
	if !errors.As(err, &usage) || usage.ExitCode() != 2 || *called {
		t.Fatalf("Dispatch: err=%v (%T) called=%v, want UsageError exit 2 and no handler run", err, err, *called)
	}
	if !strings.Contains(err.Error(), "required flag --a missing (required when --b is x)") {
		t.Fatalf("error = %q, want conditional requirement diagnostic", err.Error())
	}
}

// DHF-TEST: keel/requirement-187 (keel/ac-811)
func TestDispatchAcceptsConditionNotHolding(t *testing.T) {
	root, called := requiredWhenFixture("")

	if err := root.Dispatch(context.Background(), []string{"run", "--b", "z"}); err != nil || !*called {
		t.Fatalf("Dispatch: err=%v called=%v, want handler run and exit 0", err, *called)
	}
}

// DHF-TEST: keel/requirement-187 (keel/ac-812)
func TestDispatchConditionMatchesConditionFlagDefault(t *testing.T) {
	root, called := requiredWhenFixture("x")

	err := root.Dispatch(context.Background(), []string{"run"})
	var usage UsageError
	if !errors.As(err, &usage) || usage.ExitCode() != 2 || *called {
		t.Fatalf("Dispatch: err=%v (%T) called=%v, want UsageError exit 2 and no handler run", err, err, *called)
	}
	if !strings.Contains(err.Error(), "required flag --a missing (required when --b is x)") {
		t.Fatalf("error = %q, want conditional requirement diagnostic", err.Error())
	}
}

// DHF-TEST: keel/requirement-187 (keel/ac-813)
func TestDispatchAbsentConditionFlagWithoutDefaultMatchesNothing(t *testing.T) {
	root, called := requiredWhenFixture("")
	// A consumer-preset target must not count as a supplied value.
	*root.Subcommands[0].Flags[0].StringTarget = "x"

	if err := root.Dispatch(context.Background(), []string{"run"}); err != nil || !*called {
		t.Fatalf("Dispatch: err=%v called=%v, want handler run and exit 0", err, *called)
	}
}

// DHF-TEST: keel/requirement-186 (keel/ac-814)
func TestHelpJSONFlagCarriesRequiredWhen(t *testing.T) {
	root, _ := requiredWhenFixture("")

	flags := helpJSONFlagsByName(t, root, "run")
	want := []any{map[string]any{"flag": "b", "in": []any{"x", "y"}}}
	if got := flags["a"]["required_when"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("a required_when = %#v, want %#v", got, want)
	}
	if got, ok := flags["a"]["required"].(bool); !ok || got {
		t.Fatalf("a required = %#v, want false", flags["a"]["required"])
	}
	if _, ok := flags["b"]["required_when"]; ok {
		t.Fatalf("b carries required_when: %#v", flags["b"])
	}
}

// DHF-TEST: keel/requirement-187 (keel/ac-815)
func TestCommandHelpStatesFlagRequirements(t *testing.T) {
	var c string
	root, _ := requiredWhenFixture("", FlagSpec{Name: "c", Value: "n", Short: "Count.", Required: true, StringTarget: &c})
	var help bytes.Buffer
	root.SetHelpOutput(&help, &help)

	if err := root.Dispatch(context.Background(), []string{"help", "run"}); err != nil {
		t.Fatalf("Dispatch(help run): %v", err)
	}
	lines := strings.Split(help.String(), "\n")
	for flag, suffix := range map[string]string{
		"  --a id": "(required when --b is x or y)",
		"  --c n":  "(required)",
	} {
		at := -1
		for i, line := range lines {
			if line == flag {
				at = i
			}
		}
		if at < 0 || at+1 >= len(lines) || !strings.HasSuffix(lines[at+1], suffix) {
			t.Fatalf("help for %q lacks description ending %q:\n%s", flag, suffix, help.String())
		}
	}
}

// DHF-TEST: keel/requirement-187
func TestRequirementSuffixJoinsMultipleConditions(t *testing.T) {
	got := requirementSuffix(FlagSpec{RequiredWhen: []FlagCondition{
		{Flag: "b", In: []string{"x", "y"}},
		{Flag: "c", In: []string{"p"}},
	}})
	if want := " (required when --b is x or y or when --c is p)"; got != want {
		t.Fatalf("requirementSuffix = %q, want %q", got, want)
	}
}

// conditionTree builds a tree for ValidateTree whose leaf run declares flags.
func conditionTree(flags ...FlagSpec) *CommandSpec {
	return &CommandSpec{
		Name:        "tool",
		Config:      Config{Program: "tool"},
		Subcommands: []*CommandSpec{{Name: "run", Flags: flags, Handler: noopHandler}},
	}
}

// DHF-TEST: keel/requirement-187 (keel/ac-816)
func TestValidateTreeRejectsConditionOnUndeclaredFlag(t *testing.T) {
	root := conditionTree(
		FlagSpec{Name: "b", Value: "mode", Enum: []string{"x", "y", "z"}},
		FlagSpec{Name: "a", Value: "id", RequiredWhen: []FlagCondition{{Flag: "nope", In: []string{"x"}}}},
	)

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a condition on an undeclared flag")
	}
	for _, want := range []string{"run", "--a", "nope"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

// DHF-TEST: keel/requirement-187 (keel/ac-817)
func TestValidateTreeRejectsConditionValueOutsideEnum(t *testing.T) {
	root := conditionTree(
		FlagSpec{Name: "b", Value: "mode", Enum: []string{"x", "y"}},
		FlagSpec{Name: "a", Value: "id", RequiredWhen: []FlagCondition{{Flag: "b", In: []string{"w"}}}},
	)

	err := root.ValidateTree()
	if err == nil {
		t.Fatal("ValidateTree accepted a condition value outside the Enum")
	}
	for _, want := range []string{"--a", "--b", "w"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

// DHF-TEST: keel/requirement-187 (keel/ac-818)
func TestValidateTreeRejectsMalformedConditions(t *testing.T) {
	var flagB bool
	valid := []FlagCondition{{Flag: "b", In: []string{"x"}}}
	for name, root := range map[string]*CommandSpec{
		"bool condition flag": conditionTree(
			FlagSpec{Name: "b", BoolTarget: &flagB},
			FlagSpec{Name: "a", Value: "id", RequiredWhen: []FlagCondition{{Flag: "b", In: []string{"true"}}}},
		),
		"self reference": conditionTree(
			FlagSpec{Name: "a", Value: "id", Enum: []string{"x"}, RequiredWhen: []FlagCondition{{Flag: "a", In: []string{"x"}}}},
		),
		"empty in": conditionTree(
			FlagSpec{Name: "b", Value: "mode", Enum: []string{"x"}},
			FlagSpec{Name: "a", Value: "id", RequiredWhen: []FlagCondition{{Flag: "b"}}},
		),
		"required and required when": conditionTree(
			FlagSpec{Name: "b", Value: "mode", Enum: []string{"x"}},
			FlagSpec{Name: "a", Value: "id", Required: true, RequiredWhen: valid},
		),
	} {
		t.Run(name, func(t *testing.T) {
			err := root.ValidateTree()
			if err == nil || !strings.Contains(err.Error(), "--a") {
				t.Fatalf("ValidateTree = %v, want an error naming --a", err)
			}
		})
	}

	ok := conditionTree(
		FlagSpec{Name: "b", Value: "mode", Enum: []string{"x"}},
		FlagSpec{Name: "a", Value: "id", RequiredWhen: valid},
	)
	if err := ok.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected a valid condition: %v", err)
	}
}
