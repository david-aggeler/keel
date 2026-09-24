package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// startTree is a two-level tree whose every handler records that it ran.
func startTree(invoked *[]string) *CommandSpec {
	record := func(name string) Handler {
		return func(context.Context, []string) error {
			*invoked = append(*invoked, name)
			return nil
		}
	}
	return &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			Version:      "1.2.3",
			RootSummary:  "tool does things.",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
		},
		Subcommands: []*CommandSpec{
			{Name: "run", Use: "run", Short: "Run it.", Handler: record("run")},
			{Name: "group", Short: "A group.", Subcommands: []*CommandSpec{
				{Name: "leaf", Use: "group leaf", Short: "A leaf.", Handler: record("group leaf")},
			}},
		},
	}
}

// startAndDispatch drives the consumer loop Start prescribes: return the code
// when Start served the invocation, otherwise dispatch the words.
func startAndDispatch(argv []string) (stdout, stderr string, code int, done bool, invoked []string) {
	tree := startTree(&invoked)
	var out, errOut bytes.Buffer
	tree.SetHelpOutput(&out, &errOut)
	_, words, code, done := tree.Start(argv)
	if !done {
		if err := tree.Dispatch(context.Background(), words); err != nil {
			code = 1
		}
	}
	return out.String(), errOut.String(), code, done, invoked
}

// DHF-TEST: keel/requirement-172 (keel/ac-737)
func TestStartServesRequestedHelpBeforeAnyHandlerIdenticallyInEveryMode(t *testing.T) {
	for _, request := range [][]string{
		{"--help"},
		{"-h"},
		{"help"},
		{"help", "run"},
		{"help", "group", "leaf"},
		{"help", "mode"},
		{"run", "--help"},
		{"group", "leaf", "-h"},
		{"--help-all"},
		{"run", "--help-all"},
		{"--help-json"},
		{"group", "--help-json"},
		{"--version"},
		{"run", "--version"},
	} {
		t.Run(strings.Join(request, " "), func(t *testing.T) {
			var human string
			for _, mode := range []string{"human", "ai", "json"} {
				stdout, stderr, code, done, invoked := startAndDispatch(append([]string{"--mode", mode, "-q"}, request...))
				if !done || code != 0 || stderr != "" || stdout == "" {
					t.Fatalf("--mode %s %q: done=%v code=%d stderr=%q stdout empty=%v; want served, 0, no stderr, stdout", mode, request, done, code, stderr, stdout == "")
				}
				if len(invoked) > 0 {
					t.Fatalf("--mode %s %q invoked handler %q", mode, request, invoked)
				}
				if mode == "human" {
					human = stdout
				} else if stdout != human {
					t.Fatalf("--mode %s %q stdout differs from human:\n%s\n---\n%s", mode, request, human, stdout)
				}
			}
		})
	}
}

// DHF-TEST: keel/requirement-172 (keel/ac-738)
func TestStartWritesUsageErrorHelpToStderrWithUsageExitCode(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{argv: []string{"help", "nope"}, want: `unknown help topic "nope"`},
		{argv: []string{"help", "group", "nope"}, want: `unknown help topic "group nope"`},
		{argv: []string{"--nope"}, want: `tool: unknown flag "--nope"`},
		{argv: []string{"--nope", "run"}, want: `tool: unknown flag "--nope"`},
		{argv: []string{"--color", "red"}, want: `tool: unknown --color "red"`},
		{argv: []string{"-q", "-v"}, want: "tool: --quiet and --verbose are mutually exclusive"},
		{argv: nil, want: "Usage:"},
	} {
		t.Run(strings.Join(tc.argv, " "), func(t *testing.T) {
			var human string
			for _, mode := range []string{"human", "ai", "json"} {
				stdout, stderr, code, done, invoked := startAndDispatch(append([]string{"--mode", mode}, tc.argv...))
				if !done || code != 2 || stdout != "" {
					t.Fatalf("--mode %s %q: done=%v code=%d stdout=%q; want served, 2, empty stdout", mode, tc.argv, done, code, stdout)
				}
				if len(invoked) > 0 {
					t.Fatalf("--mode %s %q invoked handler %q", mode, tc.argv, invoked)
				}
				if !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, "Usage:") {
					t.Fatalf("--mode %s %q stderr lacks %q or help text:\n%s", mode, tc.argv, tc.want, stderr)
				}
				if mode == "human" {
					human = stderr
				} else if stderr != human {
					t.Fatalf("--mode %s %q stderr differs from human:\n%s\n---\n%s", mode, tc.argv, human, stderr)
				}
			}
		})
	}
}

