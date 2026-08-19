package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
)

// gateStartedNames returns, in order, the stages a run reported as started, so a
// test can see exactly which stages executed rather than only what they returned.
func gateStartedNames(records []map[string]any) []string {
	var names []string
	for _, rec := range records {
		if rec["msg"] != "gate started" {
			continue
		}
		name, _ := rec["gate"].(string)
		names = append(names, name)
	}
	return names
}

// DHF-TEST: keel/requirement-136 (keel/ac-541)
func TestGateStageRunsOnlyTheNamedStage(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}

	logger, cap := testLogger("keel-dev")
	if err := runGateStage(context.Background(), logger, nil, root, "command-tree"); err != nil {
		t.Fatalf("gate stage command-tree should pass over a valid tree: %v", err)
	}
	if got := gateStartedNames(cap.AllJSON()); len(got) != 1 || got[0] != "command-tree" {
		t.Fatalf("stages started = %v, want only [command-tree]", got)
	}
}

// DHF-TEST: keel/requirement-136 (keel/ac-541)
func TestGateStageRefusesAnUnknownStageNamingIt(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}

	err = runGateStage(context.Background(), discardLogger(), nil, root, "no-such-stage")
	var usage cli.UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("unknown stage error = %v, want a cli.UsageError", err)
	}
	if !strings.Contains(err.Error(), "no-such-stage") {
		t.Fatalf("unknown stage refusal must name the offending token, got %q", err.Error())
	}
}

// DHF-TEST: keel/requirement-136 (keel/ac-542)
func TestBareCIStillRunsEveryStageInBatteryOrder(t *testing.T) {
	tree := commandTree()
	ci := commandSpecByPath(tree, "ci")
	if ci == nil || ci.Handler == nil {
		t.Fatal("keel-dev ci must stay an invocable leaf command")
	}
	if len(ci.Subcommands) != 0 {
		t.Fatalf("keel-dev ci must stay a leaf; it declares %d subcommands", len(ci.Subcommands))
	}
	want := cli.PositionalSpec{Name: "args", Min: 0, Max: 0}
	if len(ci.Positionals) != 1 || ci.Positionals[0] != want {
		t.Fatalf("ci positionals = %+v, want %+v — the bare form takes no stage token", ci.Positionals, want)
	}

	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	var battery []string
	for _, s := range ciSteps(context.Background(), discardLogger(), root) {
		battery = append(battery, s.name)
	}
	if !stringSliceEqual(battery, gateStageNames()) {
		t.Fatalf("bare battery = %v, want every declared stage in order %v", battery, gateStageNames())
	}

	var ran []string
	record := func(name string) step {
		return step{name: name, fn: func(context.Context, *slog.Logger, string) error {
			ran = append(ran, name)
			return nil
		}}
	}
	steps := []step{record("one"), record("two"), record("three")}
	if err := runGateSteps(context.Background(), discardLogger(), nil, root, steps); err != nil {
		t.Fatalf("battery loop failed: %v", err)
	}
	if !stringSliceEqual(ran, []string{"one", "two", "three"}) {
		t.Fatalf("battery ran %v, want every step in declaration order", ran)
	}
}

