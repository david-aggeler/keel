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

// DHF-TEST: keel/requirement-62, keel/requirement-74, keel/requirement-75, keel/requirement-76, keel/requirement-83, keel/requirement-87
func TestKeelDemoDevServesReferenceConsumerTestBridge(t *testing.T) {
	exe := buildDemoDev(t)
	root := t.TempDir()

	discoveryOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "discover", "--format", "json")
	if code != 0 {
		t.Fatalf("discover exit = %d, want 0\n%s", code, discoveryOut)
	}
	var discovery vscode.DiscoveryDocument
	decodeJSON(t, discoveryOut, &discovery)
	assertItem(t, discovery.Items, testbridge.MaintenanceGroupID, "group", false)
	assertItem(t, discovery.Items, "keel::desired-state", "group", false)
	assertItem(t, discovery.Items, "keel-demo-dev::lanes", "group", false)
	assertItem(t, discovery.Items, "keel-demo-dev::frameworks", "group", false)
	assertItem(t, discovery.Items, testbridge.MaintenanceDetectLanesID, "maintenance", true)
	clearStateItem := assertItem(t, discovery.Items, testbridge.MaintenanceClearStateID, "maintenance", true)
	if !strings.Contains(clearStateItem.Label, "clear local test state") {
		t.Fatalf("clear-state item label = %q, want clear local test state", clearStateItem.Label)
	}
	assertItem(t, discovery.Items, "keel-demo-dev::maintenance::block-bad-lane", "maintenance", true)
	assertItem(t, discovery.Items, "keel-demo-dev::maintenance::unblock-bad-lane", "maintenance", true)
	if got, want := discovery.Capabilities.ClearStateTestIDs, []string{testbridge.MaintenanceClearStateID}; !stringSlicesEqual(got, want) {
		t.Fatalf("clear_state_test_ids = %v, want %v", got, want)
	}
	if stringSlicesContain(discovery.Capabilities.ClearStateTestIDs, idUnblockBadLane) {
		t.Fatalf("clear_state_test_ids aliases unblock-bad-lane: %v", discovery.Capabilities.ClearStateTestIDs)
	}
	assertMissingItem(t, discovery.Items, "keel::desired-state::group::test-preconditions")
	assertMissingItem(t, discovery.Items, "keel::desired-state::group::app-db-data-set")
	for _, id := range []string{"keel-demo-dev::desired-state::docker-env", "keel-demo-dev::desired-state::dataset::small"} {
		assertMissingItem(t, discovery.Items, id)
	}
	assertMissingItem(t, discovery.Items, "keel-demo-dev::lane::go-pass")

	detectOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", idDetectLanes)
	if code != 0 {
		t.Fatalf("detect-lanes maintenance exit = %d, want 0\n%s", code, detectOut)
	}
	assertRunEvent(t, decodeRunEvents(t, detectOut), "passed", idDetectLanes, "wrote .vscode/test-lanes.json")
	assertDemoLanesFile(t, root)
	discoveryOut, code = runDemoDev(t, root, exe, "test-bridge", "tests", "discover", "--format", "json")
	if code != 0 {
		t.Fatalf("post-detect discover exit = %d, want 0\n%s", code, discoveryOut)
	}
	decodeJSON(t, discoveryOut, &discovery)
	assertItem(t, discovery.Items, "keel-demo-dev::lane::go-pass", "lane", true)
	assertItem(t, discovery.Items, "keel-demo-dev::lane::go-fail", "lane", true)
	assertItem(t, discovery.Items, "keel-demo-dev::lane::fake-smoke", "lane", true)
	assertItem(t, discovery.Items, "go::test::passing::TestReferencePass", "test", true)
	assertItem(t, discovery.Items, "go::test::failing::TestReferenceFailure", "test", true)
	assertItem(t, discovery.Items, "keel::desired-state::group::test-preconditions", "group", true)
	dataSetGroup := assertItem(t, discovery.Items, "keel::desired-state::group::app-db-data-set", "group", false)
	if dataSetGroup.SortText != "b.020" || !strings.Contains(strings.Join(dataSetGroup.Limitations, " "), "mutually_exclusive=true") {
		t.Fatalf("data-set discovery group = %+v, want order and exclusivity surfaced", dataSetGroup)
	}
	for _, id := range []string{"keel-demo-dev::desired-state::docker-env", "keel-demo-dev::desired-state::dataset::small"} {
		item := assertItem(t, discovery.Items, id, "group", true)
		if item.ParentID != "keel::desired-state::group::test-preconditions" && item.ParentID != "keel::desired-state::group::app-db-data-set" {
			t.Fatalf("desired-state row %s parent = %q, want derived desired-state group", id, item.ParentID)
		}
	}

	desiredStateOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "desired-state", "--format", "json", "--id", "keel-demo-dev::lane::fake-smoke")
	if code != 0 {
		t.Fatalf("desired-state exit = %d, want 0\n%s", code, desiredStateOut)
	}
	var desiredState vscode.DesiredStateDocument
	decodeJSON(t, desiredStateOut, &desiredState)
	assertDesiredState(t, desiredState.Groups, "docker-env", "ready", "ready", "verified")
	assertDesiredState(t, desiredState.Groups, "postgres", "present+seeded", "present+seeded", "verified")
	assertDesiredState(t, desiredState.Groups, "service-a", "running", "running", "verified")
	assertDesiredState(t, desiredState.Groups, "service-b", "running", "running", "verified")
	assertDesiredState(t, desiredState.Groups, "service-c", "running", "running", "verified")
	assertExclusiveDataSetGroup(t, desiredState.Groups)

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

	clearStateOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", testbridge.MaintenanceClearStateID)
	if code != 0 {
		t.Fatalf("clear-state maintenance exit = %d, want 0\n%s", code, clearStateOut)
	}
	clearStateEvents := decodeRunEvents(t, clearStateOut)
	assertRunEvent(t, clearStateEvents, "passed", testbridge.MaintenanceClearStateID, "")
	assertRunFinished(t, clearStateEvents, 0)
	if blocked, err := blockedLane(root); err != nil || blocked != "" {
		t.Fatalf("clear-state blocked lane = %q err=%v, want empty", blocked, err)
	}
	unblockedFailOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", "keel-demo-dev::lane::go-fail")
	if code == 0 {
		t.Fatalf("post-clear go-fail lane exit = 0, want non-zero\n%s", unblockedFailOut)
	}
	assertRunEvent(t, decodeRunEvents(t, unblockedFailOut), "failed", "keel-demo-dev::lane::go-fail", "real Go test failed")

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

