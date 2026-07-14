package testbridge_test

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

	"github.com/david-aggeler/keel/cli"
	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// DHF-TEST: keel/requirement-58
func TestCommandSpecOwnsCanonicalBridgeWire(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	spec := testbridge.CommandSpec(fake)
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &bytes.Buffer{},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		RunID:    func() string { return "run-fixed" },
	})

	protocol := protocolFromContext(t, ctx)
	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var discovery vscode.DiscoveryDocument
	decodeJSON(t, protocol, &discovery)
	if discovery.Workspace != "consumer-node" || discovery.ModulePath != "example.dev/tool" || len(discovery.Items) != 1 {
		t.Fatalf("discovery = %+v, want provider document through package envelope", discovery)
	}

	protocol.Reset()
	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--format", "json", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	var desired vscode.SetupPlan
	decodeJSON(t, protocol, &desired)
	if got := fake.calls; got != "discover,desiredState:demo::lane::fast" {
		t.Fatalf("provider calls = %q, want discover,desiredState:demo::lane::fast", got)
	}
	if len(desired.Groups) != 1 || len(desired.Groups[0].Rows) != 1 || desired.Groups[0].Rows[0].Action != "reconcile_during_run" || !desired.Groups[0].Rows[0].Owned {
		t.Fatalf("desired state groups = %+v, want owned reconcile_during_run row", desired.Groups)
	}

	protocol.Reset()
	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("run dispatch: %v", err)
	}
	if !fake.sawRunLock {
		t.Fatal("runner did not observe package-owned run.lock serialization")
	}
	events := decodeEvents(t, protocol.String())
	if len(events) != 3 || events[0].Event != "run_started" || events[1].Event != "passed" || events[2].Event != "run_finished" || events[2].RunID == "" {
		t.Fatalf("events = %+v, want stamped run_started, runner event, terminal run_finished", events)
	}
	if events[2].RunID != "run-fixed" {
		t.Fatalf("run id = %q, want runtime override", events[2].RunID)
	}

	protocol.Reset()
	if err := spec.Dispatch(ctx, []string{"test-bridge", "config", "init"}); err != nil {
		t.Fatalf("config init dispatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".vscode", "test-bridge.json")); err != nil {
		t.Fatalf("config init did not write test-bridge.json: %v", err)
	}
}

// DHF-TEST: keel/requirement-60
func TestArgvContractForDesiredStateAndRun(t *testing.T) {
	spec := testbridge.CommandSpec(newFakeBridge(t.TempDir()))
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: t.TempDir(), Protocol: io.Discard})

	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover --format json: %v", err)
	}
	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--format", "json"}); err != nil {
		t.Fatalf("desired-state --format json: %v", err)
	}
	err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "run", "--format", "json", "--id", "demo::lane::fast"})
	var usage cli.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "unknown flag \"--format\"") {
		t.Fatalf("run --format err = %v, want usage error rejecting --format", err)
	}
}

// DHF-TEST: keel/requirement-60
func TestDesiredStateIsReadOnly(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	fake := newFakeBridge(root)
	fake.mutateDuringRun = marker
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("desired-state mutated workspace marker, stat err=%v", err)
	}

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("run dispatch: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("run did not perform runner-owned reconciliation: %v", err)
	}
}