// DHF-TEST: keel/requirement-136 (keel/ac-543)
func TestGateStageFailureMatchesTheSameStageInsideTheBattery(t *testing.T) {
	requireTool(t, "git")
	requireTool(t, "gofmt")

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	writeFile(t, dir, "unformatted.go", "package p\n\nvar    Bad = 1\n")
	mustRun(t, dir, "git", "add", "unformatted.go")

	ctx := context.Background()
	steps := ciSteps(ctx, discardLogger(), dir)
	through := -1
	for i, s := range steps {
		if s.name == "gofmt" {
			through = i
			break
		}
	}
	if through < 0 {
		t.Fatal("ci gate is missing the gofmt stage")
	}

	batteryErr := runGateSteps(ctx, discardLogger(), nil, dir, steps[:through+1])
	aloneLogger, aloneCap := testLogger("keel-dev")
	aloneErr := runGateStage(ctx, aloneLogger, nil, dir, "gofmt")

	if batteryErr == nil {
		t.Fatal("gofmt must fail over an unformatted tracked file inside the battery")
	}
	if aloneErr == nil {
		t.Fatal("gofmt must fail over an unformatted tracked file when run alone")
	}
	if batteryErr.Error() != aloneErr.Error() {
		t.Fatalf("failure output differs:\nalone:   %s\nbattery: %s", aloneErr.Error(), batteryErr.Error())
	}
	if got := gateStartedNames(aloneCap.AllJSON()); !stringSliceEqual(got, []string{"gofmt"}) {
		t.Fatalf("stages started alone = %v, want only [gofmt]", got)
	}
}

// DHF-TEST: keel/requirement-136 (keel/ac-543)
func TestGateStageFailureCarriesTheMeansOfComplianceEitherWay(t *testing.T) {
	remedy := "remedy: register the coinage in the committed dictionary."
	failing := step{name: "remedy-probe", fn: func(context.Context, *slog.Logger, string) error {
		return withRemedy(errors.New("stage failed"), remedy)
	}}
	green := step{name: "before", fn: func(context.Context, *slog.Logger, string) error { return nil }}

	ctx := context.Background()
	inBattery := runGateSteps(ctx, discardLogger(), nil, ".", []step{green, failing})
	alone := runGateSteps(ctx, discardLogger(), nil, ".", []step{failing})
	if inBattery == nil || alone == nil {
		t.Fatalf("both runs must fail; battery=%v alone=%v", inBattery, alone)
	}
	if inBattery.Error() != alone.Error() {
		t.Fatalf("failure output differs:\nalone:   %s\nbattery: %s", alone.Error(), inBattery.Error())
	}
	if !strings.Contains(alone.Error(), remedy) {
		t.Fatalf("stage run alone dropped the means-of-compliance text, got %q", alone.Error())
	}
}

// DHF-TEST: keel/requirement-136 (keel/ac-544)
func TestGateStageInventoryMatchesTheRunningBatteryBothWays(t *testing.T) {
	tree := commandTree()
	gate := commandSpecByPath(tree, "gate")
	if gate == nil {
		t.Fatal("keel-dev declares no gate namespace")
	}
	declared := make([]string, 0, len(gate.Subcommands))
	for _, sub := range gate.Subcommands {
		if sub.Handler == nil {
			t.Fatalf("gate stage %q is listed but has no handler — listed and not invocable", sub.Name)
		}
		declared = append(declared, sub.Name)
	}
	if !stringSliceEqual(declared, gateStageNames()) {
		t.Fatalf("declared stage commands = %v, want the battery's stages %v", declared, gateStageNames())
	}

	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	var running []string
	for _, s := range ciSteps(context.Background(), discardLogger(), root) {
		running = append(running, s.name)
	}
	if !stringSliceEqual(declared, running) {
		t.Fatalf("declared stage commands = %v, want the stages a real run executes %v", declared, running)
	}

	var buf bytes.Buffer
	if err := tree.RenderHelpJSON(&buf); err != nil {
		t.Fatalf("RenderHelpJSON: %v", err)
	}
	var inventory []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(buf.Bytes(), &inventory); err != nil {
		t.Fatalf("parse command inventory: %v", err)
	}
	listed := map[string]bool{}
	for _, cmd := range inventory {
		listed[cmd.Path] = true
	}
	invocable := map[string]bool{}
	for _, name := range declared {
		invocable[name] = true
		if !listed["gate "+name] {
			t.Errorf("stage %q is invocable but missing from the command inventory", name)
		}
	}
	for path := range listed {
		stage, ok := strings.CutPrefix(path, "gate ")
		if ok && !invocable[stage] {
			t.Errorf("command inventory lists gate stage %q that is not invocable", stage)
		}
	}
}
