package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/david-aggeler/keel/cli"
	"github.com/david-aggeler/keel/vscode"
)

const (
	vscodeGroupMaintenance = "keel::maintenance"
	vscodeGroupLanes       = "keel::lanes"
	vscodeGroupFrameworks  = "keel::frameworks"

	vscodeMaintenanceUnlock       = "keel::maintenance::unlock"
	vscodeMaintenanceDetectLanes  = "keel::maintenance::detect-lanes"
	vscodeMaintenanceClearResults = "keel::maintenance::clear-results"
	vscodeMaintenanceClearState   = "keel::maintenance::clear-state"

	vscodeLaneLint         = "keel::lane::lint"
	vscodeLaneTestFast     = "keel::lane::test-fast"
	vscodeLaneTestCoverage = "keel::lane::test-coverage"
	vscodeLaneVSIXGate     = "keel::lane::vsix-ci"
	vscodeLaneCI           = "keel::lane::ci"
)

var vscodeLaneIDs = []string{vscodeLaneLint, vscodeLaneTestFast, vscodeLaneTestCoverage, vscodeLaneVSIXGate, vscodeLaneCI}

type testLanesFile struct {
	Version int            `json:"version"`
	Lanes   []testFileLane `json:"lanes"`
}

type testFileLane struct {
	ID            string       `json:"id"`
	Label         string       `json:"label"`
	Order         string       `json:"order"`
	Description   string       `json:"description"`
	Members       []laneMember `json:"members"`
	Prerequisites []string     `json:"prerequisites"`
}

type laneMember struct {
	Go      string
	Root    string
	VSIX    string
	Lane    string
	rawKeys []string
}

func (m *laneMember) UnmarshalJSON(data []byte) error {
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.rawKeys = m.rawKeys[:0]
	for key, value := range raw {
		m.rawKeys = append(m.rawKeys, key)
		switch key {
		case "go":
			m.Go = value
		case "root":
			m.Root = value
		case "vsix":
			m.VSIX = value
		case "lane":
			m.Lane = value
		}
	}
	sort.Strings(m.rawKeys)
	return nil
}

type laneFinding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type effectiveLane struct {
	lane          testFileLane
	id            string
	goPackages    []string
	vsixFiles     []string
	systemLanes   []string
	laneRefs      []string
	prerequisites []string
	findings      []laneFinding
}

type lanesState struct {
	root         string
	path         string
	file         testLanesFile
	byID         map[string]testFileLane
	effective    map[string]effectiveLane
	diagnostics  []vscode.TestItem
	wholeFileErr error
}

type lanesListDocument struct {
	Version     int              `json:"version"`
	Workspace   string           `json:"workspace"`
	GeneratedAt time.Time        `json:"generated_at"`
	Lanes       []laneListRecord `json:"lanes"`
}

type laneListRecord struct {
	ID            string                `json:"id"`
	Source        string                `json:"source"`
	Label         string                `json:"label"`
	Order         string                `json:"order"`
	Description   string                `json:"description,omitempty"`
	Members       []laneMemberListEntry `json:"members"`
	Expanded      laneExpandedMembers   `json:"expanded"`
	Prerequisites []string              `json:"prerequisites"`
	Findings      []laneFinding         `json:"findings"`
	LastRun       *laneLastRun          `json:"last_run"`
}

type laneMemberListEntry map[string]string

type laneExpandedMembers struct {
	GoPackages  []string `json:"go_packages"`
	VSIXFiles   []string `json:"vsix_files"`
	SystemLanes []string `json:"system_lanes"`
	LaneRefs    []string `json:"lane_refs"`
}

type laneLastRun struct {
	RunID      string    `json:"run_id"`
	At         time.Time `json:"at"`
	DurationMS int64     `json:"duration_ms"`
	ExitCode   int       `json:"exit_code"`
}

type lanesDetectDocument struct {
	Version   int                `json:"version"`
	File      string             `json:"file"`
	Written   bool               `json:"written"`
	Added     []lanesDetectEntry `json:"added"`
	Unchanged []lanesDetectEntry `json:"unchanged"`
	Skipped   []lanesDetectEntry `json:"skipped"`
}

type lanesDetectEntry struct {
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
	Order    string `json:"order,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Packages int    `json:"packages,omitempty"`
}

func vscodeCommandSpec() *cli.CommandSpec {
	return &cli.CommandSpec{
		Name:  "vscode",
		Short: "Emit VS Code test-runner protocol documents.",
		Subcommands: []*cli.CommandSpec{
			{
				Name:  "config",
				Short: "Initialize or upgrade VS Code test bridge config.",
				Subcommands: []*cli.CommandSpec{
					{Name: "init", Use: "vscode config init", Short: "Write .vscode/test-bridge.json if absent.", Handler: handleVSCodeConfigInit},
					{Name: "upgrade", Use: "vscode config upgrade", Short: "Upgrade .vscode/test-bridge.json to the current schema.", Handler: handleVSCodeConfigUpgrade},
				},
			},
			{
				Name:  "tests",
				Short: "VS Code test discovery, setup planning, and lane runs.",
				Subcommands: []*cli.CommandSpec{
					{Name: "discover", Use: "vscode tests discover [--format json]", Short: "Emit the VS Code discovery document.", Flags: []cli.FlagSpec{{Name: "format", Value: "json", Short: "Output format."}}, Handler: handleVSCodeTestsDiscover},
					{Name: "plan", Use: "vscode tests plan [--format json] [--id test-id]", Short: "Emit the VS Code setup plan.", Flags: []cli.FlagSpec{{Name: "format", Value: "json", Short: "Output format."}, {Name: "id", Value: "test-id", Short: "Selected test id."}}, Handler: handleVSCodeTestsPlan},
					{Name: "run", Use: "vscode tests run --id test-id", Short: "Run selected VS Code test lanes.", Flags: []cli.FlagSpec{{Name: "id", Value: "test-id", Short: "Selected test id."}}, Handler: handleVSCodeTestsRun},
				},
			},
			{
				Name:  "lanes",
				Short: "List or detect data-driven test lanes.",
				Subcommands: []*cli.CommandSpec{
					{Name: "list", Use: "vscode lanes list [--format json]", Short: "Emit effective lane definitions.", Flags: []cli.FlagSpec{{Name: "format", Value: "json", Short: "Output format."}}, Handler: handleVSCodeLanesList},
					{Name: "detect", Use: "vscode lanes detect [--format json] [--dry-run]", Short: "Append detected category lanes.", Flags: []cli.FlagSpec{{Name: "format", Value: "json", Short: "Output format."}, {Name: "dry-run", Short: "Report without writing."}}, Handler: handleVSCodeLanesDetect},
				},
			},
			{
				Name:  "demo",
				Short: "Persist VS Code demo-block lane state.",
				Subcommands: []*cli.CommandSpec{
					{Name: "block", Use: "vscode demo block <lane-id>", Short: "Persist a synthetic block for a VS Code lane.", Handler: handleVSCodeDemoBlock},
					{Name: "unblock", Use: "vscode demo unblock", Short: "Clear persisted demo-block state.", Handler: handleVSCodeDemoUnblock},
					{Name: "status", Use: "vscode demo status", Short: "Emit the current demo-block state as JSON.", Handler: handleVSCodeDemoStatus},
				},
			},
		},
	}
}

// DHF-REQ: keel/requirement-40
func handleVSCodeConfigInit(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return cli.NewUsageError("vscode config init takes no arguments: got %q", args)
	}
	state := stateFrom(ctx)
	res, err := vscode.InitTestBridgeConfig(state.root)
	if err != nil {
		return err
	}
	if state.logger != nil {
		state.logger.Info("vscode config init", "path", res.Path, "changed", res.Changed, "version", res.ToVersion)
	}
	return nil
}

// DHF-REQ: keel/requirement-40
func handleVSCodeConfigUpgrade(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return cli.NewUsageError("vscode config upgrade takes no arguments: got %q", args)
	}
	state := stateFrom(ctx)
	res, err := vscode.UpgradeTestBridgeConfig(state.root)
	if err != nil {
		return err
	}
	if state.logger != nil {
		state.logger.Info("vscode config upgrade", "path", res.Path, "changed", res.Changed, "from_version", res.FromVersion, "to_version", res.ToVersion)
	}
	return nil
}

func handleVSCodeTestsDiscover(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	if err := rejectUnsupportedFormat(args); err != nil {
		return err
	}
	return writeVSCodeDiscovery(state.root, state.protocol)
}

func handleVSCodeTestsPlan(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	ids, err := parseVSCodeIDs(args, true)
	if err != nil {
		return err
	}
	return writeVSCodePlan(state.root, ids, state.protocol)
}

func handleVSCodeLanesList(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	if err := rejectUnsupportedFormat(args); err != nil {
		return err
	}
	return writeVSCodeLanesList(state.root, state.protocol)
}

func handleVSCodeLanesDetect(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	dryRun, err := parseVSCodeLanesDetectArgs(args)
	if err != nil {
		return err
	}
	return writeVSCodeLanesDetect(state.root, dryRun, state.protocol)
}

type vscodeDemoBlockState struct {
	BlockedLane string    `json:"blocked_lane"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type vscodeDemoBlockStatus struct {
	BlockedLane string `json:"blocked_lane,omitempty"`
	Source      string `json:"source"`
	Path        string `json:"path"`
}

// DHF-REQ: keel/requirement-41
func handleVSCodeDemoBlock(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return cli.NewUsageError("vscode demo block requires exactly one lane id")
	}
	laneID := args[0]
	if !knownVSCodeLaneID(laneID) {
		return cli.NewUsageError("unknown vscode lane id %q", laneID)
	}
	state := stateFrom(ctx)
	if err := writeVSCodeDemoBlockState(state.root, laneID); err != nil {
		return err
	}
	if state.logger != nil {
		state.logger.Info("vscode demo block", "lane_id", laneID, "path", vscodeDemoBlockStatePath(state.root))
	}
	return nil
}

