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

	"github.com/david-aggeler/keel/vscode"
)

// DHF-TEST: keel/requirement-35, keel/requirement-37
func TestVSCodeRunBlockedLaneUsesEngineProtocol(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	writeFile(t, root, "main_test.go", "package p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n")

	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", "keel::lane::test-fast")

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

// DHF-TEST: keel/requirement-41
func TestVSCodeDemoBlockVerbsPersistStateAndRejectUnknownLanes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	var protocol bytes.Buffer
	ctx := contextWithVSCodeTestState(root, &protocol)
	if err := handleVSCodeDemoBlock(ctx, []string{vscodeLaneTestFast}); err != nil {
		t.Fatalf("demo block: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".devtools", "vscode-demo-block.json")); err != nil {
		t.Fatalf("demo block state was not persisted: %v", err)
	}

	readiness := newKeelWorkspaceProfile(root).PrepareLane(context.Background(), vscodeLaneTestFast)
	if readiness.Ready() || len(readiness.Blocked) != 1 || readiness.Blocked[0].Resource != "KEEL_VSCODE_DEMO_BLOCK" {
		t.Fatalf("persisted demo block readiness = %+v, want KEEL_VSCODE_DEMO_BLOCK block", readiness)
	}

	protocol.Reset()
	if err := handleVSCodeDemoStatus(ctx, nil); err != nil {
		t.Fatalf("demo status: %v", err)
	}
	var status struct {
		BlockedLane string `json:"blocked_lane"`
		Source      string `json:"source"`
	}
	if err := json.Unmarshal(protocol.Bytes(), &status); err != nil {
		t.Fatalf("demo status JSON: %v\n%s", err, protocol.String())
	}
	if status.BlockedLane != vscodeLaneTestFast || status.Source != "state" {
		t.Fatalf("demo status = %+v, want persisted test-fast block", status)
	}

	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", vscodeLaneLint)
	readiness = newKeelWorkspaceProfile(root).PrepareLane(context.Background(), vscodeLaneTestFast)
	if !readiness.Ready() {
		t.Fatalf("env block for a different lane should take precedence over state, got %+v", readiness)
	}
	readiness = newKeelWorkspaceProfile(root).PrepareLane(context.Background(), vscodeLaneLint)
	if readiness.Ready() || readiness.Blocked[0].Detail != vscodeLaneLint {
		t.Fatalf("env block readiness = %+v, want lint block", readiness)
	}

	if err := handleVSCodeDemoUnblock(ctx, nil); err != nil {
		t.Fatalf("demo unblock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".devtools", "vscode-demo-block.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("demo unblock should remove persisted state, stat err=%v", err)
	}
	if err := handleVSCodeDemoBlock(ctx, []string{"keel::lane::missing"}); err == nil || !strings.Contains(err.Error(), "unknown vscode lane id") {
		t.Fatalf("unknown lane block err = %v, want structured refusal", err)
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
	if !strings.Contains(string(body), `"command": "bin/custom"`) || !strings.Contains(string(body), `"version": 2`) {
		t.Fatalf("upgraded config did not preserve command and stamp current version:\n%s", body)
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

	var discover bytes.Buffer
	if err := writeVSCodeDiscovery(root, &discover); err != nil {
		t.Fatalf("writeVSCodeDiscovery: %v", err)
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

	var plan bytes.Buffer
	if err := writeVSCodePlan(root, []string{"keel::lane::test-fast"}, &plan); err != nil {
		t.Fatalf("writeVSCodePlan: %v", err)
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

	var discover bytes.Buffer
	if err := writeVSCodeDiscovery(root, &discover); err != nil {
		t.Fatalf("writeVSCodeDiscovery: %v", err)
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

// DHF-TEST: keel/requirement-47
func TestVSCodeMaintenanceItemsAdvertiseCapabilitiesAndRunActions(t *testing.T) {
	root := t.TempDir()
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

	var discover bytes.Buffer
	if err := writeVSCodeDiscovery(root, &discover); err != nil {
		t.Fatalf("writeVSCodeDiscovery: %v", err)
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
	if _, err := os.Stat(filepath.Join(root, ".devtools", "vscode-demo-block.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clear-state should remove devtool state, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".vscode", "test-bridge.json")); err != nil {
		t.Fatalf("clear-state must not remove bridge config: %v", err)
	}
}

// DHF-TEST: keel/requirement-43
func TestVSCodeDiscoveryEmitsGoTestTreeFromStubGo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\nfunc TestMetrics(t *testing.T) {}\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2 $3" in
  "list -json ./...")
    printf '{"ImportPath":"github.com/david-aggeler/keel/log","Dir":"%s","TestGoFiles":["logging_test.go"]}\n' "$PWD/log"
    ;;
  "test -list .")
    case "$4" in
      ./log)
        printf 'TestLog\n'
        printf 'TestMetrics\n'
        ;;
    esac
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var discover bytes.Buffer
	if err := writeVSCodeDiscovery(root, &discover); err != nil {
		t.Fatalf("writeVSCodeDiscovery: %v\ncalls:\n%s", err, calls(t, callsFile))
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
		"go::root":                   {parent: "keel::frameworks", label: "d.1 Go", kind: "root", runnable: true},
		"go::pkg::log":               {parent: "go::root", label: "log", kind: "package", runnable: true},
		"go::test::log::TestLog":     {parent: "go::pkg::log", label: "TestLog", kind: "test", runnable: true},
		"go::test::log::TestMetrics": {parent: "go::pkg::log", label: "TestMetrics", kind: "test", runnable: true},
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

	var plan bytes.Buffer
	if err := writeVSCodePlan(root, []string{"go::test::log::TestLog"}, &plan); err != nil {
		t.Fatalf("writeVSCodePlan for go test: %v", err)
	}
	var setup vscode.SetupPlan
	if err := json.Unmarshal(plan.Bytes(), &setup); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, plan.String())
	}
	if len(setup.Items) != 1 || setup.Items[0].ID != "go::test::log::TestLog" || setup.Items[0].Kind != "test" || setup.Items[0].Framework != "go" {
		t.Fatalf("go selection plan items = %+v, want one Go test item", setup.Items)
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

	var plan bytes.Buffer
	if err := writeVSCodePlan(root, []string{"go::pkg::log", "go::root"}, &plan); err != nil {
		t.Fatalf("writeVSCodePlan for go package/root: %v", err)
	}
	var setup vscode.SetupPlan
	if err := json.Unmarshal(plan.Bytes(), &setup); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, plan.String())
	}
	if len(setup.Items) != 2 || setup.Items[0].Kind != "package" || setup.Items[0].Label != "log" || setup.Items[1].Kind != "root" {
		t.Fatalf("go package/root plan items = %+v", setup.Items)
	}
}

// DHF-TEST: keel/requirement-43
func TestVSCodeDiscoveryReportsGoEnumerationFailures(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
printf 'go list exploded\n' >&2
exit 7`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var discover bytes.Buffer
	err := writeVSCodeDiscovery(root, &discover)
	if err == nil || !strings.Contains(err.Error(), "vscode discover go list") || !strings.Contains(err.Error(), "go list exploded") {
		t.Fatalf("writeVSCodeDiscovery err = %v, want loud go list failure\ncalls:\n%s", err, calls(t, callsFile))
	}
}

// DHF-TEST: keel/requirement-39
func TestVSCodeCoverageLaneEmitsPersistedCoverageArtifact(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	writeFile(t, root, "main_test.go", "package p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n")

	var discover bytes.Buffer
	if err := writeVSCodeDiscovery(root, &discover); err != nil {
		t.Fatalf("writeVSCodeDiscovery: %v", err)
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
	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", "keel::lane::test-fast")

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
	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", "")
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
	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", "keel::lane::test-fast")

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
	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", "keel::lane::test-fast")
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