// DHF-TEST: keel/requirement-172 (keel/ac-738)
func TestStartServesAnInvalidModeAsAUsageError(t *testing.T) {
	stdout, stderr, code, done, _ := startAndDispatch([]string{"--mode", "bogus", "--help"})
	if !done || code != 2 || stdout != "" || !strings.Contains(stderr, `tool: unknown --mode "bogus"`) {
		t.Fatalf("--mode bogus: done=%v code=%d stdout=%q stderr=%q", done, code, stdout, stderr)
	}
}

// DHF-TEST: keel/requirement-172
func TestStartHandsNonHelpInvocationsToTheConsumer(t *testing.T) {
	var invoked []string
	tree := startTree(&invoked)
	var out, errOut bytes.Buffer
	tree.SetHelpOutput(&out, &errOut)
	cfg, words, code, done := tree.Start([]string{"group", "--mode", "ai", "leaf", "-v"})
	if done || code != 0 {
		t.Fatalf("Start(group leaf) done=%v code=%d, want not served", done, code)
	}
	if cfg != (RuntimeConfig{Mode: ModeAI, Verbose: true}) {
		t.Fatalf("cfg = %+v", cfg)
	}
	if err := tree.Dispatch(context.Background(), words); err != nil {
		t.Fatal(err)
	}
	if strings.Join(invoked, ",") != "group leaf" || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("invoked %q, stdout %q, stderr %q", invoked, out.String(), errOut.String())
	}

	rooted := startTree(&invoked)
	rooted.Config.RootHandler = func(context.Context, []string) error { return nil }
	if _, words, _, done := rooted.Start(nil); done || len(words) != 0 {
		t.Fatalf("bare invocation with a RootHandler: done=%v words=%q, want handed to Dispatch", done, words)
	}
}

// DHF-TEST: keel/requirement-172
func TestStartVersionLineDefaultsToUnknown(t *testing.T) {
	var invoked []string
	tree := startTree(&invoked)
	tree.Config.Version = ""
	var out bytes.Buffer
	tree.SetHelpOutput(&out, &out)
	if _, _, code, done := tree.Start([]string{"--version"}); !done || code != 0 || out.String() != "unknown\n" {
		t.Fatalf("--version without Config.Version: done=%v code=%d out=%q", done, code, out.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

// DHF-TEST: keel/requirement-100, keel/requirement-172
func TestStartReportsAHelpJSONWriteFailureOnStderr(t *testing.T) {
	var invoked []string
	tree := startTree(&invoked)
	var errOut bytes.Buffer
	tree.SetHelpOutput(failingWriter{}, &errOut)
	if _, _, code, done := tree.Start([]string{"--help-json"}); !done || code != 1 || !strings.Contains(errOut.String(), "tool: stdout closed") {
		t.Fatalf("--help-json write failure: done=%v code=%d stderr=%q", done, code, errOut.String())
	}
}

// DHF-TEST: keel/requirement-155, keel/requirement-172
func TestDispatchServesHelpThroughTheHelpRenderer(t *testing.T) {
	var invoked []string
	tree := startTree(&invoked)
	var out, errOut bytes.Buffer
	tree.SetHelpOutput(&out, &errOut)
	if err := tree.Dispatch(context.Background(), []string{"help", "run"}); err != nil || !strings.Contains(out.String(), "run:") || errOut.Len() != 0 {
		t.Fatalf("Dispatch(help run) err=%v stdout=%q stderr=%q", err, out.String(), errOut.String())
	}
	out.Reset()
	err := tree.Dispatch(context.Background(), []string{"help", "nope"})
	var usage UsageError
	if !errors.As(err, &usage) || out.Len() != 0 || !strings.Contains(errOut.String(), `unknown help topic "nope"`) {
		t.Fatalf("Dispatch(help nope) err=%v stdout=%q stderr=%q", err, out.String(), errOut.String())
	}
	if len(invoked) > 0 {
		t.Fatalf("help invoked handler %q", invoked)
	}
}
