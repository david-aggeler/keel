package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/term"
)

// DHF-TEST: keel/requirement-164 (keel/ac-697)
func TestKeelDemoExplicitHelpAndVersionRenderToStdoutOnly(t *testing.T) {
	exe := buildDemoExe(t)
	for _, arg := range []string{"-h", "--help", "--help-all", "--help-json", "--version"} {
		t.Run(arg, func(t *testing.T) {
			stdout, stderr, code := runDemoStreams(t, exe, arg)
			if code != 0 {
				t.Fatalf("keel-demo %s exit = %d, want 0\nstderr:\n%s", arg, code, stderr)
			}
			if strings.TrimSpace(stdout) == "" {
				t.Fatalf("keel-demo %s stdout is empty", arg)
			}
			if stderr != "" {
				t.Fatalf("keel-demo %s stderr = %q, want empty", arg, stderr)
			}
		})
	}
	stdout, stderr, code := runDemoStreams(t, exe, "--color=sometimes")
	if code != 2 || stdout != "" || stderr == "" {
		t.Fatalf("keel-demo --color=sometimes code=%d stdout=%q stderr=%q, want usage error on stderr, exit 2", code, stdout, stderr)
	}
}

func buildDemoExe(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "keel-demo")
	build := exec.Command("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\noutput:\n%s", err, out)
	}
	return exe
}

func runDemoStreams(t *testing.T, exe string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = t.TempDir()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), stderr.String(), 0
	case errors.As(err, &exitErr):
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}
	t.Fatalf("keel-demo %v failed before process exit: %v", args, err)
	return "", "", -1
}

// DHF-TEST: keel/requirement-166 (keel/ac-708), keel/requirement-109
func TestKeelDemoHandlesEveryOperatorPolicyFlag(t *testing.T) {
	checks := map[string]func(*testing.T){
		"quiet":    func(t *testing.T) { assertQuietIsAConsoleFloor(t) },
		"color":    func(t *testing.T) { assertColorPolicy(t) },
		"no-input": func(t *testing.T) { assertNoInput(t) },
		"plain":    func(t *testing.T) { assertPlain(t) },
	}
	assertEveryGlobalFlagHasParity(t, checks)
}

// consumerRun builds keel-demo's real logger config for argv with the console
// on a buffer and both file sinks in a temp dir, emits one record at each
// severity, and returns the console output plus the normalized .log and
// .jsonl records.
func consumerRun(t *testing.T, argv ...string) (console, text, jsonl string) {
	t.Helper()
	dir := t.TempDir()
	var buf bytes.Buffer
	cfg := loggerConfig(parseRuntime(t, argv...))
	cfg.Writer = &buf
	cfg.TextDir = dir
	cfg.JSONLDir = dir
	logger, err := logging.New(cfg)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	logger.Debug("probe-debug")
	logger.Info("probe-info")
	logger.Warn("probe-warn")
	logger.Error("probe-error")
	_ = logger.Close()
	return buf.String(), normalizedSink(t, dir, ".log"), normalizedSink(t, dir, ".jsonl")
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
	cfg, _, err := cli.ParseGlobalConfig(argv)
	if err != nil {
		t.Fatalf("ParseGlobalConfig(%q): %v", argv, err)
	}
	return cfg
}

// normalizedSink returns the records of the one file with ext under dir with
// their timestamps removed, so two runs compare record for record.
func normalizedSink(t *testing.T, dir, ext string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"+ext))
	if err != nil || len(matches) != 1 {
		t.Fatalf("want one %s sink in %s, got %v (err %v)", ext, dir, matches, err)
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read %s: %v", matches[0], err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if ext == ".jsonl" {
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("parse %s line %q: %v", ext, line, err)
			}
			delete(rec, "ts")
			delete(rec, "time")
			norm, _ := json.Marshal(rec)
			out = append(out, string(norm))
			continue
		}
		fields := strings.SplitN(line, "\t", 2)
		out = append(out, fields[len(fields)-1])
	}
	return strings.Join(out, "\n")
}

// DHF-TEST: keel/requirement-166 (keel/ac-706)
func assertQuietIsAConsoleFloor(t *testing.T) {
	loudConsole, loudText, loudJSONL := consumerRun(t)
	quietConsole, quietText, quietJSONL := consumerRun(t, "-q")
	if !strings.Contains(loudConsole, "probe-info") {
		t.Fatalf("baseline console missing Info:\n%s", loudConsole)
	}
	for _, want := range []string{"probe-warn", "probe-error"} {
		if !strings.Contains(quietConsole, want) {
			t.Fatalf("-q console missing %s:\n%s", want, quietConsole)
		}
	}
	for _, gone := range []string{"probe-debug", "probe-info"} {
		if strings.Contains(quietConsole, gone) {
			t.Fatalf("-q console still carries %s:\n%s", gone, quietConsole)
		}
	}
	if !strings.Contains(quietText, "probe-debug") || !strings.Contains(quietJSONL, "probe-debug") {
		t.Fatalf("-q file sinks lost the Debug record:\n%s\n%s", quietText, quietJSONL)
	}
	if loudText != quietText {
		t.Fatalf(".log records differ with -q:\nwithout:\n%s\nwith:\n%s", loudText, quietText)
	}
	if loudJSONL != quietJSONL {
		t.Fatalf(".jsonl records differ with -q:\nwithout:\n%s\nwith:\n%s", loudJSONL, quietJSONL)
	}
}

func assertColorPolicy(t *testing.T) {
	if console, _, _ := consumerRun(t, "--color=always"); !strings.Contains(console, "\x1b[") {
		t.Fatalf("--color=always console has no escape sequence:\n%q", console)
	}
	for _, argv := range [][]string{{"--color=never"}, {}} {
		if console, _, _ := consumerRun(t, argv...); strings.Contains(console, "\x1b[") {
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
		t.Fatal("--no-input: the consumer's terminal still permits prompting")
	}
}

// DHF-TEST: keel/requirement-166 (keel/ac-707)
func assertPlain(t *testing.T) {
	plain := consumerTerminal(t, "--plain")
	if plain.Color() || plain.Animation() || plain.Prompt() {
		t.Fatalf("--plain: color=%v animation=%v prompt=%v, want all false", plain.Color(), plain.Animation(), plain.Prompt())
	}
	if console, _, _ := consumerRun(t, "--plain", "--color=always"); strings.Contains(console, "\x1b[") {
		t.Fatalf("--plain console carries an escape sequence:\n%q", console)
	}
	if level := parseRuntime(t, "--plain").ConsoleLevel(slog.LevelInfo); level != slog.LevelInfo {
		t.Fatalf("--plain changed the console floor to %v", level)
	}
}
