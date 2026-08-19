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

	keel "github.com/david-aggeler/keel"
	"github.com/david-aggeler/keel/cli"
	procexec "github.com/david-aggeler/keel/exec"
	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

const (
	idRoot        = "keel-demo-dev::root"
	idMaintenance = testbridge.MaintenanceGroupID
	idDesired     = "keel-demo-dev" + testbridge.DesiredStateGroupIDSuffix
	idLanes       = "keel-demo-dev::lanes"
	idFrameworks  = "keel-demo-dev::frameworks"
	idGoFramework = "keel-demo-dev::frameworks::go"
	idFakeFamily  = "keel-demo-dev::frameworks::fake"
	idSlowFamily  = "keel-demo-dev::frameworks::slow"

	idLaneGoPass    = "keel-demo-dev::lane::go-pass"
	idLaneGoFail    = "keel-demo-dev::lane::go-fail"
	idLaneFakeSmoke = "keel-demo-dev::lane::fake-smoke"
	idLaneSlow      = "keel-demo-dev::lane::slow-provisioning"

	idTestGoPass = "go::test::passing::TestReferencePass"
	idTestGoFail = "go::test::failing::TestReferenceFailure"

	idTestFakeEnvironment = "fake::test::provisioning::Environment"
	idTestFakeDatabase    = "fake::test::provisioning::Database"
	idTestFakeServices    = "fake::test::provisioning::Services"

	idDetectLanes    = testbridge.MaintenanceDetectLanesID
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
	idDesiredSeedCache = "keel-demo-dev::desired-state::seed-cache"
	idDataSetEmpty     = "keel-demo-dev::desired-state::dataset::empty"
	idDataSetSmall     = "keel-demo-dev::desired-state::dataset::small"
	idDataSetFull      = "keel-demo-dev::desired-state::dataset::full"

	idPreconditionsGroup = idDesired + "::group::test-preconditions"

	demoDataSetEmpty = "empty/stopped"
	demoDataSetSmall = "small"
	demoDataSetFull  = "full"
)

// demoSlowLaneMembers are the slow lane's three members. Three rather than one,
// because the behaviour the slow lane demonstrates is partial progress: members
// settle one after another while the rest are still in flight (keel/ac-581).
var demoSlowLaneMembers = []struct {
	id          string
	label       string
	description string
}{
	{id: "slow::test::provisioning::Warmup", label: "Slow warmup", description: "waits out the fake warmup step"},
	{id: "slow::test::provisioning::Migrate", label: "Slow migration", description: "waits out the fake migration step"},
	{id: "slow::test::provisioning::Verify", label: "Slow verification", description: "waits out the fake verify step"},
}

// demoSlowRunDelayDefault is the fake work time the demo's slow precondition row
// and slow lane take. It exists so a run is on screen long enough to watch:
// every other demo item settles in microseconds, which renders the transitional
// state of a run unobservable (keel/ac-580, keel/ac-581). It is fake demo time —
// it reaches no real infrastructure.
const demoSlowRunDelayDefault = 10 * time.Second

// demoSlowRunDelay is the fake work time in force. It is a variable for one
// reason: the demo's own tests shorten it, so the keel gate never waits ten
// seconds for demo content. There is no flag, environment variable, or config
// row behind it — for anyone running keel-demo-dev the slow row and the slow
// lane are always slow.
var demoSlowRunDelay = demoSlowRunDelayDefault

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	tree := demoDevCommandTree(demoBridge{})
	if err := tree.ValidateTree(); err != nil {
		fmt.Fprintln(os.Stderr, "keel-demo-dev: "+err.Error())
		return 1
	}

	cfg, words, err := cli.ParseGlobalConfig(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keel-demo-dev: "+err.Error())
		return 2
	}
	if cfg.Version {
		// DHF-REQ: keel/requirement-110
		fmt.Fprintln(os.Stdout, versionString())
		return 0
	}
	if cfg.HelpAll {
		tree.RenderAllHelp(os.Stderr)
		return 0
	}
	if cfg.HelpJSON {
		// DHF-REQ: keel/requirement-100
		if err := tree.RenderHelpJSON(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "keel-demo-dev: "+err.Error())
			return 1
		}
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

