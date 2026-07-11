package vscode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// This file holds the consumer-neutral robustness contract for the VS Code
// test-runner engine (keel/requirement-23): the run-event stream is made
// tolerant and terminal-guaranteed, missing prerequisites surface as a
// structured lane-blocked result rather than a silent failure, and every run
// carries a consumer/run/node attribution triple. It imports nothing under
// github.com/david-aggeler/keel — the guarantees travel with the engine when it is
// donated to keel/vscode.

// --- Attribution triple (cross-node / cross-consumer accountability) ---

// RunAttribution is the triple that makes a run accountable across nodes and
// consumers: the consumer dev tool that produced it, the run id, and the
// worktree/node it executed on.
//
// DHF-REQ: keel/requirement-23
type RunAttribution struct {
	ConsumerID string
	RunID      string
	Node       string
}

// NewRunAttribution builds the attribution triple for runID from the profile.
//
// DHF-REQ: keel/requirement-23
func NewRunAttribution(p WorkspaceProfile, runID string) RunAttribution {
	return RunAttribution{
		ConsumerID: p.ConsumerID(),
		RunID:      runID,
		Node:       p.Node(),
	}
}

// Complete reports whether every leg of the attribution triple is present.
func (a RunAttribution) Complete() bool {
	return a.ConsumerID != "" && a.RunID != "" && a.Node != ""
}

// LogLine renders the triple as a single structured per-run log line — the
// first observability step for cross-node/cross-consumer accountability.
func (a RunAttribution) LogLine() string {
	return fmt.Sprintf("run %s consumer=%s node=%s", a.RunID, a.ConsumerID, a.Node)
}

// Stamp attaches the attribution to a run event: the run id onto run_id and the
// node onto the workspace field, so every emitted record is traceable to the
// run (and thereby, via its log line, to the consumer). The consumer id is not
// a wire field — the run-event source enum is closed — so it rides the run log
// line rather than each event.
func (a RunAttribution) Stamp(e RunEvent) RunEvent {
	e.RunID = a.RunID
	if a.Node != "" {
		e.Workspace = a.Node
	}
	return e
}

// --- Run-event stream normalization (tolerance + terminal guarantee) ---

// knownRunEvents is the closed set of event values the producer emits and
// validates its own output against. Consumers tolerate anything outside it.
var knownRunEvents = map[string]struct{}{
	"run_started":  {},
	"test_started": {},
	"output":       {},
	"passed":       {},
	"failed":       {},
	"errored":      {},
	"cancelled":    {},
	"skipped":      {},
	"artifact":     {},
	"run_finished": {},
}

// IsKnownRunEvent reports whether event is one of the producer's closed enum
// values. A consumer renders anything else as output rather than dropping it.
//
// DHF-REQ: keel/requirement-23
func IsKnownRunEvent(event string) bool {
	_, ok := knownRunEvents[event]
	return ok
}

// NormalizeRunEvents reads a producer's run-event JSONL stream from r and emits
// normalized events to emit, upholding the run-event robustness contract:
//
//   - a line that is not a JSON object, or a JSON object with no event value,
//     is rendered as an output event (leading-noise tolerance) rather than
//     aborting run detection;
//   - a JSON event whose value is not in the producer enum is rendered as
//     output AND passed to logf, never silently dropped;
//   - if the stream ends without a terminal run_finished — whether the producer
//     exited cleanly or crashed (producerErr != nil) — a synthetic errored
//     event describing the fault plus a terminal run_finished with a non-zero
//     exit code is emitted, so a run never "just stops".
//
// It returns the exit code of the terminal run_finished (the producer's own
// when it emitted one, else the synthetic non-zero code). logf may be nil.
//
// DHF-REQ: keel/requirement-23
func NormalizeRunEvents(r io.Reader, producerErr error, emit RunEventWriter, logf func(string)) int {
	sawTerminal := false
	terminalExit := 0

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Event == "" {
			// Non-JSON or event-less line: tolerate as output, do not abort.
			emit(RunEvent{Event: "output", Message: line})
			continue
		}
		if !IsKnownRunEvent(event.Event) {
			if logf != nil {
				logf(fmt.Sprintf("unknown run event %q rendered as output", event.Event))
			}
			emit(RunEvent{Event: "output", Message: line})
			continue
		}
		if event.Event == "run_finished" {
			sawTerminal = true
			if event.ExitCode != nil {
				terminalExit = *event.ExitCode
			}
		}
		emit(event)
	}
	if err := scanner.Err(); err != nil && producerErr == nil {
		// A mid-stream read failure is itself a producer fault.
		producerErr = err
	}

	if sawTerminal {
		return terminalExit
	}
	msg := "run stream ended without a terminal run_finished"
	if producerErr != nil {
		msg = "run producer failed: " + producerErr.Error()
	}
	emit(RunEvent{Event: "errored", Message: msg})
	code := 1
	emit(RunEvent{Event: "run_finished", Message: msg, ExitCode: &code})
	return code
}

