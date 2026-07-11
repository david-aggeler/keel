package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/david-aggeler/keel/cli"
	"github.com/david-aggeler/keel/vscode"
)

const (
	vscodeLaneLint         = "keel::lane::lint"
	vscodeLaneTestFast     = "keel::lane::test-fast"
	vscodeLaneTestCoverage = "keel::lane::test-coverage"
)

var vscodeLaneIDs = []string{vscodeLaneLint, vscodeLaneTestFast, vscodeLaneTestCoverage}

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

// DHF-REQ: keel/requirement-39
func writeVSCodeDiscovery(root string, out io.Writer) error {
	doc := vscode.DiscoveryDocument{
		Version:     1,
		Workspace:   workspaceNode(root),
		ModulePath:  modulePath,
		GeneratedAt: time.Now().UTC(),
		Capabilities: vscode.DiscoveryCapabilities{
			ClearResults:              true,
			RefreshInvalidatesResults: true,
			NeutralParentRollups:      true,
		},
		Items: []vscode.TestItem{
			{ID: "keel::root", Label: "keel", Kind: "workspace", Runnable: false, Profiles: []string{"run"}},
			laneItem(vscodeLaneLint, "lint"),
			laneItem(vscodeLaneTestFast, "test-fast"),
			laneItem(vscodeLaneTestCoverage, "test-coverage"),
		},
	}
	return encodeProtocolDocument(out, doc)
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

func laneItem(id, label string) vscode.TestItem {
	profiles := []string{"run"}
	if id == vscodeLaneTestCoverage {
		profiles = []string{"coverage"}
	}
	return vscode.TestItem{
		ID:                id,
		ParentID:          "keel::root",
		Label:             label,
		Kind:              "lane",
		Framework:         "go",
		Runner:            "keel-dev",
		RunnerLabel:       "keel-dev",
		Runnable:          true,
		Profiles:          profiles,
		LaneID:            id,
		RequiredResources: []string{"go-toolchain", "keel-module-root", "stub-binaries"},
	}
}

func selectedPlanItems(ids []string) []vscode.SetupPlanItem {
	if len(ids) == 0 {
		ids = vscodeLaneIDs
	}
	items := make([]vscode.SetupPlanItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, vscode.SetupPlanItem{ID: id, Label: strings.TrimPrefix(id, "keel::lane::"), Kind: "lane", LaneID: id, Runnable: true, RequiredResources: []string{"go-toolchain", "keel-module-root", "stub-binaries"}})
	}
	return items
}

func runVSCodeLane(ctx context.Context, logger *slog.Logger, root, laneID, runID string, writer vscode.RunEventWriter) (int, error) {
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
	default:
		return 2, cli.NewUsageError("unknown vscode lane id %q", laneID)
	}
	return 0, nil
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

// DHF-REQ: keel/requirement-37
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
	if blocked := os.Getenv("KEEL_VSCODE_DEMO_BLOCK"); blocked != "" && blocked == laneID {
		return vscode.LaneReadiness{Blocked: []vscode.BlockedPrereq{{Resource: "KEEL_VSCODE_DEMO_BLOCK", Detail: blocked}}}
	}
	if _, err := exec.LookPath("go"); err != nil {
		return vscode.LaneReadiness{Blocked: []vscode.BlockedPrereq{{Resource: "go-toolchain", Detail: err.Error()}}}
	}
	if _, err := os.Stat(filepath.Join(p.root, "go.mod")); err != nil {
		return vscode.LaneReadiness{Blocked: []vscode.BlockedPrereq{{Resource: "keel-module-root", Detail: err.Error()}}}
	}
	return vscode.LaneReadiness{}
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
	path := filepath.Join(runDir, "run.lock")
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
