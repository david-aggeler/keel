package vscode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigUpgradeMigratesV3ToV4EnablingEveryClass holds keel/ac-556: a valid
// version-3 config carrying no display block upgrades to version 4 with every
// fact class enabled, and command, args, displayName and env survive the
// rewrite with their values intact — so upgrading a workspace hides nothing
// that was visible beforehand.
//
// DHF-TEST: keel/requirement-139
func TestConfigUpgradeMigratesV3ToV4EnablingEveryClass(t *testing.T) {
	root := t.TempDir()
	path := TestBridgeConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const before = `{"version":3,"command":"bin/custom","args":["--flag","value"],"displayName":"Custom","env":{"A":"B"}}` + "\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	res, err := UpgradeTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("UpgradeTestBridgeConfig: %v", err)
	}
	if !res.Changed || res.FromVersion != 3 || res.ToVersion != 4 {
		t.Fatalf("upgrade result = %+v, want a change from 3 to 4", res)
	}

	cfg, err := ReadTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("ReadTestBridgeConfig: %v", err)
	}
	if cfg.Version != 4 {
		t.Fatalf("upgraded version = %d, want 4", cfg.Version)
	}
	if cfg.Command != "bin/custom" || cfg.DisplayName != "Custom" {
		t.Fatalf("upgrade did not preserve command/displayName: %+v", cfg)
	}
	if len(cfg.Args) != 2 || cfg.Args[0] != "--flag" || cfg.Args[1] != "value" {
		t.Fatalf("upgrade did not preserve args: %+v", cfg.Args)
	}
	if cfg.Env["A"] != "B" || len(cfg.Env) != 1 {
		t.Fatalf("upgrade did not preserve env: %+v", cfg.Env)
	}
	if cfg.Display == nil {
		t.Fatal("upgraded config carries no display block")
	}
	if *cfg.Display != DefaultDisplayConfig() {
		t.Fatalf("upgraded display = %+v, want every class enabled", *cfg.Display)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read upgraded config: %v", err)
	}
	for _, want := range []string{`"description": true`, `"lastRun": true`, `"desiredState": true`, `"findings": true`, `"version": 4`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("upgraded config body missing %s:\n%s", want, body)
		}
	}
}

// TestConfigUpgradeIsIdempotentAtV4 keeps the migration byte-stable: running
// it against a file that is already current rewrites nothing.
//
// DHF-TEST: keel/requirement-139
func TestConfigUpgradeIsIdempotentAtV4(t *testing.T) {
	root := t.TempDir()
	path := TestBridgeConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":3,"command":"bin/custom","args":[],"displayName":"Custom"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := UpgradeTestBridgeConfig(root); err != nil {
		t.Fatalf("first upgrade: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	res, err := UpgradeTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	if res.Changed {
		t.Fatal("second upgrade rewrote an already-current config")
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("second upgrade changed the bytes\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestDefaultTestBridgeConfigEmitsEveryClassEnabled keeps the template honest:
// a freshly initialized workspace states its display contract rather than
// leaving a reader to infer it.
//
// DHF-TEST: keel/requirement-139
func TestDefaultTestBridgeConfigEmitsEveryClassEnabled(t *testing.T) {
	cfg := DefaultTestBridgeConfig()
	if cfg.Version != CurrentConfigVersion {
		t.Fatalf("default version = %d, want %d", cfg.Version, CurrentConfigVersion)
	}
	if cfg.Display == nil || *cfg.Display != DefaultDisplayConfig() {
		t.Fatalf("default display = %+v, want every class enabled", cfg.Display)
	}
}

// TestReadTestBridgeConfigRejectsAnUnknownDisplayKey holds the Go half of
// keel/ac-566: a misspelled toggle is refused by name rather than silently
// ignored, because a silently ignored toggle reads to a user as a broken
// feature rather than a typo.
//
// DHF-TEST: keel/requirement-139
func TestReadTestBridgeConfigRejectsAnUnknownDisplayKey(t *testing.T) {
	root := t.TempDir()
	path := TestBridgeConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":4,"command":"bin/keel-dev","args":[],"displayName":"Keel","display":{"finding":false}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := ReadTestBridgeConfig(root)
	if err == nil {
		t.Fatal("ReadTestBridgeConfig accepted an unknown display key")
	}
	if !strings.Contains(err.Error(), "finding") {
		t.Fatalf("error %q does not name the unknown key", err)
	}
}

// TestDisplayConfigDefaultsEveryAbsentClassToEnabled holds the tolerant half of
// the display contract: an absent block, and an absent key inside a present
// block, both mean enabled. A class added in a later version therefore cannot
// disappear from an older workspace's tree.
//
// DHF-TEST: keel/requirement-139
func TestDisplayConfigDefaultsEveryAbsentClassToEnabled(t *testing.T) {
	root := t.TempDir()
	path := TestBridgeConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":4,"command":"bin/keel-dev","args":[],"displayName":"Keel","display":{"findings":false}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := ReadTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("ReadTestBridgeConfig: %v", err)
	}
	want := DisplayConfig{Description: true, LastRun: true, DesiredState: true, Findings: false}
	if cfg.Display == nil || *cfg.Display != want {
		t.Fatalf("display = %+v, want %+v", cfg.Display, want)
	}
	if got := cfg.DisplayOrDefault(); got != want {
		t.Fatalf("DisplayOrDefault() = %+v, want %+v", got, want)
	}

	if err := os.WriteFile(path, []byte(`{"version":4,"command":"bin/keel-dev","args":[],"displayName":"Keel"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	absent, err := ReadTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("ReadTestBridgeConfig: %v", err)
	}
	if absent.Display != nil {
		t.Fatalf("absent display decoded to %+v, want nil", absent.Display)
	}
	if got := absent.DisplayOrDefault(); got != DefaultDisplayConfig() {
		t.Fatalf("DisplayOrDefault() = %+v, want every class enabled", got)
	}
}

// TestDisplayConfigRoundTripsThroughJSON keeps the wire names of the toggles
// equal to the class names the renderer declares, so the config file and the
// renderer cannot disagree about what a class is called.
//
// DHF-TEST: keel/requirement-139
func TestDisplayConfigRoundTripsThroughJSON(t *testing.T) {
	body, err := json.Marshal(DefaultDisplayConfig())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]bool
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != len(DisplayClassOrder) {
		t.Fatalf("display config has %d keys, want one per class (%d)", len(decoded), len(DisplayClassOrder))
	}
	for _, class := range DisplayClassOrder {
		if !decoded[string(class)] {
			t.Fatalf("display config carries no enabled key %q; got %v", class, decoded)
		}
	}
}