// DHF-REQ: keel/requirement-108, keel/requirement-111
func demoDevCommandTree(bridge testbridge.Bridge) *cli.CommandSpec {
	tree := testbridge.CommandSpec(bridge)
	tree.Config = cli.Config{
		Program:      "keel-demo-dev",
		Version:      versionString(),
		RootSummary:  "keel-demo-dev serves a reference consumer test bridge.",
		Usage:        "keel-demo-dev <command> [args]",
		HelpUsage:    "keel-demo-dev help [command]",
		CommandUsage: "keel-demo-dev <command> --help",
	}
	return tree
}

type demoBridge struct{}

func (demoBridge) Workspace() testbridge.Workspace {
	return testbridge.Workspace{Root: workingRoot(), Node: "keel-demo-dev", ModulePath: "github.com/david-aggeler/keel-demo-dev"}
}

func (demoBridge) Metadata() vscode.DevtoolMetadata {
	return vscode.DevtoolMetadata{Name: "keel-demo-dev", Version: versionString(), Commit: "demo", BuiltAt: "demo"}
}

func (demoBridge) ConfigTemplate() vscode.TestBridgeConfig {
	return vscode.TestBridgeConfig{
		Version:     vscode.CurrentConfigVersion,
		Command:     filepath.Join("bin", executableName()),
		Args:        []string{},
		DisplayName: "Keel Demo Dev",
	}
}

// DHF-REQ: keel/requirement-110
func versionString() string {
	return keel.Version()
}

// DHF-REQ: keel/requirement-62
func (b demoBridge) Discover(ctx context.Context) (vscode.DiscoveryDocument, error) {
	ws := b.workspace(ctx)
	items := []vscode.TestItem{
		group(idRoot, "", "Keel Demo Dev"),
		group(idDesired, idRoot, "B - Desired State"),
		group(idLanes, idRoot, "C - Lanes"),
		group(idFrameworks, idRoot, "D - Frameworks"),
		group(idGoFramework, idFrameworks, "Go"),
		group(idFakeFamily, idFrameworks, "Fake infrastructure"),
		group(idSlowFamily, idFrameworks, "Slow fake infrastructure"),
		maintenance(idBlockBadLane, idMaintenance, "block failing Go lane"),
		maintenance(idUnblockBadLane, idMaintenance, "unblock failing Go lane"),
	}
	if hasDemoLanesFile(ws.Root) {
		lanes, err := demoLanes(ws.Root)
		if err != nil {
			return vscode.DiscoveryDocument{}, err
		}
		items = append(items, lanes...)
		items = append(items,
			test(idTestGoPass, idGoFramework, "TestReferencePass", "a real Go test that passes", idLaneGoPass),
			test(idTestGoFail, idGoFramework, "TestReferenceFailure", "a real Go test that fails", idLaneGoFail),
			test(idTestFakeEnvironment, idFakeFamily, "Fake environment", "brings the fake environment up", idLaneFakeSmoke),
			test(idTestFakeDatabase, idFakeFamily, "Fake database", "seeds the fake app database", idLaneFakeSmoke),
			test(idTestFakeServices, idFakeFamily, "Fake services", "starts fake services a, b and c", idLaneFakeSmoke),
		)
		for _, member := range demoSlowLaneMembers {
			items = append(items, test(member.id, idSlowFamily, member.label, member.description, idLaneSlow))
		}
		items = append(items)
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
		},
		Items: items,
	}, nil
}

