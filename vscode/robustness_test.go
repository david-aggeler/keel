package vscode

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeProfile is a stub WorkspaceProfile for exercising the neutral robustness
// engine without a consumer dev tool. prepare is the desired-state hook the
// engine drives before a run.
type fakeProfile struct {
	consumerID  string
	node        string
	remediation string
	prepare     func(ctx context.Context, laneID string) LaneReadiness
}

func (f fakeProfile) Repo() string            { return "/repo" }
func (f fakeProfile) ModulePath() string      { return "github.com/david-aggeler/keel" }
func (f fakeProfile) LogDir() string          { return "/repo/.devtools/logs" }
func (f fakeProfile) MaxOutputBytes() int     { return 16 << 20 }
func (f fakeProfile) RemediationHint() string { return f.remediation }
func (f fakeProfile) ConsumerID() string      { return f.consumerID }
func (f fakeProfile) Node() string            { return f.node }
func (f fakeProfile) PrepareLane(ctx context.Context, laneID string) LaneReadiness {
	if f.prepare == nil {
		return LaneReadiness{}
	}
	return f.prepare(ctx, laneID)
}

// capture collects emitted events for assertions.
func capture() (RunEventWriter, *[]RunEvent) {
	var events []RunEvent
	w := func(e RunEvent) { events = append(events, e) }
	return w, &events
}

// DHF-TEST: keel/requirement-23
func TestEnginePrepare_UnmetPrereqYieldsStructuredLaneBlocked(t *testing.T) {
	prof := fakeProfile{
		remediation: "start the demo core services first",
		prepare: func(_ context.Context, _ string) LaneReadiness {
			return LaneReadiness{Blocked: []BlockedPrereq{{Resource: "demo-core-services", Detail: "not up"}}}
		},
	}
	emit, events := capture()
	ready := NewEngine(prof).Prepare(context.Background(), "e2e-mock", []string{"keel::lane::e2e-mock"}, emit)
	if ready {
		t.Fatal("Prepare returned ready=true for an unmet prerequisite; want a structured lane-blocked result")
	}
	var sawFailed, sawTerminal bool
	for _, e := range *events {
		switch e.Event {
		case "failed":
			sawFailed = true
			if e.TestID != "keel::lane::e2e-mock" {
				t.Fatalf("lane-blocked failed event has test_id %q, want the selected lane id", e.TestID)
			}
			if !strings.Contains(e.Message, "demo-core-services") {
				t.Fatalf("lane-blocked message %q does not name the blocked resource", e.Message)
			}
			if !strings.Contains(e.Message, "start the demo core services first") {
				t.Fatalf("lane-blocked message %q does not carry the remediation hint", e.Message)
			}
		case "run_finished":
			sawTerminal = true
			if e.ExitCode == nil || *e.ExitCode == 0 {
				t.Fatalf("lane-blocked run_finished must carry a non-zero exit_code, got %v", e.ExitCode)
			}
		}
	}
	if !sawFailed {
		t.Fatal("no failed event emitted for the blocked lane — a silent failure")
	}
	if !sawTerminal {
		t.Fatal("lane-blocked result did not end with a terminal run_finished")
	}
}

// DHF-TEST: keel/requirement-23
func TestEnginePrepare_SatisfiedPrereqLetsRunProceed(t *testing.T) {
	prof := fakeProfile{prepare: func(_ context.Context, _ string) LaneReadiness { return LaneReadiness{} }}
	emit, events := capture()
	if ready := NewEngine(prof).Prepare(context.Background(), "test-fast", []string{"keel::lane::test-fast"}, emit); !ready {
		t.Fatal("Prepare returned ready=false when all prerequisites were satisfied")
	}
	if len(*events) != 0 {
		t.Fatalf("Prepare emitted %d events on a ready lane, want none", len(*events))
	}
}