// DHF-REQ: keel/requirement-41
func handleVSCodeDemoUnblock(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return cli.NewUsageError("vscode demo unblock takes no arguments: got %q", args)
	}
	state := stateFrom(ctx)
	if err := os.Remove(vscodeDemoBlockStatePath(state.root)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if state.logger != nil {
		state.logger.Info("vscode demo unblock", "path", vscodeDemoBlockStatePath(state.root))
	}
	return nil
}

// DHF-REQ: keel/requirement-41
func handleVSCodeDemoStatus(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return cli.NewUsageError("vscode demo status takes no arguments: got %q", args)
	}
	state := stateFrom(ctx)
	return encodeProtocolDocument(state.protocol, currentVSCodeDemoBlockStatus(state.root))
}

// DHF-REQ: keel/requirement-35, keel/requirement-36, keel/requirement-37
func handleVSCodeTestsRun(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	ids, err := parseVSCodeIDs(args, false)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return cli.NewUsageError("vscode tests run requires at least one --id")
	}

	runID := newVSCodeRunID()
	profile := newKeelWorkspaceProfile(state.root)
	attr := vscode.NewRunAttribution(profile, runID)
	if state.logger != nil {
		state.logger.Info(attr.LogLine())
	}
	stamper := vscode.EventStamper{
		Now:       time.Now,
		RunID:     runID,
		Source:    "vscode",
		Workspace: attr.Node,
		Logf:      func(message string) { state.logger.Warn("vscode protocol event rejected", "detail", message) },
	}
	writer, closeWriter, err := newVSCodeRunWriter(state.root, state.protocol, stamper, state.logger)
	if err != nil {
		return err
	}
	defer closeWriter()

	finished := false
	exitCode := 1
	defer func() {
		if recovered := recover(); recovered != nil {
			writer(vscode.RunEvent{Event: "errored", Message: fmt.Sprintf("panic: %v", recovered)})
			writer(vscode.RunEvent{Event: "run_finished", Message: "panic during vscode run", ExitCode: &exitCode})
			panic(recovered)
		}
		if !finished {
			writer(vscode.RunEvent{Event: "errored", Message: "run ended before terminal event"})
			writer(vscode.RunEvent{Event: "run_finished", Message: "run ended before terminal event", ExitCode: &exitCode})
		}
	}()

	writer(vscode.RunEvent{Event: "run_started", Live: boolPtr(true), Requested: runRequestsForIDs(state.root, ids)})

	if len(ids) == 1 && ids[0] == vscodeMaintenanceUnlock {
		exitCode, err = runVSCodeMaintenance(state.root, ids[0])
		if err != nil {
			writer(vscode.RunEvent{Event: "errored", Message: err.Error()})
			writer(vscode.RunEvent{Event: "run_finished", Message: err.Error(), ExitCode: &exitCode})
			finished = true
			return vscodeRunError{exitCode: exitCode, msg: err.Error()}
		}
		writer(vscode.RunEvent{Event: "passed", TestID: ids[0]})
		writer(vscode.RunEvent{Event: "run_finished", ExitCode: &exitCode})
		finished = true
		return nil
	}

	releaseLock, err := acquireVSCodeRunLock(state.root, ids, runID)
	if err != nil {
		writer(vscode.RunEvent{Event: "errored", Message: err.Error()})
		writer(vscode.RunEvent{Event: "run_finished", Message: err.Error(), ExitCode: &exitCode})
		finished = true
		return vscodeRunError{exitCode: 1, msg: err.Error()}
	}
	defer func() {
		if err := releaseLock(); err != nil && state.logger != nil {
			state.logger.Warn("release vscode run lock", "error", err.Error())
		}
	}()

	laneID := laneForIDs(ids)
	if ready := vscode.NewEngine(profile).Prepare(ctx, laneID, ids, writer); !ready {
		finished = true
		return vscodeRunError{exitCode: 1, msg: "vscode lane blocked"}
	}

	exitCode, err = runVSCodeLane(ctx, state.logger, state.root, laneID, runID, writer)
	if err != nil {
		writer(vscode.RunEvent{Event: "errored", Message: err.Error()})
		writer(vscode.RunEvent{Event: "run_finished", Message: err.Error(), ExitCode: &exitCode})
		finished = true
		return vscodeRunError{exitCode: exitCode, msg: err.Error()}
	}
	writer(vscode.RunEvent{Event: "passed", TestID: laneID})
	writer(vscode.RunEvent{Event: "run_finished", ExitCode: &exitCode})
	finished = true
	return nil
}

// DHF-REQ: keel/requirement-39, keel/requirement-43, keel/requirement-46, keel/requirement-48, keel/requirement-51
func writeVSCodeDiscovery(root string, out io.Writer) error {
	items := []vscode.TestItem{
		groupItem(vscodeGroupMaintenance, "", "a. Maintenance", "a"),
		groupItem(vscodeGroupLanes, "", "b. Lanes", "b"),
		groupItem(vscodeGroupFrameworks, "", "d. Frameworks", "d"),
		maintenanceItem(vscodeMaintenanceDetectLanes, "a.1 detect lanes", ordinalSortText("a.1")),
		maintenanceItem(vscodeMaintenanceUnlock, "a.2 unlock test bridge", ordinalSortText("a.2")),
		maintenanceItem(vscodeMaintenanceClearResults, "a.3 clear test results", ordinalSortText("a.3")),
		maintenanceItem(vscodeMaintenanceClearState, "a.4 clear local test state", ordinalSortText("a.4")),
		laneItem(vscodeLaneLint, "b.1 lint", ordinalSortText("b.1")),
		laneItem(vscodeLaneTestFast, "b.2 test-fast", ordinalSortText("b.2")),
		laneItem(vscodeLaneTestCoverage, "b.3 test-coverage", ordinalSortText("b.3")),
		laneItem(vscodeLaneVSIXGate, "b.10 vsix ci", ordinalSortText("b.10")),
		laneItem(vscodeLaneCI, "b.30 ci", ordinalSortText("b.30")),
	}
	goItems, err := discoverGoTestItems(context.Background(), root)
	if err != nil {
		return err
	}
	lanes, err := loadLanesState(root)
	if err != nil {
		return err
	}
	items = append(items, lanes.discoveryItems()...)
	items = append(items, goItems...)
	vsixItems, err := discoverVSIXTestItems(root)
	if err != nil {
		return err
	}
	items = append(items, vsixItems...)
	doc := vscode.DiscoveryDocument{
		Version:     1,
		Workspace:   workspaceNode(root),
		ModulePath:  modulePath,
		GeneratedAt: time.Now().UTC(),
		Capabilities: vscode.DiscoveryCapabilities{
			ClearResults:              true,
			RefreshInvalidatesResults: true,
			NeutralParentRollups:      true,
			ClearResultsTestIDs:       []string{vscodeMaintenanceClearResults},
			ClearStateTestIDs:         []string{vscodeMaintenanceClearState},
		},
		Items: items,
	}
	return encodeProtocolDocument(out, doc)
}

func groupItem(id, parentID, label, sortText string) vscode.TestItem {
	return vscode.TestItem{
		ID:       id,
		ParentID: parentID,
		Label:    label,
		SortText: sortText,
		Kind:     "group",
		Runnable: false,
		Profiles: []string{},
	}
}

func maintenanceItem(id, label, sortText string) vscode.TestItem {
	return vscode.TestItem{
		ID:          id,
		ParentID:    vscodeGroupMaintenance,
		Label:       label,
		SortText:    sortText,
		Kind:        "maintenance",
		Framework:   "keel",
		Runner:      "keel-dev",
		RunnerLabel: "keel-dev",
		Runnable:    true,
		Profiles:    []string{"run"},
	}
}