// DHF-REQ: keel/requirement-62, keel/requirement-75, keel/requirement-76, keel/requirement-88
func (b demoBridge) DesiredState(ctx context.Context, ids []string) (testbridge.DesiredStateDeclaration, error) {
	root := b.workspace(ctx).Root
	if !hasDemoLanesFile(root) || (selectedDataSetRowsOnly(ids) && !testbridge.DesiredStateReportRequested(ctx)) {
		return testbridge.DesiredStateDeclaration{
			TeardownPolicy: "demo-only fake resources; no teardown command mutates real infrastructure",
		}, nil
	}
	activeDataSet := currentDemoDataSet(root)
	return testbridge.DesiredStateDeclaration{
		Groups: []testbridge.DesiredStateGroup{
			{
				Label: "Test Preconditions",
				Order: 10,
				Rows: []testbridge.DesiredStateRow{
					prereq(root, idDesiredDockerEnv, "docker-env", "dependency", "ready", "absent", "provision_demo_environment", false),
					prereq(root, idDesiredPostgres, "postgres", "fixture-data", "present+seeded", "missing", "create_and_seed_demo_database", false),
					prereq(root, idDesiredServiceA, "service-a", "service", "running", "stopped", "start_demo_service", true),
					prereq(root, idDesiredServiceB, "service-b", "service", "running", "stopped", "start_demo_service", true),
					prereq(root, idDesiredServiceC, "service-c", "service", "running", "stopped", "start_demo_service", true),
					prereq(root, idDesiredSDK, "sdk", "tool", "installed", "missing", "install_demo_sdk", true),
					prereq(root, idDesiredDNS, "dns", "host-port-set", "resolves", "missing", "seed_demo_dns", true),
					prereq(root, idDesiredPing, "ping", "dependency", "reachable", "timeout", "probe_demo_endpoint", true),
					prereq(root, idDesiredSeedCache, "seed-cache", "fixture-data", "warmed", "cold", "warm_demo_seed_cache", false),
				},
			},
			{
				Label:             "app-db data set",
				Order:             20,
				MutuallyExclusive: true,
				Rows: []testbridge.DesiredStateRow{
					dataSet(idDataSetEmpty, "app-db-empty-stopped", demoDataSetEmpty, activeDataSet, "select_empty_stopped_data_set"),
					dataSet(idDataSetSmall, "app-db-small", demoDataSetSmall, activeDataSet, "reuse_small_data_set"),
					dataSet(idDataSetFull, "app-db-full", demoDataSetFull, activeDataSet, "select_full_data_set"),
				},
			},
		},
		TeardownPolicy: "demo-only fake resources; no teardown command mutates real infrastructure",
	}, nil
}

// ReconcileDesiredStateRow performs the seed-cache row's named action during a
// run. It is the one demo row whose action takes real time, so the Explorer
// shows a precondition row in flight rather than only its settled result
// (keel/ac-580). The work is fake: it waits, and it touches nothing.
//
// Every other row is left to the bridge's read-only path by reporting the row
// unhandled.
//
// DHF-REQ: keel/requirement-62, keel/requirement-75
func (demoBridge) ReconcileDesiredStateRow(ctx context.Context, req testbridge.DesiredStateRowRunRequest, emit vscode.RunEventWriter) (bool, int, error) {
	if req.RunID != idDesiredSeedCache {
		return false, 0, nil
	}
	emit(vscode.RunEvent{Event: "output", TestID: req.RunID, Message: "warm_demo_seed_cache is warming the fake seed cache"})
	if err := demoSleep(ctx, demoSlowRunDelay); err != nil {
		return true, 1, err
	}
	emit(vscode.RunEvent{Event: "output", TestID: req.RunID, Message: "warm_demo_seed_cache warmed the fake seed cache"})
	return true, 0, nil
}

// demoSleep waits for the demo's fake work time and reports a canceled run as
// an error rather than returning early as though the work had been done.
func demoSleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

// DHF-REQ: keel/requirement-87
func (b demoBridge) ClearState(_ context.Context, req testbridge.RunRequest, _ vscode.RunEventWriter) (int, error) {
	if err := os.RemoveAll(filepath.Join(req.Root, ".devtools", "keel-demo-dev")); err != nil {
		return 1, err
	}
	return 0, nil
}

