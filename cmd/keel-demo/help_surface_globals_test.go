package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
)

// globalsInventoryElement is the --help-json element subset these tests read.
type globalsInventoryElement struct {
	Kind      string `json:"kind"`
	Invocable *bool  `json:"invocable"`
	Flags     []struct {
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
		Enum    []string `json:"enum"`
	} `json:"flags"`
}

// DHF-TEST: keel/requirement-100 (keel/ac-715)
func TestKeelDemoHelpJSONCarriesGlobalFlagsAndRoot(t *testing.T) {
	out := captureHelpSurface(t, "--help-json")
	var inventory []globalsInventoryElement
	if err := json.Unmarshal([]byte(out), &inventory); err != nil {
		t.Fatalf("--help-json stdout is not a JSON array: %v\n%s", err, out)
	}
	idx := slices.IndexFunc(inventory, func(e globalsInventoryElement) bool { return e.Kind == "root" })
	if idx < 0 {
		t.Fatalf("--help-json has no root element:\n%s", out)
	}
	root := inventory[idx]
	if root.Invocable == nil || *root.Invocable != true {
		t.Fatalf("root invocable = %v, want true", root.Invocable)
	}
	for _, spec := range cli.GlobalFlagSpecs() {
		found := false
		for _, f := range root.Flags {
			if f.Name != spec.Name {
				continue
			}
			found = true
			if spec.Alias != "" && !slices.Equal(f.Aliases, []string{spec.Alias}) {
				t.Fatalf("--%s aliases = %q, want [%s]", spec.Name, f.Aliases, spec.Alias)
			}
			if !slices.Equal(f.Enum, spec.Enum) {
				t.Fatalf("--%s enum = %q, want %q", spec.Name, f.Enum, spec.Enum)
			}
		}
		if !found {
			t.Fatalf("--help-json root element lacks global flag --%s:\n%s", spec.Name, out)
		}
	}
}

// DHF-TEST: keel/requirement-101 (keel/ac-716)
func TestKeelDemoHelpAdvertisesEveryAcceptedGlobalSpelling(t *testing.T) {
	out := captureHelpSurface(t, "--help")
	lines := strings.Split(out, "\n")
	usageAt := slices.Index(lines, "Usage:")
	if usageAt < 0 || usageAt+1 >= len(lines) {
		t.Fatalf("--help has no Usage: block:\n%s", out)
	}
	synopsis := lines[usageAt+1]
	for _, line := range lines[usageAt+2:] {
		if !strings.HasPrefix(line, "   ") {
			break
		}
		synopsis += " " + strings.TrimSpace(line)
	}
	inSynopsis := map[string]bool{}
	for _, token := range strings.FieldsFunc(synopsis, func(r rune) bool { return strings.ContainsRune(" []|", r) }) {
		inSynopsis[token] = true
	}
	inSection := map[string]bool{}
	sectionAt := slices.Index(lines, "Global flags:")
	if sectionAt < 0 {
		t.Fatalf("--help has no Global flags: section:\n%s", out)
	}
	for _, line := range lines[sectionAt+1:] {
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "  -") {
			for _, token := range strings.Fields(strings.ReplaceAll(line, ",", " ")) {
				inSection[token] = true
			}
		}
	}
	for _, spec := range cli.GlobalFlagSpecs() {
		spellings := []string{"--" + spec.Name}
		if spec.Alias != "" {
			spellings = append(spellings, "-"+spec.Alias)
		}
		for _, spelling := range spellings {
			if !inSynopsis[spelling] {
				t.Fatalf("root Usage synopsis lacks %q: %q", spelling, synopsis)
			}
			if !inSection[spelling] {
				t.Fatalf("Global flags section lacks %q:\n%s", spelling, out)
			}
		}
	}
}

func captureHelpSurface(t *testing.T, arg string) string {
	t.Helper()
	out, code := captureRunOutput(t, func() int { return run([]string{arg}) })
	if code != 0 {
		t.Fatalf("run(%s) exit = %d, want 0\n%s", arg, code, out)
	}
	return out
}