func writeVSCodePlan(root string, ids []string, out io.Writer) error {
	profile := newKeelWorkspaceProfile(root)
	_, goErr := exec.LookPath("go")
	goReady := goErr == nil
	plan := vscode.SetupPlan{
		Version:     1,
		Devtool:     vscode.DevtoolMetadata{Name: "keel-dev", Version: versionString(), Commit: buildCommit()},
		Workspace:   profile.Node(),
		GeneratedAt: time.Now().UTC(),
		Items:       selectedPlanItems(ids),
		RequiredResources: []string{
			"go-toolchain",
			"keel-module-root",
			"stub-binaries",
		},
		DesiredState: []vscode.DesiredState{
			{Resource: "go-toolchain", Kind: "tool", Desired: "available", Current: statusWord(goReady), Status: statusWord(goReady), Action: "install-go", Message: "Go toolchain is required.", Reusable: true, Owned: false},
			{Resource: "keel-module-root", Kind: "workspace", Desired: modulePath, Current: modulePath, Status: "ready", Action: "none", Message: "keel module root resolved.", Reusable: true, Owned: false},
			{Resource: "stub-binaries", Kind: "build", Desired: "buildable", Current: "checked-by-ci", Status: "ready", Action: "none", Message: "stub binaries are built by keel-dev ci.", Reusable: true, Owned: false},
		},
		Checks: []vscode.PrereqCheck{
			{ID: "go-toolchain", OK: goReady, Message: "go is on PATH"},
			{ID: "keel-module-root", OK: root != "", Message: "keel module root resolved", Detail: root},
			{ID: "stub-binaries", OK: true, Message: "stub binaries build in the gate"},
		},
		Actions: []vscode.SetupPlanAction{
			{Resource: "go-toolchain", Status: statusWord(goReady), Message: "Install Go if this check is blocked.", Reusable: true, Owned: false},
		},
		Teardown: vscode.SetupPlanTeardown{Policy: "none"},
	}
	return encodeProtocolDocument(out, plan)
}

// DHF-REQ: keel/requirement-52
func writeVSCodeLanesList(root string, out io.Writer) error {
	lanes, err := loadLanesState(root)
	if err != nil {
		return err
	}
	doc := lanesListDocument{
		Version:     1,
		Workspace:   workspaceNode(root),
		GeneratedAt: time.Now().UTC(),
	}
	for _, id := range vscodeLaneIDs {
		short := strings.TrimPrefix(id, "keel::lane::")
		doc.Lanes = append(doc.Lanes, laneListRecord{
			ID:            id,
			Source:        "system",
			Label:         short,
			Order:         systemLaneOrder(id),
			Members:       []laneMemberListEntry{},
			Expanded:      laneExpandedMembers{},
			Prerequisites: laneRequiredResources(id),
			Findings:      []laneFinding{},
			LastRun:       latestLaneRun(root, id),
		})
	}
	ids := make([]string, 0, len(lanes.effective))
	for id := range lanes.effective {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := lanes.effective[ids[i]], lanes.effective[ids[j]]
		if a.lane.Order == b.lane.Order {
			return a.lane.ID < b.lane.ID
		}
		return ordinalSortText(a.lane.Order) < ordinalSortText(b.lane.Order)
	})
	for _, id := range ids {
		eff := lanes.effective[id]
		doc.Lanes = append(doc.Lanes, laneListRecord{
			ID:          eff.id,
			Source:      "file",
			Label:       eff.lane.Label,
			Order:       eff.lane.Order,
			Description: eff.lane.Description,
			Members:     laneMembersForList(eff.lane.Members),
			Expanded: laneExpandedMembers{
				GoPackages:  eff.goPackages,
				VSIXFiles:   eff.vsixFiles,
				SystemLanes: eff.systemLanes,
				LaneRefs:    eff.laneRefs,
			},
			Prerequisites: eff.prerequisites,
			Findings:      eff.findings,
			LastRun:       latestLaneRun(root, eff.id),
		})
	}
	return encodeProtocolDocument(out, doc)
}

