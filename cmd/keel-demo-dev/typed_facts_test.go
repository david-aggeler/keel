package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// TestDemoBridgeCarriesTypedFactsOnLanes holds keel/requirement-138 for the
// second in-repo producer. keel-demo-dev has never carried a lane duration at
// all, so this is the first assertion that the shared attribution resolves the
// same streams for it as it does for keel-dev (ac-550, ac-564, ac-549).
//
// DHF-TEST: keel/requirement-138
func TestDemoBridgeCarriesTypedFactsOnLanes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(demoLanesPath(root), []byte(`{"version":1,"lanes":[]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".devtools", "vscode-runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	// One stream attributable to go-pass alone, and one multi-selection
	// stream that names go-fail among others so go-fail stays unmeasured.
	streams := map[string]string{
		"pass.jsonl": strings.Join([]string{
			`{"version":1,"event":"run_started","time":"2026-07-13T11:00:00Z","run_id":"pass","requested":[{"id":"` + idLaneGoPass + `","label":"pass"}]}`,
			`{"version":1,"event":"run_finished","time":"2026-07-13T11:00:02.500Z","run_id":"pass","exit_code":0}`,
		}, "\n") + "\n",
		"multi.jsonl": strings.Join([]string{
			`{"version":1,"event":"run_started","time":"2026-07-13T11:05:00Z","run_id":"multi","requested":[{"id":"` + idLaneGoFail + `","label":"fail"},{"id":"` + idLaneGoPass + `","label":"pass"}]}`,
			`{"version":1,"event":"run_finished","time":"2026-07-13T11:05:01Z","run_id":"multi","exit_code":1}`,
		}, "\n") + "\n",
	}
	for name, body := range streams {
		if err := os.WriteFile(filepath.Join(root, ".devtools", "vscode-runs", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root})
	doc, err := demoBridge{}.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if err := testbridge.ValidateDocument(doc); err != nil {
		t.Fatalf("Discover emitted a document the boundary refuses: %v", err)
	}

	byID := map[string]vscode.TestItem{}
	for _, item := range doc.Items {
		byID[item.ID] = item
	}

	pass, ok := byID[idLaneGoPass]
	if !ok {
		t.Fatalf("discovery missing the go-pass lane: %+v", doc.Items)
	}
	if pass.LastRun == nil {
		t.Fatalf("go-pass lane carries no typed last_run: %+v", pass)
	}
	if pass.LastRun.DurationMS == nil || *pass.LastRun.DurationMS != 2500 {
		t.Errorf("go-pass last_run.duration_ms = %v, want the measured 2500", pass.LastRun.DurationMS)
	}
	if pass.LastRun.ExitCode == nil || *pass.LastRun.ExitCode != 0 {
		t.Errorf("go-pass last_run.exit_code = %v, want 0", pass.LastRun.ExitCode)
	}
	if pass.Description == "" {
		t.Errorf("go-pass lane carries no scalar description (keel/ac-549)")
	}

	fail, ok := byID[idLaneGoFail]
	if !ok {
		t.Fatalf("discovery missing the go-fail lane: %+v", doc.Items)
	}
	if fail.LastRun != nil {
		t.Errorf("go-fail last_run = %+v, want absent — its only candidate stream is a multi-selection run (keel/ac-564)", fail.LastRun)
	}

	encoded, err := json.Marshal(fail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"last_run"`) {
		t.Errorf("go-fail carries a last_run member with no attributable stream:\n%s", encoded)
	}
}
