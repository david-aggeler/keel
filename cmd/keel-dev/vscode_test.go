package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// DHF-TEST: keel/requirement-35, keel/requirement-37
func TestVSCodeRunBlockedLaneUsesEngineProtocol(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	writeFile(t, root, "main_test.go", "package p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n")

	t.Setenv("PATH", t.TempDir())

	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::test-fast"})
	if err == nil {
		t.Fatal("blocked lane returned nil error; want non-zero terminal failure")
	}

	events := decodeRunEvents(t, protocol.String())
	if len(events) < 3 {
		t.Fatalf("events = %+v, want run_started, failed, run_finished", events)
	}
	if events[0].Event != "run_started" {
		t.Fatalf("first event = %+v, want run_started", events[0])
	}
	if events[1].Event != "failed" || !strings.Contains(events[1].Message, "lane blocked:") {
		t.Fatalf("second event = %+v, want engine lane blocked failure", events[1])
	}
	if events[len(events)-1].Event != "run_finished" {
		t.Fatalf("last event = %+v, want run_finished", events[len(events)-1])
	}
	if events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode == 0 {
		t.Fatalf("terminal event = %+v, want non-zero exit_code", events[len(events)-1])
	}
	for _, event := range events {
		if event.RunID == "" || event.Workspace == "" || event.Version != 1 || event.Time.IsZero() {
			t.Fatalf("event missing production stamp: %+v", event)
		}
	}
}

func TestVSCodeHandlersDispatchDiscoveryPlanAndLintRun(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	var discover bytes.Buffer
	if err := handleVSCodeTestsDiscover(contextWithVSCodeTestState(root, &discover), []string{"--format", "json"}); err != nil {
		t.Fatalf("discover handler: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discover JSON: %v", err)
	}
	if !discoveryHasLane(doc, vscodeLaneLint) {
		t.Fatalf("discover missing lint lane: %+v", doc.Items)
	}

	var plan bytes.Buffer
	if err := handleVSCodeTestsPlan(contextWithVSCodeTestState(root, &plan), []string{"--format", "json", "--id", vscodeLaneLint}); err != nil {
		t.Fatalf("plan handler: %v", err)
	}
	var setup vscode.SetupPlan
	if err := json.Unmarshal(plan.Bytes(), &setup); err != nil {
		t.Fatalf("plan JSON: %v", err)
	}
	if len(setup.Items) != 1 || setup.Items[0].ID != vscodeLaneLint {
		t.Fatalf("plan items = %+v, want lint lane", setup.Items)
	}

	var protocol bytes.Buffer
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", vscodeLaneLint}); err != nil {
		t.Fatalf("lint run handler: %v\n%s", err, protocol.String())
	}
	events := decodeRunEvents(t, protocol.String())
	if events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode != 0 {
		t.Fatalf("lint run terminal = %+v, want exit 0", events[len(events)-1])
	}
}

// DHF-TEST: keel/requirement-63
func TestCanonicalTestBridgeRunUsesPackageOwnedDispatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Dir(testbridge.RunLockPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testbridge.RunLockPath(root), []byte(`{"pid":1,"created_at":"2026-07-13T00:00:00Z","ids":["keel::lane::lint"],"token":"foreign"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var protocol bytes.Buffer
	err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", vscodeLaneLint})
	if err == nil || !strings.Contains(err.Error(), "keel/testbridge: run lock already exists") {
		t.Fatalf("canonical run err = %v, want package-owned run lock refusal", err)
	}
	events := decodeRunEvents(t, protocol.String())
	if len(events) != 3 || events[0].Event != "run_started" || events[1].Event != "errored" || events[2].Event != "run_finished" {
		t.Fatalf("canonical run events = %+v, want package-owned start/error/finish stream", events)
	}
}

// DHF-TEST: keel/requirement-63
func TestCanonicalTestBridgeRunEventsUseResolvedWorkspace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Dir(testbridge.RunLockPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testbridge.RunLockPath(root), []byte(`{"pid":1,"created_at":"2026-07-13T00:00:00Z","ids":["keel::lane::lint"],"token":"foreign"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var plan bytes.Buffer
	if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &plan), []string{"test-bridge", "tests", "desired-state", "--format", "json", "--id", vscodeLaneLint}); err != nil {
		t.Fatalf("canonical desired-state: %v", err)
	}
	var setup vscode.SetupPlan
	if err := json.Unmarshal(plan.Bytes(), &setup); err != nil {
		t.Fatalf("desired-state JSON: %v\n%s", err, plan.String())
	}

	var protocol bytes.Buffer
	err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", vscodeLaneLint})
	if err == nil || !strings.Contains(err.Error(), "keel/testbridge: run lock already exists") {
		t.Fatalf("canonical run err = %v, want package-owned run lock refusal", err)
	}
	for _, event := range decodeRunEvents(t, protocol.String()) {
		if event.Workspace != setup.Workspace {
			t.Fatalf("run event workspace = %q, want desired-state workspace %q in %+v", event.Workspace, setup.Workspace, event)
		}
	}
}

// DHF-TEST: keel/requirement-64
func TestVSCodePlanCarriesComparableDevtoolIdentity(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	var plan bytes.Buffer
	if err := handleVSCodeTestsPlan(contextWithVSCodeTestState(root, &plan), []string{"--format", "json", "--id", vscodeLaneLint}); err != nil {
		t.Fatalf("plan handler: %v", err)
	}
	var setup vscode.SetupPlan
	if err := json.Unmarshal(plan.Bytes(), &setup); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, plan.String())
	}
	if setup.Devtool.Name != "keel-dev" || setup.Devtool.Version == "" || setup.Devtool.Commit == "" || setup.Devtool.BuiltAt == "" {
		t.Fatalf("devtool identity = %+v, want name plus version, commit, built_at", setup.Devtool)
	}
}

// DHF-TEST: keel/requirement-64
func TestVSCodeRunLegacyFormatArgvFailsAsVersionSkew(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	err := commandTree().Dispatch(contextWithVSCodeTestState(root, io.Discard), []string{"vscode", "tests", "run", "--format", "jsonl", "--id", vscodeLaneLint})
	if err == nil {
		t.Fatal("legacy vscode tests run --format returned nil error")
	}
	message := err.Error()
	for _, want := range []string{"version skew", "VSIX", "legacy", "devtool", "keel-dev"} {
		if !strings.Contains(message, want) {
			t.Fatalf("legacy skew error = %q, want %q", message, want)
		}
	}
	if strings.Contains(message, "unknown flag") || strings.Contains(message, "usage:") {
		t.Fatalf("legacy skew error leaked usage text: %q", message)
	}
}

// DHF-TEST: keel/requirement-61
func TestVSCodeCommandSpecOmitsDemoSubtree(t *testing.T) {
	spec := vscodeCommandSpec()
	for _, sub := range spec.Subcommands {
		if sub.Name == "demo" {
			t.Fatalf("vscode command spec still exposes demo subtree: %+v", sub)
		}
	}
}

// DHF-TEST: keel/requirement-40
func TestVSCodeConfigHandlersInitAndUpgrade(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")

	if err := handleVSCodeConfigInit(contextWithVSCodeTestState(root, io.Discard), nil); err != nil {
		t.Fatalf("config init: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, ".vscode", "test-bridge.json"))
	if err != nil {
		t.Fatalf("read initialized config: %v", err)
	}
	if !strings.Contains(string(body), `"command": "bin/keel-dev"`) {
		t.Fatalf("initialized config does not contain keel devtool command:\n%s", body)
	}

	writeFile(t, root, filepath.Join(".vscode", "test-bridge.json"), `{"version":1,"command":"bin/custom","args":["vscode","tests"],"displayName":"Custom"}`+"\n")
	if err := handleVSCodeConfigUpgrade(contextWithVSCodeTestState(root, io.Discard), nil); err != nil {
		t.Fatalf("config upgrade: %v", err)
	}
	body, err = os.ReadFile(filepath.Join(root, ".vscode", "test-bridge.json"))
	if err != nil {
		t.Fatalf("read upgraded config: %v", err)
	}
	if !strings.Contains(string(body), `"command": "bin/custom"`) || !strings.Contains(string(body), `"version": 3`) || !strings.Contains(string(body), `"args": []`) {
		t.Fatalf("upgraded config did not preserve command and stamp current version:\n%s", body)
	}
}

// DHF-TEST: keel/requirement-42
func TestVSCodeBridgeDocsPinCanonicalTestBridgeArgv(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "docs", "vscode-bridge.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"go run ./cmd/keel-dev test-bridge tests discover --format json",
		"go run ./cmd/keel-dev test-bridge tests desired-state --format json --id keel::lane::test-fast",
		"go run ./cmd/keel-dev test-bridge tests run --id keel::lane::test-fast",
		"go run ./cmd/keel-dev test-bridge config upgrade",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("docs/vscode-bridge.md missing canonical argv %q", want)
		}
	}
	for _, forbidden := range []string{
		"vscode tests plan",
		"vscode tests discover",
		"vscode tests run",
		"vscode config upgrade",
		"vscode demo",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("docs/vscode-bridge.md still contains stale wire argv %q", forbidden)
		}
	}
}

// DHF-TEST: keel/requirement-61
func TestVSIXChangelogSignalsDemoWireRemoval(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "vsix", "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"demo-toggle", "vscode demo", "keel-demo-dev"} {
		if !strings.Contains(text, want) {
			t.Fatalf("vsix/CHANGELOG.md missing demo removal signal %q", want)
		}
	}
}

func contextWithVSCodeTestState(root string, protocol io.Writer) context.Context {
	return withRunStateProtocol(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, root, protocol)
}