// DHF-REQ: keel/requirement-52
func writeVSCodeLanesDetect(root string, dryRun bool, out io.Writer) error {
	lanes, err := loadLanesState(root)
	if err != nil {
		return err
	}
	if lanes.wholeFileErr != nil {
		return lanes.wholeFileErr
	}
	families, err := detectGoFamilies(root)
	if err != nil {
		return err
	}
	covered := map[string]bool{}
	for _, eff := range lanes.effective {
		for _, pkg := range eff.goPackages {
			family := goPackageFamily(pkg)
			if family != "" {
				covered[family] = true
			}
		}
	}
	doc := lanesDetectDocument{Version: 1, File: ".vscode/test-lanes.json"}
	nextSlot := nextDetectionOrderSlot(lanes.file.Lanes)
	for _, family := range sortedKeys(families) {
		id := "go-" + family
		if _, exists := lanes.byID[id]; exists {
			doc.Unchanged = append(doc.Unchanged, lanesDetectEntry{ID: id, Reason: "lane id already declared"})
			continue
		}
		if covered[family] {
			doc.Skipped = append(doc.Skipped, lanesDetectEntry{ID: id, Reason: "covered by existing lane"})
			continue
		}
		order := fmt.Sprintf("b.%d", nextSlot)
		nextSlot++
		doc.Added = append(doc.Added, lanesDetectEntry{ID: id, Label: family, Order: order, Packages: familyPackageCount(root, family)})
	}
	if len(doc.Added) > 0 && !dryRun {
		updated := lanes.file
		if updated.Version == 0 {
			updated.Version = 1
		}
		for _, added := range doc.Added {
			updated.Lanes = append(updated.Lanes, testFileLane{
				ID:          added.ID,
				Label:       added.Label,
				Order:       added.Order,
				Description: fmt.Sprintf("detected category - %d packages", added.Packages),
				Members:     []laneMember{{Go: "./" + added.Label + "/...", rawKeys: []string{"go"}}},
			})
		}
		data, err := json.MarshalIndent(updated, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(lanes.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(lanes.path, append(data, '\n'), 0o644); err != nil {
			return err
		}
		doc.Written = true
	}
	return encodeProtocolDocument(out, doc)
}

func laneItem(id, label, sortText string) vscode.TestItem {
	profiles := []string{"run"}
	if id == vscodeLaneTestCoverage {
		profiles = []string{"coverage"}
	}
	return vscode.TestItem{
		ID:                id,
		ParentID:          vscodeGroupLanes,
		Label:             label,
		SortText:          sortText,
		Kind:              "lane",
		Framework:         "go",
		Runner:            "keel-dev",
		RunnerLabel:       "keel-dev",
		Runnable:          true,
		Profiles:          profiles,
		LaneID:            id,
		RequiredResources: laneRequiredResources(id),
	}
}

func laneRequiredResources(id string) []string {
	resources := []string{"go-toolchain", "keel-module-root", "stub-binaries"}
	if id == vscodeLaneVSIXGate {
		resources = append(resources, "pnpm")
	}
	return resources
}

// DHF-REQ: keel/requirement-51, keel/requirement-52, keel/requirement-54
func loadLanesState(root string) (lanesState, error) {
	state := lanesState{
		root:      root,
		path:      filepath.Join(root, ".vscode", "test-lanes.json"),
		byID:      map[string]testFileLane{},
		effective: map[string]effectiveLane{},
	}
	data, err := os.ReadFile(state.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		state.wholeFileErr = err
		state.diagnostics = append(state.diagnostics, lanesDiagnosticItem("lanes-file", err.Error()))
		return state, nil
	}
	if err := json.Unmarshal(data, &state.file); err != nil {
		state.wholeFileErr = err
		state.diagnostics = append(state.diagnostics, lanesDiagnosticItem("lanes-file", err.Error()))
		return state, nil
	}
	if state.file.Version != 1 {
		state.wholeFileErr = fmt.Errorf("unsupported test-lanes.json version %d", state.file.Version)
		state.diagnostics = append(state.diagnostics, lanesDiagnosticItem("lanes-file", state.wholeFileErr.Error()))
		return state, nil
	}
	seen := map[string]bool{}
	for _, lane := range state.file.Lanes {
		if lane.ID == "" {
			state.diagnostics = append(state.diagnostics, lanesDiagnosticItem("lane-missing-id", "lane id is required"))
			continue
		}
		if seen[lane.ID] || knownSystemLaneShortID(lane.ID) {
			state.diagnostics = append(state.diagnostics, lanesDiagnosticItem(lane.ID, "duplicate lane id "+lane.ID))
			continue
		}
		seen[lane.ID] = true
		state.byID[lane.ID] = lane
	}
	for id := range state.byID {
		if _, err := state.expand(id, nil, 0); err != nil {
			state.diagnostics = append(state.diagnostics, lanesDiagnosticItem(id, err.Error()))
		}
	}
	return state, nil
}

func (s *lanesState) expand(id string, stack []string, depth int) (effectiveLane, error) {
	if got, ok := s.effective[id]; ok {
		return got, nil
	}
	lane, ok := s.byID[id]
	if !ok {
		return effectiveLane{}, fmt.Errorf("unresolved lane ref %q", id)
	}
	if depth > 8 {
		path := append(append([]string{}, stack...), id)
		return effectiveLane{}, fmt.Errorf("lane composition depth > 8: %s", strings.Join(path, " -> "))
	}
	for _, seen := range stack {
		if seen == id {
			path := append(append([]string{}, stack...), id)
			return effectiveLane{}, fmt.Errorf("lane composition cycle: %s", strings.Join(path, " -> "))
		}
	}
	if lane.Label == "" || lane.Order == "" || len(lane.Members) == 0 {
		return effectiveLane{}, fmt.Errorf("lane %q missing required label, order, or members", id)
	}
	eff := effectiveLane{lane: lane, id: "keel::lane::" + id}
	prereq := map[string]bool{}
	for _, resource := range lane.Prerequisites {
		prereq[resource] = true
	}
	pkgSet := map[string]bool{}
	systemSet := map[string]bool{}
	vsixSet := map[string]bool{}
	for _, member := range lane.Members {
		switch {
		case len(member.rawKeys) != 1:
			return effectiveLane{}, fmt.Errorf("unknown member form in lane %q", id)
		case member.Go != "":
			for _, pkg := range packagesForGoPattern(s.path, member.Go) {
				pkgSet[pkg] = true
			}
			prereq["go-toolchain"] = true
			prereq["keel-module-root"] = true
			if len(packagesForGoPattern(s.path, member.Go)) == 0 {
				eff.findings = append(eff.findings, laneFinding{Rule: "V6", Severity: "warning", Message: "go member matches no test-bearing packages: " + member.Go})
			}
		case member.Root != "":
			switch member.Root {
			case "go":
				for _, pkg := range packagesForGoPattern(s.path, "./...") {
					pkgSet[pkg] = true
				}
				prereq["go-toolchain"] = true
				prereq["keel-module-root"] = true
			case "vsix":
				prereq["pnpm"] = true
			default:
				return effectiveLane{}, fmt.Errorf("unknown root member %q in lane %q", member.Root, id)
			}
		case member.VSIX != "":
			vsixSet[filepath.ToSlash(member.VSIX)] = true
			prereq["pnpm"] = true
			if !vsixTestFileExists(filepath.Dir(filepath.Dir(s.path)), member.VSIX) {
				eff.findings = append(eff.findings, laneFinding{Rule: "V10", Severity: "warning", Message: "vsix test file not found: " + member.VSIX})
			}
		case member.Lane != "":
			if short, ok := systemLaneShortID(member.Lane); ok {
				if !systemSet[short] {
					eff.systemLanes = append(eff.systemLanes, "keel::lane::"+short)
					systemSet[short] = true
				}
				for _, resource := range laneRequiredResources("keel::lane::" + short) {
					prereq[resource] = true
				}
				continue
			}
			child, err := s.expand(member.Lane, append(stack, id), depth+1)
			if err != nil {
				return effectiveLane{}, err
			}
			eff.laneRefs = append(eff.laneRefs, "keel::lane::"+member.Lane)
			for _, pkg := range child.goPackages {
				pkgSet[pkg] = true
			}
			for _, sys := range child.systemLanes {
				if !systemSet[sys] {
					eff.systemLanes = append(eff.systemLanes, sys)
					systemSet[sys] = true
				}
			}
			for _, file := range child.vsixFiles {
				vsixSet[file] = true
			}
			for _, resource := range child.prerequisites {
				prereq[resource] = true
			}
		default:
			return effectiveLane{}, fmt.Errorf("unknown member form in lane %q", id)
		}
	}
	eff.goPackages = sortedKeys(pkgSet)
	eff.vsixFiles = sortedKeys(vsixSet)
	eff.prerequisites = orderedResources(prereq)
	s.effective[id] = eff
	return eff, nil
}

func (s lanesState) discoveryItems() []vscode.TestItem {
	items := append([]vscode.TestItem{}, s.diagnostics...)
	if s.wholeFileErr != nil {
		return items
	}
	ids := make([]string, 0, len(s.effective))
	for id := range s.effective {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.effective[ids[i]], s.effective[ids[j]]
		if a.lane.Order == b.lane.Order {
			return a.lane.ID < b.lane.ID
		}
		return ordinalSortText(a.lane.Order) < ordinalSortText(b.lane.Order)
	})
	for _, id := range ids {
		eff := s.effective[id]
		item := laneItem(eff.id, eff.lane.Order+" "+eff.lane.Label, ordinalSortText(eff.lane.Order))
		item.RequiredResources = eff.prerequisites
		if eff.lane.Description != "" {
			item.Limitations = append(item.Limitations, eff.lane.Description)
		}
		if hint := laneDurationHint(latestLaneRun(s.root, eff.id)); hint != "" {
			item.Limitations = append(item.Limitations, hint)
		}
		for _, finding := range eff.findings {
			item.Limitations = append(item.Limitations, finding.Rule+" "+finding.Severity+": "+finding.Message)
		}
		items = append(items, item)
		items = append(items, s.coverItems(eff)...)
	}
	return items
}

// DHF-REQ: keel/requirement-54
func (s lanesState) coverItems(eff effectiveLane) []vscode.TestItem {
	if len(eff.goPackages) == 0 && len(eff.vsixFiles) == 0 && len(eff.laneRefs) == 0 {
		return nil
	}
	coversID := eff.id + "::covers"
	items := []vscode.TestItem{{
		ID:       coversID,
		ParentID: eff.id,
		Label:    "covers",
		Kind:     "group",
		Runnable: false,
		Profiles: []string{},
	}}
	seen := map[string]bool{}
	addAlias := func(canonicalID, label, kind string) {
		if canonicalID == "" || seen[canonicalID] {
			return
		}
		seen[canonicalID] = true
		items = append(items, vscode.TestItem{
			ID:          coversID + "::" + StableIDSegment(canonicalID),
			ParentID:    coversID,
			Label:       label,
			Kind:        kind,
			Runnable:    false,
			Profiles:    []string{},
			CanonicalID: canonicalID,
		})
	}
	packages, _ := parseGoTestPackages(s.root)
	byPkg := map[string]discoveredGoPackage{}
	for _, pkg := range packages {
		byPkg[pkg.rel] = pkg
	}
	for _, pkgRel := range eff.goPackages {
		pkgID := "go::pkg::" + filepath.ToSlash(pkgRel)
		addAlias(pkgID, pkgRel, "package")
		for _, file := range byPkg[pkgRel].files {
			fileID := "go::file::" + filepath.ToSlash(file.rel)
			addAlias(fileID, filepath.Base(file.rel), "file")
			for _, test := range file.tests {
				addAlias("go::test::"+filepath.ToSlash(pkgRel)+"::"+test.name, test.name, "test")
			}
		}
	}
	for _, rel := range eff.vsixFiles {
		addAlias("vsix::file::"+filepath.ToSlash(rel), filepath.Base(rel), "file")
	}
	for _, laneID := range eff.laneRefs {
		addAlias(laneID, strings.TrimPrefix(laneID, "keel::lane::"), "lane")
	}
	return items
}

func laneDurationHint(last *laneLastRun) string {
	if last == nil || last.DurationMS < 0 {
		return ""
	}
	totalSeconds := float64(last.DurationMS) / 1000
	if totalSeconds > 90 {
		seconds := int(totalSeconds + 0.5)
		return fmt.Sprintf("· last %dm %02ds", seconds/60, seconds%60)
	}
	return fmt.Sprintf("· last %.1fs", totalSeconds)
}

func lanesDiagnosticItem(id, message string) vscode.TestItem {
	return vscode.TestItem{
		ID:          "keel::lane-diagnostic::" + StableIDSegment(id),
		ParentID:    vscodeGroupLanes,
		Label:       "lanes diagnostic: " + message,
		Kind:        "group",
		Runnable:    false,
		Profiles:    []string{},
		Limitations: []string{message},
	}
}

func packagesForGoPattern(lanesPath, pattern string) []string {
	root := filepath.Dir(filepath.Dir(lanesPath))
	packages, err := parseGoTestPackages(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, pkg := range packages {
		if goPackageMatchesPattern(pkg.rel, pattern) {
			out = append(out, pkg.rel)
		}
	}
	sort.Strings(out)
	return out
}

func goPackageMatchesPattern(pkg, pattern string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	switch pattern {
	case "./...", "...":
		return true
	case "./":
		return pkg == "."
	}
	if strings.HasPrefix(pattern, "./") {
		pattern = strings.TrimPrefix(pattern, "./")
	}
	if strings.HasSuffix(pattern, "/...") {
		prefix := strings.TrimSuffix(pattern, "/...")
		return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
	}
	return pkg == strings.TrimSuffix(pattern, "/")
}

func vsixTestFileExists(root, rel string) bool {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, "vsix", clean)); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(root, clean))
	return err == nil
}

func knownSystemLaneShortID(id string) bool {
	_, ok := systemLaneShortID(id)
	return ok
}

func systemLaneShortID(id string) (string, bool) {
	short := strings.TrimPrefix(id, "keel::lane::")
	switch "keel::lane::" + short {
	case vscodeLaneLint, vscodeLaneTestFast, vscodeLaneTestCoverage, vscodeLaneVSIXGate, vscodeLaneCI:
		return short, true
	default:
		return "", false
	}
}

func systemLaneOrder(id string) string {
	switch id {
	case vscodeLaneLint:
		return "b.1"
	case vscodeLaneTestFast:
		return "b.2"
	case vscodeLaneTestCoverage:
		return "b.3"
	case vscodeLaneVSIXGate:
		return "b.10"
	case vscodeLaneCI:
		return "b.30"
	default:
		return ""
	}
}

func laneMembersForList(members []laneMember) []laneMemberListEntry {
	out := make([]laneMemberListEntry, 0, len(members))
	for _, member := range members {
		entry := laneMemberListEntry{}
		switch {
		case member.Go != "":
			entry["go"] = member.Go
		case member.Root != "":
			entry["root"] = member.Root
		case member.VSIX != "":
			entry["vsix"] = member.VSIX
		case member.Lane != "":
			entry["lane"] = member.Lane
		default:
			for _, key := range member.rawKeys {
				entry[key] = ""
			}
		}
		out = append(out, entry)
	}
	return out
}

