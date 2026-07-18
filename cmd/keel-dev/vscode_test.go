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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/david-aggeler/keel/cli"
	procexec "github.com/david-aggeler/keel/exec"
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
	err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "keel::lane::test-fast")
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

func TestVSCodeHandlersDispatchDiscoveryDesiredStateAndLintRun(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	seedDetectedLanes(t, root)

	var discover bytes.Buffer
	if err := dispatchTestBridgeDiscover(contextWithVSCodeTestState(root, &discover), []string{"--format", "json"}...); err != nil {
		t.Fatalf("discover handler: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discover JSON: %v", err)
	}
	if !discoveryHasLane(doc, vscodeLaneLint) {
		t.Fatalf("discover missing lint lane: %+v", doc.Items)
	}

	var desiredStateOut bytes.Buffer
	if err := dispatchTestBridgeDesiredState(contextWithVSCodeTestState(root, &desiredStateOut), []string{"--format", "json", "--id", vscodeLaneLint}...); err != nil {
		t.Fatalf("desiredState handler: %v", err)
	}
	var desiredState vscode.DesiredStateDocument
	if err := json.Unmarshal(desiredStateOut.Bytes(), &desiredState); err != nil {
		t.Fatalf("desiredState JSON: %v", err)
	}
	if desiredState.Version != 3 || len(desiredState.Groups) != 1 || len(desiredState.Groups[0].Rows) == 0 {
		t.Fatalf("desired-state document = %+v, want v3 desired-state groups", desiredState)
	}

	var protocol bytes.Buffer
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), vscodeLaneLint); err != nil {
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

	var desiredStateOut bytes.Buffer
	if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &desiredStateOut), []string{"test-bridge", "tests", "desired-state", "--format", "json", "--id", vscodeLaneLint}); err != nil {
		t.Fatalf("canonical desired-state: %v", err)
	}
	var desiredState vscode.DesiredStateDocument
	if err := json.Unmarshal(desiredStateOut.Bytes(), &desiredState); err != nil {
		t.Fatalf("desired-state JSON: %v\n%s", err, desiredStateOut.String())
	}

	var protocol bytes.Buffer
	err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", vscodeLaneLint})
	if err == nil || !strings.Contains(err.Error(), "keel/testbridge: run lock already exists") {
		t.Fatalf("canonical run err = %v, want package-owned run lock refusal", err)
	}
	for _, event := range decodeRunEvents(t, protocol.String()) {
		if event.Workspace != desiredState.Workspace {
			t.Fatalf("run event workspace = %q, want desired-state workspace %q in %+v", event.Workspace, desiredState.Workspace, event)
		}
	}
}

// DHF-TEST: keel/requirement-65
func TestCanonicalBridgeSurfaceHasNoVSCodeOrLanesVerbs(t *testing.T) {
	tree := commandTree()
	if commandSpecHasName(tree, "vscode") {
		t.Fatal("command tree still exposes vscode command")
	}
	if commandSpecHasName(tree, "lanes") {
		t.Fatal("command tree still exposes lanes verb")
	}

	err := tree.Dispatch(contextWithVSCodeTestState(t.TempDir(), io.Discard), []string{"vscode"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("vscode dispatch err = %v, want unknown command", err)
	}

	testBridge := commandSpecByPath(tree, "test-bridge")
	if testBridge == nil {
		t.Fatal("missing canonical test-bridge command")
	}
	got := commandLeafUses(testBridge)
	want := []string{
		"test-bridge config init",
		"test-bridge config upgrade",
		"test-bridge tests discover [--format json]",
		"test-bridge tests desired-state [--format json] [--id test-id]",
		"test-bridge tests run [--dry-run] --id test-id",
	}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("test-bridge leaves = %#v, want %#v", got, want)
	}
}

// DHF-TEST: keel/requirement-65
func TestDetectLanesProducesFileBackedLaneTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "exec"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("exec", "exec_test.go"), "package exec\n\nimport \"testing\"\n\nfunc TestExec(t *testing.T) {}\n")

	before, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery before detect: %v", err)
	}
	lanesGroup, ok := discoveryItemByID(before, vscodeGroupLanes)
	if !ok || lanesGroup.Label != "C - Lanes" {
		t.Fatalf("lanes group before detect = %+v, ok=%v; want C - Lanes", lanesGroup, ok)
	}
	for _, item := range before.Items {
		if item.ParentID == vscodeGroupLanes {
			t.Fatalf("fresh workspace emitted lane child before detect: %+v", item)
		}
	}

	var protocol bytes.Buffer
	if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", vscodeMaintenanceDetectLanes}); err != nil {
		t.Fatalf("detect lanes maintenance run: %v\n%s", err, protocol.String())
	}
	if !runEventsContain(decodeRunEvents(t, protocol.String()), "passed", vscodeMaintenanceDetectLanes) {
		t.Fatalf("detect lanes maintenance events = %s", protocol.String())
	}

	data, err := os.ReadFile(filepath.Join(root, ".vscode", "test-lanes.json"))
	if err != nil {
		t.Fatalf("detect lanes did not write test-lanes.json: %v", err)
	}
	for _, want := range []string{`"id": "lint"`, `"id": "test-fast"`, `"id": "test-coverage"`, `"id": "vsix-ci"`, `"id": "ci"`, `"id": "go-exec"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("test-lanes.json missing %s:\n%s", want, data)
		}
	}

	after, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery after detect: %v", err)
	}
	for _, want := range []string{vscodeLaneLint, vscodeLaneTestFast, vscodeLaneTestCoverage, vscodeLaneVSIXGate, vscodeLaneCI, "keel::lane::go-exec"} {
		if !discoveryHasLane(after, want) {
			t.Fatalf("discovery after detect missing %q: %+v", want, after.Items)
		}
	}
}

// DHF-TEST: keel/requirement-65, keel/requirement-51
func TestDetectedGateLanesExposeCoversFromFileMembers(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "vsix", "src", "test", "suite"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("vsix", "src", "test", "suite", "extension.test.ts"), "suite('x', () => {});\n")
	seedDetectedLanes(t, root)

	discovery, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", err)
	}
	for parent, canonical := range map[string]string{
		"keel::lane::test-fast::covers":     "go::root",
		"keel::lane::test-coverage::covers": "go::root",
		"keel::lane::vsix-ci::covers":       "vsix::root",
		"keel::lane::ci::covers":            "keel::lane::lint",
	} {
		if !discoveryHasAlias(discovery, parent, canonical) {
			t.Fatalf("discovery missing covers alias parent=%q canonical=%q: %+v", parent, canonical, discovery.Items)
		}
	}
	if !discoveryHasAlias(discovery, "keel::lane::ci::covers", "keel::lane::test-coverage") {
		t.Fatalf("ci covers should include test-coverage lane: %+v", discovery.Items)
	}
}

// DHF-TEST: keel/requirement-65
func TestDetectLanesNormalizesModuleRootPackageFamily(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	writeFile(t, root, "root_test.go", "package keel\n\nimport \"testing\"\n\nfunc TestRoot(t *testing.T) {}\n")

	seedDetectedLanes(t, root)
	data, err := os.ReadFile(filepath.Join(root, ".vscode", "test-lanes.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"id": "go-."`) || strings.Contains(text, `"go": "././..."`) {
		t.Fatalf("detect emitted degenerate module-root lane:\n%s", text)
	}
	if !strings.Contains(text, `"id": "go-root"`) || !strings.Contains(text, `"go": "./"`) {
		t.Fatalf("detect missing normalized module-root lane:\n%s", text)
	}
}

