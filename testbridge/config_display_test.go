package testbridge_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// TestUpgradeConfigMigratesV3ToCurrentEnablingEveryClass holds keel/ac-556 on the
// verb the operator actually runs: `keel-dev test-bridge config-upgrade`
// resolves through testbridge.UpgradeConfig, so the migration ladder that
// matters is this one. A v3 workspace gains a display block with every class
// enabled and keeps command, args, displayName and env unchanged.
//
// DHF-TEST: keel/requirement-139
func TestUpgradeConfigMigratesV3ToCurrentEnablingEveryClass(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const before = `{"version":3,"command":"bin/custom","args":["--flag"],"displayName":"Custom","env":{"A":"B"}}` + "\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	res, err := testbridge.UpgradeConfig(root, vscode.DefaultTestBridgeConfig())
	if err != nil {
		t.Fatalf("upgrade config: %v", err)
	}
	if !res.Changed || res.FromVersion != 3 || res.ToVersion != vscode.CurrentConfigVersion {
		t.Fatalf("upgrade result = %+v, want a change from 3 to %d", res, vscode.CurrentConfigVersion)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read upgraded config: %v", err)
	}
	var cfg vscode.TestBridgeConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("parse upgraded config: %v", err)
	}
	if cfg.Version != vscode.CurrentConfigVersion {
		t.Fatalf("upgraded version = %d, want %d", cfg.Version, vscode.CurrentConfigVersion)
	}
	if cfg.Command != "bin/custom" || cfg.DisplayName != "Custom" || len(cfg.Args) != 1 || cfg.Args[0] != "--flag" || cfg.Env["A"] != "B" {
		t.Fatalf("upgrade did not preserve consumer-owned members: %+v", cfg)
	}
	if cfg.Display == nil || *cfg.Display != vscode.DefaultDisplayConfig() {
		t.Fatalf("upgraded display = %+v, want every class enabled", cfg.Display)
	}
}

// TestUpgradeConfigClimbsEveryRungFromV1 keeps the ladder whole: a version-1
// file reaches the current version through every intermediate step rather than
// jumping the newest one, which is how a config arrives at version 4 carrying
// none of version 4's content.
//
// DHF-TEST: keel/requirement-139
func TestUpgradeConfigClimbsEveryRungFromV1(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := testbridge.UpgradeConfig(root, vscode.DefaultTestBridgeConfig()); err != nil {
		t.Fatalf("upgrade config: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read upgraded config: %v", err)
	}
	var cfg vscode.TestBridgeConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("parse upgraded config: %v", err)
	}
	if cfg.Version != vscode.CurrentConfigVersion {
		t.Fatalf("upgraded version = %d, want %d", cfg.Version, vscode.CurrentConfigVersion)
	}
	if cfg.Display == nil || *cfg.Display != vscode.DefaultDisplayConfig() {
		t.Fatalf("upgraded display = %+v, want every class enabled", cfg.Display)
	}
}
