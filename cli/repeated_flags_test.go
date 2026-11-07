package cli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-185 (keel/ac-804)
func TestStartRejectsRepeatedConsumerGlobal(t *testing.T) {
	for _, argv := range [][]string{
		{"--target", "coal", "ci", "--target", "gold"},
		{"--target", "coal", "ci", "--target", "coal"},
		{"-t", "coal", "ci", "--target=coal"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			var target string
			root, _, stderr := consumerGlobalsTree([]FlagSpec{{Name: "target", Alias: "t", Value: "instance", StringTarget: &target}}, nil)

			_, words, code, done := root.Start(argv)
			if code != 2 || !done || words != nil || !strings.Contains(stderr.String(), "--target given more than once") {
				t.Fatalf("Start(%q): words=%q code=%d done=%v stderr=%q", argv, words, code, done, stderr.String())
			}
		})
	}
}

// DHF-TEST: keel/requirement-185 (keel/ac-804)
func TestStartRejectsRepeatedConsumerBoolGlobal(t *testing.T) {
	var dry bool
	root, _, stderr := consumerGlobalsTree([]FlagSpec{{Name: "dry", Alias: "n", BoolTarget: &dry}}, nil)

	_, _, code, done := root.Start([]string{"-n", "ci", "--dry"})
	if code != 2 || !done || !strings.Contains(stderr.String(), "--dry given more than once") {
		t.Fatalf("Start: code=%d done=%v stderr=%q", code, done, stderr.String())
	}
}

// DHF-TEST: keel/requirement-185 (keel/ac-805)
func TestDispatchRejectsRepeatedNonRepeatableCommandFlag(t *testing.T) {
	for _, tt := range []struct {
		args []string
		flag string
	}{
		{args: []string{"run", "--out", "a", "--out", "b"}, flag: "--out"},
		{args: []string{"run", "--out", "a", "-o", "a"}, flag: "--out"},
		{args: []string{"run", "--out=a", "--out=a"}, flag: "--out"},
		{args: []string{"run", "--force", "--force"}, flag: "--force"},
		{args: []string{"run", "-ff"}, flag: "--force"},
		{args: []string{"run", "--tag", "x", "--tag", "y"}, flag: "--tag"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var out string
			var force bool
			var tags []string
			called := false
			root := parserTestRoot(&CommandSpec{
				Name: "run",
				Use:  "run",
				Flags: []FlagSpec{
					{Name: "out", Alias: "o", Value: "path", StringTarget: &out},
					{Name: "force", Alias: "f", BoolTarget: &force},
					{Name: "tag", Value: "name", StringSliceTarget: &tags},
				},
				Handler: func(context.Context, []string) error {
					called = true
					return nil
				},
			})

			err := root.Dispatch(context.Background(), tt.args)
			var usage UsageError
			if !errors.As(err, &usage) || usage.ExitCode() != 2 || called || !strings.Contains(err.Error(), tt.flag+" given more than once") {
				t.Fatalf("Dispatch(%q): err=%v called=%v", tt.args, err, called)
			}
		})
	}
}

// DHF-TEST: keel/requirement-185 (keel/ac-806)
func TestDispatchAcceptsRepeatedRepeatableFlag(t *testing.T) {
	var ids []string
	calls := 0
	root := parserTestRoot(&CommandSpec{
		Name:  "run",
		Use:   "run",
		Flags: []FlagSpec{{Name: "id", Value: "id", Repeatable: true, StringSliceTarget: &ids}},
		Handler: func(context.Context, []string) error {
			calls++
			return nil
		},
	})

	if err := root.Dispatch(context.Background(), []string{"run", "--id", "a", "--id", "b"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if calls != 1 || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("calls=%d ids=%q", calls, ids)
	}
}
