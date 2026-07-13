package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// DHF-TEST: keel/requirement-62
func TestKeelDemoDevServesReferenceConsumerTestBridge(t *testing.T) {
	exe := buildDemoDev(t)
	root := t.TempDir()

	discoveryOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "discover", "--format", "json")
	if code != 0 {
		t.Fatalf("discover exit = %d, want 0\n%s", code, discoveryOut)
	}
	var discovery vscode.DiscoveryDocument
	decodeJSON(t, discoveryOut, &discovery)
	assertItem(t, discovery.Items, "keel-demo-dev::maintenance", "group", false)
	assertItem(t, discovery.Items, "keel-demo-dev::lanes", "group", false)
	assertItem(t, discovery.Items, "keel-demo-dev::frameworks", "group", false)
	assertItem(t, discovery.Items, "keel-demo-dev::lane::go-pass", "lane", true)
	assertItem(t, discovery.Items, "keel-demo-dev::lane::go-fail", "lane", true)
	assertItem(t, discovery.Items, "keel-demo-dev::lane::fake-smoke", "lane", true)
	assertItem(t, discovery.Items, "keel-demo-dev::maintenance::block-bad-lane", "maintenance", true)
	assertItem(t, discovery.Items, "keel-demo-dev::maintenance::unblock-bad-lane", "maintenance", true)
	assertItem(t, discovery.Items, "go::test::passing::TestReferencePass", "test", true)
	assertItem(t, discovery.Items, "go::test::failing::TestReferenceFailure", "test", true)

	planOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "desired-state", "--format", "json", "--id", "keel-demo-dev::lane::fake-smoke")
	if code != 0 {
		t.Fatalf("desired-state exit = %d, want 0\n%s", code, planOut)
	}
	var plan vscode.SetupPlan
	decodeJSON(t, planOut, &plan)
	assertDesiredState(t, plan.DesiredState, "environment", "ready", "absent", "provision_demo_environment")
	assertDesiredState(t, plan.DesiredState, "database", "present+seeded", "missing", "create_and_seed_demo_database")
	assertDesiredState(t, plan.DesiredState, "service-a", "running", "stopped", "start_demo_service")
	assertDesiredState(t, plan.DesiredState, "service-b", "running", "stopped", "start_demo_service")
	assertDesiredState(t, plan.DesiredState, "service-c", "running", "stopped", "start_demo_service")

	failOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", "keel-demo-dev::lane::go-fail")
	if code == 0 {
		t.Fatalf("go-fail lane exit = 0, want non-zero\n%s", failOut)
	}
	events := decodeRunEvents(t, failOut)
	assertRunEvent(t, events, "failed", "keel-demo-dev::lane::go-fail", "real Go test failed")
	assertRunFinished(t, events, 1)

	blockOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", "keel-demo-dev::maintenance::block-bad-lane")
	if code != 0 {
		t.Fatalf("block maintenance exit = %d, want 0\n%s", code, blockOut)
	}
	blockedOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", "keel-demo-dev::lane::go-fail")
	if code == 0 {
		t.Fatalf("blocked go-fail lane exit = 0, want non-zero\n%s", blockedOut)
	}
	events = decodeRunEvents(t, blockedOut)
	assertRunEvent(t, events, "failed", "keel-demo-dev::lane::go-fail", "lane blocked")

	unblockOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", "keel-demo-dev::maintenance::unblock-bad-lane")
	if code != 0 {
		t.Fatalf("unblock maintenance exit = %d, want 0\n%s", code, unblockOut)
	}

	configOut, code := runDemoDev(t, root, exe, "test-bridge", "config", "init")
	if code != 0 {
		t.Fatalf("config init exit = %d, want 0\n%s", code, configOut)
	}
	cfgData, err := os.ReadFile(filepath.Join(root, ".vscode", "test-bridge.json"))
	if err != nil {
		t.Fatalf("read initialized config: %v", err)
	}
	if !strings.Contains(string(cfgData), "keel-demo-dev") {
		t.Fatalf("initialized config does not point at keel-demo-dev:\n%s", cfgData)
	}
}

// DHF-TEST: keel/requirement-62
func TestKeelDemoDevUnblockMaintenanceIsIdempotent(t *testing.T) {
	exe := buildDemoDev(t)
	root := t.TempDir()

	unblockOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", "keel-demo-dev::maintenance::unblock-bad-lane")
	if code != 0 {
		t.Fatalf("clean-workspace unblock maintenance exit = %d, want 0\n%s", code, unblockOut)
	}
	events := decodeRunEvents(t, unblockOut)
	assertRunEvent(t, events, "passed", "keel-demo-dev::maintenance::unblock-bad-lane", "unblocked demo lanes")
	assertRunFinished(t, events, 0)
}