// DHF-TEST: keel/requirement-64
func TestVSCodeDesiredStateCarriesComparableDevtoolIdentity(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	var desiredStateOut bytes.Buffer
	if err := dispatchTestBridgeDesiredState(contextWithVSCodeTestState(root, &desiredStateOut), []string{"--format", "json", "--id", vscodeLaneLint}...); err != nil {
		t.Fatalf("desiredState handler: %v", err)
	}
	var desiredState vscode.DesiredStateDocument
	if err := json.Unmarshal(desiredStateOut.Bytes(), &desiredState); err != nil {
		t.Fatalf("desiredState JSON: %v\n%s", err, desiredStateOut.String())
	}
	if desiredState.Devtool.Name != "keel-dev" || desiredState.Devtool.Version == "" || desiredState.Devtool.Commit == "" || desiredState.Devtool.BuiltAt == "" {
		t.Fatalf("devtool identity = %+v, want name plus version, commit, built_at", desiredState.Devtool)
	}
}

// DHF-TEST: keel/requirement-65
func TestLegacyVSCodeCommandIsUnknown(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	err := commandTree().Dispatch(contextWithVSCodeTestState(root, io.Discard), []string{"vscode", "tests", "run", "--format", "jsonl", "--id", vscodeLaneLint})
	if err == nil {
		t.Fatal("legacy vscode command returned nil error")
	}
	message := err.Error()
	if !strings.Contains(message, "unknown command") || !strings.Contains(message, "vscode") {
		t.Fatalf("legacy vscode error = %q, want unknown command", message)
	}
}

// DHF-TEST: keel/requirement-65
func TestVSCodeSourceHasNoLegacyAliasLayer(t *testing.T) {
	body, err := os.ReadFile("vscode.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, forbidden := range []string{
		"func vscodeCommandSpec(",
		"func dispatchTestBridgeAlias(",
		"func handleVSCodeTestsDiscover(",
		"func handleVSCodeTestsDesiredState(",
		"func handleVSCodeTestsRun(",
		"func handleVSCodeLanesList(",
		"func handleVSCodeLanesDetect(",
		"func parseVSCodeLanesDetectArgs(",
		`Use: "vscode `,
		`"vscode", "tests"`,
		`"vscode", "lanes"`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("legacy vscode alias layer still contains %q", forbidden)
		}
	}
}

// DHF-TEST: keel/requirement-40
func TestVSCodeConfigHandlersInitAndUpgrade(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")

	if err := dispatchTestBridgeConfigInit(contextWithVSCodeTestState(root, io.Discard)); err != nil {
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
	if err := dispatchTestBridgeConfigUpgrade(contextWithVSCodeTestState(root, io.Discard)); err != nil {
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
		"vscode tests desired-state",
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

func dispatchTestBridgeConfigInit(ctx context.Context) error {
	return commandTree().Dispatch(ctx, []string{"test-bridge", "config", "init"})
}

func dispatchTestBridgeConfigUpgrade(ctx context.Context) error {
	return commandTree().Dispatch(ctx, []string{"test-bridge", "config", "upgrade"})
}

func dispatchTestBridgeDiscover(ctx context.Context, args ...string) error {
	canonical := append([]string{"test-bridge", "tests", "discover"}, args...)
	return commandTree().Dispatch(ctx, canonical)
}

func dispatchTestBridgeDesiredState(ctx context.Context, args ...string) error {
	canonical := append([]string{"test-bridge", "tests", "desired-state"}, args...)
	return commandTree().Dispatch(ctx, canonical)
}

func dispatchTestBridgeRun(ctx context.Context, ids ...string) error {
	canonical := []string{"test-bridge", "tests", "run"}
	for _, id := range ids {
		canonical = append(canonical, "--id", id)
	}
	return commandTree().Dispatch(ctx, canonical)
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

func TestVSCodeDiscoveryAndDesiredStateExposeKeelLaneSet(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	seedDetectedLanes(t, root)
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

	built1, buildErr1 := buildVSCodeDesiredStateDocument(root, []string{"keel::lane::test-fast"})
	if buildErr1 != nil {
		t.Fatalf("buildVSCodeDesiredStateDocument: %v", buildErr1)
	}
	var desiredStateOut bytes.Buffer
	if err := testbridge.EncodeDocument(&desiredStateOut, built1); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var desiredState vscode.DesiredStateDocument
	if err := json.Unmarshal(desiredStateOut.Bytes(), &desiredState); err != nil {
		t.Fatalf("desiredState JSON: %v\n%s", err, desiredStateOut.String())
	}
	if desiredState.Version != 3 || !desiredStateHasRunID(desiredState.Groups, vscodeDesiredStateGoToolchain) {
		t.Fatalf("desired-state document = %+v, want v3 desired-state go-toolchain row", desiredState)
	}
}

// DHF-TEST: keel/requirement-46, keel/requirement-69, keel/requirement-74
func TestVSCodeDiscoveryEmitsStructuredOrderedTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")

	var discover bytes.Buffer
	if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &discover), []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("canonical discover: %v", err)
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
		if strings.Contains(item.ID, "a.") || strings.Contains(item.ID, "b.") || strings.Contains(item.ID, "c.") || strings.Contains(item.ID, "d.") {
			t.Fatalf("item id encodes ordinal %q for label %q", item.ID, item.Label)
		}
		assertDiscoveryKindAllowedBySchema(t, item.Kind)
	}

	wantTop := map[string]struct {
		label string
		sort  string
	}{
		"keel::maintenance":   {label: "A - Test Bridge Maintenance", sort: "a"},
		"keel::desired-state": {label: "B - Desired State", sort: "b"},
		"keel::lanes":         {label: "C - Lanes", sort: "c"},
		"keel::frameworks":    {label: "D - Frameworks", sort: "d"},
	}
	if len(top) != len(wantTop) {
		t.Fatalf("top-level groups = %+v, want exactly %d groups", top, len(wantTop))
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
	wantOrder := []string{"keel::maintenance", "keel::desired-state", "keel::lanes", "keel::frameworks"}
	topIDs := make([]string, 0, len(top))
	for id := range top {
		topIDs = append(topIDs, id)
	}
	sort.Slice(topIDs, func(i, j int) bool { return top[topIDs[i]].SortText < top[topIDs[j]].SortText })
	if !stringSlicesEqual(topIDs, wantOrder) {
		t.Fatalf("top-level sorted ids = %v, want %v", topIDs, wantOrder)
	}
	desiredGroup, ok := discoveryItemByID(doc, "keel::desired-state::group::test-preconditions")
	if !ok || desiredGroup.ParentID != "keel::desired-state" || desiredGroup.Label != "Test Preconditions" || desiredGroup.SortText != "b.010" || strings.Join(desiredGroup.Limitations, " ") != "mutually_exclusive=false" {
		t.Fatalf("desired-state group = %+v, ok=%v", desiredGroup, ok)
	}
	var desiredRow vscode.TestItem
	for _, item := range doc.Items {
		if item.ParentID == desiredGroup.ID && strings.Contains(item.Label, "go-toolchain") {
			desiredRow = item
			break
		}
	}
	if desiredRow.ID == "" || desiredRow.SortText != "b.010.001" || desiredRow.Label != "go-toolchain: available" || !strings.Contains(strings.Join(desiredRow.Limitations, " "), "action=reuse") {
		t.Fatalf("desired-state row = %+v", desiredRow)
	}

	goRoot, ok := discoveryItemByID(doc, "go::root")
	if !ok {
		t.Fatal("discovery missing go::root")
	}
	if goRoot.ParentID != "keel::frameworks" || goRoot.Label != "d.1 Go" || goRoot.SortText != "d.001" {
		t.Fatalf("go::root = %+v, want parent keel::frameworks label d.1 Go sort_text d.001", goRoot)
	}
}

// DHF-TEST: keel/requirement-60, keel/requirement-74
func TestKeelDevDesiredStateRowsAreRunnableThroughCanonicalBridge(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	var discover bytes.Buffer
	if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &discover), []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("canonical discover: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	for _, id := range []string{
		"keel::desired-state::go-toolchain",
		"keel::desired-state::keel-module-root",
		"keel::desired-state::stub-binaries",
	} {
		item, ok := discoveryItemByID(doc, id)
		if !ok {
			t.Fatalf("discovery missing runnable desired-state row %q: %+v", id, doc.Items)
		}
		if item.ParentID != "keel::desired-state::group::test-preconditions" || !item.Runnable || !stringSlicesEqual(item.Profiles, []string{"run"}) {
			t.Fatalf("desired-state row %q = %+v, want runnable run-profile child of Test Preconditions", id, item)
		}
	}

	var protocol bytes.Buffer
	if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", "keel::desired-state::keel-module-root"}); err != nil {
		t.Fatalf("satisfied desired-state row run: %v\nprotocol:\n%s", err, protocol.String())
	}
	if events := decodeRunEvents(t, protocol.String()); !runEventsContain(events, "passed", "keel::desired-state::keel-module-root") {
		t.Fatalf("satisfied desired-state row events = %+v, want passed event for keel-module-root", events)
	}

	t.Setenv("PATH", t.TempDir())
	protocol.Reset()
	err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", "keel::desired-state::go-toolchain"})
	if err == nil {
		t.Fatal("blocked desired-state row returned nil error; want failing run")
	}
	events := decodeRunEvents(t, protocol.String())
	if !runEventsContain(events, "failed", "keel::desired-state::go-toolchain") || !strings.Contains(protocol.String(), "Install Go if this check is blocked.") {
		t.Fatalf("blocked desired-state row events = %+v\nprotocol:\n%s", events, protocol.String())
	}
	if events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode == 0 {
		t.Fatalf("blocked desired-state terminal event = %+v, want non-zero exit", events[len(events)-1])
	}
}

