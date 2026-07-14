// Command keel-demo-dev is a reference consumer devtool for the VS Code test
// bridge contract.
//
// DHF-REQ: keel/requirement-62
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/david-aggeler/keel/cli"
	procexec "github.com/david-aggeler/keel/exec"
	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

const (
	demoVersion = "demo"

	idRoot        = "keel-demo-dev::root"
	idMaintenance = "keel-demo-dev::maintenance"
	idDesired     = "keel::desired-state"
	idLanes       = "keel-demo-dev::lanes"
	idFrameworks  = "keel-demo-dev::frameworks"
	idGoFramework = "keel-demo-dev::frameworks::go"
	idFakeFamily  = "keel-demo-dev::frameworks::fake"

	idLaneGoPass    = "keel-demo-dev::lane::go-pass"
	idLaneGoFail    = "keel-demo-dev::lane::go-fail"
	idLaneFakeSmoke = "keel-demo-dev::lane::fake-smoke"

	idTestGoPass = "go::test::passing::TestReferencePass"
	idTestGoFail = "go::test::failing::TestReferenceFailure"

	idDetectLanes    = "keel-demo-dev::maintenance::detect-lanes"
	idBlockBadLane   = "keel-demo-dev::maintenance::block-bad-lane"
	idUnblockBadLane = "keel-demo-dev::maintenance::unblock-bad-lane"

	idDesiredDockerEnv = "keel-demo-dev::desired-state::docker-env"
	idDesiredPostgres  = "keel-demo-dev::desired-state::postgres"
	idDesiredServiceA  = "keel-demo-dev::desired-state::service-a"
	idDesiredServiceB  = "keel-demo-dev::desired-state::service-b"
	idDesiredServiceC  = "keel-demo-dev::desired-state::service-c"
	idDesiredSDK       = "keel-demo-dev::desired-state::sdk"
	idDesiredDNS       = "keel-demo-dev::desired-state::dns"
	idDesiredPing      = "keel-demo-dev::desired-state::ping"
	idDataSetEmpty     = "keel-demo-dev::desired-state::dataset::empty"
	idDataSetSmall     = "keel-demo-dev::desired-state::dataset::small"
	idDataSetFull      = "keel-demo-dev::desired-state::dataset::full"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	bridge := demoBridge{}
	tree := testbridge.CommandSpec(bridge)
	tree.Config = cli.Config{
		Program:      "keel-demo-dev",
		RootSummary:  "keel-demo-dev serves a reference consumer test bridge.",
		Usage:        "keel-demo-dev <command> [args]",
		HelpUsage:    "keel-demo-dev help [command]",
		CommandUsage: "keel-demo-dev <command> --help",
	}

	cfg, words, err := cli.ParseGlobalConfig(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keel-demo-dev: "+err.Error())
		return 2
	}
	if cfg.Version {
		fmt.Fprintln(os.Stdout, demoVersion)
		return 0
	}
	if cfg.HelpAll {
		tree.RenderAllHelp(os.Stderr)
		return 0
	}
	if len(words) > 0 && words[0] == "help" {
		tree.RenderTopicHelp(os.Stderr, words[1:])
		return 0
	}
	if cfg.Help {
		tree.RenderTopicHelp(os.Stderr, words)
		return 0
	}

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "keel-demo-dev: "+err.Error())
		return 1
	}
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: os.Stdout,
		Log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err := tree.Dispatch(ctx, words); err != nil {
		fmt.Fprintln(os.Stderr, "keel-demo-dev: "+err.Error())
		var usage cli.UsageError
		if errors.As(err, &usage) {
			return usage.ExitCode()
		}
		var runErr testbridge.RunError
		if errors.As(err, &runErr) && runErr.ExitCode != 0 {
			return runErr.ExitCode
		}
		return 1
	}
	return 0
}

type demoBridge struct{}

func (demoBridge) Workspace() testbridge.Workspace {
	return testbridge.Workspace{Root: workingRoot(), Node: "keel-demo-dev", ModulePath: "github.com/david-aggeler/keel-demo-dev"}
}

