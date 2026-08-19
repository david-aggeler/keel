package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/vscode"
)

// ordinalLabelGrammar is the ordinal prefix keel/ac-545 forbids on a label:
// a letter, a dot, digits, and a space. It is matched case-insensitively
// because the fact used to be authored lower-case and rendered upper-case,
// which is the drift the criterion closes.
var ordinalLabelGrammar = regexp.MustCompile(`(?i)^[a-z]\.[0-9]+ `)

// TestDiscoveryLabelsCarryNoOrdinalPrefix holds keel/ac-545 for the keel-dev
// producer: every label in the emitted document is free of the ordinal
// grammar, so no label is a carrier for the ordering fact.
//
// DHF-TEST: keel/requirement-137
func TestDiscoveryLabelsCarryNoOrdinalPrefix(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"),
		`{"version":1,"lanes":[`+
			`{"id":"first","label":"first lane","description":"first","members":[{"root":"go"}]},`+
			`{"id":"second","label":"c.10 second lane","description":"second","members":[{"root":"go"}]}`+
			`]}`+"\n")

	doc, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", err)
	}
	assertNoOrdinalLabels(t, doc.Items)
}

// TestGeneratedLaneFileLabelsCarryNoOrdinalPrefix holds keel/ac-545 for the
// lane rows detect-lanes seeds: the generated file is a producer input, so an
// ordinal seeded there would travel straight back onto a label.
//
// DHF-TEST: keel/requirement-137
func TestGeneratedLaneFileLabelsCarryNoOrdinalPrefix(t *testing.T) {
	generated := generatedLanesFile(t.TempDir(), map[string]bool{"log": true})
	if len(generated.Lanes) == 0 {
		t.Fatal("generated lanes file is empty")
	}
	for _, lane := range generated.Lanes {
		if ordinalLabelGrammar.MatchString(lane.Label) {
			t.Errorf("generated lane %q label %q carries an ordinal prefix (keel/ac-545)", lane.ID, lane.Label)
		}
	}
}

func assertNoOrdinalLabels(t *testing.T, items []vscode.TestItem) {
	t.Helper()
	if len(items) == 0 {
		t.Fatal("discovery document carries no items")
	}
	for _, item := range items {
		if ordinalLabelGrammar.MatchString(item.Label) {
			t.Errorf("item %q label %q matches the ordinal grammar (keel/ac-545)", item.ID, item.Label)
		}
	}
}

// TestAuthoredLaneLabelOrdinalIsDroppedAndReported holds the boundary half of
// keel/ac-545: a lanes file authored before the ordinal was retired can still
// carry the prefix inside its own label, and emitting it verbatim would put the
// ordering fact straight back onto the label. The prefix is dropped and the
// drop travels as a typed finding, so the workspace owner learns why the tree
// changed instead of finding it changed silently.
//
// DHF-TEST: keel/requirement-137
func TestAuthoredLaneLabelOrdinalIsDroppedAndReported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"),
		`{"version":1,"lanes":[{"id":"legacy","label":"c.10 legacy lane","description":"prose","members":[{"root":"go"}]}]}`+"\n")

	doc, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", err)
	}
	assertNoOrdinalLabels(t, doc.Items)

	var lane vscode.TestItem
	for _, item := range doc.Items {
		if item.ID == "keel::lane::legacy" {
			lane = item
			break
		}
	}
	if lane.ID == "" {
		t.Fatalf("discovery missing the legacy lane: %+v", doc.Items)
	}
	if lane.Label != "legacy lane" {
		t.Fatalf("lane label = %q, want the authored text with the ordinal dropped", lane.Label)
	}
	var reported bool
	for _, finding := range lane.Findings {
		if finding.Rule == "V5" && finding.Severity == "warning" && strings.Contains(finding.Message, "c.10 legacy lane") {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("lane findings = %+v, want a warning naming the dropped ordinal", lane.Findings)
	}
}
