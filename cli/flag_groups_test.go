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

// flagRuleFlags is the CR-313 fixture flag set: string flags --a and --b and
// --c with Enum x, y. Index 0 is a, 1 is b, 2 is c.
func flagRuleFlags() []FlagSpec {
	var a, b, c string
	return []FlagSpec{
		{Name: "a", Value: "v", StringTarget: &a},
		{Name: "b", Value: "v", StringTarget: &b},
		{Name: "c", Value: "mode", Enum: []string{"x", "y"}, StringTarget: &c},
	}
}

// flagRuleFixture builds the leaf run over flagRuleFlags, lets edit adjust the
// flags, and declares groups. called reports handler runs.
func flagRuleFixture(groups []FlagGroup, edit func([]FlagSpec)) (*CommandSpec, *bool) {
	flags := flagRuleFlags()
	if edit != nil {
		edit(flags)
	}
	called := false
	root := parserTestRoot(&CommandSpec{
		Name:       "run",
		Use:        "run",
		Flags:      flags,
		FlagGroups: groups,
		Handler: func(context.Context, []string) error {
			called = true
			return nil
		},
	})
	return root, &called
}

// expectUsageError dispatches args and requires a UsageError (exit 2) that
// contains want, with no handler run.
func expectUsageError(t *testing.T, root *CommandSpec, called *bool, args []string, want string) {
	t.Helper()
	*called = false
	err := root.Dispatch(context.Background(), args)
	var usage UsageError
	if !errors.As(err, &usage) || usage.ExitCode() != 2 || *called {
		t.Fatalf("Dispatch(%q): err=%v (%T) called=%v, want UsageError exit 2 and no handler run", args, err, err, *called)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Dispatch(%q) error = %q, want %q", args, err.Error(), want)
	}
}

// expectHandlerRun dispatches args and requires the handler to run and exit 0.
func expectHandlerRun(t *testing.T, root *CommandSpec, called *bool, args []string) {
	t.Helper()
	*called = false
	if err := root.Dispatch(context.Background(), args); err != nil || !*called {
		t.Fatalf("Dispatch(%q): err=%v called=%v, want handler run and exit 0", args, err, *called)
	}
}