// DHF-TEST: keel/requirement-75
func TestKeelModuleRootDesiredStateProbeReadsGoMod(t *testing.T) {
	root := t.TempDir()

	var protocol bytes.Buffer
	err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", vscodeDesiredStateModuleRoot})
	if err == nil {
		t.Fatal("module-root row without go.mod returned nil error; want failing run")
	}
	if got := protocol.String(); !runEventsContain(decodeRunEvents(t, got), "failed", vscodeDesiredStateModuleRoot) || !strings.Contains(got, "go.mod") {
		t.Fatalf("missing go.mod protocol:\n%s", got)
	}

	writeFile(t, root, "go.mod", "module example.com/not-keel\n\ngo 1.25\n")
	protocol.Reset()
	err = commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", vscodeDesiredStateModuleRoot})
	if err == nil {
		t.Fatal("module-root row with wrong module path returned nil error; want failing run")
	}
	if got := protocol.String(); !runEventsContain(decodeRunEvents(t, got), "failed", vscodeDesiredStateModuleRoot) || !strings.Contains(got, "example.com/not-keel") {
		t.Fatalf("mismatched go.mod protocol:\n%s", got)
	}

	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	protocol.Reset()
	if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", vscodeDesiredStateModuleRoot}); err != nil {
		t.Fatalf("module-root row with matching go.mod: %v\n%s", err, protocol.String())
	}
	if got := protocol.String(); !runEventsContain(decodeRunEvents(t, got), "passed", vscodeDesiredStateModuleRoot) || !strings.Contains(got, modulePath) {
		t.Fatalf("matching go.mod protocol:\n%s", got)
	}
}