func decodeRunEvents(t *testing.T, raw string) []vscode.RunEvent {
	t.Helper()
	var events []vscode.RunEvent
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event vscode.RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid run-event JSON line %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func TestVSCodeDiscoveryAndPlanExposeKeelLaneSet(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")

	built0, buildErr0 := buildVSCodeDiscovery(root)
	if buildErr0 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr0)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built0); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	for _, want := range []string{"keel::lane::lint", "keel::lane::test-fast", "keel::lane::test-coverage"} {
		if !discoveryHasLane(doc, want) {
			t.Fatalf("discovery missing lane %q: %+v", want, doc.Items)
		}
	}

	built1, buildErr1 := buildVSCodePlan(root, []string{"keel::lane::test-fast"})
	if buildErr1 != nil {
		t.Fatalf("buildVSCodePlan: %v", buildErr1)
	}
	var plan bytes.Buffer
	if err := testbridge.EncodeDocument(&plan, built1); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var setup vscode.SetupPlan
	if err := json.Unmarshal(plan.Bytes(), &setup); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, plan.String())
	}
	if len(setup.Checks) == 0 {
		t.Fatalf("plan checks empty; want keel prerequisites")
	}
}

// DHF-TEST: keel/requirement-46
func TestVSCodeDiscoveryEmitsStructuredOrderedTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")

	built2, buildErr2 := buildVSCodeDiscovery(root)
	if buildErr2 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr2)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built2); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}

	for _, forbidden := range []string{"keel::root"} {
		if item, ok := discoveryItemByID(doc, forbidden); ok {
			t.Fatalf("discovery emitted forbidden item %q: %+v", forbidden, item)
		}
	}

	top := map[string]vscode.TestItem{}
	for _, item := range doc.Items {
		if item.ParentID == "" {
			top[item.ID] = item
		}
		if strings.HasPrefix(item.Label, "c.") {
			t.Fatalf("discovery emitted reserved c.* label: %+v", item)
		}
		if strings.Contains(item.ID, "a.") || strings.Contains(item.ID, "b.") || strings.Contains(item.ID, "d.") {
			t.Fatalf("item id encodes ordinal %q for label %q", item.ID, item.Label)
		}
		assertDiscoveryKindAllowedBySchema(t, item.Kind)
	}

	wantTop := map[string]struct {
		label string
		sort  string
	}{
		"keel::maintenance": {label: "a. Maintenance", sort: "a"},
		"keel::lanes":       {label: "b. Lanes", sort: "b"},
		"keel::frameworks":  {label: "d. Frameworks", sort: "d"},
	}
	for id, want := range wantTop {
		item, ok := top[id]
		if !ok {
			t.Fatalf("top-level group %q missing; top-level items: %+v", id, top)
		}
		if item.Label != want.label || item.Kind != "group" || item.SortText != want.sort || item.Runnable {
			t.Fatalf("top-level group %q = %+v, want label=%q kind=group sort_text=%q runnable=false", id, item, want.label, want.sort)
		}
	}

	goRoot, ok := discoveryItemByID(doc, "go::root")
	if !ok {
		t.Fatal("discovery missing go::root")
	}
	if goRoot.ParentID != "keel::frameworks" || goRoot.Label != "d.1 Go" || goRoot.SortText != "d.001" {
		t.Fatalf("go::root = %+v, want parent keel::frameworks label d.1 Go sort_text d.001", goRoot)
	}
}

// DHF-TEST: keel/requirement-51
func TestVSCodeDiscoveryRendersFileLanesAndDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "exec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("exec", "exec_test.go"), "package exec\n\nimport \"testing\"\n\nfunc TestExec(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{
  "version": 1,
  "lanes": [
    {
      "id": "go-log",
      "label": "log subsystem",
      "order": "b.40",
      "description": "fast log checks",
      "members": [{"go":"./log/..."}],
      "prerequisites": ["go-toolchain"]
    },
    {
      "id": "core",
      "label": "core rollup",
      "order": "b.50",
      "members": [{"lane":"go-log"},{"lane":"lint"},{"go":"./exec/..."}],
      "prerequisites": ["keel-module-root"]
    },
    {
      "id": "bad",
      "label": "bad member",
      "order": "b.51",
      "members": [{"unknown":"x"}]
    }
  ]
}`)

	built3, buildErr3 := buildVSCodeDiscovery(root)
	if buildErr3 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr3)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built3); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}

	logLane, ok := discoveryItemByID(doc, "keel::lane::go-log")
	if !ok {
		t.Fatalf("discovery missing file lane go-log: %+v", doc.Items)
	}
	if logLane.ParentID != "keel::lanes" || logLane.Label != "b.40 log subsystem" || logLane.SortText != "b.040" || !logLane.Runnable {
		t.Fatalf("go-log lane = %+v, want runnable b.40 lane under Lanes", logLane)
	}
	core, ok := discoveryItemByID(doc, "keel::lane::core")
	if !ok {
		t.Fatalf("discovery missing composed lane core: %+v", doc.Items)
	}
	if core.Label != "b.50 core rollup" || !stringSlicesEqual(core.RequiredResources, []string{"go-toolchain", "keel-module-root", "stub-binaries"}) {
		t.Fatalf("core lane = %+v, want inherited required resources", core)
	}
	if _, ok := discoveryItemByID(doc, "keel::lane::bad"); ok {
		t.Fatalf("lane-level invalid member should suppress only bad lane: %+v", doc.Items)
	}
	if !discoveryHasDiagnosticContaining(doc, "unknown member form") {
		t.Fatalf("discovery missing lane diagnostic for unknown member: %+v", doc.Items)
	}
}

// DHF-TEST: keel/requirement-51
func TestVSCodeFileLaneRunDeduplicatesGoPackages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "exec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("exec", "exec_test.go"), "package exec\n\nimport \"testing\"\n\nfunc TestExec(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{
  "version": 1,
  "lanes": [
    {"id":"log-only","label":"log","order":"b.40","members":[{"go":"./log/..."}]},
    {"id":"core","label":"core","order":"b.50","members":[{"lane":"log-only"},{"root":"go"},{"go":"./log/..."}]}
  ]
}`)

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2 $3 $4" in
  "test -json ./exec ./log")
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/exec","Elapsed":0.01}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Elapsed":0.01}\n'
    ;;
  *)
    printf 'unexpected go invocation: %s\n' "$*" >&2
    exit 2
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var protocol bytes.Buffer
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::core"}); err != nil {
		t.Fatalf("core lane run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	gotCalls := calls(t, callsFile)
	if strings.Count(gotCalls, "./log") != 1 || !strings.Contains(gotCalls, "go test -json ./exec ./log") {
		t.Fatalf("core lane did not dedup packages into one go test invocation:\n%s", gotCalls)
	}
	events := decodeRunEvents(t, protocol.String())
	if !runEventsContain(events, "passed", "keel::lane::core") || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode != 0 {
		t.Fatalf("core lane events = %+v, want passed core lane and exit 0", events)
	}
}