// DHF-TEST: keel/requirement-62, keel/requirement-75, keel/requirement-76, keel/requirement-84
func TestKeelDemoDevDesiredStateRowsAreRunnable(t *testing.T) {
	exe := buildDemoDev(t)
	root := t.TempDir()
	detectOut, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", idDetectLanes)
	if code != 0 {
		t.Fatalf("detect-lanes maintenance exit = %d, want 0\n%s", code, detectOut)
	}

	cases := []struct {
		id      string
		code    int
		event   string
		message string
	}{
		{id: "keel-demo-dev::desired-state::docker-env", code: 0, event: "passed", message: "provision_demo_environment"},
		{id: "keel-demo-dev::desired-state::dataset::small", code: 0, event: "passed", message: "reuse_small_data_set"},
	}
	for _, tc := range cases {
		out, code := runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", tc.id)
		if code != tc.code {
			t.Fatalf("desired-state row %s exit = %d, want %d\n%s", tc.id, code, tc.code, out)
		}
		assertRunEvent(t, decodeRunEvents(t, out), tc.event, tc.id, tc.message)
	}

	out, code := runDemoDev(t, root, exe,
		"test-bridge", "tests", "run",
		"--id", "keel-demo-dev::desired-state::docker-env",
		"--id", "keel-demo-dev::desired-state::dataset::small",
	)
	if code != 0 {
		t.Fatalf("multi desired-state row exit = %d, want 0\n%s", code, out)
	}
	events := decodeRunEvents(t, out)
	assertRunEvent(t, events, "passed", "keel-demo-dev::desired-state::docker-env", "provision_demo_environment")
	assertRunEvent(t, events, "passed", "keel-demo-dev::desired-state::dataset::small", "reuse_small_data_set")

	out, code = runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", "keel::desired-state::group::test-preconditions")
	if code != 0 {
		t.Fatalf("desired-state group exit = %d, want 0\n%s", code, out)
	}
	events = decodeRunEvents(t, out)
	assertRunEvent(t, events, "passed", "keel-demo-dev::desired-state::docker-env", "provision_demo_environment")

	if err := os.Remove(demoReadyPath(root, "docker-env")); err != nil {
		t.Fatal(err)
	}
	out, code = runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", "keel-demo-dev::desired-state::docker-env")
	if code == 0 {
		t.Fatalf("broken docker-env row exit = 0, want non-zero\n%s", out)
	}
	assertRunEvent(t, decodeRunEvents(t, out), "failed", "keel-demo-dev::desired-state::docker-env", "provision_demo_environment")

	detectOut, code = runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", idDetectLanes)
	if code != 0 {
		t.Fatalf("repair detect-lanes exit = %d, want 0\n%s", code, detectOut)
	}
	out, code = runDemoDev(t, root, exe, "test-bridge", "tests", "run", "--id", "keel-demo-dev::desired-state::docker-env")
	if code != 0 {
		t.Fatalf("repaired docker-env row exit = %d, want 0\n%s", code, out)
	}
	assertRunEvent(t, decodeRunEvents(t, out), "passed", "keel-demo-dev::desired-state::docker-env", "provision_demo_environment")
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
	assertMissingItem(t, discovery.Items, idLaneFakeSmoke)

	if _, err := dispatchDemoBridge(t, root, "test-bridge", "tests", "run", "--id", idDetectLanes); err != nil {
		t.Fatalf("detect-lanes dispatch: %v", err)
	}
	discoveryOut, err = dispatchDemoBridge(t, root, "test-bridge", "tests", "discover", "--format", "json")
	if err != nil {
		t.Fatalf("post-detect discover dispatch: %v", err)
	}
	decodeJSON(t, discoveryOut, &discovery)
	assertItem(t, discovery.Items, idLaneFakeSmoke, "lane", true)

	desiredStateOut, err := dispatchDemoBridge(t, root, "test-bridge", "tests", "desired-state", "--format", "json", "--id", idLaneFakeSmoke)
	if err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	var desiredState vscode.DesiredStateDocument
	decodeJSON(t, desiredStateOut, &desiredState)
	assertDesiredState(t, desiredState.Groups, "postgres", "present+seeded", "present+seeded", "verified")

	defaultDesiredStateOut, err := dispatchDemoBridge(t, root, "test-bridge", "tests", "desired-state", "--format", "json")
	if err != nil {
		t.Fatalf("default desired-state dispatch: %v", err)
	}
	var defaultDesiredState vscode.DesiredStateDocument
	decodeJSON(t, defaultDesiredStateOut, &defaultDesiredState)
	if defaultDesiredState.Version != 3 {
		t.Fatalf("default desired-state version = %d, want 3", defaultDesiredState.Version)
	}
	assertDesiredState(t, defaultDesiredState.Groups, "docker-env", "ready", "ready", "verified")
	assertExclusiveDataSetGroup(t, defaultDesiredState.Groups)

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

func assertItem(t *testing.T, items []vscode.TestItem, id, kind string, runnable bool) vscode.TestItem {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			if item.Kind != kind || item.Runnable != runnable {
				t.Fatalf("item %s = kind %q runnable %v, want %q %v", id, item.Kind, item.Runnable, kind, runnable)
			}
			return item
		}
	}
	t.Fatalf("missing discovery item %s in %+v", id, items)
	return vscode.TestItem{}
}