// helpDescriptionAfter renders `help run` and returns the description line
// under the flag row equal to flagRow.
func helpDescriptionAfter(t *testing.T, root *CommandSpec, flagRow string) string {
	t.Helper()
	var help bytes.Buffer
	if err := root.RenderTopicHelp(&help, []string{"run"}); err != nil {
		t.Fatalf("help run: %v", err)
	}
	lines := strings.Split(help.String(), "\n")
	for i, line := range lines {
		if line == flagRow && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	t.Fatalf("help run has no row %q:\n%s", flagRow, help.String())
	return ""
}

var exactlyOneAB = FlagGroup{Kind: FlagGroupExactlyOne, Flags: []string{"a", "b"}}

// DHF-TEST: keel/requirement-188 (keel/ac-819)
func TestFlagGroupExactlyOneRejectsNone(t *testing.T) {
	root, called := flagRuleFixture([]FlagGroup{exactlyOneAB}, nil)
	expectUsageError(t, root, called, []string{"run"}, "exactly one of --a, --b required")
}

// DHF-TEST: keel/requirement-188 (keel/ac-820)
func TestFlagGroupRejectsTwoMembers(t *testing.T) {
	for _, kind := range []FlagGroupKind{FlagGroupExactlyOne, FlagGroupAtMostOne} {
		t.Run(string(kind), func(t *testing.T) {
			root, called := flagRuleFixture([]FlagGroup{{Kind: kind, Flags: []string{"a", "b"}}}, nil)
			expectUsageError(t, root, called, []string{"run", "--a", "1", "--b", "2"}, "--a and --b are mutually exclusive")
			// Group order, not argv order.
			expectUsageError(t, root, called, []string{"run", "--b", "2", "--a", "1"}, "--a and --b are mutually exclusive")
		})
	}
}

// DHF-TEST: keel/requirement-188 (keel/ac-821)
func TestFlagGroupIgnoresMemberDefault(t *testing.T) {
	root, called := flagRuleFixture([]FlagGroup{exactlyOneAB}, func(f []FlagSpec) { f[1].Default = "d" })
	expectHandlerRun(t, root, called, []string{"run", "--a", "1"})
}

// DHF-TEST: keel/requirement-188 (keel/ac-822)
func TestFlagGroupAtLeastOne(t *testing.T) {
	root, called := flagRuleFixture([]FlagGroup{{Kind: FlagGroupAtLeastOne, Flags: []string{"a", "b"}}}, nil)
	expectUsageError(t, root, called, []string{"run"}, "at least one of --a, --b required")
	expectHandlerRun(t, root, called, []string{"run", "--a", "1"})
	expectHandlerRun(t, root, called, []string{"run", "--a", "1", "--b", "2"})
}

// DHF-TEST: keel/requirement-188 (keel/ac-823)
func TestFlagGroupWhenCondition(t *testing.T) {
	group := FlagGroup{Kind: FlagGroupExactlyOne, Flags: []string{"a", "b"}, When: []FlagCondition{{Flag: "c", In: []string{"x"}}}}
	const want = "exactly one of --a, --b required (when --c is x)"

	root, called := flagRuleFixture([]FlagGroup{group}, nil)
	expectHandlerRun(t, root, called, []string{"run", "--c", "y"})
	expectUsageError(t, root, called, []string{"run", "--c", "x"}, want)

	root, called = flagRuleFixture([]FlagGroup{group}, func(f []FlagSpec) { f[2].Default = "x" })
	expectUsageError(t, root, called, []string{"run"}, want)
}

// DHF-TEST: keel/requirement-188 (keel/ac-824)
func TestHelpJSONCarriesFlagGroups(t *testing.T) {
	group := FlagGroup{Kind: FlagGroupExactlyOne, Flags: []string{"a", "b"}, When: []FlagCondition{{Flag: "c", In: []string{"x"}}}}
	root, _ := flagRuleFixture([]FlagGroup{group}, nil)
	root.Subcommands = append(root.Subcommands, &CommandSpec{Name: "other", Use: "other", Handler: noopHandler})

	var out bytes.Buffer
	if err := root.RenderHelpJSON(&out); err != nil {
		t.Fatalf("RenderHelpJSON: %v", err)
	}
	var inventory []map[string]any
	if err := json.Unmarshal(out.Bytes(), &inventory); err != nil {
		t.Fatalf("help-json is not a JSON array: %v", err)
	}
	byPath := map[string]map[string]any{}
	for _, e := range inventory {
		byPath[e["path"].(string)] = e
	}
	want := []any{map[string]any{
		"kind":  "exactly_one",
		"flags": []any{"a", "b"},
		"when":  []any{map[string]any{"flag": "c", "in": []any{"x"}}},
	}}
	if got := byPath["run"]["flag_groups"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("run flag_groups = %#v, want %#v", got, want)
	}
	if _, ok := byPath["other"]["flag_groups"]; ok {
		t.Fatalf("other carries flag_groups: %#v", byPath["other"])
	}
}

// DHF-TEST: keel/requirement-188 (keel/ac-825)
func TestCommandHelpStatesFlagRules(t *testing.T) {
	root, _ := flagRuleFixture([]FlagGroup{
		exactlyOneAB,
		{Kind: FlagGroupAtMostOne, Flags: []string{"a", "b"}, When: []FlagCondition{{Flag: "c", In: []string{"x"}}}},
	}, nil)
	var help bytes.Buffer
	if err := root.RenderTopicHelp(&help, []string{"run"}); err != nil {
		t.Fatalf("help run: %v", err)
	}
	text := help.String()
	flagsAt, rulesAt := strings.Index(text, "\nFlags:\n"), strings.Index(text, "\nFlag rules:\n")
	if flagsAt < 0 || rulesAt < flagsAt {
		t.Fatalf("help run lacks Flag rules after Flags:\n%s", text)
	}
	rules := text[rulesAt:]
	for _, line := range []string{"\n  exactly one of --a | --b\n", "\n  at most one of --a | --b (when --c is x)\n"} {
		if !strings.Contains(rules, line) {
			t.Fatalf("Flag rules lack line %q:\n%s", line, text)
		}
	}
}

// DHF-TEST: keel/requirement-188
func TestHelpAllAndUngroupedHelpRenderFlagRules(t *testing.T) {
	root, _ := flagRuleFixture([]FlagGroup{exactlyOneAB}, nil)
	var all bytes.Buffer
	root.RenderAllHelp(&all)
	if !strings.Contains(all.String(), "Flag rules:\n  exactly one of --a | --b\n") {
		t.Fatalf("--help-all lacks the Flag rules section:\n%s", all.String())
	}

	plain, _ := flagRuleFixture(nil, nil)
	var help bytes.Buffer
	if err := plain.RenderTopicHelp(&help, []string{"run"}); err != nil {
		t.Fatalf("help run: %v", err)
	}
	if strings.Contains(help.String(), "Flag rules:") {
		t.Fatalf("help without groups renders Flag rules:\n%s", help.String())
	}
}

// DHF-TEST: keel/requirement-188
func TestFlagRuleRowsJoinConditions(t *testing.T) {
	var out bytes.Buffer
	printFlagGroupRows(&out, []FlagGroup{{
		Kind:  FlagGroupAtLeastOne,
		Flags: []string{"a", "b"},
		When:  []FlagCondition{{Flag: "c", In: []string{"x", "y"}}, {Flag: "d", Present: true}},
	}}, 0)
	if want := "  at least one of --a | --b (when --c is x or y or when --d is supplied)\n"; out.String() != want {
		t.Fatalf("rows = %q, want %q", out.String(), want)
	}
}

// groupTree builds a ValidateTree tree whose leaf run declares the fixture
// flags, adjusted by edit, and groups.
func groupTree(groups []FlagGroup, edit func([]FlagSpec)) *CommandSpec {
	flags := flagRuleFlags()
	if edit != nil {
		edit(flags)
	}
	return &CommandSpec{
		Name:        "tool",
		Config:      Config{Program: "tool"},
		Subcommands: []*CommandSpec{{Name: "run", Flags: flags, FlagGroups: groups, Handler: noopHandler}},
	}
}

// DHF-TEST: keel/requirement-188 (keel/ac-826)
func TestValidateTreeRejectsMalformedFlagGroups(t *testing.T) {
	whenCX := []FlagCondition{{Flag: "c", In: []string{"x"}}}
	cases := []struct {
		name  string
		group FlagGroup
		edit  func([]FlagSpec)
	}{
		{"unknown kind", FlagGroup{Kind: "one_of", Flags: []string{"a", "b"}}, nil},
		{"one member", FlagGroup{Kind: FlagGroupExactlyOne, Flags: []string{"a"}}, nil},
		{"undeclared member", FlagGroup{Kind: FlagGroupExactlyOne, Flags: []string{"a", "nope"}}, nil},
		{"duplicate member", FlagGroup{Kind: FlagGroupExactlyOne, Flags: []string{"a", "a"}}, nil},
		{"required member", exactlyOneAB, func(f []FlagSpec) { f[0].Required = true }},
		{"required when member", exactlyOneAB, func(f []FlagSpec) {
			f[2].Enum = []string{"x"}
			f[0].RequiredWhen = whenCX
		}},
		{"when value outside enum", FlagGroup{Kind: FlagGroupExactlyOne, Flags: []string{"a", "b"}, When: []FlagCondition{{Flag: "c", In: []string{"w"}}}}, nil},
		{"when names member", FlagGroup{Kind: FlagGroupExactlyOne, Flags: []string{"a", "c"}, When: whenCX}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := groupTree([]FlagGroup{tc.group}, tc.edit).ValidateTree()
			if err == nil || !strings.Contains(err.Error(), "run") {
				t.Fatalf("ValidateTree = %v, want an error naming run", err)
			}
		})
	}
}