// DHF-TEST: keel/requirement-52
func TestVSCodeLanesListAndDetect(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, dir := range []string{"log", "exec", ".vscode"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("exec", "exec_test.go"), "package exec\n\nimport \"testing\"\n\nfunc TestExec(t *testing.T) {}\n")
	lanesPath := filepath.Join(root, ".vscode", "test-lanes.json")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[{"id":"go-log","label":"log","order":"b.40","members":[{"go":"./log/..."}]}]}`+"\n")
	before, err := os.ReadFile(lanesPath)
	if err != nil {
		t.Fatal(err)
	}

	var list bytes.Buffer
	if err := handleVSCodeLanesList(contextWithVSCodeTestState(root, &list), []string{"--format", "json"}); err != nil {
		t.Fatalf("lanes list: %v", err)
	}
	var listed struct {
		Lanes []struct {
			ID       string `json:"id"`
			Source   string `json:"source"`
			Expanded struct {
				GoPackages []string `json:"go_packages"`
			} `json:"expanded"`
		} `json:"lanes"`
	}
	if err := json.Unmarshal(list.Bytes(), &listed); err != nil {
		t.Fatalf("list JSON: %v\n%s", err, list.String())
	}
	if !lanesListHasPackage(listed.Lanes, "keel::lane::go-log", "log") {
		t.Fatalf("lanes list missing expanded log lane: %+v", listed.Lanes)
	}

	var dry bytes.Buffer
	if err := handleVSCodeLanesDetect(contextWithVSCodeTestState(root, &dry), []string{"--format", "json", "--dry-run"}); err != nil {
		t.Fatalf("lanes detect --dry-run: %v", err)
	}
	if after, err := os.ReadFile(lanesPath); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("dry-run changed lanes file: err=%v before=%q after=%q", err, before, after)
	}
	var dryDoc lanesDetectDocument
	if err := json.Unmarshal(dry.Bytes(), &dryDoc); err != nil {
		t.Fatalf("dry-run JSON: %v\n%s", err, dry.String())
	}
	if dryDoc.Written || len(dryDoc.Added) != 1 || dryDoc.Added[0].ID != "go-exec" {
		t.Fatalf("dry-run doc = %+v, want go-exec added and written=false", dryDoc)
	}

	var detect bytes.Buffer
	if err := handleVSCodeLanesDetect(contextWithVSCodeTestState(root, &detect), []string{"--format", "json"}); err != nil {
		t.Fatalf("lanes detect: %v", err)
	}
	var detectDoc lanesDetectDocument
	if err := json.Unmarshal(detect.Bytes(), &detectDoc); err != nil {
		t.Fatalf("detect JSON: %v\n%s", err, detect.String())
	}
	if !detectDoc.Written || len(detectDoc.Added) != 1 || detectDoc.Added[0].ID != "go-exec" {
		t.Fatalf("detect doc = %+v, want go-exec written", detectDoc)
	}
	afterWrite, err := os.ReadFile(lanesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(afterWrite), `"id": "go-exec"`) || !strings.Contains(string(afterWrite), `"id": "go-log"`) {
		t.Fatalf("detect did not append go-exec while preserving go-log:\n%s", afterWrite)
	}
	// Round-trip guard: detect must rewrite the file with every existing member
	// still in its lowercase `{"go":...}` form. A capitalized `{"Go":...}` re-read
	// as an unknown member form would silently corrupt the hand-authored lane.
	if strings.Contains(string(afterWrite), `"Go"`) {
		t.Fatalf("detect wrote capitalized member keys — corrupts hand-authored lanes on reload:\n%s", afterWrite)
	}
	var relist bytes.Buffer
	if err := handleVSCodeLanesList(contextWithVSCodeTestState(root, &relist), []string{"--format", "json"}); err != nil {
		t.Fatalf("lanes list after detect: %v", err)
	}
	var relisted struct {
		Lanes []struct {
			ID       string `json:"id"`
			Source   string `json:"source"`
			Expanded struct {
				GoPackages []string `json:"go_packages"`
			} `json:"expanded"`
		} `json:"lanes"`
	}
	if err := json.Unmarshal(relist.Bytes(), &relisted); err != nil {
		t.Fatalf("relist JSON: %v\n%s", err, relist.String())
	}
	if !lanesListHasPackage(relisted.Lanes, "keel::lane::go-log", "log") {
		t.Fatalf("after detect, go-log no longer resolves to the log package (corrupted round-trip): %+v", relisted.Lanes)
	}

	var second bytes.Buffer
	if err := handleVSCodeLanesDetect(contextWithVSCodeTestState(root, &second), []string{"--format", "json"}); err != nil {
		t.Fatalf("lanes detect second: %v", err)
	}
	var secondDoc lanesDetectDocument
	if err := json.Unmarshal(second.Bytes(), &secondDoc); err != nil {
		t.Fatalf("second detect JSON: %v\n%s", err, second.String())
	}
	secondBytes, err := os.ReadFile(lanesPath)
	if err != nil {
		t.Fatal(err)
	}
	if secondDoc.Written || len(secondDoc.Added) != 0 || !bytes.Equal(secondBytes, afterWrite) {
		t.Fatalf("second detect not idempotent: doc=%+v", secondDoc)
	}
}

func lanesListHasPackage(lanes []struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Expanded struct {
		GoPackages []string `json:"go_packages"`
	} `json:"expanded"`
}, id, pkg string) bool {
	for _, lane := range lanes {
		if lane.ID != id {
			continue
		}
		for _, got := range lane.Expanded.GoPackages {
			if got == pkg {
				return true
			}
		}
	}
	return false
}

// DHF-TEST: keel/requirement-52
func TestVSCodeDetectLanesMaintenanceItemRunsDetect(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, dir := range []string{"exec", ".vscode"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, filepath.Join("exec", "exec_test.go"), "package exec\n\nimport \"testing\"\n\nfunc TestExec(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[]}`+"\n")

	built4, buildErr4 := buildVSCodeDiscovery(root)
	if buildErr4 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr4)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built4); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	item, ok := discoveryItemByID(doc, vscodeMaintenanceDetectLanes)
	if !ok || item.Label != "a.1 detect lanes" || !item.Runnable {
		t.Fatalf("detect lanes maintenance item = %+v, ok=%v", item, ok)
	}

	var protocol bytes.Buffer
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", vscodeMaintenanceDetectLanes}); err != nil {
		t.Fatalf("detect lanes maintenance run: %v\n%s", err, protocol.String())
	}
	if !strings.Contains(protocol.String(), "go-exec") || !runEventsContain(decodeRunEvents(t, protocol.String()), "passed", vscodeMaintenanceDetectLanes) {
		t.Fatalf("detect lanes maintenance events = %s", protocol.String())
	}
}

// DHF-TEST: keel/requirement-53
func TestVSCodeRunStartedCarriesRequestedSelection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	t.Setenv("PATH", t.TempDir())

	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", vscodeLaneTestFast})
	if err == nil {
		t.Fatal("blocked run returned nil; want non-zero")
	}
	events := decodeRunEvents(t, protocol.String())
	if len(events) == 0 || events[0].Event != "run_started" {
		t.Fatalf("events = %+v, want run_started first", events)
	}
	if got := events[0].Requested; len(got) != 1 || got[0].ID != vscodeLaneTestFast || got[0].Label != "test-fast" {
		t.Fatalf("run_started requested = %+v, want exact selected lane", got)
	}
}

// DHF-TEST: keel/requirement-53
func TestVSCodeDiscoveryAppendsExactLaneDurationHint(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[{"id":"go-log","label":"log","order":"b.40","members":[{"go":"./log/..."}]}]}`+"\n")
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join(".devtools", "vscode-runs", "old.jsonl"), strings.Join([]string{
		`{"version":1,"event":"run_started","time":"2026-07-12T10:00:00Z","run_id":"old","requested":[{"id":"keel::lane::go-log","label":"log"}]}`,
		`{"version":1,"event":"run_finished","time":"2026-07-12T10:00:08Z","run_id":"old","exit_code":0}`,
	}, "\n")+"\n")
	writeFile(t, root, filepath.Join(".devtools", "vscode-runs", "multi.jsonl"), strings.Join([]string{
		`{"version":1,"event":"run_started","time":"2026-07-12T10:10:00Z","run_id":"multi","requested":[{"id":"keel::lane::go-log","label":"log"},{"id":"keel::lane::lint","label":"lint"}]}`,
		`{"version":1,"event":"run_finished","time":"2026-07-12T10:10:01Z","run_id":"multi","exit_code":0}`,
	}, "\n")+"\n")
	writeFile(t, root, filepath.Join(".devtools", "vscode-runs", "new.jsonl"), strings.Join([]string{
		`{"version":1,"event":"run_started","time":"2026-07-12T10:20:00Z","run_id":"new","requested":[{"id":"keel::lane::go-log","label":"log"}]}`,
		`{"version":1,"event":"run_finished","time":"2026-07-12T10:20:09.800Z","run_id":"new","exit_code":1}`,
	}, "\n")+"\n")

	built5, buildErr5 := buildVSCodeDiscovery(root)
	if buildErr5 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr5)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built5); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	lane, ok := discoveryItemByID(doc, "keel::lane::go-log")
	if !ok {
		t.Fatalf("discovery missing go-log lane: %+v", doc.Items)
	}
	if got := strings.Join(lane.Limitations, " "); !strings.Contains(got, "last 9.8s") {
		t.Fatalf("lane limitations = %q, want newest exact single-lane duration hint", got)
	}
}

// DHF-TEST: keel/requirement-54
func TestVSCodeDiscoveryEmitsLaneCoversAndVSIXFileItems(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, dir := range []string{"log", ".vscode", filepath.Join("vsix", "src", "test", "suite")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("vsix", "src", "test", "suite", "extension.test.ts"), "suite('x', () => {});\n")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{
  "version": 1,
  "lanes": [
    {"id":"go-log","label":"log","order":"b.40","members":[{"go":"./log/..."}]},
    {"id":"ui","label":"ui","order":"b.41","members":[{"vsix":"src/test/suite/extension.test.ts"},{"vsix":"src/test/suite/missing.test.ts"}]},
    {"id":"combo","label":"combo","order":"b.42","members":[{"lane":"go-log"}]}
  ]
}`+"\n")

	built6, buildErr6 := buildVSCodeDiscovery(root)
	if buildErr6 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr6)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built6); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	if _, ok := discoveryItemByID(doc, "vsix::file::src/test/suite/extension.test.ts"); !ok {
		t.Fatalf("discovery missing static vsix file item: %+v", doc.Items)
	}
	if !discoveryHasAlias(doc, "keel::lane::go-log::covers", "go::pkg::log") ||
		!discoveryHasAlias(doc, "keel::lane::go-log::covers", "go::file::log/logging_test.go") ||
		!discoveryHasAlias(doc, "keel::lane::go-log::covers", "go::test::log::TestLog") {
		t.Fatalf("go-log covers aliases missing package/file/test descendants: %+v", doc.Items)
	}
	if !discoveryHasAlias(doc, "keel::lane::combo::covers", "keel::lane::go-log") {
		t.Fatalf("combo covers should alias referenced lane only: %+v", doc.Items)
	}
	if discoveryHasAlias(doc, "keel::lane::combo::covers", "go::pkg::log") {
		t.Fatalf("combo covers must not re-expand the referenced lane's packages (single-level alias only): %+v", doc.Items)
	}
	ui, ok := discoveryItemByID(doc, "keel::lane::ui")
	if !ok || !ui.Runnable {
		t.Fatalf("ui lane should render despite missing vsix warning: %+v ok=%v", ui, ok)
	}
	if !strings.Contains(strings.Join(ui.Limitations, " "), "V10") || !discoveryHasAlias(doc, "keel::lane::ui::covers", "vsix::file::src/test/suite/extension.test.ts") {
		t.Fatalf("ui lane limitations/covers = %+v items=%+v", ui.Limitations, doc.Items)
	}
}

// DHF-TEST: keel/requirement-54
func TestVSCodeFileLaneRunPassesVSIXFileFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, dir := range []string{".vscode", filepath.Join("vsix", "src", "test", "suite")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, filepath.Join("vsix", "src", "test", "suite", "extension.test.ts"), "suite('x', () => {});\n")
	writeFile(t, root, filepath.Join("vsix", "src", "test", "suite", "tree.test.ts"), "suite('y', () => {});\n")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[{"id":"ui","label":"ui","order":"b.40","members":[{"vsix":"src/test/suite/extension.test.ts"},{"vsix":"src/test/suite/tree.test.ts"}]}]}`+"\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "exit 0")
	stub(t, bin, callsFile, "pnpm", `