// DHF-TEST: keel/requirement-62
func TestDemoBridgeCommandSpecCoversProviderAndRunPaths(t *testing.T) {
	root := t.TempDir()

	discoveryOut, err := dispatchDemoBridge(t, root, "test-bridge", "tests", "discover", "--format", "json")
	if err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var discovery vscode.DiscoveryDocument
	decodeJSON(t, discoveryOut, &discovery)
	assertItem(t, discovery.Items, idLaneFakeSmoke, "lane", true)

	planOut, err := dispatchDemoBridge(t, root, "test-bridge", "tests", "desired-state", "--format", "json", "--id", idLaneFakeSmoke)
	if err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	var plan vscode.SetupPlan
	decodeJSON(t, planOut, &plan)
	assertDesiredState(t, plan.DesiredState, "database", "present+seeded", "missing", "create_and_seed_demo_database")

	defaultPlanOut, err := dispatchDemoBridge(t, root, "test-bridge", "tests", "desired-state", "--format", "json")
	if err != nil {
		t.Fatalf("default desired-state dispatch: %v", err)
	}
	var defaultPlan vscode.SetupPlan
	decodeJSON(t, defaultPlanOut, &defaultPlan)
	for _, want := range []string{idLaneFakeSmoke, idLaneGoPass, idLaneGoFail} {
		if !setupPlanHasItem(defaultPlan.Items, want) {
			t.Fatalf("default desired-state items missing %s: %+v", want, defaultPlan.Items)
		}
	}

	runOut, err := dispatchDemoBridge(t, root, "test-bridge", "tests", "run", "--id", idLaneFakeSmoke)
	if err != nil {
		t.Fatalf("fake smoke run dispatch: %v", err)
	}
	assertRunFinished(t, decodeRunEvents(t, runOut), 0)

	runOut, err = dispatchDemoBridge(t, root, "test-bridge", "tests", "run", "--id", idLaneGoPass)
	if err != nil {
		t.Fatalf("go pass run dispatch: %v", err)
	}
	assertRunEvent(t, decodeRunEvents(t, runOut), "passed", idLaneGoPass, "real Go test passed")

	if _, err = dispatchDemoBridge(t, root, "test-bridge", "tests", "run", "--id", idBlockBadLane); err != nil {
		t.Fatalf("block maintenance dispatch: %v", err)
	}
	runOut, err = dispatchDemoBridge(t, root, "test-bridge", "tests", "run", "--id", idLaneGoFail)
	if err == nil {
		t.Fatalf("blocked failing lane dispatch succeeded, want RunError")
	}
	assertRunEvent(t, decodeRunEvents(t, runOut), "failed", idLaneGoFail, "lane blocked")

	if _, err = dispatchDemoBridge(t, root, "test-bridge", "tests", "run", "--id", idUnblockBadLane); err != nil {
		t.Fatalf("unblock maintenance dispatch: %v", err)
	}
	runOut, err = dispatchDemoBridge(t, root, "test-bridge", "tests", "run", "--id", idLaneGoFail)
	if err == nil {
		t.Fatalf("failing lane dispatch succeeded, want RunError")
	}
	assertRunEvent(t, decodeRunEvents(t, runOut), "failed", idLaneGoFail, "real Go test failed")
}

