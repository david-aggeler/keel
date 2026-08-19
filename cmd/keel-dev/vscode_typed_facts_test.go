package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// TestVSCodeDiscoveryCarriesTypedFindingsAndScalarDescription holds keel/ac-551
// and the carriage half of keel/ac-549: a lane's validation finding travels as
// a rule/severity/message triple with severity on the closed enum, the lane's
// own prose travels in the scalar description, and neither the finding text nor
// the duration text appears in that description.
//
// DHF-TEST: keel/requirement-138
func TestVSCodeDiscoveryCarriesTypedFindingsAndScalarDescription(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"),
		`{"version":1,"lanes":[{"id":"go-absent","label":"absent","order":"b.40","description":"the lane's own prose","members":[{"go":"./absent/..."}]}]}`+"\n")

	built, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", err)
	}
	var encoded bytes.Buffer
	if err := testbridge.EncodeDocument(&encoded, built); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(encoded.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v", err)
	}
	lane, ok := discoveryItemByID(doc, "keel::lane::go-absent")
	if !ok {
		t.Fatalf("discovery missing go-absent lane: %+v", doc.Items)
	}

	if lane.Description != "the lane's own prose" {
		t.Fatalf("lane description = %q, want the producer's own prose (keel/ac-549)", lane.Description)
	}
	if len(lane.Findings) != 1 {
		t.Fatalf("lane findings = %+v, want exactly the one validation finding (keel/ac-551)", lane.Findings)
	}
	finding := lane.Findings[0]
	if finding.Rule != "V6" {
		t.Errorf("finding rule = %q, want %q", finding.Rule, "V6")
	}
	if !vscode.IsFindingSeverity(finding.Severity) || finding.Severity != "warning" {
		t.Errorf("finding severity = %q, want the closed-enum value %q", finding.Severity, "warning")
	}
	if !strings.Contains(finding.Message, "no test-bearing packages") {
		t.Errorf("finding message = %q, want the rule's own message", finding.Message)
	}
	if strings.Contains(finding.Message, finding.Rule) || strings.Contains(finding.Message, finding.Severity) {
		t.Errorf("finding message = %q, want rule and severity separate from it (keel/ac-551)", finding.Message)
	}
	if strings.Contains(lane.Description, finding.Message) || strings.Contains(lane.Description, finding.Rule) {
		t.Errorf("lane description = %q, want no finding text (keel/ac-551)", lane.Description)
	}
	// The prose channel is the scalar description and carries the producer's
	// own prose alone (keel/ac-549).
	if lane.Description != "the lane's own prose" {
		t.Errorf("lane description = %q, want the producer's own prose alone (keel/ac-549)", lane.Description)
	}
}

// TestVSCodeDiagnosticItemsCarryScalarDescription holds keel/ac-549 for the
// items whose whole prose channel is one diagnostic string. Those are the
// items that would have gone mute when the prose array retired, so each has to
// carry its text on the scalar channel.
//
// DHF-TEST: keel/requirement-138
func TestVSCodeDiagnosticItemsCarryScalarDescription(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A lane naming a member form the reader does not know raises a lanes
	// diagnostic item.
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"),
		`{"version":1,"lanes":[{"id":"broken","label":"broken","order":"b.40","members":[{"nonsense":"./x"}]}]}`+"\n")
	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A Go test file that does not parse raises a file item carrying the
	// parse error as its whole prose channel.
	writeFile(t, root, filepath.Join("broken", "broken_test.go"), "package broken\n\nfunc TestX(t *testing.T {\n")

	built, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", err)
	}
	checked := 0
	for _, item := range built.Items {
		// A diagnostic item is the non-runnable item whose whole reason to
		// exist is the text it reports.
		if item.Runnable || (!strings.Contains(item.ID, "diagnostic") && item.Kind != "file") {
			continue
		}
		checked++
		if item.Description == "" {
			t.Errorf("diagnostic item %q carries no description, so its text is mute (keel/ac-549)", item.ID)
		}
	}
	if checked == 0 {
		t.Fatal("fixture produced no diagnostic item, so the invariant proved nothing")
	}
}