case "$*" in
  "--dir "*"/vsix run test:headless -- src/test/suite/extension.test.ts src/test/suite/tree.test.ts")
    exit 0
    ;;
  *)
    printf 'unexpected pnpm invocation: %s\n' "$*" >&2
    exit 2
    ;;
esac`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var protocol bytes.Buffer
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::ui"}); err != nil {
		t.Fatalf("ui lane run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	if got := calls(t, callsFile); !strings.Contains(got, "pnpm --dir "+filepath.Join(root, "vsix")+" run test:headless -- src/test/suite/extension.test.ts src/test/suite/tree.test.ts") {
		t.Fatalf("pnpm call missing exact vsix file filter:\n%s", got)
	}
}

func discoveryHasAlias(doc vscode.DiscoveryDocument, parentID, canonicalID string) bool {
	for _, item := range doc.Items {
		if item.ParentID == parentID && item.CanonicalID == canonicalID {
			return true
		}
	}
	return false
}

func TestVSCodeLaneEdgeCases(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":2,"lanes":[]}`+"\n")
	lanes, err := loadLanesState(root)
	if err != nil {
		t.Fatalf("load versioned lanes: %v", err)
	}
	if lanes.wholeFileErr == nil || len(lanes.discoveryItems()) != 1 {
		t.Fatalf("unsupported version state = %+v", lanes)
	}

	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[{"id":"a","label":"a","order":"b.40","members":[{"lane":"b"}]},{"id":"b","label":"b","order":"b.41","members":[{"lane":"a"}]}]}`+"\n")
	lanes, err = loadLanesState(root)
	if err != nil {
		t.Fatalf("load cyclic lanes: %v", err)
	}
	// A cycle invalidates the whole file: it sets wholeFileErr and renders as
	// exactly one file-level diagnostic (not one per lane in the cycle).
	if lanes.wholeFileErr == nil {
		t.Fatalf("cyclic lanes did not set wholeFileErr: %+v", lanes)
	}
	if items := lanes.discoveryItems(); len(items) != 1 || !discoveryItemsContain(items, "cycle") {
		t.Fatalf("cyclic lanes want exactly one cycle diagnostic, got: %+v", items)
	}
	// And detect must refuse to write into the invalid file (req-52).
	var cycleDetect bytes.Buffer
	if err := handleVSCodeLanesDetect(contextWithVSCodeTestState(root, &cycleDetect), []string{"--format", "json"}); err == nil {
		t.Fatalf("lanes detect must refuse a cyclic file, got success:\n%s", cycleDetect.String())
	}

	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[{"id":"bad","label":"bad","order":"b.40","members":[{"unknown":"x"}]}]}`+"\n")
	var protocol bytes.Buffer
	err = handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::bad"})
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid lane run err = %v\n%s", err, protocol.String())
	}

	if _, err := parseVSCodeLanesDetectArgs([]string{"--format", "yaml"}); err == nil {
		t.Fatal("non-json lanes detect format should fail")
	}
	if _, err := parseVSCodeLanesDetectArgs([]string{"--dry-run", "--bogus"}); err == nil {
		t.Fatal("unknown lanes detect flag should fail")
	}
	if hint := laneDurationHint(&laneLastRun{DurationMS: 192000}); hint != "· last 3m 12s" {
		t.Fatalf("long duration hint = %q", hint)
	}
	if hint := laneDurationHint(nil); hint != "" {
		t.Fatalf("nil duration hint = %q", hint)
	}
	if got := goPackageMatchesPattern(".", "./"); !got {
		t.Fatal("root go pattern should match root package")
	}
	if got := goPackageFamily("."); got != "." {
		t.Fatalf("root package family = %q", got)
	}
	if items, err := discoverVSIXTestItems(filepath.Join(root, "missing")); err != nil || len(items) != 0 {
		t.Fatalf("absent vsix discovery = %v, %v", items, err)
	}

	t.Setenv("PATH", t.TempDir())
	if err := runVSIXFileSelection(context.Background(), discardLogger(), root, []string{"src/test/suite/x.test.ts"}); err == nil || !strings.Contains(err.Error(), "pnpm") {
		t.Fatalf("missing pnpm err = %v", err)
	}
}

func TestVSCodeLaneAdditionalErrorBranches(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), "{")
	lanes, err := loadLanesState(root)
	if err != nil {
		t.Fatalf("load malformed lanes: %v", err)
	}
	if lanes.wholeFileErr == nil || !discoveryItemsContain(lanes.discoveryItems(), "unexpected") {
		t.Fatalf("malformed lanes state = %+v items=%+v", lanes.wholeFileErr, lanes.discoveryItems())
	}
	var protocol bytes.Buffer
	if err := writeVSCodeLanesDetect(root, false, &protocol); err == nil {
		t.Fatal("detect should fail on malformed lanes file")
	}

	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[{"id":"dup","label":"one","order":"b.40","members":[{"root":"go"}]},{"id":"dup","label":"two","order":"b.41","members":[{"root":"go"}]},{"id":"empty","label":"empty","order":"b.42","members":[]},{"id":"root-vsix","label":"vsix","order":"b.43","members":[{"root":"vsix"},{"lane":"vsix-ci"}]}]}`+"\n")
	lanes, err = loadLanesState(root)
	if err != nil {
		t.Fatalf("load duplicate lanes: %v", err)
	}
	items := lanes.discoveryItems()
	if !discoveryItemsContain(items, "duplicate lane id") || !discoveryItemsContain(items, "missing required") {
		t.Fatalf("duplicate/empty diagnostics missing: %+v", items)
	}
	if _, ok := lanes.effective["root-vsix"]; !ok {
		t.Fatalf("root-vsix lane did not expand: %+v", lanes.effective)
	}
	var list bytes.Buffer
	if err := writeVSCodeLanesList(root, &list); err != nil {
		t.Fatalf("lanes list with diagnostics: %v", err)
	}
	if !strings.Contains(list.String(), `"root":"vsix"`) || !strings.Contains(list.String(), `"lane":"vsix-ci"`) {
		t.Fatalf("list did not serialize root/lane members:\n%s", list.String())
	}

	if code, err := runVSCodeMaintenance(root, vscodeMaintenanceDetectLanes); code != 2 || err == nil {
		t.Fatalf("detect maintenance without writer = code %d err %v, want usage", code, err)
	}
	if code, err := runVSCodeMaintenance(root, "keel::maintenance::missing"); code != 2 || err == nil {
		t.Fatalf("unknown maintenance = code %d err %v, want usage", code, err)
	}
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), "{")
	var events []vscode.RunEvent
	err = runVSCodeDetectLanesMaintenance(root, func(event vscode.RunEvent) { events = append(events, event) })
	if err == nil || len(events) == 0 || events[0].Event != "output" {
		t.Fatalf("detect maintenance malformed err/events = %v %+v", err, events)
	}
}

func discoveryItemsContain(items []vscode.TestItem, text string) bool {
	for _, item := range items {
		if strings.Contains(item.Label, text) || strings.Contains(strings.Join(item.Limitations, "\n"), text) {
			return true
		}
	}
	return false
}

// DHF-TEST: keel/requirement-47
func TestVSCodeMaintenanceItemsAdvertiseCapabilitiesAndRunActions(t *testing.T) {
	root := t.TempDir()
	if code, err := runVSCodeMaintenance(root, "keel::maintenance::clear-state"); err != nil || code != 0 {
		t.Fatalf("clear-state without existing devtools = code %d, err %v; want code 0", code, err)
	}
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".devtools"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join(".vscode", "test-bridge.json"), `{"version":2,"command":"bin/keel-dev","args":["vscode","tests"],"displayName":"Keel"}`+"\n")
	writeFile(t, root, filepath.Join(".devtools", "vscode-demo-block.json"), `{"blocked_lane":"keel::lane::test-fast","updated_at":"2026-07-12T00:00:00Z"}`+"\n")
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(runDir, "run.lock")
	if err := os.WriteFile(lockPath, []byte(`{"pid":12345,"created_at":"2026-07-12T00:00:00Z","ids":["keel::lane::test-fast"],"token":"foreign"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	built7, buildErr7 := buildVSCodeDiscovery(root)
	if buildErr7 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr7)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built7); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	if got, want := doc.Capabilities.ClearResultsTestIDs, []string{"keel::maintenance::clear-results"}; !stringSlicesEqual(got, want) {
		t.Fatalf("clear_results_test_ids = %v, want %v", got, want)
	}
	if got, want := doc.Capabilities.ClearStateTestIDs, []string{"keel::maintenance::clear-state"}; !stringSlicesEqual(got, want) {
		t.Fatalf("clear_state_test_ids = %v, want %v", got, want)
	}
	for id, want := range map[string]struct {
		label string
		sort  string
	}{
		"keel::maintenance::unlock":        {label: "a.2 unlock test bridge", sort: "a.002"},
		"keel::maintenance::clear-results": {label: "a.3 clear test results", sort: "a.003"},
		"keel::maintenance::clear-state":   {label: "a.4 clear local test state", sort: "a.004"},
	} {
		item, ok := discoveryItemByID(doc, id)
		if !ok {
			t.Fatalf("discovery missing maintenance item %q", id)
		}
		if item.ParentID != "keel::maintenance" || item.Kind != "maintenance" || item.Label != want.label || item.SortText != want.sort || !item.Runnable {
			t.Fatalf("maintenance item %q = %+v, want parent maintenance label=%q sort=%q runnable", id, item, want.label, want.sort)
		}
	}

	var protocol bytes.Buffer
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::maintenance::unlock"}); err != nil {
		t.Fatalf("unlock maintenance run: %v\nprotocol:\n%s", err, protocol.String())
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unlock should remove stranded run.lock, stat err=%v", err)
	}
	if events := decodeRunEvents(t, protocol.String()); !runEventsContain(events, "passed", "keel::maintenance::unlock") || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode != 0 {
		t.Fatalf("unlock events = %+v, want passed and run_finished exit 0", events)
	}

	protocol.Reset()
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::maintenance::clear-state"}); err != nil {
		t.Fatalf("clear-state maintenance run: %v\nprotocol:\n%s", err, protocol.String())
	}
	clearStateEvents := decodeRunEvents(t, protocol.String())
	clearStateRunID := clearStateEvents[0].RunID
	streamPath := filepath.Join(root, ".devtools", "vscode-runs", clearStateRunID+".jsonl")
	stream, err := os.ReadFile(streamPath)
	if err != nil {
		t.Fatalf("clear-state should preserve active run stream at %s: %v", streamPath, err)
	}
	streamEvents := decodeRunEvents(t, string(stream))
	if streamEvents[len(streamEvents)-1].Event != "run_finished" || streamEvents[len(streamEvents)-1].ExitCode == nil || *streamEvents[len(streamEvents)-1].ExitCode != 0 {
		t.Fatalf("clear-state stream terminal event = %+v, want run_finished exit 0", streamEvents[len(streamEvents)-1])
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clear-state should release its active run lock, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".devtools", "vscode-demo-block.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clear-state should remove devtool state, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".vscode", "test-bridge.json")); err != nil {
		t.Fatalf("clear-state must not remove bridge config: %v", err)
	}
}

