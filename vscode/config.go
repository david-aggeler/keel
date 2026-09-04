package vscode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const CurrentConfigVersion = 5

// launcherOnlyArgsSinceVersion is the config version from which args carry the
// launcher only, the protocol tokens being appended by the reader. It is
// stated separately from CurrentConfigVersion so that a later version bump
// keeps enforcing the rule against every version that ever declared it.
//
// DHF-REQ: keel/requirement-59
const launcherOnlyArgsSinceVersion = 3

type TestBridgeConfig struct {
	Version     int               `json:"version"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	DisplayName string            `json:"displayName"`
	Env         map[string]string `json:"env,omitempty"`
	// Display carries one toggle per rendered fact class. Absent — the shape
	// every config below version 4 has — means every class is enabled, so a
	// workspace that has not migrated renders exactly what it rendered before.
	//
	// DHF-REQ: keel/requirement-139
	Display *DisplayConfig `json:"display,omitempty"`
	// DiscoveryMaxBufferBytes is the workspace's optional override of the
	// discovery/desired-state stdout size bound the VSIX enforces. Keel's Go
	// side neither reads nor validates it — the field exists here so the
	// config writer round-trips a workspace-set override instead of silently
	// dropping it on upgrade. Absent means the VSIX's built-in default.
	//
	// DHF-REQ: keel/requirement-163
	DiscoveryMaxBufferBytes *int `json:"discoveryMaxBufferBytes,omitempty"`
}

// DisplayOrDefault resolves the effective display configuration: an absent
// block enables every class.
//
// DHF-REQ: keel/requirement-139
func (c TestBridgeConfig) DisplayOrDefault() DisplayConfig {
	if c.Display == nil {
		return DefaultDisplayConfig()
	}
	return *c.Display
}

type ConfigUpgradeResult struct {
	Path        string
	Changed     bool
	FromVersion int
	ToVersion   int
}

// DefaultTestBridgeConfig is the VSIX-embedded template and the Go-owned source
// of truth used by `keel-dev test-bridge config init`.
//
// DHF-REQ: keel/requirement-40
func DefaultTestBridgeConfig() TestBridgeConfig {
	display := DefaultDisplayConfig()
	return TestBridgeConfig{
		Version:     CurrentConfigVersion,
		Command:     "bin/keel-dev",
		Args:        []string{},
		DisplayName: "Keel",
		Display:     &display,
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
	if cfg.Version >= launcherOnlyArgsSinceVersion && hasProtocolTokens(cfg.Args) {
		return TestBridgeConfig{}, fmt.Errorf("keel/vscode: test bridge config v%d args must be launcher-only", launcherOnlyArgsSinceVersion)
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
		next, err := MigrateTestBridgeConfig(cfg, DefaultTestBridgeConfig())
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

// MigrateTestBridgeConfig advances a config by exactly one version and is the
// only statement of the migration ladder. Both upgrade entry points — this
// package's and keel/testbridge's, which backs `keel-dev test-bridge
// config-upgrade` — climb through it, so a rung added here cannot be skipped
// by one of them. The template supplies the values an incomplete older config
// is missing.
//
// DHF-REQ: keel/requirement-65, keel/requirement-139
func MigrateTestBridgeConfig(cfg, template TestBridgeConfig) (TestBridgeConfig, error) {
	switch cfg.Version {
	case 0:
		return TestBridgeConfig{}, fmt.Errorf("keel/vscode: test bridge config version is missing or unsupported")
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
		cfg.Version = 3
		cfg.Args = trimLegacyVSCodeTestsPrefix(cfg.Args)
		if cfg.Command == "" {
			cfg.Command = template.Command
		}
		if cfg.DisplayName == "" {
			cfg.DisplayName = template.DisplayName
		}
		return cfg, nil
	case 3:
		// Every class enabled, so the upgraded workspace renders every fact it
		// rendered before the toggles existed (keel/ac-556).
		cfg.Version = 4
		if cfg.Display == nil {
			display := DefaultDisplayConfig()
			cfg.Display = &display
		}
		return cfg, nil
	case 4:
		// The display block gains the label ordinal toggle. It stays off, which
		// is what makes the upgrade state the visible change rather than undo
		// it (keel/ac-562).
		cfg.Version = 5
		if cfg.Display == nil {
			display := DefaultDisplayConfig()
			cfg.Display = &display
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
