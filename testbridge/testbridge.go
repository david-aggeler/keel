// Package testbridge provides the reusable devtool side of keel's VS Code test
// bridge contract. A consumer devtool supplies content providers; this package
// owns the canonical argv tree, protocol JSON emission, config helpers, run
// event streaming, and run.lock serialization.
//
// DHF-REQ: keel/requirement-58
package testbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/david-aggeler/keel/cli"
	"github.com/david-aggeler/keel/vscode"
)

type runtimeKey struct{}

// Runtime carries the process-local sinks and workspace root used while a
// canonical test-bridge command is executing.
type Runtime struct {
	Root     string
	Protocol io.Writer
	Log      *slog.Logger
	Now      func() time.Time
	RunID    func() string
}

// WithRuntime stores Runtime in ctx for a CommandSpec handler.
func WithRuntime(ctx context.Context, rt Runtime) context.Context {
	return context.WithValue(ctx, runtimeKey{}, rt)
}

// RuntimeFrom returns the Runtime stored in ctx.
func RuntimeFrom(ctx context.Context) (Runtime, bool) {
	rt, ok := ctx.Value(runtimeKey{}).(Runtime)
	return rt, ok
}

// Workspace identifies the consumer workspace in protocol envelopes.
type Workspace struct {
	Root       string
	Node       string
	ModulePath string
}

// DiscoveryProvider supplies the test tree content. The package wraps the
// result in the canonical argv and JSON sink discipline.
type DiscoveryProvider interface {
	Discover(context.Context) (vscode.DiscoveryDocument, error)
}

// DesiredStateProvider supplies the read-only desired-state report for a
// selection. Reconciliation belongs to Run, not this provider.
type DesiredStateProvider interface {
	DesiredState(context.Context, []string) (vscode.SetupPlan, error)
}

// Runner executes a selected run and emits run events through the package-owned
// writer.
type Runner interface {
	Run(context.Context, RunRequest, vscode.RunEventWriter) (int, error)
}

// ConfigProvider supplies the consumer's test-bridge config template.
type ConfigProvider interface {
	ConfigTemplate() vscode.TestBridgeConfig
}

// WorkspaceProvider supplies workspace metadata used by envelopes and run-event
// attribution.
type WorkspaceProvider interface {
	Workspace() Workspace
}

// MetadataProvider supplies devtool identity for desired-state documents.
type MetadataProvider interface {
	Metadata() vscode.DevtoolMetadata
}

// TestTreeProvider is an explicit content-provider name for consumers that want
// their package boundary to mirror the contract language.
type TestTreeProvider interface {
	DiscoveryProvider
}

// MaintenanceItemProvider describes maintenance items a consumer may fold into
// its discovery tree before returning a DiscoveryDocument.
type MaintenanceItemProvider interface {
	MaintenanceItems(context.Context) ([]vscode.TestItem, error)
}

// LaneProvider describes runnable lanes a consumer may fold into its discovery
// tree before returning a DiscoveryDocument.
type LaneProvider interface {
	Lanes(context.Context) ([]vscode.TestItem, error)
}

// Bridge is the provider set required by the canonical command tree.
type Bridge interface {
	DiscoveryProvider
	DesiredStateProvider
	Runner
	ConfigProvider
	WorkspaceProvider
	MetadataProvider
}

type lockExemptRunner interface {
	LockExemptRun([]string) bool
}

// RunRequest is the package-owned runner invocation contract.
type RunRequest struct {
	IDs   []string
	RunID string
	Root  string
}

// CommandSpec returns a dispatch root for the canonical protocol token:
// test-bridge tests discover|desired-state|run and config init|upgrade.
//
// DHF-REQ: keel/requirement-58, keel/requirement-60
func CommandSpec(bridge Bridge) *cli.CommandSpec {
	return &cli.CommandSpec{
		Subcommands: []*cli.CommandSpec{
			{
				Name:  "test-bridge",
				Short: "Serve VS Code test-bridge protocol commands.",
				Subcommands: []*cli.CommandSpec{
					{
						Name:  "config",
						Short: "Initialize or upgrade test bridge config.",
						Subcommands: []*cli.CommandSpec{
							{Name: "init", Use: "test-bridge config init", Short: "Write .vscode/test-bridge.json if absent.", Handler: handleConfigInit(bridge)},
							{Name: "upgrade", Use: "test-bridge config upgrade", Short: "Upgrade .vscode/test-bridge.json to the current schema.", Handler: handleConfigUpgrade(bridge)},
						},
					},
					{
						Name:  "tests",
						Short: "Discover tests, report desired state, and run selections.",
						Subcommands: []*cli.CommandSpec{
							{Name: "discover", Use: "test-bridge tests discover [--format json]", Short: "Emit the test discovery document.", Flags: []cli.FlagSpec{{Name: "format", Value: "json", Short: "Output format."}}, Handler: handleDiscover(bridge)},
							{Name: "desired-state", Use: "test-bridge tests desired-state [--format json] [--id test-id]", Short: "Emit the read-only desired-state document.", Flags: []cli.FlagSpec{{Name: "format", Value: "json", Short: "Output format."}, {Name: "id", Value: "test-id", Short: "Selected test id."}}, Handler: handleDesiredState(bridge)},
							{Name: "run", Use: "test-bridge tests run --id test-id", Short: "Run selected tests.", Flags: []cli.FlagSpec{{Name: "id", Value: "test-id", Short: "Selected test id."}}, Handler: handleRun(bridge)},
						},
					},
				},
			},
		},
	}
}