func latestLaneRun(root, laneID string) *laneLastRun {
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil
	}
	var best *laneLastRun
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		got := laneRunFromStream(filepath.Join(runDir, entry.Name()), laneID)
		if got == nil {
			continue
		}
		if best == nil || got.At.After(best.At) {
			best = got
		}
	}
	return best
}

func laneRunFromStream(path, laneID string) *laneLastRun {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var started *vscode.RunEvent
	var finished *vscode.RunEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event vscode.RunEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		switch event.Event {
		case "run_started":
			if len(event.Requested) == 1 && event.Requested[0].ID == laneID {
				copyEvent := event
				started = &copyEvent
			}
		case "run_finished":
			copyEvent := event
			finished = &copyEvent
		}
	}
	if started == nil || finished == nil || finished.ExitCode == nil {
		return nil
	}
	return &laneLastRun{RunID: started.RunID, At: started.Time, DurationMS: finished.Time.Sub(started.Time).Milliseconds(), ExitCode: *finished.ExitCode}
}

func detectGoFamilies(root string) (map[string]bool, error) {
	packages, err := parseGoTestPackages(root)
	if err != nil {
		return nil, err
	}
	families := map[string]bool{}
	for _, pkg := range packages {
		if family := goPackageFamily(pkg.rel); family != "" {
			families[family] = true
		}
	}
	return families, nil
}

func goPackageFamily(pkg string) string {
	if pkg == "." || pkg == "" {
		return "."
	}
	return strings.Split(filepath.ToSlash(pkg), "/")[0]
}

func familyPackageCount(root, family string) int {
	packages, err := parseGoTestPackages(root)
	if err != nil {
		return 0
	}
	count := 0
	for _, pkg := range packages {
		if goPackageFamily(pkg.rel) == family {
			count++
		}
	}
	return count
}

func nextDetectionOrderSlot(lanes []testFileLane) int {
	used := map[int]bool{}
	for _, lane := range lanes {
		if strings.HasPrefix(lane.Order, "b.") {
			if n, err := strconv.Atoi(strings.TrimPrefix(lane.Order, "b.")); err == nil {
				used[n] = true
			}
		}
	}
	for i := 40; i <= 99; i++ {
		if !used[i] {
			return i
		}
	}
	return 100
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func orderedResources(values map[string]bool) []string {
	order := []string{"go-toolchain", "keel-module-root", "stub-binaries", "pnpm"}
	var out []string
	for _, resource := range order {
		if values[resource] {
			out = append(out, resource)
			delete(values, resource)
		}
	}
	extra := sortedKeys(values)
	return append(out, extra...)
}

func StableIDSegment(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "diagnostic"
	}
	return out
}

func ordinalSortText(labelPrefix string) string {
	parts := strings.Split(labelPrefix, ".")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		if n, err := strconv.Atoi(parts[i]); err == nil {
			parts[i] = fmt.Sprintf("%03d", n)
		}
	}
	return strings.Join(parts, ".")
}

type discoveredGoPackage struct {
	rel   string
	files []discoveredGoFile
}

type discoveredGoFile struct {
	rel         string
	packageRel  string
	packageName string
	tests       []discoveredGoTest
	parseErr    error
}

type discoveredGoTest struct {
	name  string
	rng   vscode.Range
	order int
}

// DHF-REQ: keel/requirement-49
// DHF-REQ: keel/requirement-43, keel/requirement-46
func discoverGoTestItems(_ context.Context, root string) ([]vscode.TestItem, error) {
	packages, err := parseGoTestPackages(root)
	if err != nil {
		return nil, err
	}
	items := []vscode.TestItem{{
		ID:                "go::root",
		ParentID:          vscodeGroupFrameworks,
		Label:             "d.1 Go",
		SortText:          ordinalSortText("d.1"),
		Kind:              "root",
		Framework:         "go",
		Runner:            "go-test",
		RunnerLabel:       "Go test",
		Runnable:          true,
		Profiles:          []string{"run"},
		RequiredResources: []string{"go-toolchain", "keel-module-root"},
	}}
	for _, pkg := range packages {
		pkgID := "go::pkg::" + filepath.ToSlash(pkg.rel)
		items = append(items, vscode.TestItem{
			ID:                pkgID,
			ParentID:          "go::root",
			Label:             pkg.rel,
			Kind:              "package",
			Framework:         "go",
			Runner:            "go-test",
			RunnerLabel:       "Go test",
			Runnable:          true,
			Profiles:          []string{"run"},
			RequiredResources: []string{"go-toolchain", "keel-module-root"},
		})
		for _, file := range pkg.files {
			fileID := "go::file::" + filepath.ToSlash(file.rel)
			item := vscode.TestItem{
				ID:                fileID,
				ParentID:          pkgID,
				Label:             filepath.Base(file.rel),
				Kind:              "file",
				Framework:         "go",
				Runner:            "go-test",
				RunnerLabel:       "Go test",
				URI:               filepath.ToSlash(file.rel),
				Runnable:          file.parseErr == nil,
				Profiles:          []string{"run"},
				RequiredResources: []string{"go-toolchain", "keel-module-root"},
			}
			if file.parseErr != nil {
				item.Profiles = []string{}
				item.Limitations = []string{file.parseErr.Error()}
			}
			items = append(items, item)
			for _, test := range file.tests {
				testName := test.name
				rng := test.rng
				sortText := fmt.Sprintf("%06d", test.order)
				items = append(items, vscode.TestItem{
					ID:                "go::test::" + filepath.ToSlash(pkg.rel) + "::" + testName,
					ParentID:          fileID,
					Label:             testName,
					SortText:          sortText,
					Kind:              "test",
					Framework:         "go",
					Runner:            "go-test",
					RunnerLabel:       "Go test",
					URI:               filepath.ToSlash(file.rel),
					Range:             &rng,
					Runnable:          true,
					Profiles:          []string{"run"},
					RequiredResources: []string{"go-toolchain", "keel-module-root"},
				})
			}
		}
	}
	return items, nil
}

func parseGoTestPackages(root string) ([]discoveredGoPackage, error) {
	byPackage := map[string]*discoveredGoPackage{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && goDiscoverySkipDir(path, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		match, err := goTestFileMatchesBuild(root, rel)
		if err != nil {
			file := discoveredGoFile{
				rel:        rel,
				packageRel: goPackageRelFromFile(rel),
				parseErr:   fmt.Errorf("%s: %w", rel, err),
			}
			pkg := byPackage[file.packageRel]
			if pkg == nil {
				pkg = &discoveredGoPackage{rel: file.packageRel}
			}
			byPackage[file.packageRel] = pkg
			pkg.files = append(pkg.files, file)
			return nil
		}
		if !match {
			return nil
		}
		file := parseGoTestFile(path, rel)
		pkg := byPackage[file.packageRel]
		if pkg == nil {
			pkg = &discoveredGoPackage{rel: file.packageRel}
			byPackage[file.packageRel] = pkg
		}
		if len(file.tests) > 0 || file.parseErr != nil {
			pkg.files = append(pkg.files, file)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("vscode discover go parser walk: %w", err)
	}

	packages := make([]discoveredGoPackage, 0, len(byPackage))
	for _, pkg := range byPackage {
		if len(pkg.files) == 0 {
			continue
		}
		if err := goPackageBuildDiagnostic(root, pkg.rel); err != nil {
			for i := range pkg.files {
				if pkg.files[i].parseErr == nil {
					pkg.files[i].parseErr = fmt.Errorf("%s: package has invalid Go files: %w", pkg.files[i].rel, err)
					pkg.files[i].tests = nil
				}
			}
		}
		sort.Slice(pkg.files, func(i, j int) bool { return pkg.files[i].rel < pkg.files[j].rel })
		packages = append(packages, *pkg)
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].rel < packages[j].rel })
	return packages, nil
}

