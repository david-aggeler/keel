package testbridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// TestDetectLanesPreservesEmissionOrderWithoutAnOrdinal is the round-trip guard
// keel/ac-548 rests on, and it lands before the ordinal prefix and its
// reverse-parse are deleted so that a later red is unambiguously the deletion's
// fault. The lanes it emits carry no ordinal in any field — not in the label,
// not in sort_text — so the written file's order can only have come from the
// emission sequence.
//
// DHF-TEST: keel/requirement-137
func TestDetectLanesPreservesEmissionOrderWithoutAnOrdinal(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	emitted := []string{"zebra", "xray", "yankee"}
	for _, short := range emitted {
		fake.lanes = append(fake.lanes, vscode.TestItem{
			ID:        "demo::lane::" + short,
			Label:     short,
			Kind:      "lane",
			Framework: "go",
			Runnable:  true,
			Profiles:  []string{"run"},
			LaneID:    "demo::lane::" + short,
		})
	}

	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		RunID:    func() string { return "run-lane-order" },
	})
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "run", "--id", testbridge.MaintenanceDetectLanesID}); err != nil {
		t.Fatalf("detect-lanes dispatch: %v\n%s", err, protocol.String())
	}

	data, err := os.ReadFile(filepath.Join(root, ".vscode", "test-lanes.json"))
	if err != nil {
		t.Fatalf("read detected lanes: %v", err)
	}
	var file struct {
		Lanes []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"lanes"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("decode lanes file: %v\n%s", err, data)
	}
	if len(file.Lanes) != len(emitted) {
		t.Fatalf("lanes file holds %d rows, want %d:\n%s", len(file.Lanes), len(emitted), data)
	}
	for i, want := range emitted {
		if file.Lanes[i].ID != want {
			t.Fatalf("lane %d id = %q, want %q — the written order left the emission sequence:\n%s", i, file.Lanes[i].ID, want, data)
		}
		if file.Lanes[i].Label != want {
			t.Fatalf("lane %d label = %q, want %q", i, file.Lanes[i].Label, want)
		}
	}
	if strings.Contains(string(data), `"order"`) && !strings.Contains(string(data), `"order": ""`) {
		t.Fatalf("lanes file recovered an ordinal from an ordinal-free emission:\n%s", data)
	}
}
