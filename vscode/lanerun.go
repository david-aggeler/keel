package vscode

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// laneRunsDirParts is the workspace-relative directory every producer's run
// streams are mirrored to. Attribution reads what the bridge wrote, so the
// path is stated once here rather than by each producer.
var laneRunsDirParts = []string{".devtools", "vscode-runs"}

// LaneRun is the newest persisted run stream attributable to exactly one lane.
// It is the producer-side attribution record; LastRunFacts is what reaches the
// wire.
//
// DHF-REQ: keel/requirement-53, keel/requirement-138
type LaneRun struct {
	RunID      string
	At         time.Time
	DurationMS int64
	ExitCode   int
}

// LatestLaneRun returns the newest persisted run stream under the workspace
// whose requested set is exactly the one lane, failed runs included and
// multi-selection runs excluded, or nil when no stream is attributable.
//
// The rule is keel/requirement-53's and is unchanged by the typed carriage; it
// lives here rather than in one producer so a second producer cannot drift a
// second copy of it.
//
// DHF-REQ: keel/requirement-53, keel/requirement-138
func LatestLaneRun(root, laneID string) *LaneRun {
	runDir := filepath.Join(append([]string{root}, laneRunsDirParts...)...)
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil
	}
	var best *LaneRun
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		got := laneRunFromStream(filepath.Join(runDir, entry.Name()), laneID)
		if got == nil {
			continue
		}
		if best == nil || got.At.After(best.At) {
			best = got
		}
	}
	return best
}

// Facts projects the attribution record onto the wire. A nil receiver returns
// nil rather than a zeroed record: absence of a measurement is absence of the
// field, never a zero standing in for "never measured" (keel/ac-564).
//
// DHF-REQ: keel/requirement-138
func (r *LaneRun) Facts() *LastRunFacts {
	if r == nil {
		return nil
	}
	duration := r.DurationMS
	exitCode := r.ExitCode
	return &LastRunFacts{At: r.At, DurationMS: &duration, ExitCode: &exitCode}
}

// laneRunFromStream reads one persisted stream and reports the run it records
// only when that run was requested for exactly this lane and finished with an
// exit code. An unparseable line is skipped rather than failing the read: a
// truncated stream from an interrupted run must not cost the lane its history.
func laneRunFromStream(path, laneID string) *LaneRun {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var started *RunEvent
	var finished *RunEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event RunEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		switch event.Event {
		case "run_started":
			if len(event.Requested) == 1 && event.Requested[0].ID == laneID {
				copyEvent := event
				started = &copyEvent
			}
		case "run_finished":
			copyEvent := event
			finished = &copyEvent
		}
	}
	if started == nil || finished == nil || finished.ExitCode == nil {
		return nil
	}
	return &LaneRun{
		RunID:      started.RunID,
		At:         started.Time,
		DurationMS: finished.Time.Sub(started.Time).Milliseconds(),
		ExitCode:   *finished.ExitCode,
	}
}