func TestConfigHelpersInitUpgradeAndRefuseNewer(t *testing.T) {
	root := t.TempDir()
	template := newFakeBridge(root).ConfigTemplate()

	init, err := testbridge.InitConfig(root, template)
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	if !init.Changed || init.FromVersion != 0 || init.ToVersion != vscode.CurrentConfigVersion {
		t.Fatalf("init result = %+v, want changed 0 -> current", init)
	}
	again, err := testbridge.InitConfig(root, template)
	if err != nil {
		t.Fatalf("second init: %v", err)
	}
	if again.Changed {
		t.Fatalf("second init changed existing config: %+v", again)
	}

	path := filepath.Join(root, ".vscode", "test-bridge.json")
	old := `{"version":2,"command":"bin/custom","args":["go","run","./cmd/custom","vscode","tests"],"displayName":"Custom","env":{"A":"B"}}` + "\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	upgraded, err := testbridge.UpgradeConfig(root, template)
	if err != nil {
		t.Fatalf("upgrade config: %v", err)
	}
	if !upgraded.Changed || upgraded.FromVersion != 2 || upgraded.ToVersion != vscode.CurrentConfigVersion {
		t.Fatalf("upgrade result = %+v, want changed 2 -> current", upgraded)
	}
	var cfg vscode.TestBridgeConfig
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Command != "bin/custom" || cfg.Env["A"] != "B" {
		t.Fatalf("upgrade did not preserve consumer values: %+v", cfg)
	}
	if want := []string{"go", "run", "./cmd/custom"}; !equalStrings(cfg.Args, want) {
		t.Fatalf("upgrade args = %#v, want launcher-only %#v", cfg.Args, want)
	}

	if err := os.WriteFile(path, []byte(`{"version":999,"command":"bin/future","args":["wrapper"],"displayName":"Future"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := testbridge.UpgradeConfig(root, template); err == nil || !strings.Contains(err.Error(), "newer than this binary") {
		t.Fatalf("newer upgrade err = %v, want refusal", err)
	}
}

func TestRunErrorsAndLockConflictsUsePackagePaths(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.runErr = errors.New("runner failed")
	spec := testbridge.CommandSpec(fake)
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard, RunID: func() string { return "run-error" }})

	err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"})
	var runErr testbridge.RunError
	if !errors.As(err, &runErr) || runErr.ExitCode != 1 || !strings.Contains(runErr.Error(), "runner failed") || runErr.Unwrap() == nil {
		t.Fatalf("run error = %#v, want RunError wrapping runner failure", err)
	}

	if err := os.MkdirAll(filepath.Dir(testbridge.RunLockPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testbridge.RunLockPath(root), []byte(`{"pid":1,"created_at":"2026-07-13T00:00:00Z","ids":["x"],"token":"other"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = spec.Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"})
	if err == nil || !strings.Contains(err.Error(), "keel/testbridge: run lock already exists") {
		t.Fatalf("lock conflict err = %v, want package-prefixed lock refusal", err)
	}
}

func TestRunLockExemptionLeavesExistingLockForConsumerMaintenance(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.exemptRun = true
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard, RunID: func() string { return "run-exempt" }})
	if err := os.MkdirAll(filepath.Dir(testbridge.RunLockPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := []byte(`{"pid":1,"created_at":"2026-07-13T00:00:00Z","ids":["demo::maintenance::unlock"],"token":"foreign"}` + "\n")
	if err := os.WriteFile(testbridge.RunLockPath(root), lock, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::maintenance::unlock"}); err != nil {
		t.Fatalf("lock-exempt run dispatch: %v", err)
	}
	got, err := os.ReadFile(testbridge.RunLockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, lock) {
		t.Fatalf("lock-exempt run modified foreign lock:\n%s", got)
	}
}

// DHF-TEST: keel/requirement-67
func TestRunLockReleaseSymmetryDoesNotWarnWhenNoLockWasAcquiredOrLockIsAbsent(t *testing.T) {
	root := t.TempDir()
	var logs bytes.Buffer
	fake := newFakeBridge(root)
	fake.exemptRun = true
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: io.Discard,
		Log:      slog.New(slog.NewTextHandler(&logs, nil)),
		RunID:    func() string { return "run-exempt" },
	})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::maintenance::unlock"}); err != nil {
		t.Fatalf("lock-exempt run dispatch: %v", err)
	}
	if strings.Contains(logs.String(), "release testbridge run lock") || strings.Contains(logs.String(), "no such file or directory") {
		t.Fatalf("lock-exempt logs = %q, want no release-lock warning", logs.String())
	}

	root = t.TempDir()
	logs.Reset()
	fake = newFakeBridge(root)
	fake.removeRunLockDuringRun = true
	ctx = testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: io.Discard,
		Log:      slog.New(slog.NewTextHandler(&logs, nil)),
		RunID:    func() string { return "run-missing-lock" },
	})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("run with absent lock at release dispatch: %v", err)
	}
	if strings.Contains(logs.String(), "release testbridge run lock") || strings.Contains(logs.String(), "no such file or directory") {
		t.Fatalf("absent-lock release logs = %q, want missing lock treated as no-op", logs.String())
	}

	root = t.TempDir()
	fake = newFakeBridge(root)
	ctx = testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard, RunID: func() string { return "run-locked" }})
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("locked run dispatch: %v", err)
	}
	if _, err := os.Stat(testbridge.RunLockPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock after acquired run stat err = %v, want removed lock", err)
	}
}