func assertMissingItem(t *testing.T, items []vscode.TestItem, id string) {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			t.Fatalf("discovery item %s present before detect-lanes: %+v", id, items)
		}
	}
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

func stringSlicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertDesiredState(t *testing.T, groups []vscode.DesiredStateGroup, resource, desired, current, action string) {
	t.Helper()
	for _, group := range groups {
		for _, row := range group.Rows {
			if row.Resource == resource {
				wantAction := "reconcile_during_run"
				if current == desired {
					wantAction = "reuse"
				}
				if row.Desired != desired || row.Current != current || row.Action != wantAction || !strings.Contains(row.Message, action) {
					t.Fatalf("desired row %s = %+v, want desired %q current %q action %q message containing %q", resource, row, desired, current, wantAction, action)
				}
				return
			}
		}
	}
	t.Fatalf("missing desired-state row %s in %+v", resource, groups)
}

func assertExclusiveDataSetGroup(t *testing.T, groups []vscode.DesiredStateGroup) {
	t.Helper()
	for _, group := range groups {
		if group.Label != "app-db data set" {
			continue
		}
		if !group.MutuallyExclusive {
			t.Fatalf("data-set group is not mutually exclusive: %+v", group)
		}
		active := 0
		want := map[string]bool{"app-db-empty": false, "app-db-small": false, "app-db-full": false}
		for _, row := range group.Rows {
			if _, ok := want[row.Resource]; ok {
				want[row.Resource] = true
			}
			if row.Active {
				active++
			}
			wantAction := "reconcile_during_run"
			if row.Resource == "app-db-small" {
				wantAction = "reuse"
			}
			if row.RunID == "" || row.Action != wantAction {
				t.Fatalf("data-set row = %+v, want runnable %s row", row, wantAction)
			}
		}
		if active != 1 {
			t.Fatalf("data-set group active rows = %d, want exactly 1: %+v", active, group.Rows)
		}
		for resource, seen := range want {
			if !seen {
				t.Fatalf("data-set group missing %s: %+v", resource, group.Rows)
			}
		}
		return
	}
	t.Fatalf("missing app-db data set group in %+v", groups)
}

func assertDemoLanesFile(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".vscode", "test-lanes.json"))
	if err != nil {
		t.Fatalf("read demo lanes file: %v", err)
	}
	text := string(data)
	for _, want := range []string{"keel-demo-dev::lane::go-pass", "keel-demo-dev::lane::go-fail", "keel-demo-dev::lane::fake-smoke"} {
		if !strings.Contains(text, want) {
			t.Fatalf("test-lanes.json missing %s:\n%s", want, text)
		}
	}
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
