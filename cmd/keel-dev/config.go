package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const keelDevConfigFile = "keel-dev.yaml"

// keelDevConfig is the single committed keel-dev configuration object. It
// carries operator-tunable gate policy, while ratcheted thresholds and gate
// behavior stay in Go so weakening them remains a reviewed code diff.
//
// DHF-REQ: keel/requirement-118 (keel/ac-446, keel/ac-447, keel/ac-448, keel/ac-449, keel/ac-450, keel/ac-451)
type keelDevConfig struct {
	// Gate controls file-selection policy for keel-dev's gate steps so generated
	// or handoff paths can be excluded before individual tools see them.
	Gate gateConfig `yaml:"gate"`
	// Tools controls external tool pins for gate and release checks so the
	// expected toolchain is declared in one editable product-policy file.
	Tools toolsConfig `yaml:"tools"`
}

// gateConfig contains file-selection knobs for keel-dev's gate steps. The gate
// sequence itself stays in Go because removing a check is behavior, not tuning.
type gateConfig struct {
	// Excludes lists repo-relative file patterns removed from file-selecting
	// gate inputs so generated or handoff prose cannot red unrelated work.
	Excludes gateExcludePatterns `yaml:"excludes"`
}

// toolsConfig contains the external command pins used by gate subprocess
// steps. Presence-only tools omit both version_args and want.
type toolsConfig struct {
	// Pins lists every external gate tool and its version probe so missing or
	// drifted tools fail loud before their checks run.
	Pins []toolPinConfig `yaml:"pins"`
}

// toolPinConfig is one serialized external-tool pin. VersionArgs and Want are
// both omitted for tools whose installers pin versions but binaries cannot
// report a stable version string.
type toolPinConfig struct {
	// Name is the executable resolved on PATH so diagnostics can name exactly
	// which gate dependency is absent or drifted.
	Name string `yaml:"name"`
	// VersionArgs is the argv suffix used to print a version when the tool has
	// a stable probe; empty means the pin is presence-only.
	VersionArgs []string `yaml:"version_args,omitempty"`
	// Want is the exact substring that must appear in the version probe output;
	// empty pairs with empty version_args for a presence-only pin.
	Want string `yaml:"want,omitempty"`
}

// gateExcludePatterns is the parsed exclude-pattern list. Its YAML decoder
// owns the exclude grammar so invalid values are rejected at the property.
type gateExcludePatterns []gateExcludePattern

// defaultKeelDevConfig returns a complete configuration value. Loading starts
// from this object and overlays the committed file onto it, so absent file keys
// never collapse to Go zero values.
//
// DHF-REQ: keel/requirement-118 (keel/ac-446, keel/ac-447)
func defaultKeelDevConfig() keelDevConfig {
	return keelDevConfig{
		Gate: gateConfig{
			Excludes: mustDefaultGateExcludePatterns(
				".claude/**",
				"docs/handoffs/**",
			),
		},
		Tools: toolsConfig{
			Pins: []toolPinConfig{
				// golangci-lint carries no leading "v": the v2.12 line prints
				// "has version 2.12.2" where v2.0.2 printed "has version v2.0.2",
				// and Want is matched as a plain substring.
				{Name: "golangci-lint", VersionArgs: []string{"--version"}, Want: "2.12.2"},
				{Name: "govulncheck", VersionArgs: []string{"--version"}, Want: "v1.7.0"},
				{Name: "cspell", VersionArgs: []string{"--version"}, Want: "10.0.1"},
				{Name: "shellcheck", VersionArgs: []string{"--version"}, Want: "0.10.0"},
				{Name: "shfmt", VersionArgs: []string{"--version"}, Want: "v3.13.1"},
				{Name: "deadcode"},
				{Name: "gitleaks"},
			},
		},
	}
}

func mustDefaultGateExcludePatterns(patterns ...string) gateExcludePatterns {
	out := make(gateExcludePatterns, 0, len(patterns))
	for _, pattern := range patterns {
		parsed, err := parseGateExcludeValue(pattern)
		if err != nil {
			panic(err)
		}
		out = append(out, parsed)
	}
	return out
}

// loadKeelDevConfig reads keel-dev's committed config file from the module root.
// A missing implicit file returns defaults; malformed, unknown, or invalid
// properties fail with the file path in the error.
func loadKeelDevConfig(root string) (keelDevConfig, error) {
	return loadKeelDevConfigFile(filepath.Join(root, keelDevConfigFile), false)
}