func TestValidationRejectsInvalidProtocolDocuments(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	cases := []struct {
		name string
		doc  any
		want string
	}{
		{name: "unsupported", doc: struct{}{}, want: "unsupported protocol document"},
		{name: "discovery version", doc: vscode.DiscoveryDocument{Version: 2, Workspace: "w", ModulePath: "m", GeneratedAt: now}, want: "discovery version"},
		{name: "discovery item id", doc: vscode.DiscoveryDocument{Version: 1, Workspace: "w", ModulePath: "m", GeneratedAt: now, Items: []vscode.TestItem{{ID: "bad", Label: "bad", Kind: "lane"}}}, want: "does not match schema pattern"},
		{name: "setup status", doc: vscode.SetupPlan{Version: 2, Devtool: vscode.DevtoolMetadata{Name: "d", Version: "v"}, Workspace: "w", GeneratedAt: now, Groups: []vscode.DesiredStateGroup{{Label: "Test Preconditions", Rows: []vscode.DesiredState{{Resource: "db", Kind: "service", Desired: "up", Current: "down", Status: "bogus", Action: "reuse"}}}}, Teardown: vscode.SetupPlanTeardown{Policy: "none"}}, want: "invalid status"},
		{name: "run event", doc: vscode.RunEvent{Version: 1, Event: "bogus", Time: now}, want: "invalid event"},
		{name: "run lock", doc: vscode.RunLockFile{PID: 0, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{"x"}, Token: "t"}, want: "run-lock missing pid"},
		{name: "config", doc: vscode.TestBridgeConfig{Version: 999, Command: "bin/demo", DisplayName: "Demo"}, want: "config version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := testbridge.ValidateDocument(tc.doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateDocument err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-60
func TestDesiredStateGroupsRequireExactlyOneActiveRowInExclusiveGroups(t *testing.T) {
	now := time.Unix(2, 0).UTC()
	valid := vscode.SetupPlan{
		Version:     2,
		Devtool:     vscode.DevtoolMetadata{Name: "d", Version: "v"},
		Workspace:   "w",
		GeneratedAt: now,
		Items:       []vscode.SetupPlanItem{{ID: "demo::lane::fast", Runnable: true}},
		Groups: []vscode.DesiredStateGroup{{
			Label:             "Data set",
			Order:             10,
			MutuallyExclusive: true,
			Rows: []vscode.DesiredState{
				{Resource: "db-empty", Kind: "fixture-data", Desired: "empty", Current: "empty", Status: "satisfied", Action: "reuse", Message: "already empty", Reusable: true, Active: true},
				{Resource: "db-small", Kind: "fixture-data", Desired: "small", Current: "empty", Status: "reconcilable", Action: "reconcile_during_run", Message: "seed small", Owned: true},
			},
		}},
		Checks:   []vscode.PrereqCheck{{ID: "db", OK: true, Message: "probe only"}},
		Actions:  []vscode.SetupPlanAction{{Resource: "db-small", Status: "reconcile_during_run", Message: "seed small", Owned: true}},
		Teardown: vscode.SetupPlanTeardown{Policy: "owned-after-run"},
	}
	if err := testbridge.ValidateDocument(valid); err != nil {
		t.Fatalf("exclusive group with one active row should validate: %v", err)
	}

	noneActive := valid
	noneActive.Groups = cloneDesiredStateGroups(valid.Groups)
	noneActive.Groups[0].Rows[0].Active = false
	if err := testbridge.ValidateDocument(noneActive); err == nil || !strings.Contains(err.Error(), "exactly one active row") {
		t.Fatalf("zero-active exclusive group err = %v, want exactly one active row", err)
	}

	multipleActive := valid
	multipleActive.Groups = cloneDesiredStateGroups(valid.Groups)
	multipleActive.Groups[0].Rows[1].Active = true
	if err := testbridge.ValidateDocument(multipleActive); err == nil || !strings.Contains(err.Error(), "exactly one active row") {
		t.Fatalf("multi-active exclusive group err = %v, want exactly one active row", err)
	}
}

// Runnable rows carry a devtool-served run_id; run ids must be unique across
// the whole document so activation is unambiguous (formal_review-80).
//
// DHF-TEST: keel/requirement-60
func TestDesiredStateRowRunIDsAreUniqueAcrossThePlan(t *testing.T) {
	now := time.Unix(3, 0).UTC()
	valid := vscode.SetupPlan{
		Version:     2,
		Devtool:     vscode.DevtoolMetadata{Name: "d", Version: "v"},
		Workspace:   "w",
		GeneratedAt: now,
		Groups: []vscode.DesiredStateGroup{{
			Label: "Test Preconditions",
			Order: 10,
			Rows: []vscode.DesiredState{
				{Resource: "python", Kind: "tool", Desired: "available", Current: "available", Status: "satisfied", Action: "reuse", Message: "ok", Reusable: true},
				{RunID: "demo::action::provision-venv", Resource: "python-venv", Kind: "dependency", Desired: "provisioned", Current: "missing", Status: "reconcilable", Action: "reconcile", Message: "provision", Owned: true},
			},
		}},
		Teardown: vscode.SetupPlanTeardown{Policy: "none"},
	}
	if err := testbridge.ValidateDocument(valid); err != nil {
		t.Fatalf("plan with a served run_id should validate: %v", err)
	}

	dup := valid
	dup.Groups = cloneDesiredStateGroups(valid.Groups)
	dup.Groups[0].Rows[0].RunID = "demo::action::provision-venv"
	if err := testbridge.ValidateDocument(dup); err == nil || !strings.Contains(err.Error(), "run ids must be unique") {
		t.Fatalf("duplicate run_id err = %v, want run ids must be unique", err)
	}
}

func TestCommandSpecErrorsAndRuntimeDefaults(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	spec := testbridge.CommandSpec(fake)

	if err := spec.Dispatch(context.Background(), []string{"test-bridge", "tests", "discover"}); err != nil {
		t.Fatalf("discover with default runtime: %v", err)
	}
	if err := spec.Dispatch(context.Background(), []string{"test-bridge", "tests", "run"}); err == nil || !strings.Contains(err.Error(), "--id is required") {
		t.Fatalf("run without id err = %v, want --id required", err)
	}
	for _, args := range [][]string{
		{"test-bridge", "tests", "discover", "--format", "yaml"},
		{"test-bridge", "tests", "desired-state", "--id"},
		{"test-bridge", "tests", "desired-state", "extra"},
		{"test-bridge", "config", "init", "extra"},
		{"test-bridge", "config", "upgrade", "extra"},
	} {
		if err := spec.Dispatch(context.Background(), args); err == nil {
			t.Fatalf("Dispatch(%v) returned nil, want usage error", args)
		}
	}

	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := spec.Dispatch(testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard}), []string{"test-bridge", "config", "upgrade"}); err != nil {
		t.Fatalf("config upgrade dispatch: %v", err)
	}

	fake.discoverErr = errors.New("discover failed")
	if err := spec.Dispatch(context.Background(), []string{"test-bridge", "tests", "discover"}); err == nil || !strings.Contains(err.Error(), "discover failed") {
		t.Fatalf("discover provider err = %v, want provider failure", err)
	}
	fake.discoverErr = nil
	fake.desiredErr = errors.New("desired failed")
	if err := spec.Dispatch(context.Background(), []string{"test-bridge", "tests", "desired-state"}); err == nil || !strings.Contains(err.Error(), "desired failed") {
		t.Fatalf("desired provider err = %v, want provider failure", err)
	}

	fileRoot := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileFake := newFakeBridge(fileRoot)
	err := testbridge.CommandSpec(fileFake).Dispatch(testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: fileRoot, Protocol: io.Discard}), []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"})
	if err == nil {
		t.Fatal("run with file root returned nil, want run writer mkdir failure")
	}
}