// DHF-TEST: keel/requirement-48
func TestVSCodeSystemGateLanesDiscoverPrepareAndRun(t *testing.T) {
	originalPath := os.Getenv("PATH")
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	built8, buildErr8 := buildVSCodeDiscovery(root)
	if buildErr8 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr8)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built8); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	for id, want := range map[string]struct {
		label string
		sort  string
	}{
		"keel::lane::vsix-ci": {label: "b.10 vsix ci", sort: "b.010"},
		"keel::lane::ci":      {label: "b.30 ci", sort: "b.030"},
	} {
		item, ok := discoveryItemByID(doc, id)
		if !ok {
			t.Fatalf("discovery missing system gate lane %q", id)
		}
		if item.ParentID != "keel::lanes" || item.Kind != "lane" || item.Label != want.label || item.SortText != want.sort || !stringSlicesEqual(item.Profiles, []string{"run"}) {
			t.Fatalf("system lane %q = %+v, want parent lanes label=%q sort=%q profiles=[run]", id, item, want.label, want.sort)
		}
	}

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "exit 0")
	t.Setenv("PATH", bin)
	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::vsix-ci"})
	if err == nil {
		t.Fatal("vsix-ci without pnpm returned nil error; want structured blocked result")
	}
	blocked := decodeRunEvents(t, protocol.String())
	if !runEventsContain(blocked, "failed", "keel::lane::vsix-ci") || !strings.Contains(protocol.String(), "pnpm") {
		t.Fatalf("vsix-ci blocked events = %+v, want failed event naming pnpm", blocked)
	}
	if strings.Contains(calls(t, callsFile), "pnpm ") {
		t.Fatalf("vsix-ci should not start gate work when pnpm is absent; calls:\n%s", calls(t, callsFile))
	}

	callsFile = stubTools(t, false, false)
	goodRoot := moduleFixture(t)
	protocol.Reset()
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(goodRoot, &protocol), []string{"--id", "keel::lane::ci"}); err != nil {
		t.Fatalf("ci lane run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	if events := decodeRunEvents(t, protocol.String()); !runEventsContain(events, "passed", "keel::lane::ci") || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode != 0 {
		t.Fatalf("ci lane events = %+v, want passed and exit 0", events)
	}
	if strings.Contains(calls(t, callsFile), "pnpm ") {
		t.Fatalf("ci lane must not spawn node/pnpm; calls:\n%s", calls(t, callsFile))
	}

	t.Setenv("PATH", originalPath)
	badRoot := t.TempDir()
	writeFile(t, badRoot, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, badRoot, "bad.go", "package p\n\nvar    Y = 2\n")
	protocol.Reset()
	err = handleVSCodeTestsRun(contextWithVSCodeTestState(badRoot, &protocol), []string{"--id", "keel::lane::ci"})
	if err == nil {
		t.Fatal("failing ci lane returned nil error; want non-zero")
	}
	failed := decodeRunEvents(t, protocol.String())
	if !runEventsContain(failed, "errored", "") || failed[len(failed)-1].ExitCode == nil || *failed[len(failed)-1].ExitCode == 0 || !strings.Contains(protocol.String(), "gofmt") {
		t.Fatalf("failing ci lane events = %+v, want errored detail and non-zero run_finished", failed)
	}
}

// DHF-TEST: keel/requirement-43, keel/requirement-49
func TestVSCodeDiscoveryEmitsGoTestTreeFromParser(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\nfunc TestMetrics(t *testing.T) {}\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "printf 'discovery must not invoke go subprocesses\\n' >&2\nexit 99")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	built9, buildErr9 := buildVSCodeDiscovery(root)
	if buildErr9 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v\ncalls:\n%s", buildErr9, calls(t, callsFile))
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built9); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}

	want := map[string]struct {
		parent   string
		label    string
		kind     string
		runnable bool
	}{
		"go::root":                      {parent: "keel::frameworks", label: "d.1 Go", kind: "root", runnable: true},
		"go::pkg::log":                  {parent: "go::root", label: "log", kind: "package", runnable: true},
		"go::file::log/logging_test.go": {parent: "go::pkg::log", label: "logging_test.go", kind: "file", runnable: true},
		"go::test::log::TestLog":        {parent: "go::file::log/logging_test.go", label: "TestLog", kind: "test", runnable: true},
		"go::test::log::TestMetrics":    {parent: "go::file::log/logging_test.go", label: "TestMetrics", kind: "test", runnable: true},
	}
	for id, wantItem := range want {
		item, ok := discoveryItemByID(doc, id)
		if !ok {
			t.Fatalf("discovery missing %s in %+v\ncalls:\n%s", id, doc.Items, calls(t, callsFile))
		}
		if item.ParentID != wantItem.parent || item.Label != wantItem.label || item.Kind != wantItem.kind || item.Runnable != wantItem.runnable {
			t.Fatalf("item %s = %+v, want parent=%q label=%q kind=%q runnable=%v", id, item, wantItem.parent, wantItem.label, wantItem.kind, wantItem.runnable)
		}
	}
	if got := strings.TrimSpace(calls(t, callsFile)); got != "" {
		t.Fatalf("discovery spawned go subprocesses:\n%s", got)
	}
}

// DHF-TEST: keel/requirement-49
func TestVSCodeDiscoveryParsesGoTestsWithoutGoSubprocesses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), `package log

import "testing"

func TestLog(t *testing.T) {}

func helper() {}

func TestMetrics(t *testing.T) {}
`)

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "printf 'discovery must not invoke go subprocesses\\n' >&2\nexit 99")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	reBuilt0, reBuiltErr0 := buildVSCodeDiscovery(root)
	if reBuiltErr0 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v\ncalls:\n%s", reBuiltErr0, calls(t, callsFile))
	}
	if got := strings.TrimSpace(calls(t, callsFile)); got != "" {
		t.Fatalf("discovery spawned go subprocesses:\n%s", got)
	}

	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, reBuilt0); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	pkg, ok := discoveryItemByID(doc, "go::pkg::log")
	if !ok {
		t.Fatalf("discovery missing package item: %+v", doc.Items)
	}
	if pkg.ParentID != "go::root" || pkg.Label != "log" || pkg.Kind != "package" || !pkg.Runnable {
		t.Fatalf("package item = %+v, want runnable package under go root", pkg)
	}

	file, ok := discoveryItemByID(doc, "go::file::log/logging_test.go")
	if !ok {
		t.Fatalf("discovery missing file item: %+v", doc.Items)
	}
	if file.ParentID != "go::pkg::log" || file.Label != "logging_test.go" || file.Kind != "file" || file.URI != "log/logging_test.go" || !file.Runnable {
		t.Fatalf("file item = %+v, want runnable file with module-relative uri", file)
	}

	for _, want := range []string{"TestLog", "TestMetrics"} {
		id := "go::test::log::" + want
		item, ok := discoveryItemByID(doc, id)
		if !ok {
			t.Fatalf("discovery missing test item %q: %+v", id, doc.Items)
		}
		if item.ParentID != "go::file::log/logging_test.go" || item.Label != want || item.Kind != "test" || item.URI != "log/logging_test.go" || item.Range == nil || !item.Runnable {
			t.Fatalf("test item %q = %+v, want runnable test under file with uri and range", id, item)
		}
		if item.Range.StartLine < 4 || item.Range.StartColumn != 0 || item.Range.EndLine < item.Range.StartLine {
			t.Fatalf("test item %q range = %+v, want parser-derived function position", id, item.Range)
		}
	}
}