// DHF-TEST: keel/requirement-75
func TestStubBinariesDesiredStateProbeBuildsStubPackages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	if err := os.MkdirAll(filepath.Join(root, "exec", "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "exec", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("exec", "codex", "doc.go"), "package codex\n")
	writeFile(t, root, filepath.Join("exec", "claude", "doc.go"), "package claude\n")

	var protocol bytes.Buffer
	if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", vscodeDesiredStateStubBinaries}); err != nil {
		t.Fatalf("stub-binaries row with buildable packages: %v\n%s", err, protocol.String())
	}
	if got := protocol.String(); !runEventsContain(decodeRunEvents(t, got), "passed", vscodeDesiredStateStubBinaries) {
		t.Fatalf("buildable stub protocol:\n%s", got)
	}

	protocol.Reset()
	if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{
		"test-bridge", "tests", "run",
		"--id", vscodeDesiredStateModuleRoot,
		"--id", vscodeDesiredStateStubBinaries,
	}); err != nil {
		t.Fatalf("multi-id desired-state run with buildable packages: %v\n%s", err, protocol.String())
	}
	events := decodeRunEvents(t, protocol.String())
	if !runEventsContain(events, "passed", vscodeDesiredStateModuleRoot) || !runEventsContain(events, "passed", vscodeDesiredStateStubBinaries) {
		t.Fatalf("multi-id desired-state events = %+v", events)
	}

	writeFile(t, root, filepath.Join("exec", "codex", "broken.go"), "package codex\n\nfunc broken(\n")
	protocol.Reset()
	err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), []string{"test-bridge", "tests", "run", "--id", vscodeDesiredStateStubBinaries})
	if err == nil {
		t.Fatal("stub-binaries row with invalid package returned nil error; want failing run")
	}
	if got := protocol.String(); !runEventsContain(decodeRunEvents(t, got), "failed", vscodeDesiredStateStubBinaries) || !strings.Contains(got, "stub fixture packages") {
		t.Fatalf("broken stub protocol:\n%s", got)
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
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "keel::lane::core"); err != nil {
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

// DHF-TEST: keel/requirement-51, keel/requirement-65, keel/requirement-73
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
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[{"id":"go-log","label":"edited log","order":"b.40","description":"legacy","members":[{"go":"./log/..."}]},{"id":"manual","label":"manual","order":"b.41","members":[{"go":"./manual/..."}]}]}`+"\n")
	before, err := os.ReadFile(lanesPath)
	if err != nil {
		t.Fatal(err)
	}

	var list bytes.Buffer
	if err := writeVSCodeLanesList(root, &list); err != nil {
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
	if err := writeVSCodeLanesDetect(root, true, &dry); err != nil {
		t.Fatalf("lanes detect --dry-run: %v", err)
	}
	if after, err := os.ReadFile(lanesPath); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("dry-run changed lanes file: err=%v before=%q after=%q", err, before, after)
	}
	var dryDoc lanesDetectDocument
	if err := json.Unmarshal(dry.Bytes(), &dryDoc); err != nil {
		t.Fatalf("dry-run JSON: %v\n%s", err, dry.String())
	}
	if dryDoc.Written || !lanesDetectAdded(dryDoc, "go-exec") || !lanesDetectChanged(dryDoc, "go-log") || !lanesDetectRemoved(dryDoc, "manual") {
		t.Fatalf("dry-run doc = %+v, want go-exec added, go-log changed, manual removed, written=false", dryDoc)
	}

	var detect bytes.Buffer
	if err := writeVSCodeLanesDetect(root, false, &detect); err != nil {
		t.Fatalf("lanes detect: %v", err)
	}
	var detectDoc lanesDetectDocument
	if err := json.Unmarshal(detect.Bytes(), &detectDoc); err != nil {
		t.Fatalf("detect JSON: %v\n%s", err, detect.String())
	}
	if !detectDoc.Written || !lanesDetectAdded(detectDoc, "go-exec") || !lanesDetectChanged(detectDoc, "go-log") || !lanesDetectRemoved(detectDoc, "manual") {
		t.Fatalf("detect doc = %+v, want full-rewrite delta", detectDoc)
	}
	afterWrite, err := os.ReadFile(lanesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(afterWrite), `"id": "go-exec"`) || !strings.Contains(string(afterWrite), `"id": "go-log"`) || strings.Contains(string(afterWrite), `"id": "manual"`) || strings.Contains(string(afterWrite), "edited log") || strings.Contains(string(afterWrite), `"order": "b.40"`) {
		t.Fatalf("detect did not regenerate canonical lanes:\n%s", afterWrite)
	}
	// Round-trip guard: detect must write lowercase member keys.
	if strings.Contains(string(afterWrite), `"Go"`) {
		t.Fatalf("detect wrote capitalized member keys:\n%s", afterWrite)
	}
	var relist bytes.Buffer
	if err := writeVSCodeLanesList(root, &relist); err != nil {
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
	if err := writeVSCodeLanesDetect(root, false, &second); err != nil {
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
	if !secondDoc.Written || len(secondDoc.Added) != 0 || len(secondDoc.Changed) != 0 || len(secondDoc.Removed) != 0 || !bytes.Equal(secondBytes, afterWrite) {
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

// DHF-TEST: keel/requirement-52, keel/requirement-87
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

	var discover bytes.Buffer
	if err := dispatchTestBridgeDiscover(contextWithVSCodeTestState(root, &discover), "--format", "json"); err != nil {
		t.Fatalf("discover dispatch: %v", err)
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
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), vscodeMaintenanceDetectLanes); err != nil {
		t.Fatalf("detect lanes maintenance run: %v\n%s", err, protocol.String())
	}
	if !strings.Contains(protocol.String(), "go-exec") || !runEventsContain(decodeRunEvents(t, protocol.String()), "passed", vscodeMaintenanceDetectLanes) {
		t.Fatalf("detect lanes maintenance events = %s", protocol.String())
	}
}

// DHF-TEST: keel/requirement-53, keel/requirement-58
func TestVSCodeRunStartedCarriesRequestedSelection(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[{"id":"test-fast","label":"test-fast","order":"c.2","members":[{"root":"go"}]}]}`+"\n")
	t.Setenv("PATH", t.TempDir())

	var protocol bytes.Buffer
	err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), vscodeLaneTestFast)
	if err == nil {
		t.Fatal("blocked run returned nil; want non-zero")
	}
	events := decodeRunEvents(t, protocol.String())
	if len(events) == 0 || events[0].Event != "run_started" {
		t.Fatalf("events = %+v, want run_started first", events)
	}
	if got := events[0].Requested; len(got) != 1 || got[0].ID != vscodeLaneTestFast || got[0].Label != "c.2 test-fast" {
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
	writeFile(t, root, filepath.Join("vsix", "src", "test", "suite", "extension.test.ts"), "suite('x', () => {\n  test('alpha case', () => {});\n});\n")
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
	coversID := "keel::lane::go-log::covers"
	pkgAliasID := coversID + "::" + StableIDSegment("go::pkg::log")
	fileAliasID := coversID + "::" + StableIDSegment("go::file::log/logging_test.go")
	for _, want := range []struct {
		parentID    string
		canonicalID string
	}{
		{parentID: coversID, canonicalID: "go::pkg::log"},
		{parentID: pkgAliasID, canonicalID: "go::file::log/logging_test.go"},
		{parentID: fileAliasID, canonicalID: "go::test::log::TestLog"},
	} {
		if !discoveryHasAlias(doc, want.parentID, want.canonicalID) {
			t.Fatalf("go-log covers alias canonical=%q missing under parent %q: %+v", want.canonicalID, want.parentID, doc.Items)
		}
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
	// requirement-94 (ac-308): the vsix-member covers expands file→test.
	vsixFileAliasID := "keel::lane::ui::covers::" + StableIDSegment("vsix::file::src/test/suite/extension.test.ts")
	if !discoveryHasAlias(doc, vsixFileAliasID, "vsix::test::src/test/suite/extension.test.ts::alpha-case") {
		t.Fatalf("ui covers should nest the vsix test alias under its file alias: %+v", doc.Items)
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
  "--dir "*"/vsix run test:headless:json -- src/test/suite/extension.test.ts src/test/suite/tree.test.ts")
    exit 0
    ;;
  *)
    printf 'unexpected pnpm invocation: %s\n' "$*" >&2
    exit 2
    ;;
esac`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var protocol bytes.Buffer
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "keel::lane::ui"); err != nil {
		t.Fatalf("ui lane run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	if got := calls(t, callsFile); !strings.Contains(got, "pnpm --dir "+filepath.Join(root, "vsix")+" run test:headless:json -- src/test/suite/extension.test.ts src/test/suite/tree.test.ts") {
		t.Fatalf("pnpm call missing exact vsix file filter:\n%s", got)
	}
}

// DHF-TEST: keel/requirement-91
func TestVSCodeRunDirectVSIXSelectionsUseSelectedIDAndFileFilter(t *testing.T) {
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
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[]}`+"\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "exit 0")
	stub(t, bin, callsFile, "pnpm", `
case "$*" in
  "--dir "*"/vsix run test:headless:json -- src/test/suite/extension.test.ts src/test/suite/tree.test.ts")
    exit 0
    ;;
  "--dir "*"/vsix run test:headless:json -- src/test/suite/tree.test.ts")
    exit 0
    ;;
  *)
    printf 'unexpected pnpm invocation: %s\n' "$*" >&2
    exit 2
    ;;
esac`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cases := []struct {
		id       string
		wantCall string
	}{
		{
			id:       "vsix::root",
			wantCall: "pnpm --dir " + filepath.Join(root, "vsix") + " run test:headless:json -- src/test/suite/extension.test.ts src/test/suite/tree.test.ts",
		},
		{
			id:       "vsix::file::src/test/suite/tree.test.ts",
			wantCall: "pnpm --dir " + filepath.Join(root, "vsix") + " run test:headless:json -- src/test/suite/tree.test.ts",
		},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			var protocol bytes.Buffer
			if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), tc.id); err != nil {
				t.Fatalf("direct vsix selection run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
			}
			if got := calls(t, callsFile); !strings.Contains(got, tc.wantCall) {
				t.Fatalf("pnpm call missing exact vsix file filter %q:\n%s", tc.wantCall, got)
			}
			events := decodeRunEvents(t, protocol.String())
			if !runEventsContain(events, "test_started", tc.id) {
				t.Fatalf("run events missing selected id start for %s: %+v", tc.id, events)
			}
			if !runEventsContain(events, "passed", tc.id) {
				t.Fatalf("run events missing selected id pass for %s: %+v", tc.id, events)
			}
			if events[len(events)-1].Event != "run_finished" || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode != 0 {
				t.Fatalf("terminal event = %+v, want run_finished exit 0", events[len(events)-1])
			}
		})
	}
}