func TestValidationCoversClosedEnumsAndRequiredFields(t *testing.T) {
	now := time.Unix(2, 0).UTC()
	validDiscovery := func() vscode.DiscoveryDocument {
		return vscode.DiscoveryDocument{
			Version:     1,
			Workspace:   "w",
			ModulePath:  "m",
			GeneratedAt: now,
			Capabilities: vscode.DiscoveryCapabilities{
				ClearResults:              true,
				RefreshInvalidatesResults: true,
				NeutralParentRollups:      true,
			},
			Items: []vscode.TestItem{{ID: "demo::lane::fast", Label: "fast", Kind: "lane", Runnable: true, Profiles: []string{"run"}}},
		}
	}
	validPlan := func() vscode.SetupPlan {
		return vscode.SetupPlan{
			Version:     2,
			Devtool:     vscode.DevtoolMetadata{Name: "d", Version: "v"},
			Workspace:   "w",
			GeneratedAt: now,
			Items:       []vscode.SetupPlanItem{{ID: "i", Runnable: true}},
			Groups: []vscode.DesiredStateGroup{{
				Label: "Test Preconditions",
				Rows: []vscode.DesiredState{{
					Resource: "db",
					Kind:     "service",
					Desired:  "up",
					Current:  "down",
					Status:   "reconcilable",
					Action:   "reconcile_during_run",
					Message:  "ok",
				}},
			}},
			Checks:   []vscode.PrereqCheck{{ID: "db", OK: true, Message: "ok"}},
			Actions:  []vscode.SetupPlanAction{{Resource: "db", Status: "reconcile_during_run", Message: "ok"}},
			Teardown: vscode.SetupPlanTeardown{Policy: "none"},
		}
	}
	assertValid := func(name string, doc any) {
		t.Helper()
		if err := testbridge.ValidateDocument(doc); err != nil {
			t.Fatalf("%s should validate: %v", name, err)
		}
	}
	assertInvalid := func(name string, doc any, want string) {
		t.Helper()
		err := testbridge.ValidateDocument(doc)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s err = %v, want containing %q", name, err, want)
		}
	}

	assertValid("discovery", validDiscovery())
	discovery := validDiscovery()
	discovery.Workspace = ""
	assertInvalid("discovery required", discovery, "missing workspace")
	discovery = validDiscovery()
	discovery.Items[0].Kind = "folder"
	assertInvalid("discovery kind", discovery, "invalid kind")
	discovery = validDiscovery()
	discovery.Items[0].Profiles = []string{"profile"}
	assertInvalid("discovery profile", discovery, "invalid profile")

	assertValid("setup plan", validPlan())
	plan := validPlan()
	plan.Groups[0].Rows[0].Resource = ""
	assertInvalid("desired required", plan, "missing required fields")
	plan = validPlan()
	plan.Groups[0].Rows[0].Kind = "workspace"
	assertInvalid("desired kind", plan, "invalid kind")
	plan = validPlan()
	plan.Groups[0].Rows[0].Action = "setup_required"
	assertInvalid("desired action", plan, "invalid action")
	plan = validPlan()
	plan.Actions[0].Status = "bogus"
	assertInvalid("action status", plan, "invalid status")
	plan = validPlan()
	plan.Checks[0].ID = ""
	assertInvalid("check id", plan, "check missing id")
	plan = validPlan()
	plan.Teardown.Policy = ""
	assertInvalid("teardown", plan, "teardown policy")
	plan = validPlan()
	plan.Items[0].ID = ""
	assertInvalid("item id", plan, "item missing id")

	assertValid("run event", vscode.RunEvent{Version: 1, Event: "passed", Time: now, Source: "vscode"})
	assertInvalid("run source", vscode.RunEvent{Version: 1, Event: "passed", Time: now, Source: "consumer"}, "invalid source")
	assertInvalid("run duration", vscode.RunEvent{Version: 1, Event: "passed", Time: now, DurationMS: -1}, "negative")
	assertInvalid("run artifact", vscode.RunEvent{Version: 1, Event: "artifact", Time: now, Artifact: &vscode.RunArtifact{Name: "a", URI: "file:///tmp/a", Kind: "zip"}}, "artifact")

	assertValid("run lock", vscode.RunLockFile{PID: 1, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{"x"}, Token: "t"})
	assertInvalid("run lock time", vscode.RunLockFile{PID: 1, CreatedAt: "bad", IDs: []string{"x"}, Token: "t"}, "created_at")
	assertInvalid("run lock id", vscode.RunLockFile{PID: 1, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{""}, Token: "t"}, "empty id")
	assertInvalid("run lock token", vscode.RunLockFile{PID: 1, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{"x"}}, "token")
	assertInvalid("config missing", vscode.TestBridgeConfig{Version: vscode.CurrentConfigVersion}, "missing command")
	legacyArgs := []string{"vs" + "code", "tests"}
	assertInvalid("config protocol args", vscode.TestBridgeConfig{Version: vscode.CurrentConfigVersion, Command: "bin/demo", Args: legacyArgs, DisplayName: "Demo"}, "launcher-only")
}