// DHF-TEST: keel/requirement-49, keel/requirement-50
func TestVSCodeDiscoveryMatchesGoTestFunctionSemantics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "semantics_test.go"), `package log

import testingAlias "testing"

type T = testingAlias.T

type fakeT struct{}

func Test(t *T) {}
func TestAlias(t *testingAlias.T) {}
func TestUpper(t *testingAlias.T) {}
func Test_underscore(t *testingAlias.T) {}
func Test123(t *testingAlias.T) {}
func Testcase(t *testingAlias.T) {}
func TesticularCancer(t *testingAlias.T) {}
func TestWrongSignature(t string) {}
func TestLocalStructReceiver(t *fakeT) {}
func TestForeignSelector(t *strings.T) {}
`)

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./log")
    found_run=
    for arg in "$@"; do
      case "$arg" in
        -run=^\(Test\|TestAlias\|TestUpper\|Test_underscore\|Test123\)$) found_run=1 ;;
      esac
    done
    if [ "$found_run" != 1 ]; then
      printf 'missing Go-compatible selected file -run filter\n' >&2
      exit 2
    fi
    printf '{"Action":"run","Package":"github.com/david-aggeler/keel/log","Test":"TestAlias"}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Test":"TestAlias","Elapsed":0.01}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Elapsed":0.01}\n'
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	built10, buildErr10 := buildVSCodeDiscovery(root)
	if buildErr10 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v\ncalls:\n%s", buildErr10, calls(t, callsFile))
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built10); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	for _, want := range []string{"Test", "TestAlias", "TestUpper", "Test_underscore", "Test123"} {
		if _, ok := discoveryItemByID(doc, "go::test::log::"+want); !ok {
			t.Fatalf("discovery missing Go-compatible test %s: %+v", want, doc.Items)
		}
	}
	for _, blocked := range []string{"Testcase", "TesticularCancer", "TestWrongSignature", "TestLocalStructReceiver", "TestForeignSelector"} {
		if _, ok := discoveryItemByID(doc, "go::test::log::"+blocked); ok {
			t.Fatalf("discovery included non-Go-test function %s: %+v", blocked, doc.Items)
		}
	}

	var protocol bytes.Buffer
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "go::file::log/semantics_test.go"}); err != nil {
		t.Fatalf("go file selection run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	if !strings.Contains(calls(t, callsFile), "go test ./log -json -run=^(Test|TestAlias|TestUpper|Test_underscore|Test123)$") {
		t.Fatalf("go file selection did not use Go-compatible file test names:\n%s", calls(t, callsFile))
	}
}

// DHF-TEST: keel/requirement-49
func TestVSCodeDiscoverySkipsInactiveGoFilesAndIgnoredDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, dir := range []string{"log", "_scratch", ".hidden", "testdata", "vendor/acme", "nested"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, filepath.Join("log", "active_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestActive(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("log", "tagged_test.go"), "//go:build impossible_tag\n\npackage log\n\nimport \"testing\"\n\nfunc TestTaggedOut(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("log", "feature_windows_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestWindowsOnly(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("_scratch", "hidden_test.go"), "package scratch\n\nimport \"testing\"\n\nfunc TestScratch(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join(".hidden", "hidden_test.go"), "package hidden\n\nimport \"testing\"\n\nfunc TestHidden(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("testdata", "data_test.go"), "package testdata\n\nimport \"testing\"\n\nfunc TestData(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("vendor/acme", "vendor_test.go"), "package acme\n\nimport \"testing\"\n\nfunc TestVendor(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("nested", "go.mod"), "module nested.example\n\ngo 1.25\n")
	writeFile(t, root, filepath.Join("nested", "nested_test.go"), "package nested\n\nimport \"testing\"\n\nfunc TestNested(t *testing.T) {}\n")

	built11, buildErr11 := buildVSCodeDiscovery(root)
	if buildErr11 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr11)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built11); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	if _, ok := discoveryItemByID(doc, "go::test::log::TestActive"); !ok {
		t.Fatalf("discovery missing active test: %+v", doc.Items)
	}
	for _, absent := range []string{
		"go::test::log::TestTaggedOut",
		"go::test::log::TestWindowsOnly",
		"go::test::_scratch::TestScratch",
		"go::test::.hidden::TestHidden",
		"go::test::testdata::TestData",
		"go::test::vendor/acme::TestVendor",
		"go::test::nested::TestNested",
	} {
		if _, ok := discoveryItemByID(doc, absent); ok {
			t.Fatalf("discovery included inactive test %s: %+v", absent, doc.Items)
		}
	}
}

// DHF-TEST: keel/requirement-49
func TestVSCodeDiscoveryReportsGoParseErrorsAsDiagnosticFileItems(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("broken", "broken_test.go"), "package broken\n\nfunc TestBroken(\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "printf 'discovery must not invoke go subprocesses\\n' >&2\nexit 99")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	reBuilt1, reBuiltErr1 := buildVSCodeDiscovery(root)
	if reBuiltErr1 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v\ncalls:\n%s", reBuiltErr1, calls(t, callsFile))
	}
	if got := strings.TrimSpace(calls(t, callsFile)); got != "" {
		t.Fatalf("discovery spawned go subprocesses:\n%s", got)
	}

	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, reBuilt1); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	item, ok := discoveryItemByID(doc, "go::file::broken/broken_test.go")
	if !ok {
		t.Fatalf("discovery missing parse diagnostic file item: %+v", doc.Items)
	}
	if item.ParentID != "go::pkg::broken" || item.Kind != "file" || item.Runnable || item.URI != "broken/broken_test.go" {
		t.Fatalf("parse diagnostic item = %+v, want non-runnable file item under package", item)
	}
	if len(item.Limitations) == 0 || !strings.Contains(strings.Join(item.Limitations, "\n"), "expected") {
		t.Fatalf("parse diagnostic limitations = %v, want parse error text", item.Limitations)
	}
}

// DHF-TEST: keel/requirement-49
func TestVSCodeDiscoveryReportsPackageParseErrorsAsDiagnosticFileItems(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("broken", "broken.go"), "package broken\n\nfunc Broken(\n")
	writeFile(t, root, filepath.Join("broken", "ok_test.go"), "package broken\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n")

	built12, buildErr12 := buildVSCodeDiscovery(root)
	if buildErr12 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr12)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built12); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	item, ok := discoveryItemByID(doc, "go::file::broken/ok_test.go")
	if !ok {
		t.Fatalf("discovery missing package diagnostic file item: %+v", doc.Items)
	}
	if item.Kind != "file" || item.Runnable || item.URI != "broken/ok_test.go" {
		t.Fatalf("package diagnostic item = %+v, want non-runnable file item", item)
	}
	if len(item.Limitations) == 0 || !strings.Contains(strings.Join(item.Limitations, "\n"), "broken.go") {
		t.Fatalf("package diagnostic limitations = %v, want package parse error text", item.Limitations)
	}
	if _, ok := discoveryItemByID(doc, "go::test::broken::TestOK"); ok {
		t.Fatalf("discovery included runnable test from invalid package: %+v", doc.Items)
	}
}

// DHF-TEST: keel/requirement-43
func TestVSCodeRunGoTestSelectionUsesRunFilterAndSelectedID(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./log")
    found_run=
    for arg in "$@"; do
      case "$arg" in
        -run=^\(TestLog\)$) found_run=1 ;;
      esac
    done
    if [ "$found_run" != 1 ]; then
      printf 'missing selected -run filter\n' >&2
      exit 2
    fi
    printf '{"Action":"run","Package":"github.com/david-aggeler/keel/log","Test":"TestLog"}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Test":"TestLog","Elapsed":0.01}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Elapsed":0.01}\n'
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var protocol bytes.Buffer
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "go::test::log::TestLog"}); err != nil {
		t.Fatalf("go test selection run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	if !strings.Contains(calls(t, callsFile), "go test ./log -json -run=^(TestLog)$") {
		t.Fatalf("go test selection did not use package + exact -run filter:\n%s", calls(t, callsFile))
	}
	events := decodeRunEvents(t, protocol.String())
	if !runEventsContain(events, "passed", "go::test::log::TestLog") {
		t.Fatalf("run events missing selected test pass: %+v", events)
	}
	if events[len(events)-1].Event != "run_finished" || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode != 0 {
		t.Fatalf("terminal event = %+v, want run_finished exit 0", events[len(events)-1])
	}

	built13, buildErr13 := buildVSCodePlan(root, []string{"go::test::log::TestLog"})
	if buildErr13 != nil {
		t.Fatalf("buildVSCodePlan for go test: %v", buildErr13)
	}
	var plan bytes.Buffer
	if err := testbridge.EncodeDocument(&plan, built13); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var setup vscode.SetupPlan
	if err := json.Unmarshal(plan.Bytes(), &setup); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, plan.String())
	}
	if len(setup.Items) != 1 || setup.Items[0].ID != "go::test::log::TestLog" || setup.Items[0].Kind != "test" || setup.Items[0].Framework != "go" {
		t.Fatalf("go selection plan items = %+v, want one Go test item", setup.Items)
	}
}

// DHF-TEST: keel/requirement-50
func TestVSCodeRunGoFileSelectionRunsOnlyTestsInThatFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "selected_test.go"), `package log

import "testing"

func TestOne(t *testing.T) {}
func TestTwo(t *testing.T) {}
`)
	writeFile(t, root, filepath.Join("log", "other_test.go"), `package log

import "testing"

func TestOther(t *testing.T) {}
`)

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./log")
    found_run=
    for arg in "$@"; do
      case "$arg" in
        -run=^\(TestOne\|TestTwo\)$) found_run=1 ;;
      esac
    done
    if [ "$found_run" != 1 ]; then
      printf 'missing selected file -run filter\n' >&2
      exit 2
    fi
    printf '{"Action":"run","Package":"github.com/david-aggeler/keel/log","Test":"TestOne"}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Test":"TestOne","Elapsed":0.01}\n'
    printf '{"Action":"run","Package":"github.com/david-aggeler/keel/log","Test":"TestTwo"}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Test":"TestTwo","Elapsed":0.02}\n'
    printf '{"Action":"run","Package":"github.com/david-aggeler/keel/log","Test":"TestOther"}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Test":"TestOther","Elapsed":0.03}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Elapsed":0.04}\n'
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "go::file::log/selected_test.go"})
	if err != nil {
		t.Fatalf("go file selection run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	if !strings.Contains(calls(t, callsFile), "go test ./log -json -run=^(TestOne|TestTwo)$") {
		t.Fatalf("go file selection did not use file test names in -run filter:\n%s", calls(t, callsFile))
	}
	events := decodeRunEvents(t, protocol.String())
	for _, want := range []string{"TestOne", "TestTwo"} {
		id := "go::test::log::" + want
		if !runEventsContain(events, "test_started", id) || !runEventsContain(events, "passed", id) {
			t.Fatalf("run events missing started/pass for %s: %+v", id, events)
		}
	}
	if runEventsContain(events, "test_started", "go::test::log::TestOther") || runEventsContain(events, "passed", "go::test::log::TestOther") {
		t.Fatalf("run events included test outside selected file: %+v", events)
	}
	if events[len(events)-1].Event != "run_finished" || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode != 0 {
		t.Fatalf("terminal event = %+v, want run_finished exit 0", events[len(events)-1])
	}
}