// DHF-REQ: keel/requirement-87
func (b demoBridge) Lanes(ctx context.Context) ([]vscode.TestItem, error) {
	ws := b.workspace(ctx)
	if err := writeDemoReadyState(ws.Root); err != nil {
		return nil, err
	}
	return demoLanes(ws.Root)
}

// demoLanes builds the demo lane items and stamps the standing blocked-lane
// condition onto the lane it applies to. The condition is reported at
// discovery, where no run has taken place, because an unsatisfiable
// prerequisite holds until it is repaired — asserting a failed run for it
// would claim an outcome for a lane that never executed (keel/ac-558).
//
// DHF-REQ: keel/requirement-140
func demoLanes(root string) ([]vscode.TestItem, error) {
	items := []vscode.TestItem{
		lane(root, idLaneGoPass, idLanes, "real Go pass", "runs a real Go test module that passes", []string{"go-toolchain"}),
		lane(root, idLaneGoFail, idLanes, "real Go fail", "runs a real Go test module that fails on purpose", []string{"go-toolchain"}),
		lane(root, idLaneFakeSmoke, idLanes, "fake provisioning smoke", "walks the fake provisioning story without touching real infrastructure", []string{"demo-environment", "demo-database", "demo-services"}),
		lane(root, idLaneSlow, idLanes, "slow fake provisioning", "three fake steps, each one slow on purpose", []string{"demo-environment"}),
	}
	blocked, err := blockedLane(root)
	if err != nil {
		return nil, err
	}
	if blocked == "" {
		return items, nil
	}
	for i := range items {
		if items[i].ID != blocked {
			continue
		}
		items[i].Conditions = append(items[i].Conditions, vscode.Condition{
			Kind:    "prerequisite_unsatisfied",
			Message: blockedLaneMessage(blocked),
		})
	}
	return items, nil
}

// blockedLaneMessage is the one wording of the blocking reason, read by the
// discovery condition and by the run-scoped error alike, so the two surfaces
// cannot describe the same block differently.
func blockedLaneMessage(laneID string) string {
	return "lane blocked: " + laneID
}

// DHF-REQ: keel/requirement-87
func (b demoBridge) runOne(ctx context.Context, root, id string, emit vscode.RunEventWriter) (int, error) {
	switch id {
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
	case idDataSetEmpty:
		return selectDemoDataSet(root, id, demoDataSetEmpty, "select_empty_stopped_data_set selected empty/stopped data set", emit)
	case idDataSetSmall:
		return selectDemoDataSet(root, id, demoDataSetSmall, "reuse_small_data_set selected small data set", emit)
	case idDataSetFull:
		return selectDemoDataSet(root, id, demoDataSetFull, "select_full_data_set selected full data set", emit)
	case idLaneFakeSmoke, idTestFakeEnvironment, idTestFakeDatabase, idTestFakeServices:
		emit(vscode.RunEvent{Event: "test_started", TestID: id})
		emit(vscode.RunEvent{Event: "output", TestID: id, Message: "fake provisioning preview: environment/database/services need reconcile_during_run"})
		emit(vscode.RunEvent{Event: "passed", TestID: id, Message: "fake provisioning preview rendered"})
		return 0, nil
	case idLaneSlow:
		for _, member := range demoSlowLaneMembers {
			code, err := runSlowDemoStep(ctx, member.id, emit)
			if code != 0 || err != nil {
				return code, err
			}
		}
		return 0, nil
	case idLaneGoPass, idTestGoPass:
		return runGoLane(ctx, root, id, true, emit)
	case idLaneGoFail, idTestGoFail:
		if blocked, err := blockedLane(root); err != nil {
			return 1, err
		} else if blocked == idLaneGoFail {
			// The lane cannot execute at all, so the run-scoped surface is
			// `errored`, never `failed`: `failed` would assert an outcome for
			// a test that never ran (keel/ac-558, keel/ac-568).
			// DHF-REQ: keel/requirement-140
			emit(vscode.RunEvent{Event: "errored", TestID: idLaneGoFail, Message: blockedLaneMessage(blocked)})
			return 1, nil
		}
		return runGoLane(ctx, root, id, false, emit)
	default:
		for _, member := range demoSlowLaneMembers {
			if id == member.id {
				return runSlowDemoStep(ctx, id, emit)
			}
		}
		return 1, fmt.Errorf("unknown demo test id %q", id)
	}
}