// loadKeelDevConfigFile loads a config file path. If explicit is true, a
// missing path is an error because the caller promised that file should exist.
//
// DHF-REQ: keel/requirement-118 (keel/ac-446, keel/ac-448, keel/ac-449)
func loadKeelDevConfigFile(file string, explicit bool) (keelDevConfig, error) {
	cfg := defaultKeelDevConfig()
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("keel-dev: load config %s: %w", file, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return cfg, fmt.Errorf("keel-dev: load config %s: %w", file, err)
	}
	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("keel-dev: validate config %s: %w", file, err)
	}
	return cfg, nil
}

func (c keelDevConfig) validate() error {
	if len(c.Gate.Excludes) == 0 {
		return fmt.Errorf("gate.excludes must contain at least one pattern")
	}
	if len(c.Tools.Pins) == 0 {
		return fmt.Errorf("tools.pins must contain at least one pin")
	}
	seen := make(map[string]bool, len(c.Tools.Pins))
	for i, pin := range c.Tools.Pins {
		prefix := fmt.Sprintf("tools.pins[%d]", i)
		if strings.TrimSpace(pin.Name) == "" {
			return fmt.Errorf("%s.name is required", prefix)
		}
		if seen[pin.Name] {
			return fmt.Errorf("%s.name %q duplicates an earlier pin", prefix, pin.Name)
		}
		seen[pin.Name] = true
		if pin.Want == "" && len(pin.VersionArgs) != 0 {
			return fmt.Errorf("%s.want is required when version_args is set", prefix)
		}
		if pin.Want != "" && len(pin.VersionArgs) == 0 {
			return fmt.Errorf("%s.version_args is required when want is set", prefix)
		}
		for j, arg := range pin.VersionArgs {
			if strings.TrimSpace(arg) == "" {
				return fmt.Errorf("%s.version_args[%d] must not be empty", prefix, j)
			}
		}
	}
	return nil
}

func (c keelDevConfig) toolPins() map[string]toolPin {
	out := make(map[string]toolPin, len(c.Tools.Pins))
	for _, pin := range c.Tools.Pins {
		out[pin.Name] = toolPin{name: pin.Name, versionArgs: append([]string(nil), pin.VersionArgs...), want: pin.Want}
	}
	return out
}

// UnmarshalYAML decodes and validates every exclude pattern at gate.excludes.
func (p *gateExcludePatterns) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.SequenceNode {
		return fmt.Errorf("gate.excludes must be a sequence")
	}
	out := make(gateExcludePatterns, 0, len(value.Content))
	for i, node := range value.Content {
		if node.Kind != yaml.ScalarNode {
			return fmt.Errorf("gate.excludes[%d] must be a scalar pattern", i)
		}
		parsed, err := parseGateExcludeValue(strings.TrimSpace(node.Value))
		if err != nil {
			return fmt.Errorf("gate.excludes[%d]: %w", i, err)
		}
		out = append(out, parsed)
	}
	*p = out
	return nil
}

func parseGateExcludeValue(pattern string) (gateExcludePattern, error) {
	if pattern == "" {
		return gateExcludePattern{}, fmt.Errorf("pattern must not be empty")
	}
	if strings.Contains(pattern, "\\") {
		return gateExcludePattern{}, fmt.Errorf("pattern %q must use slash separators", pattern)
	}
	if strings.HasPrefix(pattern, "/") || filepath.IsAbs(pattern) {
		return gateExcludePattern{}, fmt.Errorf("pattern %q must be repo-relative", pattern)
	}
	if strings.HasPrefix(pattern, "!") {
		return gateExcludePattern{}, fmt.Errorf("negated pattern %q is unsupported", pattern)
	}
	if strings.Contains(pattern, "..") {
		clean := path.Clean(pattern)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
			return gateExcludePattern{}, fmt.Errorf("pattern %q must stay inside the repository", pattern)
		}
	}
	if strings.Contains(pattern, "**") {
		if strings.Count(pattern, "**") != 1 || !strings.HasSuffix(pattern, "/**") {
			return gateExcludePattern{}, fmt.Errorf("recursive pattern %q must end in /**", pattern)
		}
		prefix := strings.TrimSuffix(pattern, "/**")
		if prefix == "" || strings.ContainsAny(prefix, "*?[") {
			return gateExcludePattern{}, fmt.Errorf("recursive pattern %q must name a literal directory", pattern)
		}
		return gateExcludePattern{raw: pattern, recursivePrefix: prefix + "/"}, nil
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return gateExcludePattern{}, fmt.Errorf("invalid glob %q: %w", pattern, err)
	}
	return gateExcludePattern{raw: pattern}, nil
}
