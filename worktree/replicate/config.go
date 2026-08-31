package replicate

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/david-aggeler/keel/worktree"
)

// Preset names a built-in replication pattern set.
type Preset string

const (
	// PresetClaude expands to the local configuration used by Claude sessions.
	PresetClaude Preset = "claude"
	// PresetCodex expands to the local configuration used by Codex sessions.
	PresetCodex Preset = "codex"
)

// Config is the yaml-tagged shape of the keel.worktree.replicate subtree.
type Config struct {
	// Presets lists built-in pattern groups to expand.
	Presets []Preset `yaml:"presets"`
	// Custom lists caller-declared patterns. A YAML scalar short form is host
	// decoder sugar for a CustomEntry with Pattern set and Mode left empty.
	Custom []CustomEntry `yaml:"custom"`
}

// CustomEntry declares one custom replication pattern from configuration.
type CustomEntry struct {
	// Pattern is a repo-root-relative worktree replication pattern.
	Pattern string `yaml:"pattern"`
	// Mode selects copy or link materialization; empty resolves to copy.
	Mode worktree.ReplicateMode `yaml:"mode,omitempty"`
}

// UnmarshalYAML accepts the scalar short form or the mapping long form used in
// committed YAML policy without importing a YAML package into production code.
func (e *CustomEntry) UnmarshalYAML(unmarshal func(any) error) error {
	var short string
	if err := unmarshal(&short); err == nil {
		e.Pattern = short
		e.Mode = ""
		return nil
	}

	type raw CustomEntry
	var long raw
	if err := unmarshal(&long); err != nil {
		return err
	}
	*e = CustomEntry(long)
	return nil
}

// Resolve validates c and returns neutral worktree replication items.
func (c Config) Resolve() ([]worktree.ReplicateItem, error) {
	var items []worktree.ReplicateItem
	var presetItems []worktree.ReplicateItem
	for _, preset := range c.Presets {
		expanded, err := expandPreset(preset)
		if err != nil {
			return nil, err
		}
		presetItems = append(presetItems, expanded...)
		items = append(items, expanded...)
	}
	for _, custom := range c.Custom {
		mode := custom.Mode
		if mode == "" {
			mode = worktree.ReplicateCopy
		}
		if mode != worktree.ReplicateCopy && mode != worktree.ReplicateLink {
			return nil, fmt.Errorf("keel/worktree/replicate: unknown mode %q for pattern %q", custom.Mode, custom.Pattern)
		}
		if mode == worktree.ReplicateLink && coveredByPreset(custom.Pattern, presetItems) {
			return nil, fmt.Errorf("keel/worktree/replicate: pattern %q is covered by an enabled preset and must use copy mode", custom.Pattern)
		}
		items = append(items, worktree.ReplicateItem{Pattern: custom.Pattern, Mode: mode})
	}
	return items, nil
}

func expandPreset(preset Preset) ([]worktree.ReplicateItem, error) {
	switch preset {
	case PresetClaude:
		return copyItems(".claude/**", ".mcp.json"), nil
	case PresetCodex:
		return copyItems(".codex/**", ".agents/**"), nil
	default:
		return nil, fmt.Errorf("keel/worktree/replicate: unknown preset %q", preset)
	}
}

func copyItems(patterns ...string) []worktree.ReplicateItem {
	items := make([]worktree.ReplicateItem, 0, len(patterns))
	for _, pattern := range patterns {
		items = append(items, worktree.ReplicateItem{Pattern: pattern, Mode: worktree.ReplicateCopy})
	}
	return items
}

func coveredByPreset(pattern string, presetItems []worktree.ReplicateItem) bool {
	pattern = cleanPattern(pattern)
	for _, item := range presetItems {
		preset := cleanPattern(item.Pattern)
		if patternsIntersect(pattern, preset) {
			return true
		}
	}
	return false
}

// DHF-REQ: keel/requirement-157
func patternsIntersect(pattern, preset string) bool {
	if patternCoveredBy(pattern, preset) || patternCoveredBy(preset, pattern) {
		return true
	}
	if globMatches(pattern, preset) {
		return true
	}
	if root, ok := recursivePatternRoot(preset); ok {
		if globMatches(pattern, root) || globMatches(pattern, root+"/_") {
			return true
		}
		if patternRoot, patternRecursive := recursivePatternRoot(pattern); patternRecursive {
			return globMatches(patternRoot, root) || globMatches(root, patternRoot)
		}
	}
	return false
}

func patternCoveredBy(pattern, preset string) bool {
	if pattern == preset {
		return true
	}
	if preset == "." || preset == "*" || preset == "**" {
		return true
	}
	if strings.HasSuffix(preset, "/**") {
		root := strings.TrimSuffix(preset, "/**")
		return pattern == root || strings.HasPrefix(pattern, root+"/")
	}
	return false
}

func globMatches(pattern, name string) bool {
	matched, err := path.Match(pattern, name)
	return err == nil && matched
}

func recursivePatternRoot(pattern string) (string, bool) {
	if strings.HasSuffix(pattern, "/**") {
		return strings.TrimSuffix(pattern, "/**"), true
	}
	return "", false
}

func cleanPattern(pattern string) string {
	pattern = strings.TrimSpace(filepath.ToSlash(pattern))
	pattern = strings.TrimPrefix(pattern, "./")
	return strings.TrimRight(pattern, "/")
}
