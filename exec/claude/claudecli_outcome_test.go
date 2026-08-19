package claude

import (
	"context"
	"errors"
	"strings"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
)

// successStream is a complete, well-formed claude stream-json run whose
// terminal result event reports success.
var successStream = strings.Join([]string{
	`{"type":"system","subtype":"init"}`,
	`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`,
	`{"type":"result","is_error":false,"result":"done","num_turns":2,"duration_ms":3456,"total_cost_usd":0.0042,"usage":{"input_tokens":5,"output_tokens":7}}`,
}, "\n")

// TestRun_NonZeroExitFailsTheRunDespiteASuccessfulResultEvent proves the shared
// outcome contract's exit-code clause reaches exec/claude: the adapter used to
// discard the wait error outright, so a child that emitted a clean result event
// and then exited non-zero read as a success (keel/issue-162).
//
// DHF-TEST: keel/requirement-134
func TestRun_NonZeroExitFailsTheRunDespiteASuccessfulResultEvent(t *testing.T) {
	stub := writeStub(t, successStream, 3)

	res, err := Run(context.Background(), Request{Prompt: "x", Bin: stub})
	if err == nil {
		t.Fatal("Run returned nil err; want an error for a non-zero exit whatever the result event reported")
	}
	// keel/ac-534: the stream was decodable, so the caller still gets it.
	if res == nil {
		t.Fatal("Run returned nil Result alongside the error; want the decoded Result for a decodable failed run")
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
}

// TestRun_FailingResultEventFailsTheRunOnAZeroExit proves the mirror clause:
// a result event carrying is_error fails the run even though claude exited
// zero. The old adapter returned that Result with a nil error.
//
// DHF-TEST: keel/requirement-134
func TestRun_FailingResultEventFailsTheRunOnAZeroExit(t *testing.T) {
	stub := writeStub(t, `{"type":"result","is_error":true,"result":"Error: Reached max turns (3)","num_turns":3,"usage":{}}`, 0)

	res, err := Run(context.Background(), Request{Prompt: "x", Bin: stub})
	if err == nil {
		t.Fatal("Run returned nil err; want an error for a result event reporting failure")
	}
	if res == nil {
		t.Fatal("Run returned nil Result alongside the error; want the decoded Result (keel/ac-534)")
	}
	if !res.IsError {
		t.Error("Result.IsError = false; want the terminal event's flag surfaced")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 — the child itself succeeded", res.ExitCode)
	}
}

// TestRun_ResultCarriesExitCodeEventsAndTerminalEventPointer proves the shared
// Result shape reaches exec/claude: the child's exit code, every decoded event
// (not the terminal one alone), and a Final that points at the terminal event
// held in Events rather than at a copy.
//
// DHF-TEST: keel/requirement-134
func TestRun_ResultCarriesExitCodeEventsAndTerminalEventPointer(t *testing.T) {
	stub := writeStub(t, successStream, 0)

	res, err := Run(context.Background(), Request{Prompt: "x", Bin: stub})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Events) != 3 {
		t.Fatalf("len(Events) = %d, want 3 — every decoded event, not the terminal one alone: %+v", len(res.Events), res.Events)
	}
	if res.Final == nil {
		t.Fatal("Final is nil; want a pointer to the terminal event")
	}
	if res.Final.Type != "result" {
		t.Errorf("Final.Type = %q, want %q", res.Final.Type, "result")
	}
	if res.Final != &res.Events[len(res.Events)-1] {
		t.Error("Final does not point at the terminal event inside Events; want a pointer into the decoded stream")
	}
}

// TestRun_OutputCeilingStillReturnsNoResult proves the ceiling half of
// keel/ac-534 survives this change: a run killed at the output ceiling hands
// back no Result at all.
//
// DHF-TEST: keel/requirement-134
func TestRun_OutputCeilingStillReturnsNoResult(t *testing.T) {
	var stdout strings.Builder
	stdout.WriteString(successStream)
	for i := 0; i < 16; i++ {
		stdout.WriteString("\n" + strings.Repeat("x", 1024))
	}
	stub := writeStub(t, stdout.String(), 0)

	res, err := Run(context.Background(), Request{Prompt: "x", Bin: stub, MaxOutputBytes: 4096})
	if !errors.Is(err, procexec.ErrOutputLimitExceeded) {
		t.Fatalf("Run err = %v, want one satisfying errors.Is(err, procexec.ErrOutputLimitExceeded)", err)
	}
	if res != nil {
		t.Errorf("Run returned Result %+v at the output ceiling; want nil", res)
	}
}