// runSlowDemoStep runs one member of the slow lane: it announces the member,
// spends the demo's fake work time, and settles it. Every member is slow, so
// running the lane shows some members settled while others are still in flight
// (keel/ac-581).
//
// DHF-REQ: keel/requirement-62
func runSlowDemoStep(ctx context.Context, id string, emit vscode.RunEventWriter) (int, error) {
	emit(vscode.RunEvent{Event: "test_started", TestID: id})
	if err := demoSleep(ctx, demoSlowRunDelay); err != nil {
		return 1, err
	}
	emit(vscode.RunEvent{Event: "passed", TestID: id, Message: "slow fake provisioning step completed"})
	return 0, nil
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

func group(id, parent, label string) vscode.TestItem {
	return vscode.TestItem{ID: id, ParentID: parent, Label: label, Kind: "group", Runnable: false, Profiles: []string{}}
}

func maintenance(id, parent, label string) vscode.TestItem {
	return vscode.TestItem{ID: id, ParentID: parent, Label: label, Kind: "maintenance", Framework: "keel-demo-dev", Runner: "keel-demo-dev", RunnerLabel: "Keel Demo Dev", Runnable: true, Profiles: []string{"run"}}
}

// lane builds one demo lane item. root is the workspace the lane's run history
// is attributed from: keel-demo-dev has never carried a duration, and it gains
// one here from the same shared attribution keel-dev reads (keel/ac-550), which
// yields no last_run member at all when no stream is attributable to this lane
// alone (keel/ac-564).
//
// DHF-REQ: keel/requirement-138
func lane(root, id, parent, label, description string, resources []string) vscode.TestItem {
	return vscode.TestItem{
		ID:                id,
		ParentID:          parent,
		Label:             label,
		Kind:              "lane",
		Framework:         "keel-demo-dev",
		Runner:            "keel-demo-dev",
		RunnerLabel:       "Keel Demo Dev",
		Runnable:          true,
		Profiles:          []string{"run"},
		LaneID:            id,
		RequiredResources: resources,
		Description:       description,
		LastRun:           vscode.LatestLaneRun(root, id).Facts(),
	}
}

// test builds one demo test item. Every runnable item the demo serves carries a
// description of its own, bounded to forty characters, so the
// tree shows per-item prose rather than a family-wide sentence (keel/ac-583).
//
// DHF-REQ: keel/requirement-62
func test(id, parent, label, description, laneID string) vscode.TestItem {
	return vscode.TestItem{ID: id, ParentID: parent, Label: label, Kind: "test", Framework: "keel-demo-dev", Runner: "keel-demo-dev", RunnerLabel: "Keel Demo Dev", Runnable: true, Profiles: []string{"run"}, LaneID: laneID, Description: description}
}

func prereq(root, runID, resource, kind, want, missing, actionName string, reusable bool) testbridge.DesiredStateRow {
	return testbridge.DesiredStateRow{
		RunID:    runID,
		Resource: resource,
		Kind:     kind,
		Desired:  want,
		Reusable: reusable,
		Owned:    !reusable,
		Probe: func(context.Context, testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
			if demoPrereqReady(root, resource) {
				return testbridge.DesiredStateProbeResult{
					Current:   want,
					Satisfied: true,
					Message:   "named action " + actionName + " verified this fake resource from workspace state",
				}
			}
			return testbridge.DesiredStateProbeResult{
				Current:   missing,
				Satisfied: false,
				Message:   "named action " + actionName + " would reconcile this fake resource during a demo run",
			}
		},
	}
}

func dataSet(runID, resource, want, activeDataSet, actionName string) testbridge.DesiredStateRow {
	return testbridge.DesiredStateRow{
		RunID:    runID,
		Resource: resource,
		Kind:     "fixture-data",
		Desired:  want,
		Owned:    true,
		// The active fact is derived from this probe (keel/requirement-75); the
		// demo declares only the resource, its desired value, and how to observe
		// it.
		Probe: func(context.Context, testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
			return testbridge.DesiredStateProbeResult{
				Current:   activeDataSet,
				Satisfied: activeDataSet == want,
				Message:   "named action " + actionName + " selected the active fake data set",
			}
		},
	}
}

func blockStatePath(root string) string {
	return filepath.Join(root, ".devtools", "keel-demo-dev", "blocked-lane.json")
}

func demoLanesPath(root string) string {
	return filepath.Join(root, ".vscode", "test-lanes.json")
}

func demoReadyPath(root, resource string) string {
	return filepath.Join(root, ".devtools", "keel-demo-dev", "ready", resource)
}

func demoDataSetPath(root string) string {
	return filepath.Join(root, ".devtools", "keel-demo-dev", "active-data-set")
}

func hasDemoLanesFile(root string) bool {
	_, err := os.Stat(demoLanesPath(root))
	return err == nil
}

func writeDemoReadyState(root string) error {
	for _, resource := range []string{"docker-env", "postgres", "service-a", "service-b", "service-c", "sdk", "dns", "ping", "seed-cache"} {
		if err := writeDemoPrereqReady(root, resource); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(demoDataSetPath(root)), 0o755); err != nil {
		return err
	}
	return nil
}

func writeDemoPrereqReady(root, resource string) error {
	path := demoReadyPath(root, resource)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("ready\n"), 0o644)
}