// DHF-TEST: keel/requirement-188
func TestValidateTreeAcceptsWellFormedFlagGroups(t *testing.T) {
	conditioned := groupTree([]FlagGroup{
		{Kind: FlagGroupExactlyOne, Flags: []string{"a", "b"}, When: []FlagCondition{{Flag: "c", In: []string{"x"}}}},
		{Kind: FlagGroupAtMostOne, Flags: []string{"a", "b"}, When: []FlagCondition{{Flag: "c", Present: true}}},
	}, nil)
	// at_least_one tolerates a RequiredWhen member.
	requiredWhenMember := groupTree([]FlagGroup{{Kind: FlagGroupAtLeastOne, Flags: []string{"a", "b"}}},
		func(f []FlagSpec) { f[1].RequiredWhen = []FlagCondition{{Flag: "c", In: []string{"y"}}} })
	for _, root := range []*CommandSpec{conditioned, requiredWhenMember} {
		if err := root.ValidateTree(); err != nil {
			t.Fatalf("ValidateTree rejected well-formed groups: %v", err)
		}
	}
}

// DHF-TEST: keel/requirement-188
func TestFlagGroupCountsBoolMemberAndSkipsSatisfiedGroups(t *testing.T) {
	var force bool
	root, called := flagRuleFixture([]FlagGroup{
		{Kind: FlagGroupAtMostOne, Flags: []string{"a", "force"}},
		{Kind: FlagGroupAtLeastOne, Flags: []string{"a", "force"}},
	}, nil)
	leaf := root.Subcommands[0]
	leaf.Flags = append(leaf.Flags, FlagSpec{Name: "force", BoolTarget: &force})

	expectHandlerRun(t, root, called, []string{"run", "--force"})
	expectUsageError(t, root, called, []string{"run", "--force", "--a", "1"}, "--a and --force are mutually exclusive")
}

