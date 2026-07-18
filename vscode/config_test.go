package vscode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-40
func TestTestBridgeConfigSchemaIsEmbeddedAndVersionPinned(t *testing.T) {
	body, err := SchemaBytes(SchemaTestBridgeConfig)
	if err != nil {
		t.Fatalf("read test bridge config schema: %v", err)
	}
	var schema struct {
		ID         string `json:"$id"`
		Title      string `json:"title"`
		Properties map[string]struct {
			Const int `json:"const"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body, &schema); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if !strings.Contains(schema.ID, "/test-bridge-config.schema.json") {
		t.Fatalf("schema id = %q, want test-bridge-config schema id", schema.ID)
	}
	if got := schema.Properties["version"].Const; got != CurrentConfigVersion {
		t.Fatalf("schema version const = %d, want CurrentConfigVersion %d", got, CurrentConfigVersion)
	}
}

// DHF-TEST: keel/requirement-40
func TestUpgradeTestBridgeConfigMigratesPreservesUserValuesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := []byte(`{
  "version": 2,
  "command": "bin/custom-dev",
  "args": ["go", "run", "./cmd/custom-dev", "` + "vs" + `code", "tests"],
  "displayName": "Custom",
  "env": {"CUSTOM": "1"}
}
`)
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := UpgradeTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("upgrade config: %v", err)
	}
	if !res.Changed || res.FromVersion != 2 || res.ToVersion != CurrentConfigVersion {
		t.Fatalf("upgrade result = %+v, want changed 2 -> current", res)
	}
	got, err := ReadTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("read upgraded config: %v", err)
	}
	if got.Command != "bin/custom-dev" || got.DisplayName != "Custom" || got.Env["CUSTOM"] != "1" {
		t.Fatalf("upgrade did not preserve user values: %+v", got)
	}
	if want := []string{"go", "run", "./cmd/custom-dev"}; !equalStrings(got.Args, want) {
		t.Fatalf("upgraded args = %#v, want launcher-only %#v", got.Args, want)
	}

	firstBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := UpgradeTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	secondBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatalf("second upgrade changed file: %+v", second)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("idempotent upgrade rewrote bytes:\nfirst:\n%s\nsecond:\n%s", firstBytes, secondBytes)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-40, keel/requirement-65
func TestUpgradeTestBridgeConfigMigratesVersionOneDefaultsAndRejectsMissingVersion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := UpgradeTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("upgrade v1 config: %v", err)
	}
	if !res.Changed || res.FromVersion != 1 || res.ToVersion != CurrentConfigVersion {
		t.Fatalf("v1 upgrade result = %+v, want changed 1 -> current", res)
	}
	cfg, err := ReadTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("read upgraded v1 config: %v", err)
	}
	if cfg.Command != DefaultTestBridgeConfig().Command || cfg.DisplayName != DefaultTestBridgeConfig().DisplayName {
		t.Fatalf("v1 defaults = %+v, want default command/displayName", cfg)
	}

	if err := os.WriteFile(path, []byte(`{"command":"bin/legacy"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = UpgradeTestBridgeConfig(root)
	if err == nil || !strings.Contains(err.Error(), "missing or unsupported") {
		t.Fatalf("missing version upgrade error = %v, want unsupported-version error", err)
	}
}

// DHF-TEST: keel/requirement-40
func TestUpgradeTestBridgeConfigRefusesNewerVersionWithoutWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	newer := []byte(`{"version":999,"command":"bin/future","args":["wrapper"],"displayName":"Future"}` + "\n")
	if err := os.WriteFile(path, newer, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := UpgradeTestBridgeConfig(root)
	if err == nil || !strings.Contains(err.Error(), "newer than this binary") {
		t.Fatalf("upgrade newer config error = %v, want clear newer-than-binary refusal", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, newer) {
		t.Fatalf("newer config was rewritten:\n%s", after)
	}
	if cfg, err := ReadTestBridgeConfig(root); err != nil || cfg.Command != "bin/future" {
		t.Fatalf("tolerant read of newer config = %+v, %v", cfg, err)
	}
}

// DHF-TEST: keel/requirement-40
func TestInitTestBridgeConfigWritesDefaultTemplate(t *testing.T) {
	root := t.TempDir()
	res, err := InitTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	if !res.Changed {
		t.Fatalf("init result = %+v, want changed", res)
	}
	cfg, err := ReadTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("read initialized config: %v", err)
	}
	if cfg.Version != CurrentConfigVersion || cfg.Command != "bin/keel-dev" || cfg.DisplayName != "Keel" {
		t.Fatalf("default config = %+v, want current keel template", cfg)
	}
	if len(cfg.Args) != 0 {
		t.Fatalf("default args = %#v, want launcher-only empty args", cfg.Args)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-40
func TestInitTestBridgeConfigPreservesExistingConfigAndReportsNoChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{"version":3,"command":"bin/custom","args":[],"displayName":"Custom"}` + "\n")
	if err := os.WriteFile(path, existing, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := InitTestBridgeConfig(root)
	if err != nil {
		t.Fatalf("init existing config: %v", err)
	}
	if res.Changed || res.FromVersion != CurrentConfigVersion || res.ToVersion != CurrentConfigVersion {
		t.Fatalf("existing init result = %+v, want no change at current version", res)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, existing) {
		t.Fatalf("InitTestBridgeConfig rewrote existing config:\n%s", after)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-40
func TestReadAndInitTestBridgeConfigRejectMalformedAndProtocolArgs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{bad json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTestBridgeConfig(root); err == nil || !strings.Contains(err.Error(), "parse test bridge config") {
		t.Fatalf("ReadTestBridgeConfig malformed err = %v, want parse failure", err)
	}
	if _, err := InitTestBridgeConfig(root); err == nil || !strings.Contains(err.Error(), "parse test bridge config") {
		t.Fatalf("InitTestBridgeConfig malformed err = %v, want parse failure", err)
	}

	if err := os.WriteFile(path, []byte(`{"version":3,"command":"bin/tool","args":["vscode","config"],"displayName":"Tool"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTestBridgeConfig(root); err == nil || !strings.Contains(err.Error(), "launcher-only") {
		t.Fatalf("ReadTestBridgeConfig protocol args err = %v, want launcher-only refusal", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
