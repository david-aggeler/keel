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
	return LatestRunsForIDs(root, []string{laneID})[laneID]
}

// LatestRunsForIDs answers LatestLaneRun for a whole set of ids in one pass over
// the run directory, keyed by id and holding only the ids a run is attributable
// to. Attribution is the same rule LatestLaneRun states; the only difference is
// cost, and the difference matters: a tree stamps every runnable item it serves
// on every refresh, so a per-id directory walk would re-read the retained
// streams once per item.
//
// DHF-REQ: keel/requirement-53, keel/requirement-138
func LatestRunsForIDs(root string, ids []string) map[string]*LaneRun {
	best := map[string]*LaneRun{}
	if len(ids) == 0 {
		return best
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	runDir := filepath.Join(append([]string{root}, laneRunsDirParts...)...)
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return best
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		for id, got := range runsFromStream(filepath.Join(runDir, entry.Name())) {
			if _, ok := wanted[id]; !ok {
				continue
			}
			if held, ok := best[id]; !ok || got.At.After(held.At) {
				best[id] = got
			}
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

// runsFromStream reads one persisted stream and reports, per id the stream
// attributes a run to, the run it records. A stream may hold more than one run,
// so the runs are keyed by the id each was requested for; a run requested for
// anything other than exactly one id is attributable to no id and is dropped.
// An unparseable line is skipped rather than failing the read: a truncated
// stream from an interrupted run must not cost the item its history.
//
// It is the one place that decides whether a stream yields a run, so the two
// ways a stream can fail to state a measurement are refused here rather than by
// each consumer of the attribution (keel/ac-573): the finish must belong to the
// same run as the start, and it must not precede it. Either way no run is
// attributed, which keel/ac-564 already renders as an absent last_run.
//
// DHF-REQ: keel/requirement-138
func runsFromStream(path string) map[string]*LaneRun {
	runs := map[string]*LaneRun{}
	file, err := os.Open(path)
	if err != nil {
		return runs
	}
	defer file.Close()
	starts := map[string]*RunEvent{}
	finishes := map[string]*RunEvent{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event RunEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		switch event.Event {
		case "run_started":
			if len(event.Requested) == 1 {
				copyEvent := event
				starts[event.Requested[0].ID] = &copyEvent
			}
		case "run_finished":
			copyEvent := event
			finishes[event.RunID] = &copyEvent
		}
	}
	for id, started := range starts {
		finished := finishes[started.RunID]
		if finished == nil || finished.ExitCode == nil {
			continue
		}
		if finished.Time.Before(started.Time) {
			continue
		}
		runs[id] = &LaneRun{
			RunID:      started.RunID,
			At:         started.Time,
			DurationMS: finished.Time.Sub(started.Time).Milliseconds(),
			ExitCode:   *finished.ExitCode,
		}
	}
	return runs
}