// DHF-REQ: keel/requirement-54
func discoverVSIXTestItems(root string) ([]vscode.TestItem, error) {
	base := filepath.Join(root, "vsix", "src", "test")
	if _, err := os.Stat(base); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	items := []vscode.TestItem{{
		ID:                "vsix::root",
		ParentID:          vscodeGroupFrameworks,
		Label:             "d.2 Mocha (vsix)",
		SortText:          ordinalSortText("d.2"),
		Kind:              "root",
		Framework:         "vsix",
		Runner:            "mocha",
		RunnerLabel:       "Mocha",
		Runnable:          true,
		Profiles:          []string{"run"},
		RequiredResources: []string{"pnpm"},
	}}
	var files []string
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".spec.ts") || strings.HasSuffix(name, ".test.tsx") || strings.HasSuffix(name, ".spec.tsx") {
			rel, err := filepath.Rel(filepath.Join(root, "vsix"), path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, rel := range files {
		items = append(items, vscode.TestItem{
			ID:                "vsix::file::" + rel,
			ParentID:          "vsix::root",
			Label:             filepath.Base(rel),
			Kind:              "file",
			Framework:         "vsix",
			Runner:            "mocha",
			RunnerLabel:       "Mocha",
			URI:               filepath.ToSlash(filepath.Join("vsix", rel)),
			Runnable:          true,
			Profiles:          []string{"run"},
			RequiredResources: []string{"pnpm"},
		})
	}
	return items, nil
}

func goDiscoverySkipDir(path, name string) bool {
	switch name {
	case "vendor", "testdata", "node_modules":
		return true
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	_, err := os.Stat(filepath.Join(path, "go.mod"))
	return err == nil
}

func goTestFileMatchesBuild(root, rel string) (bool, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("invalid module-relative Go file %q", rel)
	}
	dir := filepath.Join(root, filepath.Dir(clean))
	return build.Default.MatchFile(dir, filepath.Base(clean))
}

func goPackageBuildDiagnostic(root, rel string) error {
	dir := root
	if rel != "." {
		dir = filepath.Join(root, filepath.FromSlash(rel))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var packageName string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		match, err := build.Default.MatchFile(dir, name)
		if err != nil {
			return err
		}
		if !match {
			continue
		}
		src, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			return err
		}
		if src.Name == nil {
			continue
		}
		if packageName == "" {
			packageName = src.Name.Name
			continue
		}
		if src.Name.Name != packageName {
			return fmt.Errorf("found packages %s and %s in %s", packageName, src.Name.Name, dir)
		}
	}
	return nil
}

func parseGoTestFile(path, rel string) discoveredGoFile {
	fset := token.NewFileSet()
	src, err := parser.ParseFile(fset, path, nil, 0)
	file := discoveredGoFile{
		rel:        rel,
		packageRel: goPackageRelFromFile(rel),
	}
	if src != nil && src.Name != nil {
		file.packageName = src.Name.Name
	}
	if err != nil {
		file.parseErr = fmt.Errorf("%s: %w", rel, err)
		return file
	}
	testingName, testingDot := testingImportBinding(src)
	aliases := fileTypeAliases(src)
	for _, decl := range src.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil || !isGoTestName(fn.Name.Name) {
			continue
		}
		if !isGoTestFunc(fn, testingName, testingDot, aliases) {
			continue
		}
		file.tests = append(file.tests, discoveredGoTest{
			name:  fn.Name.Name,
			rng:   goRange(fset, fn.Pos(), fn.End()),
			order: len(file.tests) + 1,
		})
	}
	return file
}

func isGoTestName(name string) bool {
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	if len(name) == len("Test") {
		return true
	}
	r, _ := utf8.DecodeRuneInString(name[len("Test"):])
	return !unicode.IsLower(r)
}

func goPackageRelFromFile(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		return "."
	}
	return dir
}

// testingImportBinding reports how the standard "testing" package is bound in
// src: its local selector name (default "testing", or the import alias) and
// whether it is dot-imported. When testing is absent or blank-imported, name is
// "" and dot is false, so no function can qualify as a runnable go test — this
// keeps discovery from advertising `func TestX(t *fake.T)` or a local
// `type T struct{}` receiver that `go test` would reject.
func testingImportBinding(src *ast.File) (name string, dot bool) {
	const testingImportPath = `"testing"`
	for _, imp := range src.Imports {
		if imp.Path == nil || imp.Path.Value != testingImportPath {
			continue
		}
		switch {
		case imp.Name == nil:
			return "testing", false
		case imp.Name.Name == ".":
			return "", true
		case imp.Name.Name == "_":
			return "", false
		default:
			return imp.Name.Name, false
		}
	}
	return "", false
}

// fileTypeAliases collects same-file type aliases (`type X = Y`) so a test
// parameter written against a package-local alias of testing.T (e.g.
// `type T = testing.T; func TestX(t *T)`, which go test accepts) still resolves.
// Non-alias definitions (`type T struct{}`) are deliberately excluded.
func fileTypeAliases(src *ast.File) map[string]ast.Expr {
	var aliases map[string]ast.Expr
	for _, decl := range src.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !ts.Assign.IsValid() || ts.Name == nil {
				continue
			}
			if aliases == nil {
				aliases = map[string]ast.Expr{}
			}
			aliases[ts.Name.Name] = ts.Type
		}
	}
	return aliases
}

func isGoTestFunc(fn *ast.FuncDecl, testingName string, testingDot bool, aliases map[string]ast.Expr) bool {
	if fn.Type == nil || fn.Type.TypeParams != nil && len(fn.Type.TypeParams.List) > 0 || fn.Type.Results != nil && len(fn.Type.Results.List) > 0 || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 || len(fn.Type.Params.List[0].Names) > 1 {
		return false
	}
	star, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	return typeDenotesTestingT(star.X, testingName, testingDot, aliases, 0)
}

// typeDenotesTestingT reports whether a type expression names testing.T: the
// bound testing selector (`testing.T` / `alias.T`), a dot-imported bare `T`, or
// a same-file alias chain that resolves to one of those. Anything else — a
// foreign `fake.T` selector or a local `type T struct{}` — is rejected, so
// discovery only advertises functions go test would actually run.
func typeDenotesTestingT(expr ast.Expr, testingName string, testingDot bool, aliases map[string]ast.Expr, depth int) bool {
	if depth > 8 {
		return false
	}
	switch x := expr.(type) {
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && testingName != "" && id.Name == testingName && x.Sel != nil && x.Sel.Name == "T"
	case *ast.Ident:
		if testingDot && x.Name == "T" {
			return true
		}
		if target, ok := aliases[x.Name]; ok {
			return typeDenotesTestingT(target, testingName, testingDot, aliases, depth+1)
		}
		return false
	default:
		return false
	}
}

func goRange(fset *token.FileSet, start, end token.Pos) vscode.Range {
	startPos := fset.Position(start)
	endPos := fset.Position(end)
	return vscode.Range{
		StartLine:   max(startPos.Line-1, 0),
		StartColumn: max(startPos.Column-1, 0),
		EndLine:     max(endPos.Line-1, 0),
		EndColumn:   max(endPos.Column-1, 0),
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func selectedPlanItems(ids []string) []vscode.SetupPlanItem {
	if len(ids) == 0 {
		ids = vscodeLaneIDs
	}
	items := make([]vscode.SetupPlanItem, 0, len(ids))
	for _, id := range ids {
		if selection, ok := vscode.ParseGoItemID(id); ok {
			items = append(items, vscode.SetupPlanItem{
				ID:                id,
				Label:             goSelectionLabel(selection, id),
				Kind:              selection.Kind,
				Framework:         "go",
				Runner:            "go-test",
				RunnerLabel:       "Go test",
				Runnable:          true,
				RequiredResources: []string{"go-toolchain", "keel-module-root"},
			})
			continue
		}
		items = append(items, vscode.SetupPlanItem{ID: id, Label: strings.TrimPrefix(id, "keel::lane::"), Kind: "lane", LaneID: id, Runnable: true, RequiredResources: []string{"go-toolchain", "keel-module-root", "stub-binaries"}})
	}
	return items
}

func goSelectionLabel(selection vscode.GoSelection, id string) string {
	switch {
	case selection.TestName != "":
		return selection.TestName
	case selection.Pkg != "":
		return selection.Pkg
	default:
		return id
	}
}

// DHF-REQ: keel/requirement-48
func runVSCodeLane(ctx context.Context, logger *slog.Logger, root, laneID, runID string, writer vscode.RunEventWriter) (int, error) {
	if strings.HasPrefix(laneID, "keel::maintenance::") {
		if laneID == vscodeMaintenanceDetectLanes {
			if err := runVSCodeDetectLanesMaintenance(root, writer); err != nil {
				return 1, err
			}
			return 0, nil
		}
		return runVSCodeMaintenance(root, laneID)
	}
	// DHF-REQ: keel/requirement-43
	if selection, ok := vscode.ParseGoItemID(laneID); ok {
		if logger == nil {
			logger = vscodeDiscardLogger()
		}
		if err := runVSCodeGoSelection(ctx, logger, root, laneID, selection, writer); err != nil {
			return gateExitCode(err), err
		}
		return 0, nil
	}
	if strings.HasPrefix(laneID, "keel::lane::") && !knownVSCodeLaneID(laneID) {
		if logger == nil {
			logger = vscodeDiscardLogger()
		}
		if err := runVSCodeFileLane(ctx, logger, root, laneID, runID, writer); err != nil {
			return gateExitCode(err), err
		}
		return 0, nil
	}
	switch laneID {
	case vscodeLaneLint:
		if err := runLint(root); err != nil {
			return 1, err
		}
	case vscodeLaneTestFast:
		if logger == nil {
			logger = vscodeDiscardLogger()
		}
		if err := runStep(ctx, logger, root, step{name: "vscode:test-fast", program: "go", args: []string{"test", "./..."}}); err != nil {
			return gateExitCode(err), err
		}
	case vscodeLaneTestCoverage:
		if logger == nil {
			logger = vscodeDiscardLogger()
		}
		if err := runVSCodeTestCoverage(ctx, logger, root, runID, writer); err != nil {
			return 1, err
		}
	case vscodeLaneVSIXGate:
		if logger == nil {
			logger = vscodeDiscardLogger()
		}
		if err := runVSIXGate(ctx, logger, root); err != nil {
			return gateExitCode(err), err
		}
	case vscodeLaneCI:
		if logger == nil {
			logger = vscodeDiscardLogger()
		}
		if err := runCI(ctx, logger, root); err != nil {
			return gateExitCode(err), err
		}
	default:
		return 2, cli.NewUsageError("unknown vscode lane id %q", laneID)
	}
	return 0, nil
}

// DHF-REQ: keel/requirement-51
func runVSCodeFileLane(ctx context.Context, logger *slog.Logger, root, laneID, runID string, writer vscode.RunEventWriter) error {
	lanes, err := loadLanesState(root)
	if err != nil {
		return err
	}
	shortID := strings.TrimPrefix(laneID, "keel::lane::")
	eff, ok := lanes.effective[shortID]
	if !ok {
		for _, item := range lanes.diagnostics {
			if strings.Contains(item.ID, StableIDSegment(shortID)) {
				return fmt.Errorf("vscode lane %s invalid: %s", laneID, strings.Join(item.Limitations, "; "))
			}
		}
		return cli.NewUsageError("unknown vscode lane id %q", laneID)
	}
	for _, systemLane := range eff.systemLanes {
		exit, err := runVSCodeLane(ctx, logger, root, systemLane, runID, writer)
		if err != nil {
			return vscodeRunError{exitCode: exit, msg: err.Error()}
		}
	}
	if len(eff.goPackages) > 0 {
		args := []string{"test", "-json"}
		for _, pkg := range eff.goPackages {
			args = append(args, vscode.GoPackageArg(pkg))
		}
		stdout, stderr, err := capture(ctx, logger, root, "go", args...)
		emitLaneGoPackageEvents(stdout, modulePath, writer)
		if err != nil {
			return fmt.Errorf("go test %s: %w: %s", strings.Join(args[1:], " "), err, strings.TrimSpace(stderr))
		}
	}
	if len(eff.vsixFiles) > 0 {
		if err := runVSIXFileSelection(ctx, logger, root, eff.vsixFiles); err != nil {
			return err
		}
	}
	return nil
}

// DHF-REQ: keel/requirement-54
func runVSIXFileSelection(ctx context.Context, logger *slog.Logger, root string, files []string) error {
	if _, err := exec.LookPath("pnpm"); err != nil {
		return fmt.Errorf("vscode run vsix files: required tool %q not found on PATH", "pnpm")
	}
	args := []string{"--dir", filepath.Join(root, "vsix"), "run", "test:headless", "--"}
	args = append(args, files...)
	if err := runStep(ctx, logger, root, step{name: "vscode:vsix-files", program: "pnpm", args: args}); err != nil {
		return err
	}
	return nil
}

func emitLaneGoPackageEvents(raw, modulePath string, writer vscode.RunEventWriter) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		var event vscode.GoTestJSONEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || event.Package == "" || event.Test != "" {
			continue
		}
		switch event.Action {
		case "pass", "fail", "skip":
			pkg := vscode.GoEventPackageRel(event.Package, modulePath)
			if pkg == "" {
				continue
			}
			writer(vscode.RunEvent{
				Event:      vscode.StatusEventName(event.Action),
				TestID:     "go::pkg::" + filepath.ToSlash(pkg),
				DurationMS: vscode.GoElapsedMillis(event.Elapsed, time.Now()),
			})
		}
	}
}