func cloneDesiredStateGroups(groups []vscode.DesiredStateGroup) []vscode.DesiredStateGroup {
	cloned := append([]vscode.DesiredStateGroup(nil), groups...)
	for i := range cloned {
		cloned[i].Rows = append([]vscode.DesiredState(nil), cloned[i].Rows...)
	}
	return cloned
}

type fakeBridge struct {
	root                   string
	calls                  string
	mutateDuringRun        string
	sawRunLock             bool
	exemptRun              bool
	removeRunLockDuringRun bool
	runErr                 error
	discoverErr            error
	desiredErr             error
}

func newFakeBridge(root string) *fakeBridge {
	return &fakeBridge{root: root}
}

func (f *fakeBridge) Metadata() vscode.DevtoolMetadata {
	return vscode.DevtoolMetadata{Name: "demo-dev", Version: "v0.0.0", Commit: "abc123", BuiltAt: "test"}
}

func (f *fakeBridge) Workspace() testbridge.Workspace {
	return testbridge.Workspace{Root: f.root, Node: "consumer-node", ModulePath: "example.dev/tool"}
}

func (f *fakeBridge) ConfigTemplate() vscode.TestBridgeConfig {
	return vscode.TestBridgeConfig{Version: vscode.CurrentConfigVersion, Command: "bin/demo-dev", Args: []string{}, DisplayName: "Demo"}
}