// DHF-TEST: keel/requirement-50
func TestVSCodeRunGoFileSelectionReportsParseFailure(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "broken_test.go"), "package log\n\nfunc TestBroken(\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "printf 'go test must not run after a file parse failure\\n' >&2\nexit 2")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "go::file::log/broken_test.go"})
	if err == nil {
		t.Fatalf("go file selection parse failure returned nil\nprotocol:\n%s\ncalls:\n%s", protocol.String(), calls(t, callsFile))
	}
	if got := strings.TrimSpace(calls(t, callsFile)); got != "" {
		t.Fatalf("go test ran despite parse failure:\n%s", got)
	}
	events := decodeRunEvents(t, protocol.String())
	if !runEventsContain(events, "errored", "") || !strings.Contains(protocol.String(), "broken_test.go") || !strings.Contains(protocol.String(), "expected") {
		t.Fatalf("parse failure events = %+v, want errored event naming parse error", events)
	}
	if events[len(events)-1].Event != "run_finished" || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode == 0 {
		t.Fatalf("terminal event = %+v, want non-zero run_finished", events[len(events)-1])
	}
}

// DHF-TEST: keel/requirement-50
func TestVSCodeRunGoFileSelectionRejectsInactiveFile(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		want  string
		setup func(t *testing.T, root string)
	}{
		{
			name: "build constraints",
			id:   "go::file::log/tagged_test.go",
			want: "excluded by build constraints",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
					t.Fatal(err)
				}
				writeFile(t, root, filepath.Join("log", "tagged_test.go"), "//go:build impossible_tag\n\npackage log\n\nimport \"testing\"\n\nfunc TestTaggedOut(t *testing.T) {}\n")
			},
		},
		{
			name: "ignored directory",
			id:   "go::file::_scratch/hidden_test.go",
			want: "outside the active Go package set",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "_scratch"), 0o755); err != nil {
					t.Fatal(err)
				}
				writeFile(t, root, filepath.Join("_scratch", "hidden_test.go"), "package scratch\n\nimport \"testing\"\n\nfunc TestHidden(t *testing.T) {}\n")
			},
		},
		{
			name: "nested module",
			id:   "go::file::nested/nested_test.go",
			want: "nested Go module",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
					t.Fatal(err)
				}
				writeFile(t, root, filepath.Join("nested", "go.mod"), "module nested.example\n\ngo 1.25\n")
				writeFile(t, root, filepath.Join("nested", "nested_test.go"), "package nested\n\nimport \"testing\"\n\nfunc TestNested(t *testing.T) {}\n")
			},
		},
		{
			name: "not test file",
			id:   "go::file::log/helper.go",
			want: "not a *_test.go file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
					t.Fatal(err)
				}
				writeFile(t, root, filepath.Join("log", "helper.go"), "package log\n\nfunc Helper() {}\n")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
			writeFile(t, root, "go.sum", "")
			tt.setup(t, root)

			bin := t.TempDir()
			callsFile := filepath.Join(bin, "calls.log")
			stub(t, bin, callsFile, "go", "printf 'go test must not run for inactive file selections\\n' >&2\nexit 2")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			var protocol bytes.Buffer
			err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", tt.id})
			if err == nil {
				t.Fatalf("inactive go file selection returned nil\nprotocol:\n%s\ncalls:\n%s", protocol.String(), calls(t, callsFile))
			}
			if got := strings.TrimSpace(calls(t, callsFile)); got != "" {
				t.Fatalf("go test ran despite inactive file selection:\n%s", got)
			}
			events := decodeRunEvents(t, protocol.String())
			if !runEventsContain(events, "errored", "") || !strings.Contains(protocol.String(), tt.want) {
				t.Fatalf("inactive file events = %+v, want errored event containing %q", events, tt.want)
			}
			if events[len(events)-1].Event != "run_finished" || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode == 0 {
				t.Fatalf("terminal event = %+v, want non-zero run_finished", events[len(events)-1])
			}
		})
	}
}

// DHF-TEST: keel/requirement-43
func TestVSCodeRunGoPackageSelectionRunsPackageWithoutRunFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./log")
    for arg in "$@"; do
      case "$arg" in
        -run=*)
          printf 'unexpected -run for package selection\n' >&2
          exit 2
          ;;
      esac
    done
    printf '{"Action":"run","Package":"github.com/david-aggeler/keel/log"}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Elapsed":0.02}\n'
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var protocol bytes.Buffer
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "go::pkg::log"}); err != nil {
		t.Fatalf("go package run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	if !strings.Contains(calls(t, callsFile), "go test ./log -json") || strings.Contains(calls(t, callsFile), "-run=") {
		t.Fatalf("go package selection used wrong command:\n%s", calls(t, callsFile))
	}
	events := decodeRunEvents(t, protocol.String())
	if !runEventsContain(events, "passed", "go::pkg::log") {
		t.Fatalf("run events missing package pass: %+v", events)
	}

	built14, buildErr14 := buildVSCodePlan(root, []string{"go::pkg::log", "go::root"})
	if buildErr14 != nil {
		t.Fatalf("buildVSCodePlan for go package/root: %v", buildErr14)
	}
	var plan bytes.Buffer
	if err := testbridge.EncodeDocument(&plan, built14); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var setup vscode.SetupPlan
	if err := json.Unmarshal(plan.Bytes(), &setup); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, plan.String())
	}
	if len(setup.Items) != 2 || setup.Items[0].Kind != "package" || setup.Items[0].Label != "log" || setup.Items[1].Kind != "root" {
		t.Fatalf("go package/root plan items = %+v", setup.Items)
	}
}

// DHF-TEST: keel/requirement-49
func TestVSCodeDiscoveryDoesNotRequireGoToolchain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "printf 'go subprocess should not run during discovery\\n' >&2\nexit 7")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := buildVSCodeDiscovery(root); err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v\ncalls:\n%s", err, calls(t, callsFile))
	}
	if got := strings.TrimSpace(calls(t, callsFile)); got != "" {
		t.Fatalf("discovery spawned go subprocesses:\n%s", got)
	}
}

// DHF-TEST: keel/requirement-39
func TestVSCodeCoverageLaneEmitsPersistedCoverageArtifact(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	writeFile(t, root, "main_test.go", "package p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n")

	built15, buildErr15 := buildVSCodeDiscovery(root)
	if buildErr15 != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", buildErr15)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built15); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	if got := doc["module_path"]; got != modulePath {
		t.Fatalf("discovery module_path = %v, want %q", got, modulePath)
	}
	items, ok := doc["items"].([]any)
	if !ok {
		t.Fatalf("discovery items missing: %+v", doc)
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || item["id"] != vscodeLaneTestCoverage {
			continue
		}
		profiles, ok := item["profiles"].([]any)
		if !ok || len(profiles) != 1 || profiles[0] != "coverage" {
			t.Fatalf("coverage lane profiles = %v, want [coverage]", item["profiles"])
		}
		goto foundCoverageLane
	}
	t.Fatalf("discovery missing coverage lane: %+v", items)