// forbiddenFixture is the ac-827 fixture: --b declares Enum x, y, z and --a is
// not allowed when --b is x.
func forbiddenFixture(bDefault string) (*CommandSpec, *bool) {
	return flagRuleFixture(nil, func(f []FlagSpec) {
		f[1].Enum = []string{"x", "y", "z"}
		f[1].Default = bDefault
		f[0].ForbiddenWhen = []FlagCondition{{Flag: "b", In: []string{"x"}}}
	})
}

// DHF-TEST: keel/requirement-189 (keel/ac-827)
func TestDispatchRejectsForbiddenFlag(t *testing.T) {
	root, called := forbiddenFixture("")
	expectUsageError(t, root, called, []string{"run", "--b", "x", "--a", "1"}, "--a not allowed when --b is x")
}

// DHF-TEST: keel/requirement-189 (keel/ac-828)
func TestForbiddenWhenUsesEffectiveValue(t *testing.T) {
	root, called := forbiddenFixture("")
	expectHandlerRun(t, root, called, []string{"run", "--b", "z", "--a", "1"})

	root, called = forbiddenFixture("x")
	expectUsageError(t, root, called, []string{"run", "--a", "1"}, "--a not allowed when --b is x")
	// An absent forbidden flag is never refused.
	expectHandlerRun(t, root, called, []string{"run"})
}

// DHF-TEST: keel/requirement-189 (keel/ac-829)
func TestHelpStatesForbiddenWhen(t *testing.T) {
	root, _ := forbiddenFixture("")

	flags := helpJSONFlagsByName(t, root, "run")
	want := []any{map[string]any{"flag": "b", "in": []any{"x"}}}
	if got := flags["a"]["forbidden_when"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("a forbidden_when = %#v, want %#v", got, want)
	}
	if _, ok := flags["b"]["forbidden_when"]; ok {
		t.Fatalf("b carries forbidden_when: %#v", flags["b"])
	}
	if got := helpDescriptionAfter(t, root, "  --a v"); !strings.HasSuffix(got, "(not allowed when --b is x)") {
		t.Fatalf("--a description = %q, want suffix (not allowed when --b is x)", got)
	}
}

