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

type internalBridge struct {
	root string
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
