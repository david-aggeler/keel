package vscode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const CurrentConfigVersion = 3

type TestBridgeConfig struct {
	Version     int               `json:"version"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	DisplayName string            `json:"displayName"`
	Env         map[string]string `json:"env,omitempty"`
}

type ConfigUpgradeResult struct {
	Path        string
	Changed     bool
	FromVersion int
	ToVersion   int
}

// DefaultTestBridgeConfig is the VSIX-embedded template and the Go-owned source
// of truth used by `keel-dev vscode config init`.
//
// DHF-REQ: keel/requirement-40
func DefaultTestBridgeConfig() TestBridgeConfig {
	return TestBridgeConfig{
		Version:     CurrentConfigVersion,
		Command:     "bin/keel-dev",
		Args:        []string{},
		DisplayName: "Keel",
	}
}

func TestBridgeConfigPath(root string) string {
	return filepath.Join(root, ".vscode", "test-bridge.json")
}

// ReadTestBridgeConfig tolerantly reads the config object. Newer configs are
// accepted so older extensions can keep operating without mutating the file.
//
// DHF-REQ: keel/requirement-40
func ReadTestBridgeConfig(root string) (TestBridgeConfig, error) {
	data, err := os.ReadFile(TestBridgeConfigPath(root))
	if err != nil {
		return TestBridgeConfig{}, err
	}
	var cfg TestBridgeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return TestBridgeConfig{}, fmt.Errorf("keel/vscode: parse test bridge config: %w", err)
	}
	if cfg.Version >= CurrentConfigVersion && hasProtocolTokens(cfg.Args) {
		return TestBridgeConfig{}, fmt.Errorf("keel/vscode: test bridge config v3 args must be launcher-only")
	}
	return cfg, nil
}

// InitTestBridgeConfig writes the current default template. It refuses to
// clobber an existing config; upgrades are handled by UpgradeTestBridgeConfig.
//
// DHF-REQ: keel/requirement-40
func InitTestBridgeConfig(root string) (ConfigUpgradeResult, error) {
	target := TestBridgeConfigPath(root)
	if _, err := os.Stat(target); err == nil {
		cfg, readErr := ReadTestBridgeConfig(root)
		if readErr != nil {
			return ConfigUpgradeResult{}, readErr
		}
		return ConfigUpgradeResult{Path: target, FromVersion: cfg.Version, ToVersion: cfg.Version}, nil
	} else if !os.IsNotExist(err) {
		return ConfigUpgradeResult{}, err
	}
	if err := writeConfigFile(target, DefaultTestBridgeConfig()); err != nil {
		return ConfigUpgradeResult{}, err
	}
	return ConfigUpgradeResult{Path: target, Changed: true, FromVersion: 0, ToVersion: CurrentConfigVersion}, nil
}

// UpgradeTestBridgeConfig migrates supported older configs to the current
// version while preserving user-owned values. It is byte-idempotent and refuses
// newer-than-binary configs without writing.
//
// DHF-REQ: keel/requirement-40
func UpgradeTestBridgeConfig(root string) (ConfigUpgradeResult, error) {
	target := TestBridgeConfigPath(root)
	before, err := os.ReadFile(target)
	if err != nil {
		return ConfigUpgradeResult{}, err
	}
	var cfg TestBridgeConfig
	if err := json.Unmarshal(before, &cfg); err != nil {
		return ConfigUpgradeResult{}, fmt.Errorf("keel/vscode: parse test bridge config: %w", err)
	}
	from := cfg.Version
	if from > CurrentConfigVersion {
		return ConfigUpgradeResult{}, fmt.Errorf("keel/vscode: test bridge config version %d is newer than this binary supports (%d); refusing to write", from, CurrentConfigVersion)
	}
	for cfg.Version < CurrentConfigVersion {
		next, err := migrateTestBridgeConfig(cfg)
		if err != nil {
			return ConfigUpgradeResult{}, err
		}
		cfg = next
	}
	after, err := marshalConfig(cfg)
	if err != nil {
		return ConfigUpgradeResult{}, err
	}
	if bytes.Equal(before, after) {
		return ConfigUpgradeResult{Path: target, FromVersion: from, ToVersion: cfg.Version}, nil
	}
	if err := os.WriteFile(target, after, 0o644); err != nil {
		return ConfigUpgradeResult{}, err
	}
	return ConfigUpgradeResult{Path: target, Changed: true, FromVersion: from, ToVersion: cfg.Version}, nil
}

func migrateTestBridgeConfig(cfg TestBridgeConfig) (TestBridgeConfig, error) {
	switch cfg.Version {
	case 0:
		return TestBridgeConfig{}, fmt.Errorf("keel/vscode: test bridge config version is missing or unsupported")
	case 1:
		cfg.Version = 2
		if cfg.Command == "" {
			cfg.Command = DefaultTestBridgeConfig().Command
		}
		if len(cfg.Args) == 0 {
			cfg.Args = []string{"vscode", "tests"}
		}
		if cfg.DisplayName == "" {
			cfg.DisplayName = DefaultTestBridgeConfig().DisplayName
		}
		return cfg, nil
	case 2:
		cfg.Version = CurrentConfigVersion
		cfg.Args = trimLegacyVSCodeTestsPrefix(cfg.Args)
		if cfg.Command == "" {
			cfg.Command = DefaultTestBridgeConfig().Command
		}
		if cfg.DisplayName == "" {
			cfg.DisplayName = DefaultTestBridgeConfig().DisplayName
		}
		return cfg, nil
	default:
		return TestBridgeConfig{}, fmt.Errorf("keel/vscode: unsupported test bridge config version %d", cfg.Version)
	}
}

func trimLegacyVSCodeTestsPrefix(args []string) []string {
	out := append([]string(nil), args...)
	if len(out) >= 2 && out[len(out)-2] == "vscode" && out[len(out)-1] == "tests" {
		return out[:len(out)-2]
	}
	return out
}

func hasProtocolTokens(args []string) bool {
	for i, arg := range args {
		if arg == "test-bridge" {
			return true
		}
		if arg == "vscode" && i+1 < len(args) && (args[i+1] == "tests" || args[i+1] == "config") {
			return true
		}
	}
	return false
}

func writeConfigFile(target string, cfg TestBridgeConfig) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	body, err := marshalConfig(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(target, body, 0o644)
}

func marshalConfig(cfg TestBridgeConfig) ([]byte, error) {
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}
