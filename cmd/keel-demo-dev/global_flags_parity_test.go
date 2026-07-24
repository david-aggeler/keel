package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
)

// DHF-TEST: keel/requirement-109
func TestKeelDemoDevRoutesEveryGlobalActionFlag(t *testing.T) {
	wantVersion := rootVersionSemver(t)

	for _, c := range globalActionFlagCases(t) {
		t.Run(c.arg, func(t *testing.T) {
			out, code := captureDemoDevOutput(t, func() int { return run([]string{c.arg}) })
			assertGlobalActionFlagOutput(t, c, out, code, "keel-demo-dev", wantVersion)
		})
	}
}

// DHF-TEST: keel/requirement-110 (keel/ac-391)
func TestKeelDemoDevVersionComesFromRootVersionFile(t *testing.T) {
	want := rootVersionSemver(t)

	out, code := captureDemoDevOutput(t, func() int { return run([]string{"--version"}) })
	got := strings.TrimSpace(out)
	if code != 0 {
		t.Fatalf("keel-demo-dev --version exit = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.HasPrefix(got, want) {
		t.Fatalf("keel-demo-dev --version = %q, want prefix %q", got, want)
	}
	if got == "dev" || got == "demo" {
		t.Fatalf("keel-demo-dev --version reported placeholder %q", got)
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
	arg   string
	field string
}

func globalActionFlagCases(t *testing.T) []globalActionFlagCase {
	t.Helper()
	base, _, err := cli.ParseGlobalConfig(nil)
	if err != nil {
		t.Fatalf("parse baseline global config: %v", err)
	}

	var cases []globalActionFlagCase
	for _, spec := range cli.GlobalFlagSpecs() {
		cfg, rest, err := cli.ParseGlobalConfig([]string{"--" + spec.Name})
		if err != nil || len(rest) != 0 {
			continue
		}
		for _, field := range changedRuntimeBoolFields(base, cfg) {
			if field == "Verbose" || field == "NoHeader" {
				continue
			}
			cases = append(cases, globalActionFlagCase{arg: "--" + spec.Name, field: field})
		}
	}
	if len(cases) == 0 {
		t.Fatal("GlobalFlagSpecs produced no action flags")
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].arg < cases[j].arg })
	return cases
}

func changedRuntimeBoolFields(base, cfg cli.RuntimeConfig) []string {
	baseValue := reflect.ValueOf(base)
	cfgValue := reflect.ValueOf(cfg)
	cfgType := cfgValue.Type()
	var changed []string
	for i := 0; i < cfgValue.NumField(); i++ {
		if cfgValue.Field(i).Kind() != reflect.Bool {
			continue
		}
		if cfgValue.Field(i).Bool() != baseValue.Field(i).Bool() {
			changed = append(changed, cfgType.Field(i).Name)
		}
	}
	return changed
}

func assertGlobalActionFlagOutput(t *testing.T, c globalActionFlagCase, out string, code int, program, wantVersion string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("%s %s exit = %d, want 0\noutput:\n%s", program, c.arg, code, out)
	}
	if strings.Contains(out, "usage: "+program+" <command> [args]") {
		t.Fatalf("%s %s reached empty-args dispatch usage:\n%s", program, c.arg, out)
	}

	switch c.field {
	case "Help", "HelpAll":
		if !strings.Contains(out, program) {
			t.Fatalf("%s %s output missing program name %q:\n%s", program, c.arg, program, out)
		}
	case "HelpJSON":
		var inventory []map[string]any
		if err := json.Unmarshal([]byte(out), &inventory); err != nil {
			t.Fatalf("%s %s output is not JSON inventory: %v\n%s", program, c.arg, err, out)
		}
		if len(inventory) == 0 {
			t.Fatalf("%s %s emitted empty JSON inventory", program, c.arg)
		}
	case "Version":
		got := strings.TrimSpace(out)
		if !strings.HasPrefix(got, wantVersion) {
			t.Fatalf("%s %s output = %q, want prefix %q", program, c.arg, got, wantVersion)
		}
		if got == "dev" || got == "demo" {
			t.Fatalf("%s %s reported placeholder %q", program, c.arg, got)
		}
	default:
		if strings.TrimSpace(out) == "" {
			t.Fatalf("%s %s output is empty for action field %s", program, c.arg, c.field)
		}
	}
}