// DHF-TEST: keel/requirement-189
func TestForbiddenSuffixFollowsRequirementSuffix(t *testing.T) {
	got := requirementSuffix(FlagSpec{RequiredWhen: []FlagCondition{{Flag: "c", In: []string{"x"}}}}) +
		forbiddenSuffix(FlagSpec{ForbiddenWhen: []FlagCondition{{Flag: "b", In: []string{"x", "y"}}, {Flag: "d", Present: true}}})
	if want := " (required when --c is x) (not allowed when --b is x or y or when --d is supplied)"; got != want {
		t.Fatalf("suffixes = %q, want %q", got, want)
	}
}

// DHF-TEST: keel/requirement-189 (keel/ac-830)
func TestValidateTreeRejectsMalformedForbiddenWhen(t *testing.T) {
	cases := map[string]func([]FlagSpec){
		"undeclared flag": func(f []FlagSpec) { f[0].ForbiddenWhen = []FlagCondition{{Flag: "nope", In: []string{"x"}}} },
		"self reference":  func(f []FlagSpec) { f[0].ForbiddenWhen = []FlagCondition{{Flag: "a", In: []string{"x"}}} },
		"value outside enum": func(f []FlagSpec) {
			f[1].Enum = []string{"x", "y"}
			f[0].ForbiddenWhen = []FlagCondition{{Flag: "b", In: []string{"w"}}}
		},
		"empty in": func(f []FlagSpec) {
			f[1].Enum = []string{"x", "y"}
			f[0].ForbiddenWhen = []FlagCondition{{Flag: "b"}}
		},
		"required and forbidden": func(f []FlagSpec) {
			f[1].Enum = []string{"x", "y"}
			f[0].Required = true
			f[0].ForbiddenWhen = []FlagCondition{{Flag: "b", In: []string{"x"}}}
		},
		"overlapping values": func(f []FlagSpec) {
			f[1].Enum = []string{"x", "y"}
			f[0].RequiredWhen = []FlagCondition{{Flag: "b", In: []string{"x"}}}
			f[0].ForbiddenWhen = []FlagCondition{{Flag: "b", In: []string{"x", "y"}}}
		},
		"overlapping presence": func(f []FlagSpec) {
			f[0].RequiredWhen = []FlagCondition{{Flag: "b", Present: true}}
			f[0].ForbiddenWhen = []FlagCondition{{Flag: "b", Present: true}}
		},
		"no enum": func(f []FlagSpec) { f[0].ForbiddenWhen = []FlagCondition{{Flag: "b", In: []string{"x"}}} },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			err := groupTree(nil, edit).ValidateTree()
			if err == nil || !strings.Contains(err.Error(), "--a") {
				t.Fatalf("ValidateTree = %v, want an error naming --a", err)
			}
		})
	}
}

// DHF-TEST: keel/requirement-189
func TestValidateTreeAcceptsDisjointRequiredAndForbiddenWhen(t *testing.T) {
	root := groupTree(nil, func(f []FlagSpec) {
		f[1].Enum = []string{"x", "y"}
		f[0].RequiredWhen = []FlagCondition{{Flag: "b", In: []string{"x"}}, {Flag: "c", Present: true}}
		f[0].ForbiddenWhen = []FlagCondition{{Flag: "b", In: []string{"y"}}}
	})
	if err := root.ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected disjoint conditions: %v", err)
	}
}

// A presence condition overlaps any value condition on the same flag:
// supplying --c x makes both hold.
//
// DHF-TEST: keel/requirement-189 (keel/ac-830)
func TestValidateTreeRejectsPresenceOverlappingValueCondition(t *testing.T) {
	for name, mutate := range map[string]func([]FlagSpec){
		"required presence, forbidden value": func(f []FlagSpec) {
			f[0].RequiredWhen = []FlagCondition{{Flag: "c", Present: true}}
			f[0].ForbiddenWhen = []FlagCondition{{Flag: "c", In: []string{"x"}}}
		},
		"required value, forbidden presence": func(f []FlagSpec) {
			f[0].RequiredWhen = []FlagCondition{{Flag: "c", In: []string{"x"}}}
			f[0].ForbiddenWhen = []FlagCondition{{Flag: "c", Present: true}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := groupTree(nil, mutate).ValidateTree()
			if err == nil || !strings.Contains(err.Error(), "--a") || !strings.Contains(err.Error(), "can hold at once") {
				t.Fatalf("ValidateTree = %v, want an overlap error naming --a", err)
			}
		})
	}
}