// DHF-REQ: keel/requirement-47
func runVSCodeMaintenance(root, id string) (int, error) {
	switch id {
	case vscodeMaintenanceDetectLanes:
		return 2, cli.NewUsageError("detect lanes maintenance requires run-event writer")
	case vscodeMaintenanceUnlock:
		if err := os.Remove(vscodeRunLockPath(root)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 1, err
		}
	case vscodeMaintenanceClearResults:
		return 0, nil
	case vscodeMaintenanceClearState:
		if err := clearVSCodeDevtoolsState(root); err != nil {
			return 1, err
		}
	default:
		return 2, cli.NewUsageError("unknown vscode maintenance id %q", id)
	}
	return 0, nil
}

// DHF-REQ: keel/requirement-52
func runVSCodeDetectLanesMaintenance(root string, writer vscode.RunEventWriter) error {
	var out bytes.Buffer
	if err := writeVSCodeLanesDetect(root, false, &out); err != nil {
		writer(vscode.RunEvent{Event: "output", TestID: vscodeMaintenanceDetectLanes, Message: err.Error()})
		return err
	}
	var doc lanesDetectDocument
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		return err
	}
	for _, entry := range doc.Added {
		writer(vscode.RunEvent{Event: "output", TestID: vscodeMaintenanceDetectLanes, Message: fmt.Sprintf("added %s %s", entry.ID, entry.Order)})
	}
	for _, entry := range doc.Unchanged {
		writer(vscode.RunEvent{Event: "output", TestID: vscodeMaintenanceDetectLanes, Message: fmt.Sprintf("unchanged %s: %s", entry.ID, entry.Reason)})
	}
	for _, entry := range doc.Skipped {
		writer(vscode.RunEvent{Event: "output", TestID: vscodeMaintenanceDetectLanes, Message: fmt.Sprintf("skipped %s: %s", entry.ID, entry.Reason)})
	}
	return nil
}

func clearVSCodeDevtoolsState(root string) error {
	devtoolsDir := filepath.Join(root, ".devtools")
	entries, err := os.ReadDir(devtoolsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "vscode-runs" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(devtoolsDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// DHF-REQ: keel/requirement-50
func runVSCodeGoSelection(ctx context.Context, logger *slog.Logger, root, selectedID string, selection vscode.GoSelection, writer vscode.RunEventWriter) error {
	if selection.Kind == "file" {
		names, err := goTestNamesInFile(root, selection.File)
		if err != nil {
			return err
		}
		selection.TestNames = names
	}
	args := []string{"test", vscode.GoPackageArg(selection.Pkg), "-json"}
	if selection.TestName != "" {
		args = append(args, "-run="+vscode.GoTestNamePattern([]string{selection.TestName}))
	} else if len(selection.TestNames) > 0 {
		args = append(args, "-run="+vscode.GoTestNamePattern(selection.TestNames))
	}
	stdout, stderr, err := capture(ctx, logger, root, "go", args...)
	emitGoTestJSONEvents(stdout, selection, selectedID, modulePath, writer)
	if err != nil {
		return fmt.Errorf("go test %s: %w: %s", strings.Join(args[1:], " "), err, strings.TrimSpace(stderr))
	}
	return nil
}

func goTestNamesInFile(root, rel string) ([]string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if rel == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("vscode run go file: invalid file selection %q", rel)
	}
	slashRel := filepath.ToSlash(clean)
	if !strings.HasSuffix(slashRel, "_test.go") {
		return nil, fmt.Errorf("vscode run go file %s: not a *_test.go file", slashRel)
	}
	if goFileRelHasIgnoredDir(slashRel) {
		return nil, fmt.Errorf("vscode run go file %s: file is outside the active Go package set", slashRel)
	}
	if goFileRelInNestedModule(root, slashRel) {
		return nil, fmt.Errorf("vscode run go file %s: file is in a nested Go module", slashRel)
	}
	match, err := goTestFileMatchesBuild(root, slashRel)
	if err != nil {
		return nil, fmt.Errorf("vscode run go file %s build constraints: %w", slashRel, err)
	}
	if !match {
		return nil, fmt.Errorf("vscode run go file %s: file is excluded by build constraints or GOOS/GOARCH", slashRel)
	}
	file := parseGoTestFile(filepath.Join(root, clean), slashRel)
	if file.parseErr != nil {
		return nil, fmt.Errorf("vscode run go file parse %s: %w", slashRel, file.parseErr)
	}
	names := make([]string, 0, len(file.tests))
	for _, test := range file.tests {
		names = append(names, test.name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("vscode run go file %s: no top-level Test functions found", slashRel)
	}
	return names, nil
}

func goFileRelHasIgnoredDir(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts[:len(parts)-1] {
		if part == "vendor" || part == "testdata" || part == "node_modules" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") {
			return true
		}
	}
	return false
}

func goFileRelInNestedModule(root, rel string) bool {
	dir := filepath.Dir(filepath.Clean(filepath.FromSlash(rel)))
	for dir != "." && dir != string(filepath.Separator) {
		if _, err := os.Stat(filepath.Join(root, dir, "go.mod")); err == nil {
			return true
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return false
}

func emitGoTestJSONEvents(raw string, selection vscode.GoSelection, selectedID, modulePath string, writer vscode.RunEventWriter) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		var event vscode.GoTestJSONEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		testID := vscode.GoRunEventTestID(selection, event, selectedID, modulePath)
		switch event.Action {
		case "run":
			if event.Test != "" && vscode.OutputBelongsToGoSelection(selection, event) {
				writer(vscode.RunEvent{Event: "test_started", TestID: testID})
			}
		case "pass", "fail", "skip":
			if vscode.GoJSONResultBelongsToSelection(selection, event) {
				writer(vscode.RunEvent{
					Event:      vscode.StatusEventName(event.Action),
					TestID:     testID,
					DurationMS: vscode.GoElapsedMillis(event.Elapsed, time.Now()),
				})
			}
		case "output":
			if vscode.OutputBelongsToGoSelection(selection, event) {
				writer(vscode.RunEvent{Event: "output", TestID: testID, Message: event.Output})
			}
		}
	}
}

func vscodeDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func parseVSCodeIDs(args []string, allowEmpty bool) ([]string, error) {
	ids := make([]string, 0)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) || args[i+1] != "json" {
				return nil, cli.NewUsageError("--format supports only json")
			}
			i++
		case "--id":
			if i+1 >= len(args) {
				return nil, cli.NewUsageError("--id requires a test id")
			}
			i++
			ids = append(ids, args[i])
		default:
			return nil, cli.NewUsageError("unknown vscode tests argument %q", args[i])
		}
	}
	if !allowEmpty && len(ids) == 0 {
		return nil, cli.NewUsageError("--id is required")
	}
	return ids, nil
}