// DHF-TEST: keel/requirement-23
func TestNormalizeRunEvents_UnknownEventRenderedAsOutputAndLogged(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		`{"event":"run_started"}`,
		`{"event":"heartbeat","message":"tick"}`,
		`{"event":"run_finished","exit_code":0}`,
	}, "\n"))
	emit, events := capture()
	var logs []string
	exit := NormalizeRunEvents(in, nil, emit, func(m string) { logs = append(logs, m) })
	if exit != 0 {
		t.Fatalf("exit=%d, want 0 (producer emitted its own terminal)", exit)
	}
	var renderedUnknown bool
	for _, e := range *events {
		if e.Event == "heartbeat" {
			t.Fatal("unknown event value passed through verbatim — must be rendered as output")
		}
		if e.Event == "output" && strings.Contains(e.Message, "heartbeat") {
			renderedUnknown = true
		}
	}
	if !renderedUnknown {
		t.Fatal("unknown event was not rendered as an output event (silently dropped)")
	}
	if !IsKnownRunEvent("run_started") || IsKnownRunEvent("heartbeat") {
		t.Fatal("IsKnownRunEvent does not match the producer enum")
	}
	joinedLogs := strings.Join(logs, "\n")
	if !strings.Contains(joinedLogs, "heartbeat") {
		t.Fatalf("unknown event was not logged; logs=%q", joinedLogs)
	}
}

// DHF-TEST: keel/requirement-23
func TestNormalizeRunEvents_ProducerCrashStillEmitsTerminalRunFinished(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		`{"event":"run_started"}`,
		`{"event":"test_started","test_id":"keel::lane::test-fast"}`,
	}, "\n"))
	emit, events := capture()
	exit := NormalizeRunEvents(in, errors.New("signal: killed"), emit, nil)
	if exit == 0 {
		t.Fatal("a crashed producer with no terminal must yield a non-zero exit code")
	}
	last := (*events)[len(*events)-1]
	if last.Event != "run_finished" {
		t.Fatalf("stream did not end with a synthetic run_finished; last event = %q", last.Event)
	}
	if last.ExitCode == nil || *last.ExitCode == 0 {
		t.Fatalf("synthetic terminal run_finished must carry a non-zero exit_code, got %v", last.ExitCode)
	}
	var sawErrored bool
	for _, e := range *events {
		if e.Event == "errored" && strings.Contains(e.Message, "signal: killed") {
			sawErrored = true
		}
	}
	if !sawErrored {
		t.Fatal("terminal run_finished was not preceded by an errored event describing the fault")
	}
}

// DHF-TEST: keel/requirement-23
func TestNormalizeRunEvents_LeadingNonJSONToleratedAsOutput(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"starting vscode test run...",
		"warning: something noisy {not json",
		`{"event":"run_started"}`,
		`{"event":"run_finished","exit_code":0}`,
	}, "\n"))
	emit, events := capture()
	exit := NormalizeRunEvents(in, nil, emit, nil)
	if exit != 0 {
		t.Fatalf("exit=%d, want 0 — leading noise must not abort run detection", exit)
	}
	if len(*events) < 3 {
		t.Fatalf("expected leading noise as output plus the two run events, got %d events", len(*events))
	}
	if (*events)[0].Event != "output" || !strings.Contains((*events)[0].Message, "starting vscode") {
		t.Fatalf("leading non-JSON line was not tolerated as output: %+v", (*events)[0])
	}
	// Run detection still works: run_started must be present after the noise.
	var sawStarted bool
	for _, e := range *events {
		if e.Event == "run_started" {
			sawStarted = true
		}
	}
	if !sawStarted {
		t.Fatal("run_started was not detected after leading non-JSON noise")
	}
}

// DHF-TEST: keel/requirement-23
func TestRunAttribution_TripleCarriedOnEveryRun(t *testing.T) {
	prof := fakeProfile{consumerID: "vscode", node: "/projects/keel/worktrees/cr-7"}
	attr := NewRunAttribution(prof, "20260711T120000")
	if !attr.Complete() {
		t.Fatalf("attribution triple incomplete: %+v", attr)
	}
	line := attr.LogLine()
	for _, want := range []string{"vscode", "20260711T120000", "/projects/keel/worktrees/cr-7"} {
		if !strings.Contains(line, want) {
			t.Fatalf("attribution log line %q missing %q", line, want)
		}
	}
	stamped := attr.Stamp(RunEvent{Event: "passed", TestID: "go::root"})
	if stamped.RunID != "20260711T120000" {
		t.Fatalf("Stamp did not set run_id: %q", stamped.RunID)
	}
	if stamped.Workspace != "/projects/keel/worktrees/cr-7" {
		t.Fatalf("Stamp did not set workspace to the node: %q", stamped.Workspace)
	}
}