func TestRunEntrypointRoutesProtocolHelpVersionAndErrors(t *testing.T) {
	root := t.TempDir()

	stdout, stderr, code := captureRun(t, root, "test-bridge", "tests", "discover", "--format", "json")
	if code != 0 {
		t.Fatalf("discover exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	var discovery vscode.DiscoveryDocument
	decodeJSON(t, stdout, &discovery)
	assertItem(t, discovery.Items, idMaintenance, "group", false)

	stdout, _, code = captureRun(t, root, "--version")
	if code != 0 || strings.TrimSpace(stdout) != demoVersion {
		t.Fatalf("--version = code %d stdout %q, want 0 %q", code, stdout, demoVersion)
	}

	_, stderr, code = captureRun(t, root, "--help-all")
	if code != 0 || !strings.Contains(stderr, "test-bridge") {
		t.Fatalf("--help-all = code %d stderr %q, want command help", code, stderr)
	}

	_, stderr, code = captureRun(t, root, "help", "test-bridge")
	if code != 0 || !strings.Contains(stderr, "test-bridge") {
		t.Fatalf("help test-bridge = code %d stderr %q, want topic help", code, stderr)
	}

	_, stderr, code = captureRun(t, root, "--bad-flag")
	if code != 2 || !strings.Contains(stderr, "unknown") {
		t.Fatalf("bad flag = code %d stderr %q, want usage error", code, stderr)
	}

	_, stderr, code = captureRun(t, root, "test-bridge", "tests", "run", "--id", "keel-demo-dev::lane::missing")
	if code != 1 || !strings.Contains(stderr, "unknown demo test id") {
		t.Fatalf("unknown run id = code %d stderr %q, want runtime error", code, stderr)
	}

	cfg := (demoBridge{}).ConfigTemplate()
	if cfg.Command != filepath.Join("bin", executableName()) || cfg.DisplayName != "Keel Demo Dev" {
		t.Fatalf("config template = %+v", cfg)
	}
}

func setupPlanHasItem(items []vscode.SetupPlanItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func buildDemoDev(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "keel-demo-dev")
	cmd := exec.Command("go", "build", "-o", exe, ".")
	cmd.Dir = "."
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out.String())
	}
	return exe
}

func runDemoDev(t *testing.T, root, exe string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err == nil {
		return out.String(), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return out.String(), exitErr.ExitCode()
	}
	t.Fatalf("keel-demo-dev failed before process exit: %v\n%s", err, out.String())
	return "", -1
}

func dispatchDemoBridge(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		Now:      func() time.Time { return time.Date(2026, 7, 13, 11, 0, 0, 0, time.UTC) },
		RunID:    func() string { return "run-test" },
	})
	err := testbridge.CommandSpec(demoBridge{}).Dispatch(ctx, args)
	return protocol.String(), err
}

func captureRun(t *testing.T, root string, args ...string) (string, string, int) {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	os.Stdout, os.Stderr = stdoutW, stderrW
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	code := run(args)
	_ = os.Chdir(oldWD)
	os.Stdout, os.Stderr = oldStdout, oldStderr
	_ = stdoutW.Close()
	_ = stderrW.Close()
	var stdout, stderr bytes.Buffer
	if _, err := stdout.ReadFrom(stdoutR); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if _, err := stderr.ReadFrom(stderrR); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	_ = stdoutR.Close()
	_ = stderrR.Close()
	return stdout.String(), stderr.String(), code
}

func decodeJSON(t *testing.T, raw string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, raw)
	}
}

func assertItem(t *testing.T, items []vscode.TestItem, id, kind string, runnable bool) {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			if item.Kind != kind || item.Runnable != runnable {
				t.Fatalf("item %s = kind %q runnable %v, want %q %v", id, item.Kind, item.Runnable, kind, runnable)
			}
			return
		}
	}
	t.Fatalf("missing discovery item %s in %+v", id, items)
}

func assertDesiredState(t *testing.T, rows []vscode.DesiredState, resource, desired, current, action string) {
	t.Helper()
	for _, row := range rows {
		if row.Resource == resource {
			if row.Desired != desired || row.Current != current || row.Action != "reconcile_during_run" || row.Desired == row.Current || !strings.Contains(row.Message, action) {
				t.Fatalf("desired row %s = %+v, want desired %q current %q reconcile_during_run message containing %q with desired != current", resource, row, desired, current, action)
			}
			return
		}
	}
	t.Fatalf("missing desired-state row %s in %+v", resource, rows)
}

func decodeRunEvents(t *testing.T, raw string) []vscode.RunEvent {
	t.Helper()
	var events []vscode.RunEvent
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			continue
		}
		var event vscode.RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode run event: %v\nline:%s\noutput:\n%s", err, line, raw)
		}
		events = append(events, event)
	}
	return events
}

func assertRunEvent(t *testing.T, events []vscode.RunEvent, event, testID, message string) {
	t.Helper()
	for _, got := range events {
		if got.Event == event && got.TestID == testID && strings.Contains(got.Message, message) {
			return
		}
	}
	t.Fatalf("missing event=%s testID=%s message containing %q in %+v", event, testID, message, events)
}

func assertRunFinished(t *testing.T, events []vscode.RunEvent, exitCode int) {
	t.Helper()
	for _, event := range events {
		if event.Event == "run_finished" && event.ExitCode != nil && *event.ExitCode == exitCode {
			return
		}
	}
	t.Fatalf("missing run_finished exit_code=%d in %+v", exitCode, events)
}
