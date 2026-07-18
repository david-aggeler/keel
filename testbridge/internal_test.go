package testbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/david-aggeler/keel/vscode"
)

// DHF-TEST: keel/requirement-58
func TestRunLockAcquireReleaseAndMismatch(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RunLockTokenEnv, "") // deterministic baseline regardless of test order
	release, err := acquireRunLock(root, []string{"demo::lane::fast"}, "token-1")
	if err != nil {
		t.Fatalf("acquire run lock: %v", err)
	}
	if _, err := os.Stat(RunLockPath(root)); err != nil {
		t.Fatalf("lock missing after acquire: %v", err)
	}
	// Simulate an unrelated process: no inherited ancestor token (the acquire
	// above exported token-1 into this process's env; requirement-96).
	t.Setenv(RunLockTokenEnv, "")
	if _, err := acquireRunLock(root, []string{"demo::lane::fast"}, "token-2"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second acquire err = %v, want already exists", err)
	}
	if err := os.WriteFile(RunLockPath(root), []byte(`{"pid":1,"created_at":"2026-07-13T00:00:00Z","ids":["x"],"token":"other"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := release(); err == nil || !strings.Contains(err.Error(), "token mismatch") {
		t.Fatalf("release mismatch err = %v, want token mismatch", err)
	}
	if err := os.Remove(RunLockPath(root)); err != nil {
		t.Fatal(err)
	}

	release, err = acquireRunLock(root, []string{"demo::lane::fast"}, "token-3")
	if err != nil {
		t.Fatalf("reacquire run lock: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release matching lock: %v", err)
	}
}

// A descendant run whose inherited env token matches the on-disk lock token
// proceeds without acquiring, its release is a no-op that leaves the ancestor's
// lock in place, and the ancestor's release still removes the lock cleanly and
// restores the prior environment.
//
// DHF-TEST: keel/requirement-96
func TestRunLockReentrantForDescendantRuns(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RunLockTokenEnv, "") // pin a clean baseline; acquire overwrites it
	release, err := acquireRunLock(root, []string{"vsix::root"}, "token-outer")
	if err != nil {
		t.Fatalf("outer acquire: %v", err)
	}
	if got := os.Getenv(RunLockTokenEnv); got != "token-outer" {
		t.Fatalf("exported env token = %q, want token-outer", got)
	}

	nestedRelease, err := acquireRunLock(root, []string{"keel::maintenance::detect-lanes"}, "token-nested")
	if err != nil {
		t.Fatalf("nested acquire under matching ancestor token: %v", err)
	}
	if err := nestedRelease(); err != nil {
		t.Fatalf("nested no-op release: %v", err)
	}
	data, err := os.ReadFile(RunLockPath(root))
	if err != nil {
		t.Fatalf("lock must survive the nested release: %v", err)
	}
	var current vscode.RunLockFile
	if err := json.Unmarshal(data, &current); err != nil {
		t.Fatalf("parse surviving lock: %v", err)
	}
	if current.Token != "token-outer" {
		t.Fatalf("surviving lock token = %q, want token-outer", current.Token)
	}

	if err := release(); err != nil {
		t.Fatalf("outer release after nested run: %v", err)
	}
	if _, err := os.Stat(RunLockPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock stat after outer release = %v, want not-exist", err)
	}
	if got := os.Getenv(RunLockTokenEnv); got != "" {
		t.Fatalf("env token after outer release = %q, want restored empty baseline", got)
	}
}

// Mismatched and unreadable lock state stays refused: the reentrant path only
// opens for an exact token match (fail-closed).
//
// DHF-TEST: keel/requirement-96
func TestRunLockMismatchedOrCorruptTokenStillRefused(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RunLockTokenEnv, "") // deterministic baseline regardless of test order
	release, err := acquireRunLock(root, []string{"demo::lane::fast"}, "token-1")
	if err != nil {
		t.Fatalf("acquire run lock: %v", err)
	}

	t.Setenv(RunLockTokenEnv, "token-elsewhere")
	if _, err := acquireRunLock(root, []string{"demo::lane::fast"}, "token-2"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("mismatched-token acquire err = %v, want already exists", err)
	}

	if err := os.WriteFile(RunLockPath(root), []byte("{corrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RunLockTokenEnv, "token-1")
	if _, err := acquireRunLock(root, []string{"demo::lane::fast"}, "token-2"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("corrupt-lock acquire err = %v, want already exists", err)
	}

	if err := os.Remove(RunLockPath(root)); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatalf("release with lock already gone: %v", err)
	}
}

// DHF-TEST: keel/requirement-58
func TestRunWriterStampsPersistsAndRejectsInvalidEvents(t *testing.T) {
	root := t.TempDir()
	var protocol bytes.Buffer
	var logs bytes.Buffer
	writer, closeWriter, err := newRunWriter(Runtime{
		Root:     root,
		Protocol: &protocol,
		Log:      slog.New(slog.NewTextHandler(&logs, nil)),
		Now:      func() time.Time { return time.Unix(3, 0).UTC() },
	}, Workspace{Root: root, Node: "node"}, "run-1")
	if err != nil {
		t.Fatalf("new run writer: %v", err)
	}
	writer(vscode.RunEvent{Event: "passed", TestID: "demo::lane::fast"})
	writer(vscode.RunEvent{Event: "bogus", TestID: "demo::lane::fast"})
	closeWriter()

	if !strings.Contains(protocol.String(), `"event":"passed"`) || !strings.Contains(protocol.String(), `"event":"output"`) {
		t.Fatalf("protocol events = %s, want passed and demoted output event", protocol.String())
	}
	stored, err := os.ReadFile(filepath.Join(root, ".devtools", "vscode-runs", "run-1.jsonl"))
	if err != nil {
		t.Fatalf("read stored run stream: %v", err)
	}
	if !bytes.Equal(stored, protocol.Bytes()) {
		t.Fatalf("stored run stream differs from protocol:\nstored=%s\nprotocol=%s", stored, protocol.String())
	}
	if !strings.Contains(logs.String(), "invalid run event") {
		t.Fatalf("logs = %s, want invalid event warning", logs.String())
	}
}

func TestPrivateHelpersCoverDefaultsAndErrors(t *testing.T) {
	root := t.TempDir()
	fake := newInternalBridge(root)
	rt := runtimeOrDefault(context.Background(), fake)
	if rt.Root != root || rt.Protocol == nil {
		t.Fatalf("runtime default = %+v, want root and discard protocol", rt)
	}
	if got := runtimeRoot(Runtime{}, fake); got != root {
		t.Fatalf("runtimeRoot empty = %q, want bridge root", got)
	}
	if got := (RunError{ExitCode: 7}).Error(); got != "testbridge run exited 7" {
		t.Fatalf("nil RunError text = %q", got)
	}
	if _, err := parseIDs([]string{"--format"}, true, true); err == nil || !strings.Contains(err.Error(), "--format supports only json") {
		t.Fatalf("parse missing format err = %v", err)
	}
	if err := writeDocument(Runtime{}, vscode.RunEvent{}); err == nil || !strings.Contains(err.Error(), "run-event missing") {
		t.Fatalf("write invalid doc err = %v, want validation failure", err)
	}

	fileRoot := filepath.Join(t.TempDir(), "file-root")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newRunWriter(Runtime{Root: fileRoot}, Workspace{Root: fileRoot}, "run-1"); err == nil {
		t.Fatal("newRunWriter with file root returned nil, want mkdir error")
	}
	if _, err := acquireRunLock(fileRoot, []string{"x"}, "t"); err == nil {
		t.Fatal("acquireRunLock with file root returned nil, want mkdir error")
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-87
func TestDesiredStateReportContextAndBridgeMaintenanceBranches(t *testing.T) {
	if DesiredStateReportRequested(context.Background()) {
		t.Fatal("plain context reported desired-state document mode")
	}
	if !DesiredStateReportRequested(withDesiredStateReport(context.Background())) {
		t.Fatal("withDesiredStateReport context did not report desired-state document mode")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(RunLockPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RunLockPath(root), []byte(`{"pid":1,"created_at":"2026-07-18T00:00:00Z","ids":["x"],"token":"t"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bridge := maintenanceBridge{internalBridge: newInternalBridge(root)}
	var events []vscode.RunEvent
	writer := func(event vscode.RunEvent) { events = append(events, event) }

	code, err := runBridgeMaintenance(context.Background(), &bridge, root, "run-1", MaintenanceUnlockID, writer)
	if err != nil || code != 0 {
		t.Fatalf("unlock maintenance = code %d err %v, want success", code, err)
	}
	if _, err := os.Stat(RunLockPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unlock did not remove run lock, stat err = %v", err)
	}
	code, err = runBridgeMaintenance(context.Background(), &bridge, root, "run-1", MaintenanceClearStateID, writer)
	if err != nil || code != 0 || !bridge.cleared {
		t.Fatalf("clear-state maintenance = code %d err %v cleared=%v, want success", code, err, bridge.cleared)
	}
	code, err = runBridgeMaintenance(context.Background(), newInternalBridge(root), root, "run-1", MaintenanceClearStateID, writer)
	if err == nil || code != 1 || !strings.Contains(err.Error(), "does not implement clear-state") {
		t.Fatalf("clear-state without provider = code %d err %v, want provider error", code, err)
	}
	code, err = runBridgeMaintenance(context.Background(), &bridge, root, "run-1", "keel::maintenance::bogus", writer)
	if err == nil || code != 2 || !strings.Contains(err.Error(), "unknown bridge maintenance id") {
		t.Fatalf("unknown maintenance = code %d err %v, want usage error", code, err)
	}
	if !eventsContain(events, "test_started", MaintenanceUnlockID) || !eventsContain(events, "passed", MaintenanceUnlockID) {
		t.Fatalf("maintenance events = %+v, want unlock start/pass", events)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-92
func TestPruneCompletedRunStreamsKeepsNewestCompletedAndActive(t *testing.T) {
	runDir := t.TempDir()
	writeRunStream(t, runDir, "old.jsonl", "run_finished", time.Unix(10, 0).UTC())
	writeRunStream(t, runDir, "middle.jsonl", "run_finished", time.Unix(20, 0).UTC())
	writeRunStream(t, runDir, "new.jsonl", "run_finished", time.Unix(30, 0).UTC())
	writeRunStream(t, runDir, "active.jsonl", "passed", time.Unix(40, 0).UTC())
	if err := os.WriteFile(filepath.Join(runDir, "notes.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := pruneCompletedRunStreams(runDir, 2); err != nil {
		t.Fatalf("pruneCompletedRunStreams: %v", err)
	}
	for _, want := range []string{"middle.jsonl", "new.jsonl", "active.jsonl", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(runDir, want)); err != nil {
			t.Fatalf("%s missing after prune: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(runDir, "old.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old completed stream stat = %v, want removed", err)
	}
	if err := pruneCompletedRunStreams(runDir, 0); err != nil {
		t.Fatalf("prune keep=0: %v", err)
	}
	if err := pruneCompletedRunStreams(filepath.Join(runDir, "missing"), 1); err == nil {
		t.Fatal("prune missing dir returned nil, want read error")
	}
}

func TestConfigErrorBranches(t *testing.T) {
	root := t.TempDir()
	template := vscode.TestBridgeConfig{Version: vscode.CurrentConfigVersion, Command: "bin/demo", Args: []string{}, DisplayName: "Demo"}
	path := vscode.TestBridgeConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{bad json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InitConfig(root, template); err == nil || !strings.Contains(err.Error(), "parse test bridge config") {
		t.Fatalf("InitConfig malformed err = %v, want parse failure", err)
	}
	if _, err := UpgradeConfig(root, template); err == nil || !strings.Contains(err.Error(), "parse test bridge config") {
		t.Fatalf("UpgradeConfig malformed err = %v, want parse failure", err)
	}

	if err := os.WriteFile(path, []byte(`{"version":0,"command":"bin/demo","args":[],"displayName":"Demo"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := UpgradeConfig(root, template); err == nil || !strings.Contains(err.Error(), "missing or unsupported") {
		t.Fatalf("UpgradeConfig version 0 err = %v, want unsupported", err)
	}

	if _, err := UpgradeConfig(filepath.Join(t.TempDir(), "missing"), template); err == nil {
		t.Fatal("UpgradeConfig missing file returned nil, want read error")
	}
	if err := writeConfig(filepath.Join(t.TempDir(), "config.json"), vscode.TestBridgeConfig{Version: 999}); err == nil || !strings.Contains(err.Error(), "config version") {
		t.Fatalf("writeConfig invalid template err = %v, want validation failure", err)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-58, keel/requirement-60
func TestValidateDocumentRejectsProtocolEdgeCases(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name string
		doc  any
		want string
	}{
		{
			name: "unsupported",
			doc:  struct{}{},
			want: "unsupported protocol document",
		},
		{
			name: "discovery invalid item profile",
			doc: vscode.DiscoveryDocument{
				Version:     1,
				Workspace:   "workspace",
				ModulePath:  "example.com/mod",
				GeneratedAt: now,
				Items: []vscode.TestItem{{
					ID:       "demo::test::one",
					Label:    "one",
					Kind:     "test",
					Profiles: []string{"bogus"},
				}},
			},
			want: "invalid profile",
		},
		{
			name: "desired duplicate run id",
			doc: vscode.DesiredStateDocument{
				Version:     3,
				Workspace:   "workspace",
				GeneratedAt: now,
				Devtool:     vscode.DevtoolMetadata{Name: "tool", Version: "v1"},
				Groups: []vscode.DesiredStateGroup{{
					Label: "group",
					Rows: []vscode.DesiredState{
						validDesiredState("row-a", "shared"),
						validDesiredState("row-b", "shared"),
					},
				}},
			},
			want: "run ids must be unique",
		},
		{
			name: "desired invalid row kind",
			doc: desiredStateDoc(now, vscode.DesiredState{
				Resource: "row-kind",
				Kind:     "nonsense",
				Desired:  "available",
				Current:  "missing",
				Status:   "blocked",
				Action:   "manual_setup_required",
			}),
			want: "invalid kind",
		},
		{
			name: "desired invalid row status",
			doc: desiredStateDoc(now, vscode.DesiredState{
				Resource: "row-status",
				Kind:     "tool",
				Desired:  "available",
				Current:  "missing",
				Status:   "unknown",
				Action:   "manual_setup_required",
			}),
			want: "invalid status",
		},
		{
			name: "desired invalid row action",
			doc: desiredStateDoc(now, vscode.DesiredState{
				Resource: "row-action",
				Kind:     "tool",
				Desired:  "available",
				Current:  "missing",
				Status:   "blocked",
				Action:   "wait",
			}),
			want: "invalid action",
		},
		{
			name: "run event invalid source",
			doc:  vscode.RunEvent{Version: 1, Event: "passed", Time: now, Source: "cli"},
			want: "invalid source",
		},
		{
			name: "run event negative duration",
			doc:  vscode.RunEvent{Version: 1, Event: "passed", Time: now, DurationMS: -1},
			want: "negative duration_ms",
		},
		{
			name: "run event artifact kind",
			doc: vscode.RunEvent{
				Version:  1,
				Event:    "artifact",
				Time:     now,
				TestID:   "demo::test::one",
				Artifact: &vscode.RunArtifact{Name: "bad", URI: "file:///tmp/bad", Kind: "binary"},
			},
			want: "artifact has invalid kind",
		},
		{
			name: "run lock token",
			doc:  vscode.RunLockFile{PID: 1, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{"demo::test::one"}},
			want: "token is required",
		},
		{
			name: "run lock created time",
			doc:  vscode.RunLockFile{PID: 1, CreatedAt: "not-time", IDs: []string{"demo::test::one"}, Token: "token"},
			want: "created_at",
		},
		{
			name: "run lock empty id",
			doc:  vscode.RunLockFile{PID: 1, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{""}, Token: "token"},
			want: "empty id",
		},
		{
			name: "config protocol args",
			doc:  vscode.TestBridgeConfig{Version: vscode.CurrentConfigVersion, Command: "bin/tool", DisplayName: "Tool", Args: []string{"test-bridge", "tests"}},
			want: "launcher-only",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDocument(tc.doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateDocument err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func desiredStateDoc(now time.Time, rows ...vscode.DesiredState) vscode.DesiredStateDocument {
	return vscode.DesiredStateDocument{
		Version:     3,
		Workspace:   "workspace",
		GeneratedAt: now,
		Devtool:     vscode.DevtoolMetadata{Name: "tool", Version: "v1"},
		Groups: []vscode.DesiredStateGroup{{
			Label: "group",
			Rows:  rows,
		}},
	}
}

func validDesiredState(resource, runID string) vscode.DesiredState {
	return vscode.DesiredState{
		Resource: resource,
		Kind:     "tool",
		Desired:  "available",
		Current:  "available",
		Status:   "satisfied",
		Action:   "reuse",
		RunID:    runID,
	}
}

type internalBridge struct {
	root string
}

type maintenanceBridge struct {
	internalBridge
	cleared bool
}

func (b *maintenanceBridge) ClearState(context.Context, RunRequest, vscode.RunEventWriter) (int, error) {
	b.cleared = true
	return 0, nil
}

func eventsContain(events []vscode.RunEvent, event, testID string) bool {
	for _, candidate := range events {
		if candidate.Event == event && candidate.TestID == testID {
			return true
		}
	}
	return false
}

func writeRunStream(t *testing.T, dir, name, event string, when time.Time) {
	t.Helper()
	line, err := vscode.MarshalRunEventJSONL(vscode.RunEvent{Event: event, Time: when})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), line, 0o644); err != nil {
		t.Fatal(err)
	}
}

func newInternalBridge(root string) internalBridge {
	return internalBridge{root: root}
}

func (b internalBridge) Discover(context.Context) (vscode.DiscoveryDocument, error) {
	return vscode.DiscoveryDocument{}, nil
}

func (b internalBridge) DesiredState(context.Context, []string) (DesiredStateDeclaration, error) {
	return DesiredStateDeclaration{}, nil
}

func (b internalBridge) Run(context.Context, RunRequest, vscode.RunEventWriter) (int, error) {
	return 0, nil
}

func (b internalBridge) ConfigTemplate() vscode.TestBridgeConfig {
	return vscode.TestBridgeConfig{}
}

func (b internalBridge) Workspace() Workspace {
	return Workspace{Root: b.root}
}

func (b internalBridge) Metadata() vscode.DevtoolMetadata {
	return vscode.DevtoolMetadata{}
}