// DHF-TEST: keel/requirement-91
func TestVSCodeRunDirectVSIXSelectionFailureUsesSelectedID(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, dir := range []string{".vscode", filepath.Join("vsix", "src", "test", "suite")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, filepath.Join("vsix", "src", "test", "suite", "tree.test.ts"), "suite('y', () => {});\n")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[]}`+"\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "exit 0")
	stub(t, bin, callsFile, "pnpm", `
printf 'mocha failed\n' >&2
exit 1`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	const selectedID = "vsix::file::src/test/suite/tree.test.ts"
	var protocol bytes.Buffer
	err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), selectedID)
	if err == nil {
		t.Fatalf("direct vsix selection failure returned nil\nprotocol:\n%s\ncalls:\n%s", protocol.String(), calls(t, callsFile))
	}
	events := decodeRunEvents(t, protocol.String())
	if !runEventsContain(events, "test_started", selectedID) {
		t.Fatalf("run events missing selected id start for %s: %+v", selectedID, events)
	}
	if !runEventsContain(events, "failed", selectedID) {
		t.Fatalf("run events missing selected id failure for %s: %+v", selectedID, events)
	}
	if !runEventsContain(events, "errored", "") {
		t.Fatalf("run events missing generic errored event for command failure: %+v", events)
	}
	if events[len(events)-1].Event != "run_finished" || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode == 0 {
		t.Fatalf("terminal event = %+v, want run_finished non-zero", events[len(events)-1])
	}
}

// DHF-TEST: keel/requirement-91
func TestVSCodeDiscoveredRunnableVSIXItemsAreAcceptedByRunDispatcher(t *testing.T) {
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
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[]}`+"\n")

	built, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", err)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "exit 0")
	stub(t, bin, callsFile, "pnpm", "exit 0")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, item := range doc.Items {
		if !item.Runnable || item.Framework != "vsix" {
			continue
		}
		var protocol bytes.Buffer
		err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), item.ID)
		if err != nil {
			t.Fatalf("runnable vsix discovery item %q was rejected: %v\nprotocol:\n%s\ncalls:\n%s", item.ID, err, protocol.String(), calls(t, callsFile))
		}
		if strings.Contains(protocol.String(), "unknown vscode lane id") {
			t.Fatalf("runnable vsix discovery item %q reached unknown-lane default:\n%s", item.ID, protocol.String())
		}
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

// DHF-TEST: keel/requirement-51, keel/requirement-73
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
	// DHF-TEST: keel/requirement-73
	// Detect-lanes is the recovery path for whole-file lane errors: it rewrites
	// from compiled/workspace knowledge instead of preserving the invalid file.
	var cycleDetect bytes.Buffer
	if err := writeVSCodeLanesDetect(root, false, &cycleDetect); err != nil {
		t.Fatalf("lanes detect should heal a cyclic file: %v\n%s", err, cycleDetect.String())
	}
	if after, err := os.ReadFile(filepath.Join(root, ".vscode", "test-lanes.json")); err != nil || strings.Contains(string(after), `"id": "a"`) || !strings.Contains(string(after), `"id": "test-fast"`) {
		t.Fatalf("lanes detect did not regenerate cyclic file: err=%v\n%s", err, after)
	}

	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[{"id":"bad","label":"bad","order":"b.40","members":[{"unknown":"x"}]}]}`+"\n")
	var protocol bytes.Buffer
	err = dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "keel::lane::bad")
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid lane run err = %v\n%s", err, protocol.String())
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
	if got := goPackageFamily("."); got != "root" {
		t.Fatalf("root package family = %q", got)
	}
	if items, err := discoverVSIXTestItems(filepath.Join(root, "missing")); err != nil || len(items) != 0 {
		t.Fatalf("absent vsix discovery = %v, %v", items, err)
	}

	t.Setenv("PATH", t.TempDir())
	if err := runVSIXFileSelection(context.Background(), discardLogger(), root, []string{"src/test/suite/x.test.ts"}, procexec.DefaultMaxOutputBytes, nil); err == nil || !strings.Contains(err.Error(), "pnpm") {
		t.Fatalf("missing pnpm err = %v", err)
	}
}

// DHF-TEST: keel/requirement-51, keel/requirement-73
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
	if err := writeVSCodeLanesDetect(root, false, &protocol); err != nil {
		t.Fatalf("detect should heal malformed lanes file: %v", err)
	}
	var malformedDoc lanesDetectDocument
	if err := json.Unmarshal(protocol.Bytes(), &malformedDoc); err != nil {
		t.Fatalf("malformed recovery JSON: %v\n%s", err, protocol.String())
	}
	if !malformedDoc.Written || !lanesDetectAdded(malformedDoc, "test-fast") {
		t.Fatalf("malformed recovery doc = %+v, want regenerated gate lanes", malformedDoc)
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
	if err != nil || len(events) == 0 || !runEventsContain(events, "output", vscodeMaintenanceDetectLanes) {
		t.Fatalf("detect maintenance should heal malformed lanes file: err=%v events=%+v", err, events)
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

// DHF-TEST: keel/requirement-47, keel/requirement-87
func TestVSCodeMaintenanceItemsAdvertiseCapabilitiesAndRunActions(t *testing.T) {
	root := t.TempDir()
	if code, err := (keelTestBridge{}).ClearState(context.Background(), testbridge.RunRequest{Root: root}, func(vscode.RunEvent) {}); err != nil || code != 0 {
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

	var discover bytes.Buffer
	if err := dispatchTestBridgeDiscover(contextWithVSCodeTestState(root, &discover), "--format", "json"); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	if got, want := doc.Capabilities.ClearResultsTestIDs, []string{testbridge.MaintenanceClearResultsID}; !stringSlicesEqual(got, want) {
		t.Fatalf("clear_results_test_ids = %v, want %v", got, want)
	}
	if got, want := doc.Capabilities.ClearStateTestIDs, []string{testbridge.MaintenanceClearStateID}; !stringSlicesEqual(got, want) {
		t.Fatalf("clear_state_test_ids = %v, want %v", got, want)
	}
	for id, want := range map[string]struct {
		label string
		sort  string
	}{
		testbridge.MaintenanceUnlockID:       {label: "a.2 unlock test bridge", sort: "a.002"},
		testbridge.MaintenanceClearResultsID: {label: "a.3 clear test results", sort: "a.003"},
		testbridge.MaintenanceClearStateID:   {label: "a.4 clear local test state", sort: "a.004"},
	} {
		item, ok := discoveryItemByID(doc, id)
		if !ok {
			t.Fatalf("discovery missing maintenance item %q", id)
		}
		if item.ParentID != testbridge.MaintenanceGroupID || item.Kind != "maintenance" || item.Label != want.label || item.SortText != want.sort || !item.Runnable {
			t.Fatalf("maintenance item %q = %+v, want parent maintenance label=%q sort=%q runnable", id, item, want.label, want.sort)
		}
	}

	var protocol bytes.Buffer
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), testbridge.MaintenanceUnlockID); err != nil {
		t.Fatalf("unlock maintenance run: %v\nprotocol:\n%s", err, protocol.String())
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unlock should remove stranded run.lock, stat err=%v", err)
	}
	if events := decodeRunEvents(t, protocol.String()); !runEventsContain(events, "passed", testbridge.MaintenanceUnlockID) || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode != 0 {
		t.Fatalf("unlock events = %+v, want passed and run_finished exit 0", events)
	}

	protocol.Reset()
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), testbridge.MaintenanceClearStateID); err != nil {
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

	seedDetectedLanes(t, root)
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
		"keel::lane::vsix-ci": {label: "c.10 vsix ci", sort: "c.010"},
		"keel::lane::ci":      {label: "c.30 ci", sort: "c.030"},
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
	err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "keel::lane::vsix-ci")
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
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(goodRoot, &protocol), "keel::lane::ci"); err != nil {
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
	mustRun(t, badRoot, "git", "init")
	writeFile(t, badRoot, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, badRoot, "bad.go", "package p\n\nvar    Y = 2\n")
	mustRun(t, badRoot, "git", "add", "go.mod", "bad.go")
	protocol.Reset()
	err = dispatchTestBridgeRun(contextWithVSCodeTestState(badRoot, &protocol), "keel::lane::ci")
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
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "go::file::log/semantics_test.go"); err != nil {
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
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "go::test::log::TestLog"); err != nil {
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

	built13, buildErr13 := buildVSCodeDesiredStateDocument(root, []string{"go::test::log::TestLog"})
	if buildErr13 != nil {
		t.Fatalf("buildVSCodeDesiredStateDocument for go test: %v", buildErr13)
	}
	var desiredStateOut bytes.Buffer
	if err := testbridge.EncodeDocument(&desiredStateOut, built13); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var desiredState vscode.DesiredStateDocument
	if err := json.Unmarshal(desiredStateOut.Bytes(), &desiredState); err != nil {
		t.Fatalf("desiredState JSON: %v\n%s", err, desiredStateOut.String())
	}
	if desiredState.Version != 3 || !desiredStateHasRunID(desiredState.Groups, vscodeDesiredStateModuleRoot) {
		t.Fatalf("go selection desired-state document = %+v, want v3 desired-state groups", desiredState)
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
	err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "go::file::log/selected_test.go")
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
	stub(t, bin, callsFile, "go", `case "$*" in
  "test ./exec/codex ./exec/claude")
    exit 0
    ;;
