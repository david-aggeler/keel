package cli

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func consumerGlobalsTree(flags []FlagSpec, commandFlags []FlagSpec) (*CommandSpec, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root := &CommandSpec{
		Name: "tool",
		Config: Config{
			Program:      "tool",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
			GlobalFlags:  flags,
			helpStdout:   stdout,
			helpStderr:   stderr,
		},
		Subcommands: []*CommandSpec{{
			Name:    "ci",
			Use:     "ci",
			Short:   "Run the gate.",
			Flags:   commandFlags,
			Handler: func(context.Context, []string) error { return nil },
		}},
	}
	return root, stdout, stderr
}

// DHF-TEST: keel/requirement-176 (keel/ac-753)
func TestStartBindsAndStripsConsumerValueGlobalAfterCommand(t *testing.T) {
	var target string
	root, _, _ := consumerGlobalsTree([]FlagSpec{{Name: "target", Value: "path", StringTarget: &target}}, nil)

	_, words, code, done := root.Start([]string{"ci", "--target", "/r"})
	if target != "/r" || !reflect.DeepEqual(words, []string{"ci"}) || code != 0 || done {
		t.Fatalf("Start: target=%q words=%q code=%d done=%v", target, words, code, done)
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-755)
func TestStartBindsConsumerValueGlobalEqualsFormBeforeCommand(t *testing.T) {
	var target string
	root, _, stderr := consumerGlobalsTree([]FlagSpec{{Name: "target", Value: "path", StringTarget: &target}}, nil)

	_, words, code, done := root.Start([]string{"--target=/r", "ci"})
	if target != "/r" || !reflect.DeepEqual(words, []string{"ci"}) || code != 0 || done || stderr.Len() != 0 {
		t.Fatalf("Start: target=%q words=%q code=%d done=%v stderr=%q", target, words, code, done, stderr.String())
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-756)
func TestStartBindsConsumerBoolGlobalAlias(t *testing.T) {
	var dry bool
	root, _, _ := consumerGlobalsTree([]FlagSpec{{Name: "dry", Alias: "n", BoolTarget: &dry}}, nil)

	_, words, code, done := root.Start([]string{"-n", "ci"})
	if !dry || !reflect.DeepEqual(words, []string{"ci"}) || code != 0 || done {
		t.Fatalf("Start: dry=%v words=%q code=%d done=%v", dry, words, code, done)
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-757)
func TestStartRejectsConsumerGlobalOutsideEnum(t *testing.T) {
	pick := "unchanged"
	root, _, stderr := consumerGlobalsTree([]FlagSpec{{Name: "pick", Value: "a|b", Enum: []string{"a", "b"}, StringTarget: &pick}}, nil)

	_, _, code, done := root.Start([]string{"ci", "--pick", "c"})
	if code != 2 || !done || pick != "unchanged" || !strings.Contains(stderr.String(), "--pick") {
		t.Fatalf("Start: pick=%q code=%d done=%v stderr=%q", pick, code, done, stderr.String())
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-758)
func TestStartRejectsConsumerValueGlobalWithoutValue(t *testing.T) {
	target := "unchanged"
	root, _, stderr := consumerGlobalsTree([]FlagSpec{{Name: "target", Value: "path", StringTarget: &target}}, nil)

	_, _, code, done := root.Start([]string{"ci", "--target"})
	if code != 2 || !done || target != "unchanged" || !strings.Contains(stderr.String(), "--target") {
		t.Fatalf("Start: target=%q code=%d done=%v stderr=%q", target, code, done, stderr.String())
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-759)
func TestStartKeepsKeelGlobalPrecedenceOverConsumerRedeclaration(t *testing.T) {
	var consumerMode string
	root, _, _ := consumerGlobalsTree([]FlagSpec{{Name: "mode", Value: "human|ai|json", StringTarget: &consumerMode}}, nil)

	cfg, words, code, done := root.Start([]string{"--mode", "ai", "ci"})
	if cfg.Mode != ModeAI || !reflect.DeepEqual(words, []string{"ci"}) || consumerMode != "" || code != 0 || done {
		t.Fatalf("Start: mode=%q words=%q consumerMode=%q code=%d done=%v", cfg.Mode, words, consumerMode, code, done)
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-759)
func TestStartKeepsConsumerLongNameWhenAliasCollidesWithKeelGlobal(t *testing.T) {
	var target string
	root, _, _ := consumerGlobalsTree([]FlagSpec{{Name: "target", Alias: "v", Value: "path", StringTarget: &target}}, nil)

	cfg, words, code, done := root.Start([]string{"--target", "/r", "-v", "ci"})
	if target != "/r" || !cfg.Verbose || !reflect.DeepEqual(words, []string{"ci"}) || code != 0 || done {
		t.Fatalf("Start: target=%q verbose=%v words=%q code=%d done=%v", target, cfg.Verbose, words, code, done)
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-759, keel/ac-761)
func TestStartHelpOmitsConsumerAliasThatCollidesWithKeelGlobal(t *testing.T) {
	root, stdout, _ := consumerGlobalsTree([]FlagSpec{{Name: "target", Alias: "v", Value: "path"}}, nil)

	_, _, code, done := root.Start([]string{"--help"})
	help := stdout.String()
	if code != 0 || !done || !strings.Contains(help, "--target path") || strings.Contains(help, "-v, --target") || strings.Contains(help, "-v|--target") {
		t.Fatalf("Start: code=%d done=%v stdout=%q", code, done, help)
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-759, keel/ac-761)
func TestStartSuppressesConsumerAliasWhenLongNameCollidesWithKeelGlobal(t *testing.T) {
	var target string
	root, stdout, _ := consumerGlobalsTree([]FlagSpec{{Name: "mode", Alias: "n", Value: "profile", StringTarget: &target}}, nil)

	_, words, code, done := root.Start([]string{"ci", "-n", "custom"})
	if target != "" || !reflect.DeepEqual(words, []string{"ci", "-n", "custom"}) || code != 0 || done {
		t.Fatalf("Start: target=%q words=%q code=%d done=%v", target, words, code, done)
	}
	root.Start([]string{"--help"})
	if strings.Contains(stdout.String(), "-n, --mode") || strings.Contains(stdout.String(), "-n|--mode") {
		t.Fatalf("help unexpectedly rendered suppressed consumer alias: %q", stdout.String())
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-760)
func TestStartKeepsCommandFlagPrecedenceOverConsumerGlobal(t *testing.T) {
	var global string
	root, _, _ := consumerGlobalsTree(
		[]FlagSpec{{Name: "target", Value: "path", StringTarget: &global}},
		[]FlagSpec{{Name: "target", Value: "path"}},
	)

	_, words, code, done := root.Start([]string{"ci", "--target", "/r"})
	if !reflect.DeepEqual(words, []string{"ci", "--target", "/r"}) || global != "" || code != 0 || done {
		t.Fatalf("Start: words=%q global=%q code=%d done=%v", words, global, code, done)
	}
}

// DHF-TEST: keel/requirement-176 (keel/ac-761)
func TestStartRendersParsedConsumerGlobalInRootHelp(t *testing.T) {
	var target string
	root, stdout, _ := consumerGlobalsTree([]FlagSpec{{Name: "target", Value: "path", StringTarget: &target}}, nil)

	_, _, code, done := root.Start([]string{"--help"})
	if code != 0 || !done || !strings.Contains(stdout.String(), "--target") {
		t.Fatalf("Start: code=%d done=%v stdout=%q", code, done, stdout.String())
	}
}

// DHF-TEST: keel/requirement-176
func TestStartDoesNotResolveConsumerGlobalValueAsCommand(t *testing.T) {
	var target string
	root, _, _ := consumerGlobalsTree([]FlagSpec{{Name: "target", Value: "path", StringTarget: &target}}, nil)

	_, words, code, done := root.Start([]string{"--target", "ci", "ci"})
	if target != "ci" || !reflect.DeepEqual(words, []string{"ci"}) || code != 0 || done {
		t.Fatalf("Start: target=%q words=%q code=%d done=%v", target, words, code, done)
	}
}