func (demoBridge) Metadata() vscode.DevtoolMetadata {
	return vscode.DevtoolMetadata{Name: "keel-demo-dev", Version: demoVersion, Commit: "demo", BuiltAt: "demo"}
}

func (demoBridge) ConfigTemplate() vscode.TestBridgeConfig {
	return vscode.TestBridgeConfig{
		Version:     vscode.CurrentConfigVersion,
		Command:     filepath.Join("bin", executableName()),
		Args:        []string{},
		DisplayName: "Keel Demo Dev",
	}
}

// DHF-REQ: keel/requirement-62
func (b demoBridge) Discover(ctx context.Context) (vscode.DiscoveryDocument, error) {
	ws := b.workspace(ctx)
	items := []vscode.TestItem{
		group(idRoot, "", "Keel Demo Dev"),
		group(idMaintenance, idRoot, "A - Test Bridge Maintenance"),
		group(idDesired, idRoot, "B - Desired State"),
		group(idLanes, idRoot, "C - Lanes"),
		group(idFrameworks, idRoot, "D - Frameworks"),
		group(idGoFramework, idFrameworks, "Go"),
		group(idFakeFamily, idFrameworks, "Fake infrastructure"),
		maintenance(idDetectLanes, idMaintenance, "A.1 detect lanes"),
		maintenance(idBlockBadLane, idMaintenance, "A.2 block failing Go lane"),
		maintenance(idUnblockBadLane, idMaintenance, "A.3 unblock failing Go lane"),
	}
	if hasDemoLanesFile(ws.Root) {
		items = append(items,
			lane(idLaneGoPass, idLanes, "C.10 real Go pass", []string{"go-toolchain"}),
			lane(idLaneGoFail, idLanes, "C.20 real Go fail", []string{"go-toolchain"}),
			lane(idLaneFakeSmoke, idLanes, "C.30 fake provisioning smoke", []string{"demo-environment", "demo-database", "demo-services"}),
			test(idTestGoPass, idGoFramework, "TestReferencePass", idLaneGoPass),
			test(idTestGoFail, idGoFramework, "TestReferenceFailure", idLaneGoFail),
			test("fake::test::provisioning::Preview", idFakeFamily, "Preview provisioning story", idLaneFakeSmoke),
		)
	}
	return vscode.DiscoveryDocument{
		Version:     1,
		Workspace:   ws.Root,
		ModulePath:  ws.ModulePath,
		GeneratedAt: time.Now().UTC(),
		Capabilities: vscode.DiscoveryCapabilities{
			ClearResults:              true,
			RefreshInvalidatesResults: true,
			NeutralParentRollups:      true,
			ClearStateTestIDs:         []string{idUnblockBadLane},
		},
		Items: items,
	}, nil
}

