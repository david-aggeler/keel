package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/vscode"
)

// TestLanesProducerTextComesFromTheExportedRenderer holds keel/ac-565: the
// duration and finding text keel-dev reports for a lane is byte-identical to
// what the exported renderer produces for the same typed facts. keel-dev is a
// producer of that text on the node-free surface, so if it kept its own format
// strings the second copy would be exactly the accidental duplicate the golden
// fixture exists to prevent.
//
// DHF-TEST: keel/requirement-139
func TestLanesProducerTextComesFromTheExportedRenderer(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/fixture\n\ngo 1.25\n")
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, root, ".vscode/test-lanes.json", `{"version":1,"lanes":[{"id":"go-log","label":"Go log","order":"b.10","description":"the lane prose","members":[{"go":"./log/..."}]}]}`+"\n")

	var protocol bytes.Buffer
	if err := dispatchTestBridgeDiscover(contextWithVSCodeTestState(root, &protocol), "--format", "json"); err != nil {
		t.Fatalf("discover: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(protocol.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, protocol.String())
	}
	lane, ok := discoveryItemByID(doc, "keel::lane::go-log")
	if !ok {
		t.Fatalf("discovery missing go-log lane: %+v", doc.Items)
	}

	// With the prose array retired, the producer contributes its own prose and
	// its typed facts and composes nothing: the rendered line exists only on the
	// consumer side, and the exported renderer is the one place it is spelled.
	if lane.Description != "the lane prose" {
		t.Fatalf("lane description = %q, want the producer's own prose alone", lane.Description)
	}
	if hint := vscode.FormatLastRun(lane.LastRun); hint != "" && strings.Contains(lane.Description, hint) {
		t.Fatalf("lane description %q carries the rendered duration %q; the producer must not compose", lane.Description, hint)
	}
	for _, finding := range lane.Findings {
		if rendered := vscode.FormatFinding(finding); rendered != "" && strings.Contains(lane.Description, rendered) {
			t.Fatalf("lane description %q carries the rendered finding %q", lane.Description, rendered)
		}
	}
}

// TestLanesCodePathDeclaresNoDurationOrFindingFormat holds the second half of
// keel/ac-565 mechanically: no duration or finding format string may exist in
// the lanes code path. A format string reintroduced here would render
// identically today and drift tomorrow, which is the failure the shared fixture
// cannot see because it never reads this file.
//
// DHF-TEST: keel/requirement-139
func TestLanesCodePathDeclaresNoDurationOrFindingFormat(t *testing.T) {
	body, err := os.ReadFile("vscode.go")
	if err != nil {
		t.Fatalf("read lanes code path: %v", err)
	}
	source := string(body)
	for _, forbidden := range []string{"· last ", `Severity + ": "`, `Severity+": "`} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("lanes code path still declares the format string %q; render through keel/vscode instead", forbidden)
		}
	}
}
