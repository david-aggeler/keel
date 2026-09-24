package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/term"
)

// DHF-TEST: keel/requirement-164 (keel/ac-697)
func TestKeelDemoDevExplicitHelpAndVersionRenderToStdoutOnly(t *testing.T) {
	root := t.TempDir()
	for _, arg := range []string{"-h", "--help", "--help-all", "--help-json", "--version"} {
		t.Run(arg, func(t *testing.T) {
			stdout, stderr, code := captureRun(t, root, arg)
			if code != 0 {
				t.Fatalf("keel-demo-dev %s exit = %d, want 0\nstderr:\n%s", arg, code, stderr)
			}
			if strings.TrimSpace(stdout) == "" {
				t.Fatalf("keel-demo-dev %s stdout is empty", arg)
			}
			if stderr != "" {
				t.Fatalf("keel-demo-dev %s stderr = %q, want empty", arg, stderr)
			}
		})
	}
	for _, argv := range [][]string{{"--color=sometimes"}, {"help", "no-such-topic"}} {
		stdout, stderr, code := captureRun(t, root, argv...)
		if code != 2 || stdout != "" || stderr == "" {
			t.Fatalf("keel-demo-dev %v code=%d stdout=%q stderr=%q, want usage error on stderr, exit 2", argv, code, stdout, stderr)
		}
	}
}

// DHF-TEST: keel/requirement-166 (keel/ac-708), keel/requirement-109
func TestKeelDemoDevHandlesEveryOperatorPolicyFlag(t *testing.T) {
	checks := map[string]func(*testing.T){
		"quiet":    func(t *testing.T) { assertQuietIsAConsoleFloor(t) },
		"color":    func(t *testing.T) { assertColorPolicy(t) },
		"no-input": func(t *testing.T) { assertNoInput(t) },
		"plain":    func(t *testing.T) { assertPlain(t) },
	}
	assertEveryGlobalFlagHasParity(t, checks)
}

// assertEveryGlobalFlagHasParity fails for any advertised global flag that is
// neither an action flag routed by the action-flag routing test nor
// covered by a policy check, then runs every policy check.
func assertEveryGlobalFlagHasParity(t *testing.T, checks map[string]func(*testing.T)) {
	t.Helper()
	actions := map[string]bool{}
	for _, c := range globalActionFlagCases(t) {
		actions[strings.TrimPrefix(c.arg, "--")] = true
	}
	// mode, verbose and no-header predate keel/requirement-166; their
	// consumer handling is proven by the mode and header tests.
	preexisting := map[string]bool{"mode": true, "verbose": true, "no-header": true}
	for _, spec := range cli.GlobalFlagSpecs() {
		if actions[spec.Name] || preexisting[spec.Name] {
			continue
		}
		check, ok := checks[spec.Name]
		if !ok {
			t.Fatalf("global flag --%s has no parity check", spec.Name)
		}
		t.Run(spec.Name, check)
	}
}

func parseRuntime(t *testing.T, argv ...string) cli.RuntimeConfig {
	t.Helper()
	tree := demoDevCommandTree(demoBridge{})
	var cfg cli.RuntimeConfig
	var done bool
	discardProcessStreams(t, func() {
		cfg, _, _, done = tree.Start(append(append([]string{}, argv...), tree.Subcommands[0].Name))
	})
	if done {
		t.Fatalf("Start(%q) served the invocation; want a runtime config", argv)
	}
	return cfg
}

// terminalConfig is the keel/term input the operator's --color, --no-input
// and --plain policy selects for stdout.
func terminalConfig(rt cli.RuntimeConfig) term.Config {
	return rt.TermConfig(term.Stdout)
}

// consumerConsole builds keel-demo-dev's real diagnostics logger config for
// argv with the console on a buffer, emits one record at each severity, and
// returns the console output. keel-demo-dev opens no file sink.
func consumerConsole(t *testing.T, argv ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cfg := loggerConfig(parseRuntime(t, argv...))
	cfg.Writer = &buf
	logger, err := logging.New(cfg)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	logger.Debug("probe-debug")
	logger.Info("probe-info")
	logger.Warn("probe-warn")
	logger.Error("probe-error")
	_ = logger.Close()
	return buf.String()
}

// DHF-TEST: keel/requirement-166 (keel/ac-706)
func assertQuietIsAConsoleFloor(t *testing.T) {
	if cfg := loggerConfig(parseRuntime(t, "-q")); cfg.TextDir != "" || cfg.JSONLDir != "" {
		t.Fatalf("-q opened a file sink: %+v", cfg)
	}
	if verbose := consumerConsole(t, "-v"); !strings.Contains(verbose, "probe-debug") {
		t.Fatalf("-v console missing Debug, so the floor is not threaded:\n%s", verbose)
	}
	quiet := consumerConsole(t, "-q")
	for _, want := range []string{"probe-warn", "probe-error"} {
		if !strings.Contains(quiet, want) {
			t.Fatalf("-q console missing %s:\n%s", want, quiet)
		}
	}
	for _, gone := range []string{"probe-debug", "probe-info"} {
		if strings.Contains(quiet, gone) {
			t.Fatalf("-q console still carries %s:\n%s", gone, quiet)
		}
	}
}

func assertColorPolicy(t *testing.T) {
	if console := consumerConsole(t, "--color=always"); !strings.Contains(console, "\x1b[") {
		t.Fatalf("--color=always console has no escape sequence:\n%q", console)
	}
	for _, argv := range [][]string{{"--color=never"}, {}} {
		if console := consumerConsole(t, argv...); strings.Contains(console, "\x1b[") {
			t.Fatalf("%v console carries an escape sequence off a terminal:\n%q", argv, console)
		}
	}
}

func consumerTerminal(t *testing.T, argv ...string) term.Capability {
	t.Helper()
	tc := terminalConfig(parseRuntime(t, argv...))
	tc.Probe = terminalProbe{}
	tc.Getenv = func(k string) string {
		if k == "TERM" {
			return "xterm-256color"
		}
		return ""
	}
	return term.New(tc)
}

// terminalProbe answers "terminal" on every stream.
type terminalProbe struct{}

func (terminalProbe) IsTerminal(term.Stream) bool { return true }

func (terminalProbe) Size(term.Stream) (term.Size, bool) { return term.Size{Rows: 40, Cols: 120}, true }

func assertNoInput(t *testing.T) {
	if !consumerTerminal(t).Prompt() {
		t.Fatal("precondition: baseline on a terminal does not permit prompting")
	}
	if consumerTerminal(t, "--no-input").Prompt() {
		t.Fatal("--no-input: keel-demo-dev's terminal still permits prompting")
	}
}

// DHF-TEST: keel/requirement-166 (keel/ac-707)
func assertPlain(t *testing.T) {
	plain := consumerTerminal(t, "--plain")
	if plain.Color() || plain.Animation() || plain.Prompt() {
		t.Fatalf("--plain: color=%v animation=%v prompt=%v, want all false", plain.Color(), plain.Animation(), plain.Prompt())
	}
	if console := consumerConsole(t, "--plain", "--color=always"); strings.Contains(console, "\x1b[") {
		t.Fatalf("--plain console carries an escape sequence:\n%q", console)
	}
}