foundCoverageLane:
	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./...")
    for arg in "$@"; do
      case "$arg" in
        -coverprofile=*)
          profile=${arg#-coverprofile=}
          mkdir -p "$(dirname "$profile")"
          printf 'mode: atomic\npkg/file.go:1.1,1.10 1 1\n' > "$profile"
          ;;
      esac
    done
    printf '?   \tgithub.com/david-aggeler/keel/log\t[no test files]\n'
    printf 'ok  \tgithub.com/david-aggeler/keel/vscode\t0.012s\tcoverage: 91.2%% of statements\n'
    ;;
  "tool cover")
    printf 'github.com/david-aggeler/keel/vscode/file.go:1:\tFunc\t91.2%%\n'
    printf 'total:\t(statements)\t91.2%%\n'
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldCoverageDir := filepath.Join(root, ".logs", "vscode-cover", "old-run")
	if err := os.MkdirAll(oldCoverageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(oldCoverageDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	var protocol bytes.Buffer
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", vscodeLaneTestCoverage}); err != nil {
		t.Fatalf("coverage run handler: %v\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	events := decodeRunEvents(t, protocol.String())
	finishedIndex := eventIndex(events, "run_finished")
	if finishedIndex < 0 {
		t.Fatalf("coverage run missing run_finished: %+v", events)
	}
	var artifact *vscode.RunArtifact
	var packagePasses int
	var summaryFound bool
	for i, event := range events {
		if event.Event == "artifact" && event.Artifact != nil && event.Artifact.Kind == "coverage" {
			if i > finishedIndex {
				t.Fatalf("coverage artifact emitted after run_finished: %+v", events)
			}
			if artifact != nil {
				t.Fatalf("multiple coverage artifacts: %+v", events)
			}
			artifact = event.Artifact
		}
		if event.Event == "passed" && strings.HasPrefix(event.TestID, "go::package::") && event.DurationMS > 0 {
			packagePasses++
		}
		if event.Event == "output" && strings.Contains(event.Message, "total statement coverage 91.2%") {
			summaryFound = true
		}
	}
	if artifact == nil {
		t.Fatalf("coverage run emitted no artifact{kind:coverage}: %+v", events)
	}
	if packagePasses == 0 {
		t.Fatalf("coverage run emitted no per-package passed events: %+v", events)
	}
	if !summaryFound {
		t.Fatalf("coverage run emitted no total percentage output line: %+v", events)
	}
	artifactPath := strings.TrimPrefix(artifact.URI, "file://")
	if !strings.Contains(artifactPath, filepath.Join(".logs", "vscode-cover", events[0].RunID, "cover.out")) {
		t.Fatalf("coverage artifact path = %q, want .logs/vscode-cover/<run-id>/cover.out", artifactPath)
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatalf("coverage profile not persisted at %s: %v", artifactPath, err)
	}
	if info.Size() == 0 {
		t.Fatalf("coverage profile at %s is empty", artifactPath)
	}
	if _, err := os.Stat(oldCoverageDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old coverage run dir should be swept, stat err=%v", err)
	}

	protocol.Reset()
	if err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", vscodeLaneTestFast}); err != nil {
		t.Fatalf("test-fast run handler: %v\n%s", err, protocol.String())
	}
	for _, event := range decodeRunEvents(t, protocol.String()) {
		if event.Event == "artifact" {
			t.Fatalf("non-coverage lane emitted artifact: %+v", event)
		}
	}
}

// DHF-TEST: keel/requirement-36
func TestVSCodeRunWritesStampedExternalRunStream(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	t.Setenv("PATH", t.TempDir())

	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::test-fast"})
	if err == nil {
		t.Fatal("blocked lane returned nil error; want non-zero")
	}
	protocolEvents := decodeRunEvents(t, protocol.String())
	runID := protocolEvents[0].RunID
	if runID == "" {
		t.Fatalf("run_started missing run id: %+v", protocolEvents[0])
	}

	streamPath := filepath.Join(root, ".devtools", "vscode-runs", runID+".jsonl")
	stream, readErr := os.ReadFile(streamPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	externalEvents := decodeRunEvents(t, string(stream))
	if len(externalEvents) != len(protocolEvents) {
		t.Fatalf("external stream events = %d, protocol events = %d\n%s", len(externalEvents), len(protocolEvents), stream)
	}
	for i, event := range externalEvents {
		if event.Version != 1 || event.Time.IsZero() || event.RunID != runID || event.Workspace == "" {
			t.Fatalf("external event %d missing stamp: %+v", i, event)
		}
	}
}

func discoveryHasLane(doc vscode.DiscoveryDocument, id string) bool {
	for _, item := range doc.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func discoveryItemByID(doc vscode.DiscoveryDocument, id string) (vscode.TestItem, bool) {
	for _, item := range doc.Items {
		if item.ID == id {
			return item, true
		}
	}
	return vscode.TestItem{}, false
}

func discoveryHasDiagnosticContaining(doc vscode.DiscoveryDocument, text string) bool {
	for _, item := range doc.Items {
		if item.Kind != "group" || item.Runnable {
			continue
		}
		if strings.Contains(item.Label, text) || strings.Contains(strings.Join(item.Limitations, "\n"), text) {
			return true
		}
	}
	return false
}

func assertDiscoveryKindAllowedBySchema(t *testing.T, kind string) {
	t.Helper()
	body, err := vscode.SchemaBytes(vscode.SchemaDiscovery)
	if err != nil {
		t.Fatalf("read discovery schema: %v", err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(body, &schema); err != nil {
		t.Fatalf("decode discovery schema: %v", err)
	}
	for _, allowed := range schema.Defs["test_item"].Properties["kind"].Enum {
		if kind == allowed {
			return
		}
	}
	t.Fatalf("discovery item kind %q is not allowed by embedded discovery schema enum %v", kind, schema.Defs["test_item"].Properties["kind"].Enum)
}

func runEventsContain(events []vscode.RunEvent, event, id string) bool {
	for _, got := range events {
		if got.Event == event && got.TestID == id {
			return true
		}
	}
	return false
}

func stringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func eventIndex(events []vscode.RunEvent, eventName string) int {
	for i, event := range events {
		if event.Event == eventName {
			return i
		}
	}
	return -1
}

func TestVSCodeProtocolWriterIsOnlyStdoutAllowlistGrowth(t *testing.T) {
	dir := t.TempDir()
	keeldev := filepath.Join(dir, "cmd", "keel-dev")
	if err := os.MkdirAll(keeldev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, keeldev, "main.go",
		"package main\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\nfunc newLogger() io.Writer { return os.Stdout }\nfunc newProtocolStream() io.Writer { return os.Stdout }\n")
	if err := runLint(dir); err != nil {
		t.Fatalf("newProtocolStream should be the one protocol stdout allowlist entry: %v", err)
	}

	writeFile(t, keeldev, "main.go",
		"package main\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\nfunc newLogger() io.Writer { return os.Stdout }\nfunc newProtocolStream() io.Writer { return os.Stdout }\nfunc extraProtocolStream() io.Writer { return os.Stdout }\n")
	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "extraProtocolStream") {
		t.Fatalf("unexpected stdout allowlist growth should fail, got %v", err)
	}

	writeFile(t, keeldev, "main.go",
		"package main\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\nfunc newLogger() io.Writer { return os.Stdout }\n")
	writeFile(t, keeldev, "stream.go",
		"package main\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\nfunc newProtocolStream() io.Writer { return os.Stdout }\n")
	err = runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "newProtocolStream") || !strings.Contains(err.Error(), "stream.go") {
		t.Fatalf("stdout allowlist must include file and function, got %v", err)
	}
}

func TestVSCodeArgumentAndProfileEdges(t *testing.T) {
	if _, err := parseVSCodeIDs([]string{"--format", "yaml"}, true); err == nil {
		t.Fatal("non-json format should fail")
	}
	if _, err := parseVSCodeIDs([]string{"--id"}, false); err == nil {
		t.Fatal("missing --id value should fail")
	}
	if _, err := parseVSCodeIDs([]string{"--unknown"}, true); err == nil {
		t.Fatal("unknown vscode argument should fail")
	}
	if err := rejectUnsupportedFormat([]string{"--format", "xml"}); err == nil {
		t.Fatal("rejectUnsupportedFormat should reject xml")
	}
	if got := laneForIDs([]string{"go::root", vscodeLaneTestFast}); got != vscodeLaneTestFast {
		t.Fatalf("laneForIDs = %q, want %q", got, vscodeLaneTestFast)
	}
	if got := laneForIDs([]string{"go::root"}); got != "go::root" {
		t.Fatalf("laneForIDs fallback = %q", got)
	}

	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	profile := newKeelWorkspaceProfile(root)
	if profile.Repo() == "" || profile.ModulePath() != modulePath || profile.LogDir() == "" || profile.MaxOutputBytes() == 0 {
		t.Fatalf("profile scalar methods returned empty values: %+v", profile)
	}
	if readiness := profile.PrepareLane(context.Background(), vscodeLaneLint); !readiness.Ready() {
		t.Fatalf("profile should be ready with go and module root: %+v", readiness)
	}
	if statusWord(false) != "blocked" || workspaceNode("") != "unknown" {
		t.Fatal("status/workspace fallback helpers returned unexpected values")
	}
}

// DHF-TEST: keel/requirement-38
func TestVSCodeRunKeepsStdoutProtocolAndConsoleOnStderr(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	t.Chdir(root)
	t.Setenv("PATH", t.TempDir())

	stdout, stderr := captureProcessStreams(t, func() {
		if code := run([]string{"--no-header", "-v", "vscode", "tests", "run", "--id", "keel::lane::test-fast"}); code == 0 {
			t.Fatal("blocked vscode run exit = 0, want non-zero")
		}
	})

	events := decodeRunEvents(t, stdout)
	if len(events) == 0 {
		t.Fatalf("stdout had no protocol events: %q", stdout)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var event vscode.RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("stdout contains non-protocol residue %q: %v\nstderr:\n%s", line, err, stderr)
		}
	}
	if strings.Contains(stdout, "keel-dev failed") || strings.Contains(stdout, "level=") {
		t.Fatalf("stdout contains console log residue:\n%s", stdout)
	}
	if !strings.Contains(stderr, "keel-dev failed") {
		t.Fatalf("stderr missing console failure log:\n%s", stderr)
	}
}

// DHF-TEST: keel/requirement-35
func TestVSCodeRunRefusesExistingLockAndReleasesOwnLock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(runDir, "run.lock")
	existing := `{"pid":12345,"created_at":"2026-07-11T00:00:00Z","ids":["keel::lane::test-fast"],"token":"foreign"}`
	if err := os.WriteFile(lockPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::test-fast"})
	if err == nil {
		t.Fatal("run with existing lock returned nil error; want refusal")
	}
	got, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != existing {
		t.Fatalf("existing lock was modified:\n%s", got)
	}
	events := decodeRunEvents(t, protocol.String())
	if events[len(events)-1].Event != "run_finished" || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode == 0 {
		t.Fatalf("lock refusal did not emit non-zero terminal event: %+v", events)
	}
	if !strings.Contains(protocol.String(), "run lock") {
		t.Fatalf("lock refusal protocol did not name the run lock:\n%s", protocol.String())
	}

	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	protocol.Reset()
	t.Setenv("PATH", t.TempDir())
	err = handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::test-fast"})
	if err == nil {
		t.Fatal("blocked lane returned nil error; want non-zero")
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("own run lock should be released, stat err=%v", err)
	}
}

func captureProcessStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	fn()
	if err := stdoutW.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrW.Close(); err != nil {
		t.Fatal(err)
	}
	stdoutBytes, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatal(err)
	}
	stderrBytes, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatal(err)
	}
	return string(stdoutBytes), string(stderrBytes)
}