func handleDiscover(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		if _, err := parseIDs(args, true, true); err != nil {
			return err
		}
		doc, err := bridge.Discover(ctx)
		if err != nil {
			return err
		}
		return writeDocument(runtimeOrDefault(ctx, bridge), doc)
	}
}

// DHF-REQ: keel/requirement-60
func handleDesiredState(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		ids, err := parseIDs(args, true, true)
		if err != nil {
			return err
		}
		doc, err := bridge.DesiredState(ctx, ids)
		if err != nil {
			return err
		}
		return writeDocument(runtimeOrDefault(ctx, bridge), doc)
	}
}

// DHF-REQ: keel/requirement-58
func handleRun(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		ids, err := parseIDs(args, false, false)
		if err != nil {
			return err
		}
		rt := runtimeOrDefault(ctx, bridge)
		runID := newRunID(rt)
		writer, closeWriter, err := newRunWriter(rt, bridge.Workspace(), runID)
		if err != nil {
			return err
		}
		defer closeWriter()
		exitCode := 1
		writer(vscode.RunEvent{Event: "run_started", Live: boolPtr(true), Requested: runRequests(ids)})
		if locker, ok := bridge.(lockExemptRunner); !ok || !locker.LockExemptRun(ids) {
			releaseLock, err := acquireRunLock(runtimeRoot(rt, bridge), ids, runID)
			if err != nil {
				writer(vscode.RunEvent{Event: "errored", Message: err.Error()})
				writer(vscode.RunEvent{Event: "run_finished", ExitCode: &exitCode})
				return err
			}
			defer func() {
				if err := releaseLock(); err != nil && rt.Log != nil {
					rt.Log.Warn("release testbridge run lock", "error", err.Error())
				}
			}()
		}

		exitCode, runErr := bridge.Run(ctx, RunRequest{IDs: append([]string{}, ids...), RunID: runID, Root: runtimeRoot(rt, bridge)}, writer)
		if runErr != nil {
			writer(vscode.RunEvent{Event: "errored", Message: runErr.Error()})
		}
		writer(vscode.RunEvent{Event: "run_finished", ExitCode: &exitCode})
		if runErr != nil {
			return RunError{ExitCode: exitCode, Err: runErr}
		}
		if exitCode != 0 {
			return RunError{ExitCode: exitCode, Err: fmt.Errorf("testbridge run exited %d", exitCode)}
		}
		return nil
	}
}

func handleConfigInit(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		if len(args) != 0 {
			return cli.NewUsageError("test-bridge config init takes no arguments: got %q", args)
		}
		rt := runtimeOrDefault(ctx, bridge)
		_, err := InitConfig(runtimeRoot(rt, bridge), bridge.ConfigTemplate())
		return err
	}
}

func handleConfigUpgrade(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		if len(args) != 0 {
			return cli.NewUsageError("test-bridge config upgrade takes no arguments: got %q", args)
		}
		rt := runtimeOrDefault(ctx, bridge)
		_, err := UpgradeConfig(runtimeRoot(rt, bridge), bridge.ConfigTemplate())
		return err
	}
}

func parseIDs(args []string, allowEmpty bool, allowFormat bool) ([]string, error) {
	ids := make([]string, 0)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if !allowFormat {
				return nil, cli.NewUsageError("unknown flag \"--format\"")
			}
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
			return nil, cli.NewUsageError("unknown test-bridge tests argument %q", args[i])
		}
	}
	if !allowEmpty && len(ids) == 0 {
		return nil, cli.NewUsageError("--id is required")
	}
	return ids, nil
}

func writeDocument(rt Runtime, doc any) error {
	if err := ValidateDocument(doc); err != nil {
		return err
	}
	return EncodeDocument(rt.Protocol, doc)
}

