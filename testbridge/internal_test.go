package testbridge

import (
	"bytes"
	"context"
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
	release, err := acquireRunLock(root, []string{"demo::lane::fast"}, "token-1")
	if err != nil {
		t.Fatalf("acquire run lock: %v", err)
	}
	if _, err := os.Stat(RunLockPath(root)); err != nil {
		t.Fatalf("lock missing after acquire: %v", err)
	}
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
	template := vscode.TestBridgeConfig{Version: vscode.CurrentConfigVersion, Command: "bin/demo", Args: []string{"test-bridge"}, DisplayName: "Demo"}
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

	if err := os.WriteFile(path, []byte(`{"version":0,"command":"bin/demo","args":["test-bridge"],"displayName":"Demo"}`+"\n"), 0o644); err != nil {
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

func (b internalBridge) DesiredState(context.Context, []string) (vscode.SetupPlan, error) {
	return vscode.SetupPlan{}, nil
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