// presenceFlags drops the Enum from --c so the fixture's b and c are plain
// string flags.
func presenceFlags(f []FlagSpec) { f[2].Enum = nil }

// DHF-TEST: keel/requirement-188 (keel/ac-831)
func TestRequiredWhenPresenceCondition(t *testing.T) {
	root, called := flagRuleFixture(nil, func(f []FlagSpec) {
		presenceFlags(f)
		f[1].Default = "d"
		f[2].RequiredWhen = []FlagCondition{{Flag: "b", Present: true}}
	})
	expectUsageError(t, root, called, []string{"run", "--b", "1"}, "required flag --c missing (required when --b is supplied)")
	// b's Default does not make b supplied.
	expectHandlerRun(t, root, called, []string{"run"})
}

// DHF-TEST: keel/requirement-189 (keel/ac-832)
func TestForbiddenWhenPresenceCondition(t *testing.T) {
	root, called := flagRuleFixture(nil, func(f []FlagSpec) {
		presenceFlags(f)
		f[2].ForbiddenWhen = []FlagCondition{{Flag: "a", Present: true}}
	})
	expectUsageError(t, root, called, []string{"run", "--a", "1", "--c", "2"}, "--c not allowed when --a is supplied")
	expectHandlerRun(t, root, called, []string{"run", "--c", "2"})
}

// DHF-TEST: keel/requirement-188 (keel/ac-833)
func TestFlagGroupPresenceWhen(t *testing.T) {
	root, called := flagRuleFixture([]FlagGroup{{
		Kind:  FlagGroupAtLeastOne,
		Flags: []string{"a", "b"},
		When:  []FlagCondition{{Flag: "c", Present: true}},
	}}, presenceFlags)
	expectHandlerRun(t, root, called, []string{"run"})
	expectUsageError(t, root, called, []string{"run", "--c", "1"}, "at least one of --a, --b required (when --c is supplied)")
}

// DHF-TEST: keel/requirement-188 (keel/ac-834)
func TestHelpStatesPresenceCondition(t *testing.T) {
	root, _ := flagRuleFixture(nil, func(f []FlagSpec) {
		presenceFlags(f)
		f[2].RequiredWhen = []FlagCondition{{Flag: "b", Present: true}}
	})

	flags := helpJSONFlagsByName(t, root, "run")
	want := []any{map[string]any{"flag": "b", "present": true}}
	if got := flags["c"]["required_when"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("c required_when = %#v, want %#v", got, want)
	}
	if got := helpDescriptionAfter(t, root, "  --c mode"); !strings.HasSuffix(got, "(required when --b is supplied)") {
		t.Fatalf("--c description = %q, want suffix (required when --b is supplied)", got)
	}
}

// DHF-TEST: keel/requirement-188 (keel/ac-835)
func TestValidateTreePresenceConditions(t *testing.T) {
	withCondition := func(cond FlagCondition) *CommandSpec {
		return groupTree(nil, func(f []FlagSpec) {
			presenceFlags(f)
			f[2].RequiredWhen = []FlagCondition{cond}
		})
	}
	for name, cond := range map[string]FlagCondition{
		"present with in": {Flag: "b", Present: true, In: []string{"x"}},
		"undeclared flag": {Flag: "nope", Present: true},
		"self reference":  {Flag: "c", Present: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := withCondition(cond).ValidateTree()
			if err == nil || !strings.Contains(err.Error(), "--c") {
				t.Fatalf("ValidateTree = %v, want an error naming --c", err)
			}
		})
	}
	if err := withCondition(FlagCondition{Flag: "b", Present: true}).ValidateTree(); err != nil {
		t.Fatalf("ValidateTree rejected a presence condition: %v", err)
	}
}