func demoPrereqReady(root, resource string) bool {
	_, err := os.Stat(demoReadyPath(root, resource))
	return err == nil
}

func selectedDataSetRowsOnly(ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		switch id {
		case idDataSetEmpty, idDataSetSmall, idDataSetFull:
		default:
			return false
		}
	}
	return true
}

func currentDemoDataSet(root string) string {
	data, err := os.ReadFile(demoDataSetPath(root))
	if err != nil {
		return ""
	}
	switch strings.TrimSpace(string(data)) {
	case demoDataSetEmpty:
		return demoDataSetEmpty
	case demoDataSetSmall:
		return demoDataSetSmall
	case demoDataSetFull:
		return demoDataSetFull
	default:
		return ""
	}
}

// ResetExclusiveGroup deactivates the active app-db data set so the next
// derivation yields the Unknown State reset peer as the sole active row.
//
// DHF-REQ: keel/requirement-98
func (b demoBridge) ResetExclusiveGroup(_ context.Context, req testbridge.ExclusiveResetRequest, emit vscode.RunEventWriter) (int, error) {
	if err := os.Remove(demoDataSetPath(req.Root)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 1, err
	}
	emit(vscode.RunEvent{Event: "output", TestID: req.RunID, Message: "cleared active app-db data set (" + req.GroupLabel + ")"})
	return 0, nil
}

func selectDemoDataSet(root, id, value, message string, emit vscode.RunEventWriter) (int, error) {
	if err := os.MkdirAll(filepath.Dir(demoDataSetPath(root)), 0o755); err != nil {
		return 1, err
	}
	if err := os.WriteFile(demoDataSetPath(root), []byte(value+"\n"), 0o644); err != nil {
		return 1, err
	}
	emit(vscode.RunEvent{Event: "test_started", TestID: id})
	emit(vscode.RunEvent{Event: "passed", TestID: id, Message: message})
	return 0, nil
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