// --- Lane preparation / structured lane-blocked result ---

// BlockedPrereq is one lane prerequisite that could not be satisfied.
type BlockedPrereq struct {
	Resource string
	Detail   string
}

// LaneReadiness is the outcome of preparing a lane's desired state. A non-empty
// Blocked slice means the run must not proceed.
type LaneReadiness struct {
	Blocked []BlockedPrereq
}

// Ready reports whether every prerequisite was satisfied.
func (r LaneReadiness) Ready() bool { return len(r.Blocked) == 0 }

// blockedSummary renders the blocked prerequisites for a message.
func blockedSummary(blocked []BlockedPrereq) string {
	parts := make([]string, 0, len(blocked))
	for _, b := range blocked {
		if b.Detail != "" {
			parts = append(parts, b.Resource+" ("+b.Detail+")")
		} else {
			parts = append(parts, b.Resource)
		}
	}
	return strings.Join(parts, ", ")
}

// EmitLaneBlocked writes the structured lane-blocked result: a failed event per
// selected test id carrying the blocked prerequisites and the consumer's
// remediation hint, then a terminal run_finished with a non-zero exit code.
// This is the explicit, attributable alternative to a run that fails silently
// when a prerequisite cannot be met. It returns the non-zero exit code.
//
// DHF-REQ: keel/requirement-23
func EmitLaneBlocked(testIDs []string, blocked []BlockedPrereq, hint string, emit RunEventWriter) int {
	summary := blockedSummary(blocked)
	message := "lane blocked: " + summary
	if hint != "" {
		message = message + "\n" + hint
	}
	for _, id := range testIDs {
		emit(RunEvent{Event: "failed", TestID: id, Message: message})
	}
	code := 1
	emit(RunEvent{Event: "run_finished", Message: "lane blocked: " + summary, ExitCode: &code})
	return code
}

// --- Engine glue ---

// Engine is the consumer-neutral run engine. It holds only the WorkspaceProfile
// port, so it imports nothing consumer-specific; a consumer dev tool supplies
// the profile.
type Engine struct {
	profile WorkspaceProfile
}

// NewEngine builds an Engine over a WorkspaceProfile.
func NewEngine(p WorkspaceProfile) *Engine { return &Engine{profile: p} }

// Prepare drives the profile's lane preparation ahead of a run. When a
// prerequisite cannot be met it emits the structured lane-blocked result (using
// the profile's remediation hint) and returns ready=false; otherwise it returns
// ready=true and the run may proceed.
//
// DHF-REQ: keel/requirement-23
func (e *Engine) Prepare(ctx context.Context, laneID string, testIDs []string, emit RunEventWriter) (ready bool) {
	readiness := e.profile.PrepareLane(ctx, laneID)
	if readiness.Ready() {
		return true
	}
	EmitLaneBlocked(testIDs, readiness.Blocked, e.profile.RemediationHint(), emit)
	return false
}