func rejectUnsupportedFormat(args []string) error {
	_, err := parseVSCodeIDs(args, true)
	return err
}

func parseVSCodeLanesDetectArgs(args []string) (bool, error) {
	dryRun := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) || args[i+1] != "json" {
				return false, cli.NewUsageError("--format supports only json")
			}
			i++
		case "--dry-run":
			dryRun = true
		default:
			return false, cli.NewUsageError("unknown vscode lanes detect argument %q", args[i])
		}
	}
	return dryRun, nil
}

func laneForIDs(ids []string) string {
	for _, id := range ids {
		if strings.HasPrefix(id, "keel::lane::") {
			return id
		}
	}
	return ids[0]
}

// DHF-REQ: keel/requirement-53
func runRequestsForIDs(root string, ids []string) []vscode.RunRequest {
	out := make([]vscode.RunRequest, 0, len(ids))
	labels := map[string]string{}
	if lanes, err := loadLanesState(root); err == nil {
		for _, id := range vscodeLaneIDs {
			labels[id] = strings.TrimPrefix(id, "keel::lane::")
		}
		for id, eff := range lanes.effective {
			labels["keel::lane::"+id] = eff.lane.Label
		}
	}
	for _, id := range ids {
		label := labels[id]
		if label == "" {
			if selection, ok := vscode.ParseGoItemID(id); ok {
				label = goSelectionLabel(selection, id)
			} else {
				label = strings.TrimPrefix(id, "keel::lane::")
			}
		}
		out = append(out, vscode.RunRequest{ID: id, Label: label})
	}
	return out
}

func encodeProtocolDocument(out io.Writer, doc any) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

type keelWorkspaceProfile struct {
	root string
}

// DHF-REQ: keel/requirement-37, keel/requirement-48
func newKeelWorkspaceProfile(root string) keelWorkspaceProfile {
	return keelWorkspaceProfile{root: root}
}

func (p keelWorkspaceProfile) Repo() string        { return p.root }
func (p keelWorkspaceProfile) ModulePath() string  { return modulePath }
func (p keelWorkspaceProfile) LogDir() string      { return filepath.Join(p.root, ".logs") }
func (p keelWorkspaceProfile) MaxOutputBytes() int { return 4 * 1024 * 1024 }
func (p keelWorkspaceProfile) RemediationHint() string {
	return "Run `go run ./cmd/keel-dev ci` from the keel module root and fix the blocked prerequisite."
}
func (p keelWorkspaceProfile) ConsumerID() string { return "keel-dev" }
func (p keelWorkspaceProfile) Node() string       { return workspaceNode(p.root) }
func (p keelWorkspaceProfile) PrepareLane(_ context.Context, laneID string) vscode.LaneReadiness {
	if blocked := os.Getenv("KEEL_VSCODE_DEMO_BLOCK"); blocked != "" {
		if blocked == laneID {
			return vscode.LaneReadiness{Blocked: []vscode.BlockedPrereq{{Resource: "KEEL_VSCODE_DEMO_BLOCK", Detail: blocked}}}
		}
	} else if blocked := currentVSCodeDemoBlockStatus(p.root).BlockedLane; blocked == laneID {
		return vscode.LaneReadiness{Blocked: []vscode.BlockedPrereq{{Resource: "KEEL_VSCODE_DEMO_BLOCK", Detail: blocked}}}
	}
	if _, err := exec.LookPath("go"); err != nil {
		return vscode.LaneReadiness{Blocked: []vscode.BlockedPrereq{{Resource: "go-toolchain", Detail: err.Error()}}}
	}
	if laneID == vscodeLaneVSIXGate {
		if _, err := exec.LookPath("pnpm"); err != nil {
			return vscode.LaneReadiness{Blocked: []vscode.BlockedPrereq{{Resource: "pnpm", Detail: err.Error()}}}
		}
	}
	if _, err := os.Stat(filepath.Join(p.root, "go.mod")); err != nil {
		return vscode.LaneReadiness{Blocked: []vscode.BlockedPrereq{{Resource: "keel-module-root", Detail: err.Error()}}}
	}
	return vscode.LaneReadiness{}
}

func writeVSCodeDemoBlockState(root, laneID string) error {
	path := vscodeDemoBlockStatePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encErr := json.NewEncoder(file).Encode(vscodeDemoBlockState{BlockedLane: laneID, UpdatedAt: time.Now().UTC()})
	closeErr := file.Close()
	if encErr != nil {
		return encErr
	}
	return closeErr
}

func currentVSCodeDemoBlockStatus(root string) vscodeDemoBlockStatus {
	path := vscodeDemoBlockStatePath(root)
	if blocked := os.Getenv("KEEL_VSCODE_DEMO_BLOCK"); blocked != "" {
		return vscodeDemoBlockStatus{BlockedLane: blocked, Source: "env", Path: path}
	}
	state, err := readVSCodeDemoBlockState(root)
	if err != nil || state.BlockedLane == "" {
		return vscodeDemoBlockStatus{Source: "none", Path: path}
	}
	return vscodeDemoBlockStatus{BlockedLane: state.BlockedLane, Source: "state", Path: path}
}

func readVSCodeDemoBlockState(root string) (vscodeDemoBlockState, error) {
	data, err := os.ReadFile(vscodeDemoBlockStatePath(root))
	if err != nil {
		return vscodeDemoBlockState{}, err
	}
	var state vscodeDemoBlockState
	if err := json.Unmarshal(data, &state); err != nil {
		return vscodeDemoBlockState{}, err
	}
	if !knownVSCodeLaneID(state.BlockedLane) {
		return vscodeDemoBlockState{}, fmt.Errorf("unknown persisted vscode lane id %q", state.BlockedLane)
	}
	return state, nil
}

func vscodeDemoBlockStatePath(root string) string {
	return filepath.Join(root, ".devtools", "vscode-demo-block.json")
}

func knownVSCodeLaneID(laneID string) bool {
	for _, known := range vscodeLaneIDs {
		if laneID == known {
			return true
		}
	}
	return false
}

func newVSCodeRunWriter(root string, protocol io.Writer, stamper vscode.EventStamper, logger *slog.Logger) (vscode.RunEventWriter, func(), error) {
	if protocol == nil {
		protocol = io.Discard
	}
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, nil, err
	}
	pruneOldVSCodeRuns(runDir, logger)
	external, err := os.Create(filepath.Join(runDir, stamper.RunID+".jsonl"))
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() { _ = external.Close() }
	return func(event vscode.RunEvent) {
		stamped := stamper.Stamp(event)
		line, err := vscode.MarshalRunEventJSONL(stamped)
		if err != nil {
			if logger != nil {
				logger.Error("marshal vscode run event", "error", err.Error())
			}
			return
		}
		_, _ = protocol.Write(line)
		_, _ = external.Write(line)
	}, closeFn, nil
}

func acquireVSCodeRunLock(root string, ids []string, token string) (func() error, error) {
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	path := vscodeRunLockPath(root)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("vscode run lock already exists at %s", path)
		}
		return nil, fmt.Errorf("create vscode run lock: %w", err)
	}
	lock := vscode.RunLockFile{
		PID:       os.Getpid(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		IDs:       append([]string{}, ids...),
		Token:     token,
	}
	encErr := json.NewEncoder(file).Encode(lock)
	closeErr := file.Close()
	if encErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if encErr != nil {
			return nil, encErr
		}
		return nil, closeErr
	}
	return func() error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var current vscode.RunLockFile
		if err := json.Unmarshal(data, &current); err != nil {
			return err
		}
		if current.Token != token {
			return fmt.Errorf("vscode run lock token mismatch at %s", path)
		}
		return os.Remove(path)
	}, nil
}

func vscodeRunLockPath(root string) string {
	return filepath.Join(root, ".devtools", "vscode-runs", "run.lock")
}

func pruneOldVSCodeRuns(dir string, logger *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil && logger != nil {
			logger.Warn("remove old vscode run stream", "path", path, "error", err.Error())
		}
	}
}

type vscodeRunError struct {
	exitCode int
	msg      string
}

func (e vscodeRunError) Error() string { return e.msg }

func boolPtr(v bool) *bool { return &v }

func newVSCodeRunID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z")
}

func statusWord(ok bool) string {
	if ok {
		return "ready"
	}
	return "blocked"
}

func workspaceNode(root string) string {
	if root == "" {
		return "unknown"
	}
	return filepath.Base(root)
}

func buildCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}