func equalStrings(got, want []string) bool {
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

func (f *fakeBridge) Discover(_ context.Context) (vscode.DiscoveryDocument, error) {
	if f.discoverErr != nil {
		return vscode.DiscoveryDocument{}, f.discoverErr
	}
	f.appendCall("discover")
	return vscode.DiscoveryDocument{
		Version:     1,
		Workspace:   "consumer-node",
		ModulePath:  "example.dev/tool",
		GeneratedAt: time.Unix(1, 0).UTC(),
		Capabilities: vscode.DiscoveryCapabilities{
			ClearResults:              true,
			RefreshInvalidatesResults: true,
			NeutralParentRollups:      true,
		},
		Items: []vscode.TestItem{{
			ID:       "demo::lane::fast",
			Label:    "fast",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
			LaneID:   "demo::lane::fast",
		}},
	}, nil
}

func (f *fakeBridge) DesiredState(_ context.Context, ids []string) (vscode.SetupPlan, error) {
	if f.desiredErr != nil {
		return vscode.SetupPlan{}, f.desiredErr
	}
	f.appendCall("desiredState:" + strings.Join(ids, ","))
	return vscode.SetupPlan{
		Version:           2,
		Devtool:           f.Metadata(),
		Workspace:         "consumer-node",
		GeneratedAt:       time.Unix(2, 0).UTC(),
		Items:             []vscode.SetupPlanItem{{ID: "demo::lane::fast", Runnable: true, RequiredResources: []string{"db"}}},
		RequiredResources: []string{"db"},
		Groups: []vscode.DesiredStateGroup{{
			Label: "Test Preconditions",
			Rows: []vscode.DesiredState{{
				Resource: "db",
				Kind:     "service",
				Desired:  "seeded",
				Current:  "empty",
				Status:   "reconcilable",
				Action:   "reconcile_during_run",
				Message:  "seed test database during run",
				Reusable: false,
				Owned:    true,
			}},
		}},
		Checks:   []vscode.PrereqCheck{{ID: "db", OK: true, Message: "probe only"}},
		Actions:  []vscode.SetupPlanAction{{Resource: "db", Status: "reconcile_during_run", Message: "seed during run", Reusable: false, Owned: true}},
		Teardown: vscode.SetupPlanTeardown{OwnedTemporaryResources: []string{"db"}, SharedReusableResources: []string{}, Policy: "owned-after-run"},
	}, nil
}

func (f *fakeBridge) Run(_ context.Context, req testbridge.RunRequest, emit vscode.RunEventWriter) (int, error) {
	if _, err := os.Stat(filepath.Join(f.root, ".devtools", "vscode-runs", "run.lock")); err == nil {
		f.sawRunLock = true
	}
	if f.removeRunLockDuringRun {
		if err := os.Remove(filepath.Join(f.root, ".devtools", "vscode-runs", "run.lock")); err != nil {
			return 1, err
		}
	}
	if f.mutateDuringRun != "" {
		if err := os.WriteFile(f.mutateDuringRun, []byte("run\n"), 0o644); err != nil {
			return 1, err
		}
	}
	for _, id := range req.IDs {
		emit(vscode.RunEvent{Event: "passed", TestID: id})
	}
	if f.runErr != nil {
		return 1, f.runErr
	}
	return 0, nil
}

func (f *fakeBridge) LockExemptRun([]string) bool {
	return f.exemptRun
}

func (f *fakeBridge) appendCall(call string) {
	if f.calls != "" {
		f.calls += ","
	}
	f.calls += call
}

func protocolFromContext(t *testing.T, ctx context.Context) *bytes.Buffer {
	t.Helper()
	runtime, ok := testbridge.RuntimeFrom(ctx)
	if !ok {
		t.Fatal("runtime missing")
	}
	buf, ok := runtime.Protocol.(*bytes.Buffer)
	if !ok {
		t.Fatalf("protocol writer = %T, want *bytes.Buffer", runtime.Protocol)
	}
	return buf
}

func decodeJSON(t *testing.T, buf *bytes.Buffer, out any) {
	t.Helper()
	if err := json.Unmarshal(buf.Bytes(), out); err != nil {
		t.Fatalf("decode JSON %T: %v\n%s", out, err, buf.String())
	}
}

func decodeEvents(t *testing.T, raw string) []vscode.RunEvent {
	t.Helper()
	var events []vscode.RunEvent
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var event vscode.RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode event %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

// EncodeDocument is the package-owned protocol JSON sink consumer devtools route
// their protocol output through instead of hand-rolling a json.Encoder each.
//
// DHF-TEST: keel/requirement-63
func TestEncodeDocumentOwnsCanonicalProtocolJSON(t *testing.T) {
	doc := map[string]any{"module_path": "keel", "note": "a<b>c & d"}
	var buf bytes.Buffer
	if err := testbridge.EncodeDocument(&buf, doc); err != nil {
		t.Fatalf("EncodeDocument: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("EncodeDocument output must end with a newline: %q", out)
	}
	// HTML escaping is disabled so protocol payloads stay byte-faithful.
	if !strings.Contains(out, "a<b>c & d") {
		t.Fatalf("EncodeDocument must not HTML-escape payloads: %q", out)
	}
	var round map[string]any
	if err := json.Unmarshal(buf.Bytes(), &round); err != nil {
		t.Fatalf("re-decode encoded document: %v\n%s", err, out)
	}
	if round["module_path"] != "keel" {
		t.Fatalf("round-tripped document = %+v, want module_path=keel", round)
	}
	// A nil writer is tolerated (discards), matching the package sink default.
	if err := testbridge.EncodeDocument(nil, doc); err != nil {
		t.Fatalf("EncodeDocument(nil): %v", err)
	}
}
