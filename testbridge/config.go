package testbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/david-aggeler/keel/vscode"
)

// ConfigResult reports whether config init or upgrade changed the workspace
// file.
type ConfigResult struct {
	Path        string
	Changed     bool
	FromVersion int
	ToVersion   int
}

// InitConfig writes template to .vscode/test-bridge.json when absent.
//
// DHF-REQ: keel/requirement-58
func InitConfig(root string, template vscode.TestBridgeConfig) (ConfigResult, error) {
	target := vscode.TestBridgeConfigPath(root)
	if _, err := os.Stat(target); err == nil {
		cfg, readErr := readConfig(target)
		if readErr != nil {
			return ConfigResult{}, readErr
		}
		return ConfigResult{Path: target, FromVersion: cfg.Version, ToVersion: cfg.Version}, nil
	} else if !os.IsNotExist(err) {
		return ConfigResult{}, err
	}
	if err := writeConfig(target, template); err != nil {
		return ConfigResult{}, err
	}
	return ConfigResult{Path: target, Changed: true, FromVersion: 0, ToVersion: template.Version}, nil
}

// UpgradeConfig migrates known older config files to the current version while
// preserving consumer-owned command, args, display name, and env values.
//
// DHF-REQ: keel/requirement-58
func UpgradeConfig(root string, template vscode.TestBridgeConfig) (ConfigResult, error) {
	target := vscode.TestBridgeConfigPath(root)
	before, err := os.ReadFile(target)
	if err != nil {
		return ConfigResult{}, err
	}
	var cfg vscode.TestBridgeConfig
	if err := json.Unmarshal(before, &cfg); err != nil {
		return ConfigResult{}, fmt.Errorf("keel/testbridge: parse test bridge config: %w", err)
	}
	from := cfg.Version
	if from > vscode.CurrentConfigVersion {
		return ConfigResult{}, fmt.Errorf("keel/testbridge: test bridge config version %d is newer than this binary supports (%d); refusing to write", from, vscode.CurrentConfigVersion)
	}
	for cfg.Version < vscode.CurrentConfigVersion {
		next, err := migrateConfig(cfg, template)
		if err != nil {
			return ConfigResult{}, err
		}
		cfg = next
	}
	after, err := marshalConfig(cfg)
	if err != nil {
		return ConfigResult{}, err
	}
	if bytes.Equal(before, after) {
		return ConfigResult{Path: target, FromVersion: from, ToVersion: cfg.Version}, nil
	}
	if err := os.WriteFile(target, after, 0o644); err != nil {
		return ConfigResult{}, err
	}
	return ConfigResult{Path: target, Changed: true, FromVersion: from, ToVersion: cfg.Version}, nil
}

// DHF-REQ: keel/requirement-65
func migrateConfig(cfg, template vscode.TestBridgeConfig) (vscode.TestBridgeConfig, error) {
	switch cfg.Version {
	case 0:
		return vscode.TestBridgeConfig{}, fmt.Errorf("keel/testbridge: test bridge config version is missing or unsupported")
	case 1:
		cfg.Version = 2
		if cfg.Command == "" {
			cfg.Command = template.Command
		}
		if len(cfg.Args) == 0 {
			cfg.Args = append([]string(nil), template.Args...)
		}
		if cfg.DisplayName == "" {
			cfg.DisplayName = template.DisplayName
		}
		return cfg, nil
	case 2:
		cfg.Version = vscode.CurrentConfigVersion
		cfg.Args = trimLegacyVSCodeTestsPrefix(cfg.Args)
		if cfg.Command == "" {
			cfg.Command = template.Command
		}
		if cfg.DisplayName == "" {
			cfg.DisplayName = template.DisplayName
		}
		return cfg, nil
	default:
		return vscode.TestBridgeConfig{}, fmt.Errorf("keel/testbridge: unsupported test bridge config version %d", cfg.Version)
	}
}

func trimLegacyVSCodeTestsPrefix(args []string) []string {
	out := append([]string(nil), args...)
	if len(out) >= 2 && out[len(out)-2] == "vscode" && out[len(out)-1] == "tests" {
		return out[:len(out)-2]
	}
	return out
}

func readConfig(path string) (vscode.TestBridgeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return vscode.TestBridgeConfig{}, err
	}
	var cfg vscode.TestBridgeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return vscode.TestBridgeConfig{}, fmt.Errorf("keel/testbridge: parse test bridge config: %w", err)
	}
	return cfg, nil
}

func writeConfig(path string, cfg vscode.TestBridgeConfig) error {
	if err := ValidateDocument(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := marshalConfig(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func marshalConfig(cfg vscode.TestBridgeConfig) ([]byte, error) {
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}