// EncodeDocument writes doc to out as canonical test-bridge protocol JSON: one
// JSON object followed by a newline, with HTML escaping disabled. It performs no
// schema validation — callers that emit schema-typed protocol documents validate
// through ValidateDocument (or the package dispatch path) first. Consumer
// devtools route their protocol output through this function so JSON assembly
// stays owned by keel/testbridge rather than being hand-rolled per consumer.
//
// DHF-REQ: keel/requirement-63
func EncodeDocument(out io.Writer, doc any) error {
	if out == nil {
		out = io.Discard
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

func runtimeOrDefault(ctx context.Context, bridge Bridge) Runtime {
	rt, ok := RuntimeFrom(ctx)
	if !ok {
		return Runtime{Root: bridge.Workspace().Root, Protocol: io.Discard}
	}
	if rt.Root == "" {
		rt.Root = bridge.Workspace().Root
	}
	if rt.Protocol == nil {
		rt.Protocol = io.Discard
	}
	return rt
}

func runtimeRoot(rt Runtime, bridge Bridge) string {
	if rt.Root != "" {
		return rt.Root
	}
	return bridge.Workspace().Root
}

func newRunWriter(rt Runtime, workspace Workspace, runID string) (vscode.RunEventWriter, func(), error) {
	root := rt.Root
	if root == "" {
		root = workspace.Root
	}
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, nil, err
	}
	external, err := os.Create(filepath.Join(runDir, runID+".jsonl"))
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() { _ = external.Close() }
	stamper := vscode.EventStamper{
		Now:       rt.Now,
		RunID:     runID,
		Source:    "vscode",
		Workspace: workspaceNode(workspace, root),
		Logf: func(message string) {
			if rt.Log != nil {
				rt.Log.Warn("testbridge protocol event rejected", "detail", message)
			}
		},
	}
	out := rt.Protocol
	if out == nil {
		out = io.Discard
	}
	return func(event vscode.RunEvent) {
		stamped := stamper.Stamp(event)
		if err := ValidateDocument(stamped); err != nil {
			if rt.Log != nil {
				rt.Log.Error("validate testbridge run event", "error", err.Error())
			}
			return
		}
		line, err := vscode.MarshalRunEventJSONL(stamped)
		if err != nil {
			if rt.Log != nil {
				rt.Log.Error("marshal testbridge run event", "error", err.Error())
			}
			return
		}
		_, _ = out.Write(line)
		_, _ = external.Write(line)
	}, closeFn, nil
}

func workspaceNode(workspace Workspace, root string) string {
	if workspace.Node != "" {
		return workspace.Node
	}
	if root != "" {
		return filepath.Base(root)
	}
	return "unknown"
}

// DHF-REQ: keel/requirement-58, keel/requirement-67
func acquireRunLock(root string, ids []string, token string) (func() error, error) {
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	path := RunLockPath(root)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("keel/testbridge: run lock already exists at %s", path)
		}
		return nil, fmt.Errorf("keel/testbridge: create run lock: %w", err)
	}
	lock := vscode.RunLockFile{
		PID:       os.Getpid(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		IDs:       append([]string{}, ids...),
		Token:     token,
	}
	if err := ValidateDocument(lock); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
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
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		var current vscode.RunLockFile
		if err := json.Unmarshal(data, &current); err != nil {
			return err
		}
		if current.Token != token {
			return fmt.Errorf("keel/testbridge: run lock token mismatch at %s", path)
		}
		return os.Remove(path)
	}, nil
}

// RunLockPath returns the package-owned cross-run lock path.
func RunLockPath(root string) string {
	return filepath.Join(root, ".devtools", "vscode-runs", "run.lock")
}

func newRunID(rt Runtime) string {
	if rt.RunID != nil {
		if id := rt.RunID(); id != "" {
			return id
		}
	}
	now := time.Now()
	if rt.Now != nil {
		now = rt.Now()
	}
	return "run-" + now.UTC().Format("20060102T150405.000000000Z")
}

func runRequests(ids []string) []vscode.RunRequest {
	out := make([]vscode.RunRequest, 0, len(ids))
	for _, id := range ids {
		out = append(out, vscode.RunRequest{ID: id, Label: strings.TrimPrefix(id, "keel::lane::")})
	}
	return out
}

func boolPtr(v bool) *bool { return &v }

// RunError reports a non-zero run result while preserving CLI-level error
// handling for callers.
type RunError struct {
	ExitCode int
	Err      error
}

func (e RunError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("testbridge run exited %d", e.ExitCode)
	}
	return e.Err.Error()
}

func (e RunError) Unwrap() error { return e.Err }