// DHF-REQ: keel/requirement-62
func (b demoBridge) DesiredState(ctx context.Context, ids []string) (vscode.SetupPlan, error) {
	selected := selectedItems(ids)
	return vscode.SetupPlan{
		Version:           2,
		Devtool:           b.Metadata(),
		Workspace:         b.workspace(ctx).Root,
		GeneratedAt:       time.Now().UTC(),
		Items:             selected,
		RequiredResources: []string{"demo-environment", "demo-database", "demo-services"},
		Groups: []vscode.DesiredStateGroup{
			{
				Label: "Test Preconditions",
				Order: 10,
				Rows: []vscode.DesiredState{
					desired(idDesiredDockerEnv, "docker-env", "dependency", "ready", "absent", "provision_demo_environment", false, false),
					desired(idDesiredPostgres, "postgres", "fixture-data", "present+seeded", "missing", "create_and_seed_demo_database", false, false),
					desired(idDesiredServiceA, "service-a", "service", "running", "stopped", "start_demo_service", true, false),
					desired(idDesiredServiceB, "service-b", "service", "running", "stopped", "start_demo_service", true, false),
					desired(idDesiredServiceC, "service-c", "service", "running", "stopped", "start_demo_service", true, false),
					desired(idDesiredSDK, "sdk", "tool", "installed", "missing", "install_demo_sdk", true, false),
					desired(idDesiredDNS, "dns", "host-port-set", "resolves", "missing", "seed_demo_dns", true, false),
					desired(idDesiredPing, "ping", "dependency", "reachable", "timeout", "probe_demo_endpoint", true, false),
				},
			},
			{
				Label:             "app-db data set",
				Order:             20,
				MutuallyExclusive: true,
				Rows: []vscode.DesiredState{
					desired(idDataSetEmpty, "app-db-empty", "fixture-data", "empty", "small", "select_empty_data_set", false, false),
					desired(idDataSetSmall, "app-db-small", "fixture-data", "small", "small", "reuse_small_data_set", false, true),
					desired(idDataSetFull, "app-db-full", "fixture-data", "full", "small", "select_full_data_set", false, false),
				},
			},
		},
		Checks: []vscode.PrereqCheck{
			{ID: "demo-env-preview", OK: true, Message: "fake infrastructure preview only; no real resources are mutated"},
		},
		Actions: []vscode.SetupPlanAction{
			action("environment", "provision_demo_environment", false),
			action("database", "create_and_seed_demo_database", false),
			action("service-a", "start_demo_service", true),
			action("service-b", "start_demo_service", true),
			action("service-c", "start_demo_service", true),
			action("sdk", "install_demo_sdk", true),
			action("dns", "seed_demo_dns", true),
			action("ping", "probe_demo_endpoint", true),
			action("app-db-empty", "select_empty_data_set", false),
			action("app-db-small", "reuse_small_data_set", false),
			action("app-db-full", "select_full_data_set", false),
		},
		Teardown: vscode.SetupPlanTeardown{
			OwnedTemporaryResources: []string{"environment", "database"},
			SharedReusableResources: []string{"service-a", "service-b", "service-c"},
			Policy:                  "demo-only fake resources; no teardown command mutates real infrastructure",
		},
	}, nil
}

func (b demoBridge) workspace(ctx context.Context) testbridge.Workspace {
	if rt, ok := testbridge.RuntimeFrom(ctx); ok && rt.Root != "" {
		return testbridge.Workspace{Root: rt.Root, Node: "keel-demo-dev", ModulePath: "github.com/david-aggeler/keel-demo-dev"}
	}
	return b.Workspace()
}

func workingRoot() string {
	root, _ := os.Getwd()
	return root
}

// DHF-REQ: keel/requirement-62
func (b demoBridge) Run(ctx context.Context, req testbridge.RunRequest, emit vscode.RunEventWriter) (int, error) {
	exitCode := 0
	for _, id := range req.IDs {
		code, err := b.runOne(ctx, req.Root, id, emit)
		if code != 0 && exitCode == 0 {
			exitCode = code
		}
		if err != nil {
			return code, err
		}
	}
	return exitCode, nil
}

func (b demoBridge) runOne(ctx context.Context, root, id string, emit vscode.RunEventWriter) (int, error) {
	switch id {
	case idDetectLanes:
		if err := writeDemoLanesFile(root); err != nil {
			return 1, err
		}
		emit(vscode.RunEvent{Event: "passed", TestID: id, Message: "wrote .vscode/test-lanes.json for demo lanes"})
		return 0, nil
	case idBlockBadLane:
		if err := writeBlockState(root, idLaneGoFail); err != nil {
			return 1, err
		}
		emit(vscode.RunEvent{Event: "passed", TestID: id, Message: "blocked " + idLaneGoFail})
		return 0, nil
	case idUnblockBadLane:
		if err := writeBlockState(root, ""); err != nil {
			return 1, err
		}
		emit(vscode.RunEvent{Event: "passed", TestID: id, Message: "unblocked demo lanes"})
		return 0, nil
	case idLaneFakeSmoke, "fake::test::provisioning::Preview":
		emit(vscode.RunEvent{Event: "test_started", TestID: id})
		emit(vscode.RunEvent{Event: "output", TestID: id, Message: "fake provisioning preview: environment/database/services need reconcile_during_run"})
		emit(vscode.RunEvent{Event: "passed", TestID: id, Message: "fake provisioning preview rendered"})
		return 0, nil
	case idDesiredDockerEnv, idDesiredPostgres, idDesiredServiceA, idDesiredServiceB, idDesiredServiceC, idDesiredSDK, idDesiredDNS, idDesiredPing,
		idDataSetEmpty, idDataSetSmall, idDataSetFull:
		emit(vscode.RunEvent{Event: "test_started", TestID: id})
		emit(vscode.RunEvent{Event: "passed", TestID: id, Message: "reconciled fake desired-state row " + id})
		return 0, nil
	case idLaneGoPass, idTestGoPass:
		return runGoLane(ctx, root, id, true, emit)
	case idLaneGoFail, idTestGoFail:
		if blocked, err := blockedLane(root); err != nil {
			return 1, err
		} else if blocked == idLaneGoFail {
			emit(vscode.RunEvent{Event: "failed", TestID: idLaneGoFail, Message: "lane blocked: " + blocked})
			return 1, nil
		}
		return runGoLane(ctx, root, id, false, emit)
	default:
		return 1, fmt.Errorf("unknown demo test id %q", id)
	}
}