esac
printf 'go test must not run after a file parse failure\n' >&2
exit 2`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var protocol bytes.Buffer
	err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "go::file::log/broken_test.go")
	if err == nil {
		t.Fatalf("go file selection parse failure returned nil\nprotocol:\n%s\ncalls:\n%s", protocol.String(), calls(t, callsFile))
	}
	if got := strings.TrimSpace(calls(t, callsFile)); got != "go test ./exec/codex ./exec/claude" {
		t.Fatalf("unexpected go test command after parse failure:\n%s", got)
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
		name         string
		id           string
		want         string
		desiredState func(t *testing.T, root string)
	}{
		{
			name: "build constraints",
			id:   "go::file::log/tagged_test.go",
			want: "excluded by build constraints",
			desiredState: func(t *testing.T, root string) {
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
			desiredState: func(t *testing.T, root string) {
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
			desiredState: func(t *testing.T, root string) {
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
			desiredState: func(t *testing.T, root string) {
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
			tt.desiredState(t, root)

			bin := t.TempDir()
			callsFile := filepath.Join(bin, "calls.log")
			stub(t, bin, callsFile, "go", `case "$*" in
  "test ./exec/codex ./exec/claude")
    exit 0
    ;;
esac
printf 'go test must not run for inactive file selections\n' >&2
exit 2`)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			var protocol bytes.Buffer
			err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), tt.id)
			if err == nil {
				t.Fatalf("inactive go file selection returned nil\nprotocol:\n%s\ncalls:\n%s", protocol.String(), calls(t, callsFile))
			}
			if got := strings.TrimSpace(calls(t, callsFile)); got != "go test ./exec/codex ./exec/claude" {
				t.Fatalf("unexpected go test command for inactive file selection:\n%s", got)
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
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "go::pkg::log"); err != nil {
		t.Fatalf("go package run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	if !strings.Contains(calls(t, callsFile), "go test ./log -json") || strings.Contains(calls(t, callsFile), "-run=") {
		t.Fatalf("go package selection used wrong command:\n%s", calls(t, callsFile))
	}
	events := decodeRunEvents(t, protocol.String())
	if !runEventsContain(events, "passed", "go::pkg::log") {
		t.Fatalf("run events missing package pass: %+v", events)
	}

	built14, buildErr14 := buildVSCodeDesiredStateDocument(root, []string{"go::pkg::log", "go::root"})
	if buildErr14 != nil {
		t.Fatalf("buildVSCodeDesiredStateDocument for go package/root: %v", buildErr14)
	}
	var desiredStateOut bytes.Buffer
	if err := testbridge.EncodeDocument(&desiredStateOut, built14); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var desiredState vscode.DesiredStateDocument
	if err := json.Unmarshal(desiredStateOut.Bytes(), &desiredState); err != nil {
		t.Fatalf("desiredState JSON: %v\n%s", err, desiredStateOut.String())
	}
	if desiredState.Version != 3 || !desiredStateHasRunID(desiredState.Groups, vscodeDesiredStateStubBinaries) {
		t.Fatalf("go package/root desired-state document = %+v, want v3 desired-state groups", desiredState)
	}
}

// DHF-TEST: keel/requirement-71
func TestVSCodeRunGoRootSelectionSettlesEveryStartedIDExactlyOnce(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./...")
    printf '{"Action":"run","Package":"github.com/david-aggeler/keel/log","Test":"TestLog"}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Test":"TestLog","Elapsed":0.01}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Elapsed":0.02}\n'
    printf '{"Action":"run","Package":"github.com/david-aggeler/keel/vscode","Test":"TestHelpers"}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/vscode","Test":"TestHelpers","Elapsed":0.01}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/vscode","Elapsed":0.02}\n'
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var protocol bytes.Buffer
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "go::root"); err != nil {
		t.Fatalf("go root run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	events := decodeRunEvents(t, protocol.String())
	started := map[string]int{}
	terminal := map[string]int{}
	for _, event := range events {
		switch event.Event {
		case "test_started":
			started[event.TestID]++
		case "passed", "failed", "skipped", "errored":
			terminal[event.TestID]++
		}
	}
	for id := range started {
		if terminal[id] != 1 {
			t.Fatalf("started id %q has %d terminal events, want exactly 1\nevents: %+v", id, terminal[id], events)
		}
	}
	for _, id := range []string{"go::pkg::log", "go::pkg::vscode", "go::test::log::TestLog", "go::test::vscode::TestHelpers", "go::root"} {
		if terminal[id] != 1 {
			t.Fatalf("id %q has %d terminal events, want exactly 1\nevents: %+v", id, terminal[id], events)
		}
	}
	for id, count := range terminal {
		if count > 1 {
			t.Fatalf("id %q settled %d times, want once\nevents: %+v", id, count, events)
		}
	}
}

// DHF-TEST: keel/requirement-71
func TestVSCodeRunGoSingleTestSelectionSettlesSelectedIDExactlyOnce(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "test ./log")
    printf '{"Action":"run","Package":"github.com/david-aggeler/keel/log","Test":"TestLog"}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Test":"TestLog","Elapsed":0.01}\n'
    printf '{"Action":"pass","Package":"github.com/david-aggeler/keel/log","Elapsed":0.02}\n'
    ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	const selectedID = "go::test::log::TestLog"
	var protocol bytes.Buffer
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), selectedID); err != nil {
		t.Fatalf("go single-test run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	events := decodeRunEvents(t, protocol.String())
	terminal := map[string]int{}
	for _, event := range events {
		switch event.Event {
		case "passed", "failed", "skipped", "errored":
			terminal[event.TestID]++
		}
	}
	if terminal[selectedID] != 1 {
		t.Fatalf("selected id settled %d times, want exactly once\nevents: %+v", terminal[selectedID], events)
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
	seedDetectedLanes(t, root)

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
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), vscodeLaneTestCoverage); err != nil {
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
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), vscodeLaneTestFast); err != nil {
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
	err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "keel::lane::test-fast")
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

// DHF-TEST: keel/requirement-92
func TestVSCodeRunPrunesExternalRunSpoolToRecentCompletedStreams(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	t.Setenv("PATH", t.TempDir())

	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(runDir, "run.lock")
	if err := os.WriteFile(lockPath, []byte(`{"pid":1,"created_at":"2026-07-13T00:00:00Z","ids":["x"],"token":"t"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join(".devtools", "vscode-runs", "in-flight.jsonl"), `{"version":1,"event":"run_started","run_id":"in-flight"}`+"\n")

	for i := 0; i < 37; i++ {
		var protocol bytes.Buffer
		err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "keel::lane::test-fast")
		if err == nil {
			t.Fatal("blocked lane returned nil error; want non-zero")
		}
	}

	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatal(err)
	}
	var completed int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(runDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"event":"run_finished"`) {
			completed++
		}
	}
	if completed > 32 {
		t.Fatalf("completed streams = %d, want at most 32 after retention", completed)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("run.lock was pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "in-flight.jsonl")); err != nil {
		t.Fatalf("in-flight stream was pruned: %v", err)
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

func desiredStateHasRunID(groups []vscode.DesiredStateGroup, runID string) bool {
	for _, group := range groups {
		for _, row := range group.Rows {
			if row.RunID == runID {
				return true
			}
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

func seedDetectedLanes(t *testing.T, root string) {
	t.Helper()
	if err := runVSCodeDetectLanesMaintenance(root, func(vscode.RunEvent) {}); err != nil {
		t.Fatalf("seed detected lanes: %v", err)
	}
}

func lanesDetectAdded(doc lanesDetectDocument, id string) bool {
	for _, entry := range doc.Added {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func lanesDetectRemoved(doc lanesDetectDocument, id string) bool {
	for _, entry := range doc.Removed {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func lanesDetectChanged(doc lanesDetectDocument, id string) bool {
	for _, entry := range doc.Changed {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func commandSpecHasName(spec *cli.CommandSpec, name string) bool {
	if spec.Name == name {
		return true
	}
	for _, child := range spec.Subcommands {
		if commandSpecHasName(child, name) {
			return true
		}
	}
	return false
}

func commandSpecByPath(spec *cli.CommandSpec, path ...string) *cli.CommandSpec {
	if len(path) == 0 {
		return spec
	}
	for _, child := range spec.Subcommands {
		if child.Name == path[0] {
			return commandSpecByPath(child, path[1:]...)
		}
	}
	return nil
}

func commandLeafUses(spec *cli.CommandSpec) []string {
	var out []string
	var walk func(*cli.CommandSpec)
	walk = func(node *cli.CommandSpec) {
		if len(node.Subcommands) == 0 {
			out = append(out, node.Use)
			return
		}
		for _, child := range node.Subcommands {
			walk(child)
		}
	}
	walk(spec)
	return out
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

// DHF-TEST: keel/requirement-90
func TestVSCodeVSIXGateReadinessRequiresCompleteToolchain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")

	for _, tc := range []struct {
		name        string
		present     []string
		wantBlocked string
	}{
		{
			name:        "xvfb-run absent",
			present:     []string{"go", "pnpm", "node"},
			wantBlocked: "xvfb-run",
		},
		{
			name:        "node absent",
			present:     []string{"go", "pnpm", "xvfb-run"},
			wantBlocked: "node",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			callsFile := filepath.Join(bin, "calls.log")
			for _, tool := range tc.present {
				body := "exit 0"
				if tool == "pnpm" {
					body = "printf 'unexpected pnpm invocation\\n' >&2\nexit 2"
				}
				stub(t, bin, callsFile, tool, body)
			}
			t.Setenv("PATH", bin)

			profile := newKeelWorkspaceProfile(root)
			readiness := profile.PrepareLane(context.Background(), vscodeLaneVSIXGate)
			if readiness.Ready() || len(readiness.Blocked) != 1 || readiness.Blocked[0].Resource != tc.wantBlocked {
				t.Fatalf("PrepareLane blocked = %+v, want exactly %q", readiness.Blocked, tc.wantBlocked)
			}

			var protocol bytes.Buffer
			err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), vscodeLaneVSIXGate)
			if err == nil {
				t.Fatalf("vsix-ci without %s returned nil error; want structured block", tc.wantBlocked)
			}
			events := decodeRunEvents(t, protocol.String())
			if !runEventsContain(events, "failed", vscodeLaneVSIXGate) || !strings.Contains(protocol.String(), tc.wantBlocked) {
				t.Fatalf("blocked events = %+v, protocol=%s; want failed event naming %s", events, protocol.String(), tc.wantBlocked)
			}
			if strings.Contains(calls(t, callsFile), "pnpm ") {
				t.Fatalf("vsix-ci should not start gate work when %s is absent; calls:\n%s", tc.wantBlocked, calls(t, callsFile))
			}
		})
	}

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	for _, tool := range []string{"go", "pnpm", "node", "xvfb-run"} {
		stub(t, bin, callsFile, tool, "exit 0")
	}
	t.Setenv("PATH", bin)
	if readiness := newKeelWorkspaceProfile(root).PrepareLane(context.Background(), vscodeLaneVSIXGate); !readiness.Ready() {
		t.Fatalf("PrepareLane with full vsix-ci toolchain = %+v, want ready", readiness)
	}

	seedDetectedLanes(t, root)
	built, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", err)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	item, ok := discoveryItemByID(doc, vscodeLaneVSIXGate)
	if !ok {
		t.Fatalf("discovery missing %q", vscodeLaneVSIXGate)
	}
	wantResources := []string{"go-toolchain", "keel-module-root", "pnpm", "node", "xvfb-run"}
	if !stringSlicesEqual(item.RequiredResources, wantResources) {
		t.Fatalf("vsix-ci required resources = %+v, want %+v", item.RequiredResources, wantResources)
	}
}

// DHF-TEST: keel/requirement-81
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
	if got := profile.MaxOutputBytes(); got != procexec.DefaultMaxOutputBytes {
		t.Fatalf("profile MaxOutputBytes = %d, want shared default %d", got, procexec.DefaultMaxOutputBytes)
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
		if code := run([]string{"--no-header", "-v", "test-bridge", "tests", "run", "--id", "keel::lane::test-fast"}); code == 0 {
			t.Fatal("blocked test-bridge run exit = 0, want non-zero")
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
	err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "keel::lane::test-fast")
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
	err = dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), "keel::lane::test-fast")
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

// DHF-TEST: keel/requirement-94
func TestVSCodeDiscoveryEmitsVSIXTestItemsFromStaticScan(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, dir := range []string{".vscode", filepath.Join("vsix", "src", "test", "suite")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, filepath.Join("vsix", "src", "test", "suite", "extension.test.ts"), "suite('Keel Test Bridge', () => {\n"+
		"  test('first static case', () => {});\n"+
		"  test(\"second static case\", async function () {});\n"+
		"  test(`dynamic ${'x'} title`, () => {});\n"+
		"  test('duplicated title', () => {});\n"+
		"  test('duplicated title', () => {});\n"+
		"});\n")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[]}`+"\n")

	built, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", err)
	}
	var discover bytes.Buffer
	if err := testbridge.EncodeDocument(&discover, built); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}

	const fileID = "vsix::file::src/test/suite/extension.test.ts"
	for title, slug := range map[string]string{"first static case": "first-static-case", "second static case": "second-static-case"} {
		item, ok := discoveryItemByID(doc, "vsix::test::src/test/suite/extension.test.ts::"+slug)
		if !ok {
			t.Fatalf("discovery missing vsix test item for %q: %+v", title, doc.Items)
		}
		if item.ParentID != fileID || item.Kind != "test" || item.Runnable || item.Label != title {
			t.Fatalf("vsix test item shape = %+v, want kind=test parent=%s runnable=false label=verbatim title", item, fileID)
		}
	}
	var testItems []string
	for _, item := range doc.Items {
		if strings.HasPrefix(item.ID, "vsix::test::") {
			testItems = append(testItems, item.ID)
		}
	}
	if len(testItems) != 2 {
		t.Fatalf("vsix test items = %v, want exactly the two statically-resolvable unique titles (dynamic + duplicated omitted, fail-closed)", testItems)
	}
}

