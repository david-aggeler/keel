package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
)

// DHF-TEST: keel/requirement-109
func TestKeelDemoRoutesEveryGlobalActionFlag(t *testing.T) {
	wantVersion := rootVersionSemver(t)

	for _, c := range globalActionFlagCases(t) {
		t.Run(c.arg, func(t *testing.T) {
			out, code := runDemo(t, c.arg)
			assertGlobalActionFlagOutput(t, c, out, code, "keel-demo", wantVersion)
		})
	}
}

// DHF-TEST: keel/requirement-110 (keel/ac-391)
func TestKeelDemoVersionComesFromRootVersionFile(t *testing.T) {
	want := rootVersionSemver(t)

	out, code := runDemo(t, "--version")
	got := strings.TrimSpace(out)
	if code != 0 {
		t.Fatalf("keel-demo --version exit = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.HasPrefix(got, want) {
		t.Fatalf("keel-demo --version = %q, want prefix %q", got, want)
	}
	if got == "dev" || got == "demo" {
		t.Fatalf("keel-demo --version reported placeholder %q", got)
	}
}

func rootVersionSemver(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	return strings.TrimSpace(string(body))
}

type globalActionFlagCase struct {
	arg string
}

// globalActionFlagCases lists every keel-owned global flag that asks for an
// action instead of a run: a value-less flag for which keel/cli's Start serves
// the invocation even when a command word follows it.
func globalActionFlagCases(t *testing.T) []globalActionFlagCase {
	t.Helper()
	tree := commandTree()
	word := tree.Subcommands[0].Name
	var cases []globalActionFlagCase
	for _, spec := range cli.GlobalFlagSpecs() {
		if spec.Value != "" {
			continue
		}
		arg := "--" + spec.Name
		var done bool
		discardProcessStreams(t, func() { _, _, _, done = tree.Start([]string{arg, word}) })
		if done {
			cases = append(cases, globalActionFlagCase{arg: arg})
		}
	}
	if len(cases) == 0 {
		t.Fatal("GlobalFlagSpecs produced no action flags")
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].arg < cases[j].arg })
	return cases
}

// discardProcessStreams runs fn with os.Stdout and os.Stderr pointed at the
// null device.
func discardProcessStreams(t *testing.T, fn func()) {
	t.Helper()
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = null, null
	defer func() { os.Stdout, os.Stderr = oldStdout, oldStderr }()
	fn()
}

func assertGlobalActionFlagOutput(t *testing.T, c globalActionFlagCase, out string, code int, program, wantVersion string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("%s %s exit = %d, want 0\noutput:\n%s", program, c.arg, code, out)
	}
	if strings.Contains(out, "usage: "+program+" <command> [args]") {
		t.Fatalf("%s %s reached empty-args dispatch usage:\n%s", program, c.arg, out)
	}

	switch c.arg {
	case "--help", "--help-all":
		if !strings.Contains(out, program) {
			t.Fatalf("%s %s output missing program name %q:\n%s", program, c.arg, program, out)
		}
	case "--help-json":
		var inventory []map[string]any
		if err := json.Unmarshal([]byte(out), &inventory); err != nil {
			t.Fatalf("%s %s output is not JSON inventory: %v\n%s", program, c.arg, err, out)
		}
		if len(inventory) == 0 {
			t.Fatalf("%s %s emitted empty JSON inventory", program, c.arg)
		}
	case "--version":
		got := strings.TrimSpace(out)
		if !strings.HasPrefix(got, wantVersion) {
			t.Fatalf("%s %s output = %q, want prefix %q", program, c.arg, got, wantVersion)
		}
		if got == "dev" || got == "demo" {
			t.Fatalf("%s %s reported placeholder %q", program, c.arg, got)
		}
	default:
		if strings.TrimSpace(out) == "" {
			t.Fatalf("%s %s output is empty", program, c.arg)
		}
	}
}
