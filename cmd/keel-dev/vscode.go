package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
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

	"github.com/david-aggeler/keel/cli"
	"github.com/david-aggeler/keel/vscode"
)

const (
	vscodeGroupMaintenance = "keel::maintenance"
	vscodeGroupLanes       = "keel::lanes"
	vscodeGroupFrameworks  = "keel::frameworks"

	vscodeMaintenanceUnlock       = "keel::maintenance::unlock"
	vscodeMaintenanceClearResults = "keel::maintenance::clear-results"
	vscodeMaintenanceClearState   = "keel::maintenance::clear-state"

	vscodeLaneLint         = "keel::lane::lint"
	vscodeLaneTestFast     = "keel::lane::test-fast"
	vscodeLaneTestCoverage = "keel::lane::test-coverage"
	vscodeLaneVSIXGate     = "keel::lane::vsix-ci"
	vscodeLaneCI           = "keel::lane::ci"
)

var vscodeLaneIDs = []string{vscodeLaneLint, vscodeLaneTestFast, vscodeLaneTestCoverage, vscodeLaneVSIXGate, vscodeLaneCI}

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

	writer(vscode.RunEvent{Event: "run_started", Live: boolPtr(true)})

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

// DHF-REQ: keel/requirement-39, keel/requirement-43, keel/requirement-46, keel/requirement-48
func writeVSCodeDiscovery(root string, out io.Writer) error {
	items := []vscode.TestItem{
		groupItem(vscodeGroupMaintenance, "", "a. Maintenance", "a"),
		groupItem(vscodeGroupLanes, "", "b. Lanes", "b"),
		groupItem(vscodeGroupFrameworks, "", "d. Frameworks", "d"),
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
	items = append(items, goItems...)
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

// DHF-REQ: keel/requirement-43, keel/requirement-46, keel/requirement-49
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
			switch entry.Name() {
			case ".git", ".logs", ".devtools", "node_modules":
				if path != root {
					return filepath.SkipDir
				}
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
		sort.Slice(pkg.files, func(i, j int) bool { return pkg.files[i].rel < pkg.files[j].rel })
		packages = append(packages, *pkg)
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].rel < packages[j].rel })
	return packages, nil
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
	for _, decl := range src.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
			continue
		}
		if !isGoTestFunc(fn) {
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

func goPackageRelFromFile(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		return "."
	}
	return dir
}

func isGoTestFunc(fn *ast.FuncDecl) bool {
	if fn.Type == nil || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	param := fn.Type.Params.List[0]
	switch expr := param.Type.(type) {
	case *ast.StarExpr:
		sel, ok := expr.X.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "T" {
			return false
		}
		ident, ok := sel.X.(*ast.Ident)
		return ok && ident.Name == "testing"
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

// DHF-REQ: keel/requirement-47
func runVSCodeMaintenance(root, id string) (int, error) {
	switch id {
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

func laneForIDs(ids []string) string {
	for _, id := range ids {
		if strings.HasPrefix(id, "keel::lane::") {
			return id
		}
	}
	return ids[0]
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
