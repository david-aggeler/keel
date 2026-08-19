package vscode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLatestLaneRunRefusesUnattributableStreams holds keel/ac-573: a stream
// whose finish precedes its start records no measurement anyone made, so the
// attribution yields no run at all rather than a run with a negative duration.
// The run-id pairing case is the same defect seen from the other side — an
// unrelated finish paired with a start is how a finish-before-start arises in a
// file that holds two runs.
//
// DHF-TEST: keel/requirement-138
func TestLatestLaneRunRefusesUnattributableStreams(t *testing.T) {
	const laneID = "keel::lane::go-log"
	for _, tc := range []struct {
		name       string
		stream     []string
		wantRunID  string
		wantMillis int64
	}{
		{
			name: "finish precedes start",
			stream: []string{
				`{"version":1,"event":"run_started","time":"2026-07-12T10:00:00Z","run_id":"skewed","requested":[{"id":"keel::lane::go-log","label":"log"}]}`,
				`{"version":1,"event":"run_finished","time":"2026-07-12T09:59:58Z","run_id":"skewed","exit_code":0}`,
			},
		},
		{
			name: "the only finish belongs to another run",
			stream: []string{
				`{"version":1,"event":"run_started","time":"2026-07-12T10:00:00Z","run_id":"lane","requested":[{"id":"keel::lane::go-log","label":"log"}]}`,
				`{"version":1,"event":"run_started","time":"2026-07-12T10:01:00Z","run_id":"other","requested":[{"id":"keel::lane::lint","label":"lint"}]}`,
				`{"version":1,"event":"run_finished","time":"2026-07-12T10:01:30Z","run_id":"other","exit_code":0}`,
			},
		},
		{
			name: "this run's finish followed by another run's",
			stream: []string{
				`{"version":1,"event":"run_started","time":"2026-07-12T10:00:00Z","run_id":"lane","requested":[{"id":"keel::lane::go-log","label":"log"}]}`,
				`{"version":1,"event":"run_finished","time":"2026-07-12T10:00:05Z","run_id":"lane","exit_code":0}`,
				`{"version":1,"event":"run_started","time":"2026-07-12T10:01:00Z","run_id":"other","requested":[{"id":"keel::lane::lint","label":"lint"}]}`,
				`{"version":1,"event":"run_finished","time":"2026-07-12T10:01:30Z","run_id":"other","exit_code":0}`,
			},
			wantRunID:  "lane",
			wantMillis: 5000,
		},
		{
			name: "a start and finish at the same instant is a measured zero",
			stream: []string{
				`{"version":1,"event":"run_started","time":"2026-07-12T10:00:00Z","run_id":"instant","requested":[{"id":"keel::lane::go-log","label":"log"}]}`,
				`{"version":1,"event":"run_finished","time":"2026-07-12T10:00:00Z","run_id":"instant","exit_code":0}`,
			},
			wantRunID:  "instant",
			wantMillis: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeLaneRunStream(t, tc.stream)
			got := LatestLaneRun(root, laneID)
			if tc.wantRunID == "" {
				if got != nil {
					t.Fatalf("LatestLaneRun = %+v, want no attributable run (keel/ac-573)", got)
				}
				if got.Facts() != nil {
					t.Fatal("Facts() carries a last_run for an unattributable stream (keel/ac-564)")
				}
				return
			}
			if got == nil {
				t.Fatal("LatestLaneRun = nil, want the run this lane's own start and finish record")
			}
			if got.RunID != tc.wantRunID {
				t.Errorf("LatestLaneRun run id = %q, want %q", got.RunID, tc.wantRunID)
			}
			if got.DurationMS != tc.wantMillis {
				t.Errorf("LatestLaneRun duration = %dms, want %dms", got.DurationMS, tc.wantMillis)
			}
		})
	}
}

func writeLaneRunStream(t *testing.T, lines []string) string {
	t.Helper()
	root := t.TempDir()
	runDir := filepath.Join(append([]string{root}, laneRunsDirParts...)...)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(runDir, "run.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