func runGoLane(ctx context.Context, root, id string, pass bool, emit vscode.RunEventWriter) (int, error) {
	pkgDir, err := writeGoFixture(root, pass)
	if err != nil {
		return 1, err
	}
	emit(vscode.RunEvent{Event: "test_started", TestID: id})
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: "go",
		Args:    []string{"test", "."},
		Dir:     pkgDir,
		Logger:  nopProcessLogger{},
	})
	if err != nil {
		return 1, err
	}
	result, waitErr := proc.Wait()
	out := strings.TrimSpace(result.Stdout + result.Stderr)
	if out != "" {
		emit(vscode.RunEvent{Event: "output", TestID: id, Message: out})
	}
	if waitErr != nil || result.ExitCode != 0 {
		emit(vscode.RunEvent{Event: "failed", TestID: id, Message: "real Go test failed"})
		if waitErr != nil {
			return result.ExitCode, nil
		}
		return result.ExitCode, nil
	}
	emit(vscode.RunEvent{Event: "passed", TestID: id, Message: "real Go test passed"})
	return 0, nil
}

func writeGoFixture(root string, pass bool) (string, error) {
	name := "passing"
	body := "if 1+1 != 2 { t.Fatal(\"math broke\") }"
	if !pass {
		name = "failing"
		body = "t.Fatal(\"intentional reference failure\")"
	}
	dir := filepath.Join(root, ".devtools", "keel-demo-dev", "go-lanes", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/keel-demo-dev/"+name+"\n\ngo 1.25\n"), 0o644); err != nil {
		return "", err
	}
	src := "package " + name + "\n\nimport \"testing\"\n\nfunc TestReference" + title(name) + "(t *testing.T) {\n\t" + body + "\n}\n"
	if err := os.WriteFile(filepath.Join(dir, name+"_test.go"), []byte(src), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

func selectedItems(ids []string) []vscode.SetupPlanItem {
	if len(ids) == 0 {
		ids = []string{idLaneFakeSmoke, idLaneGoPass, idLaneGoFail}
	}
	out := make([]vscode.SetupPlanItem, 0, len(ids))
	for _, id := range ids {
		out = append(out, setupItem(id))
	}
	return out
}

func setupItem(id string) vscode.SetupPlanItem {
	item := vscode.SetupPlanItem{ID: id, Runnable: true}
	switch id {
	case idLaneFakeSmoke:
		item.Label, item.Kind, item.Framework, item.Runner = "fake provisioning smoke", "lane", "fake", "keel-demo-dev"
		item.RequiredResources = []string{"demo-environment", "demo-database", "demo-services"}
	case idLaneGoPass:
		item.Label, item.Kind, item.Framework, item.Runner = "real Go pass", "lane", "go", "go-test"
		item.RequiredResources = []string{"go-toolchain"}
	case idLaneGoFail:
		item.Label, item.Kind, item.Framework, item.Runner = "real Go fail", "lane", "go", "go-test"
		item.RequiredResources = []string{"go-toolchain"}
	default:
		item.Label = id
	}
	return item
}

func group(id, parent, label string) vscode.TestItem {
	return vscode.TestItem{ID: id, ParentID: parent, Label: label, Kind: "group", Runnable: false, Profiles: []string{}}
}

func maintenance(id, parent, label string) vscode.TestItem {
	return vscode.TestItem{ID: id, ParentID: parent, Label: label, SortText: label, Kind: "maintenance", Framework: "keel-demo-dev", Runner: "keel-demo-dev", RunnerLabel: "Keel Demo Dev", Runnable: true, Profiles: []string{"run"}}
}

func lane(id, parent, label string, resources []string) vscode.TestItem {
	return vscode.TestItem{ID: id, ParentID: parent, Label: label, SortText: label, Kind: "lane", Framework: "keel-demo-dev", Runner: "keel-demo-dev", RunnerLabel: "Keel Demo Dev", Runnable: true, Profiles: []string{"run"}, LaneID: id, RequiredResources: resources}
}

func test(id, parent, label, laneID string) vscode.TestItem {
	return vscode.TestItem{ID: id, ParentID: parent, Label: label, Kind: "test", Framework: "keel-demo-dev", Runner: "keel-demo-dev", RunnerLabel: "Keel Demo Dev", Runnable: true, Profiles: []string{"run"}, LaneID: laneID}
}

func desired(runID, resource, kind, want, current, actionName string, reusable, active bool) vscode.DesiredState {
	return vscode.DesiredState{
		RunID:    runID,
		Resource: resource,
		Kind:     kind,
		Desired:  want,
		Current:  current,
		Status:   "reconcilable",
		Action:   "reconcile_during_run",
		Message:  "named action " + actionName + " would reconcile this fake resource during a demo run",
		Reusable: reusable,
		Owned:    !reusable,
		Active:   active,
	}
}

func action(resource, actionName string, reusable bool) vscode.SetupPlanAction {
	return vscode.SetupPlanAction{
		Resource: resource,
		Status:   "reconcile_during_run",
		Message:  actionName,
		Reusable: reusable,
		Owned:    !reusable,
	}
}

func blockStatePath(root string) string {
	return filepath.Join(root, ".devtools", "keel-demo-dev", "blocked-lane.json")
}

func demoLanesPath(root string) string {
	return filepath.Join(root, ".vscode", "test-lanes.json")
}

func hasDemoLanesFile(root string) bool {
	_, err := os.Stat(demoLanesPath(root))
	return err == nil
}

func writeDemoLanesFile(root string) error {
	path := demoLanesPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := map[string]any{
		"version": 1,
		"lanes": []map[string]any{
			{"id": idLaneGoPass, "label": "real Go pass", "framework": "go", "required_resources": []string{"go-toolchain"}},
			{"id": idLaneGoFail, "label": "real Go fail", "framework": "go", "required_resources": []string{"go-toolchain"}},
			{"id": idLaneFakeSmoke, "label": "fake provisioning smoke", "framework": "fake", "required_resources": []string{"demo-environment", "demo-database", "demo-services"}},
		},
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func blockedLane(root string) (string, error) {
	data, err := os.ReadFile(blockStatePath(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var state struct {
		BlockedLane string `json:"blocked_lane"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return "", err
	}
	return state.BlockedLane, nil
}

func writeBlockState(root, laneID string) error {
	path := blockStatePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if laneID == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	state := struct {
		BlockedLane string `json:"blocked_lane"`
		UpdatedAt   string `json:"updated_at"`
	}{BlockedLane: laneID, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func executableName() string {
	if strings.HasSuffix(os.Args[0], ".exe") {
		return "keel-demo-dev.exe"
	}
	return "keel-demo-dev"
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type nopProcessLogger struct{}

func (nopProcessLogger) Debug(string, ...any)                        {}
func (nopProcessLogger) Error(string, ...any)                        {}
func (nopProcessLogger) Info(string, ...any)                         {}
func (nopProcessLogger) InfoContext(context.Context, string, ...any) {}