// DHF-TEST: keel/requirement-94
func TestVSCodeEmitMochaJSONLEventsKeysPerTestIDs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "vsix", "src", "test", "suite"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("vsix", "src", "test", "suite", "extension.test.ts"), "suite('s', () => {\n  test('alpha case', () => {});\n  test('beta case', () => {});\n});\n")

	index := buildVSIXTestIndex(root)
	compiled := filepath.Join(root, "vsix", "out", "test", "suite", "extension.test.js")
	lines := []string{
		`{"version":1,"event":"run_started","time":"t"}`,
		`{"version":1,"event":"test_started","time":"t","title":"alpha case","file":"` + filepath.ToSlash(compiled) + `"}`,
		`{"version":1,"event":"passed","time":"t","title":"alpha case","file":"` + filepath.ToSlash(compiled) + `","duration_ms":7}`,
		`{"version":1,"event":"test_started","time":"t","title":"beta case"}`,
		`{"version":1,"event":"failed","time":"t","title":"beta case","message":"boom"}`,
		`{"version":1,"event":"passed","time":"t","title":"ghost case"}`,
		`not-json`,
		`{"version":1,"event":"run_finished","time":"t","passes":2,"failures":1}`,
	}
	var events []vscode.RunEvent
	emitMochaJSONLEvents([]byte(strings.Join(lines, "\n")+"\n"), index, func(event vscode.RunEvent) {
		events = append(events, event)
	})

	const alphaID = "vsix::test::src/test/suite/extension.test.ts::alpha-case"
	const betaID = "vsix::test::src/test/suite/extension.test.ts::beta-case"
	if !runEventsContain(events, "test_started", alphaID) || !runEventsContain(events, "passed", alphaID) {
		t.Fatalf("alpha events missing (file-mapped resolution): %+v", events)
	}
	if !runEventsContain(events, "test_started", betaID) || !runEventsContain(events, "failed", betaID) {
		t.Fatalf("beta events missing (unique-title fallback resolution): %+v", events)
	}
	for _, event := range events {
		if event.Event == "run_started" || event.Event == "run_finished" {
			t.Fatalf("reporter envelope must not be forwarded: %+v", event)
		}
		if strings.Contains(event.TestID, "ghost") {
			t.Fatalf("unresolvable title must be dropped fail-closed: %+v", event)
		}
		if event.Event == "passed" && event.TestID == alphaID && event.DurationMS != 7 {
			t.Fatalf("alpha passed duration = %d, want 7", event.DurationMS)
		}
		if event.Event == "failed" && event.TestID == betaID && event.Message != "boom" {
			t.Fatalf("beta failed message = %q, want boom", event.Message)
		}
	}
	if len(events) != 4 {
		t.Fatalf("events = %+v, want exactly alpha started/passed + beta started/failed", events)
	}
}

