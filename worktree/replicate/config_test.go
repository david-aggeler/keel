package replicate_test

import (
	"strings"
	"testing"

	"github.com/david-aggeler/keel/worktree"
	"github.com/david-aggeler/keel/worktree/replicate"
	"gopkg.in/yaml.v3"
)

// DHF-TEST: keel/requirement-157 (keel/ac-650)
func TestResolvePresetsExpandSymmetrically(t *testing.T) {
	items, err := (replicate.Config{
		Presets: []replicate.Preset{replicate.PresetClaude, replicate.PresetCodex},
	}).Resolve()
	if err != nil {
		t.Fatalf("resolve presets: %v", err)
	}
	want := []worktree.ReplicateItem{
		{Pattern: ".claude/**", Mode: worktree.ReplicateCopy},
		{Pattern: ".mcp.json", Mode: worktree.ReplicateCopy},
		{Pattern: ".codex/**", Mode: worktree.ReplicateCopy},
		{Pattern: ".agents/**", Mode: worktree.ReplicateCopy},
	}
	if !sameItems(items, want) {
		t.Fatalf("resolved items = %#v, want %#v", items, want)
	}
}

func TestResolveEmptyConfigReturnsNoItems(t *testing.T) {
	items, err := (replicate.Config{}).Resolve()
	if err != nil {
		t.Fatalf("resolve empty config: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("empty config items = %#v, want none", items)
	}
}

func TestResolveCustomEntriesDefaultCopyAndAllowExplicitLink(t *testing.T) {
	items, err := (replicate.Config{
		Custom: []replicate.CustomEntry{
			{Pattern: "openbrain-client.local.yaml"},
			{Pattern: ".devtools/", Mode: worktree.ReplicateLink},
		},
	}).Resolve()
	if err != nil {
		t.Fatalf("resolve custom entries: %v", err)
	}
	want := []worktree.ReplicateItem{
		{Pattern: "openbrain-client.local.yaml", Mode: worktree.ReplicateCopy},
		{Pattern: ".devtools/", Mode: worktree.ReplicateLink},
	}
	if !sameItems(items, want) {
		t.Fatalf("resolved items = %#v, want %#v", items, want)
	}
}

func TestConfigDecodesCustomShortAndLongYAMLForms(t *testing.T) {
	var cfg replicate.Config
	if err := yaml.Unmarshal([]byte(`
custom:
  - openbrain-client.local.yaml
  - pattern: .devtools/
    mode: link
`), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	items, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("resolve decoded config: %v", err)
	}
	want := []worktree.ReplicateItem{
		{Pattern: "openbrain-client.local.yaml", Mode: worktree.ReplicateCopy},
		{Pattern: ".devtools/", Mode: worktree.ReplicateLink},
	}
	if !sameItems(items, want) {
		t.Fatalf("resolved items = %#v, want %#v", items, want)
	}
}

func TestResolveRejectsUnknownPresetNamingValue(t *testing.T) {
	_, err := (replicate.Config{Presets: []replicate.Preset{"cursor"}}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("unknown preset err = %v, want error naming preset", err)
	}
}

func TestResolveRejectsUnknownCustomModeNamingPattern(t *testing.T) {
	_, err := (replicate.Config{
		Custom: []replicate.CustomEntry{{Pattern: ".devtools/", Mode: "hard-link"}},
	}).Resolve()
	if err == nil || !strings.Contains(err.Error(), ".devtools/") {
		t.Fatalf("unknown mode err = %v, want error naming pattern", err)
	}
}

// DHF-TEST: keel/requirement-157 (keel/ac-652)
func TestResolveRejectsLinkModeForPresetCoveredPattern(t *testing.T) {
	for _, pattern := range []string{".claude/**", ".claude/settings.local.json", ".mcp.json", ".codex/config.toml"} {
		t.Run(pattern, func(t *testing.T) {
			_, err := (replicate.Config{
				Presets: []replicate.Preset{replicate.PresetClaude, replicate.PresetCodex},
				Custom:  []replicate.CustomEntry{{Pattern: pattern, Mode: worktree.ReplicateLink}},
			}).Resolve()
			if err == nil || !strings.Contains(err.Error(), pattern) {
				t.Fatalf("preset-covered link err = %v, want error naming %q", err, pattern)
			}
		})
	}
}

// DHF-TEST: keel/requirement-157 (keel/ac-665)
func TestResolveRejectsLinkModeForPatternCoveringPresetExpansion(t *testing.T) {
	for _, pattern := range []string{"**", "*", "."} {
		t.Run(pattern, func(t *testing.T) {
			items, err := (replicate.Config{
				Presets: []replicate.Preset{replicate.PresetClaude, replicate.PresetCodex},
				Custom:  []replicate.CustomEntry{{Pattern: pattern, Mode: worktree.ReplicateLink}},
			}).Resolve()
			if err == nil || !strings.Contains(err.Error(), pattern) {
				t.Fatalf("preset-covering link err = %v, want error naming %q", err, pattern)
			}
			for _, item := range items {
				if item.Pattern == pattern && item.Mode == worktree.ReplicateLink {
					t.Fatalf("resolved items include link-mode custom item %#v after validation failure", item)
				}
			}
		})
	}
}

// DHF-TEST: keel/requirement-157 (keel/ac-665)
func TestResolveAllowsLinkModeForPatternOutsidePresetExpansion(t *testing.T) {
	items, err := (replicate.Config{
		Presets: []replicate.Preset{replicate.PresetClaude, replicate.PresetCodex},
		Custom:  []replicate.CustomEntry{{Pattern: "docs/**", Mode: worktree.ReplicateLink}},
	}).Resolve()
	if err != nil {
		t.Fatalf("resolve outside-preset link pattern: %v", err)
	}
	want := []worktree.ReplicateItem{
		{Pattern: ".claude/**", Mode: worktree.ReplicateCopy},
		{Pattern: ".mcp.json", Mode: worktree.ReplicateCopy},
		{Pattern: ".codex/**", Mode: worktree.ReplicateCopy},
		{Pattern: ".agents/**", Mode: worktree.ReplicateCopy},
		{Pattern: "docs/**", Mode: worktree.ReplicateLink},
	}
	if !sameItems(items, want) {
		t.Fatalf("resolved items = %#v, want %#v", items, want)
	}
}

func sameItems(got, want []worktree.ReplicateItem) bool {
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