// DHF-TEST: keel/requirement-94
func TestVSCodeRunDirectVSIXSelectionEmitsPerTestEventsFromMochaJSONL(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, dir := range []string{".vscode", filepath.Join("vsix", "src", "test", "suite")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, filepath.Join("vsix", "src", "test", "suite", "tree.test.ts"), "suite('s', () => {\n  test('alpha case', () => {});\n  test('beta case', () => {});\n});\n")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{"version":1,"lanes":[]}`+"\n")

	// A stale results file from a previous run must not leak into this run's
	// stream: the stub below rewrites it, and the runner clears it up front.
	if err := os.MkdirAll(filepath.Join(root, "vsix", ".vscode-test"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("vsix", ".vscode-test", "results.jsonl"), `{"version":1,"event":"passed","time":"t","title":"stale case"}`+"\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", "exit 0")
	stub(t, bin, callsFile, "pnpm", `
mkdir -p vsix/.vscode-test
printf '%s\n' '{"version":1,"event":"test_started","time":"t","title":"alpha case"}' > vsix/.vscode-test/results.jsonl
printf '%s\n' '{"version":1,"event":"passed","time":"t","title":"alpha case","duration_ms":5}' >> vsix/.vscode-test/results.jsonl
printf '%s\n' '{"version":1,"event":"passed","time":"t","title":"beta case"}' >> vsix/.vscode-test/results.jsonl
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	const selectedID = "vsix::file::src/test/suite/tree.test.ts"
	var protocol bytes.Buffer
	if err := dispatchTestBridgeRun(contextWithVSCodeTestState(root, &protocol), selectedID); err != nil {
		t.Fatalf("vsix selection run: %v\nprotocol:\n%s\ncalls:\n%s", err, protocol.String(), calls(t, callsFile))
	}
	events := decodeRunEvents(t, protocol.String())
	const alphaID = "vsix::test::src/test/suite/tree.test.ts::alpha-case"
	const betaID = "vsix::test::src/test/suite/tree.test.ts::beta-case"
	if !runEventsContain(events, "passed", alphaID) || !runEventsContain(events, "passed", betaID) {
		t.Fatalf("per-test passed events missing: %+v", events)
	}
	if !runEventsContain(events, "passed", selectedID) {
		t.Fatalf("selected file id must still settle: %+v", events)
	}
	for _, event := range events {
		if strings.Contains(event.TestID, "stale") {
			t.Fatalf("stale results.jsonl content leaked into the run stream: %+v", events)
		}
	}
}
