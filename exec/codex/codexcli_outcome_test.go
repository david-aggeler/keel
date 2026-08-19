package codex

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
)

// successStream is a complete, well-formed codex stream whose terminal event
// reports success.
var successStream = []string{
	`{"type":"task_started"}`,
	`{"type":"agent_message","text":"hi"}`,
	`{"type":"result","text":"done"}`,
}

// TestRun_NonZeroExitFailsTheRunDespiteASuccessfulStream proves the shared
// outcome contract's exit-code clause reaches exec/codex: a stub that emits a
// complete stream whose terminal event reports success and then exits non-zero
// must fail the run. The retired local rule keyed failure on an EMPTY event
// stream, which a real run never produces, so this exact case read as success
// (keel/issue-162).
//
// DHF-TEST: keel/requirement-134
func TestRun_NonZeroExitFailsTheRunDespiteASuccessfulStream(t *testing.T) {
	dir := t.TempDir()
	stub := writeStreamStub(t, filepath.Join(dir, "argv.txt"), filepath.Join(dir, "stdinlen.txt"), successStream, 2)

	res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
	if err == nil {
		t.Fatal("Run returned nil err; want an error for a non-zero exit whatever the stream contained")
	}
	// keel/ac-534: the stream was decodable, so the caller still gets it.
	if res == nil {
		t.Fatal("Run returned nil Result alongside the error; want the decoded Result for a decodable failed run")
	}
	if res.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2", res.ExitCode)
	}
	if len(res.Events) != len(successStream) {
		t.Errorf("len(Events) = %d, want %d", len(res.Events), len(successStream))
	}
}

// TestRun_FailingTerminalEventFailsTheRunOnAZeroExit proves the mirror clause:
// a codex terminal event reporting failure fails the run even though the child
// exited zero, so a clean exit code cannot mask codex's own verdict.
//
// DHF-TEST: keel/requirement-134
func TestRun_FailingTerminalEventFailsTheRunOnAZeroExit(t *testing.T) {
	for name, terminal := range map[string]string{
		"error event":          `{"type":"error","message":"codex blew up"}`,
		"result with is_error": `{"type":"result","is_error":true,"text":"codex blew up"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			lines := []string{`{"type":"task_started"}`, terminal}
			stub := writeStreamStub(t, filepath.Join(dir, "argv.txt"), filepath.Join(dir, "stdinlen.txt"), lines, 0)

			res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
			if err == nil {
				t.Fatal("Run returned nil err; want an error for a terminal event reporting failure")
			}
			if res == nil {
				t.Fatal("Run returned nil Result alongside the error; want the decoded Result (keel/ac-534)")
			}
			if res.ExitCode != 0 {
				t.Errorf("ExitCode = %d, want 0 — the child itself succeeded", res.ExitCode)
			}
		})
	}
}

// TestRun_ResultCarriesExitCodeEventsAndTerminalEventPointer proves the shared
// Result shape: the child's exit code, every decoded event, and a Final that
// points at the terminal event held in Events rather than at a copy.
//
// DHF-TEST: keel/requirement-134
func TestRun_ResultCarriesExitCodeEventsAndTerminalEventPointer(t *testing.T) {
	dir := t.TempDir()
	stub := writeStreamStub(t, filepath.Join(dir, "argv.txt"), filepath.Join(dir, "stdinlen.txt"), successStream, 0)

	res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Events) != len(successStream) {
		t.Fatalf("len(Events) = %d, want %d", len(res.Events), len(successStream))
	}
	if res.Final == nil {
		t.Fatal("Final is nil; want a pointer to the terminal event")
	}
	if res.Final != &res.Events[len(res.Events)-1] {
		t.Error("Final does not point at the terminal event inside Events; want a pointer into the decoded stream")
	}
}

// TestRun_OutputCeilingStillReturnsNoResult proves the ceiling half of
// keel/ac-534 survives this change: a run killed at the output ceiling hands
// back no Result at all, because the truncated fragment records nothing about
// its own truncation and would read as a complete run.
//
// DHF-TEST: keel/requirement-134
func TestRun_OutputCeilingStillReturnsNoResult(t *testing.T) {
	dir := t.TempDir()
	lines := append([]string{}, successStream...)
	for i := 0; i < 16; i++ {
		lines = append(lines, strings.Repeat("x", 1024))
	}
	stub := writeStreamStub(t, filepath.Join(dir, "argv.txt"), filepath.Join(dir, "stdinlen.txt"), lines, 0)

	res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub, MaxOutputBytes: 4096})
	if !errors.Is(err, procexec.ErrOutputLimitExceeded) {
		t.Fatalf("Run err = %v, want one satisfying errors.Is(err, procexec.ErrOutputLimitExceeded)", err)
	}
	if res != nil {
		t.Errorf("Run returned Result %+v at the output ceiling; want nil", res)
	}
}

// TestRun_FailedRunWithEventsCarriesStderrOnTheError proves keel/ac-16's stderr
// clause holds on the failure path the shared outcome contract opened. The
// retired rule raised an error only when NO events had been parsed, so this
// case — a decodable stream followed by a non-zero exit — previously produced
// no error at all and had no stderr carriage to test. It is now the common
// failure shape, and codex's stderr must still reach the caller or the failure
// is undiagnosable.
//
// DHF-TEST: keel/requirement-7
func TestRun_FailedRunWithEventsCarriesStderrOnTheError(t *testing.T) {
	dir := t.TempDir()
	const boom = "CODEX_BOOM_c3d4"
	stub := writeStderrStub(t, successStream, boom, "", 4)

	res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
	if err == nil {
		t.Fatal("Run returned nil err; want an error for a non-zero exit")
	}
	if res == nil {
		t.Fatal("Run returned nil Result; want the decoded stream alongside the error")
	}
	if !strings.Contains(err.Error(), boom) {
		t.Errorf("error %q does not carry codex's stderr marker %q; the failure cause was dropped", err.Error(), boom)
	}
}

// TestRun_FailingTerminalEventCarriesStderrOnTheError proves the same clause on
// the other new failure path: a terminal event reporting failure on a zero
// exit.
//
// DHF-TEST: keel/requirement-7
func TestRun_FailingTerminalEventCarriesStderrOnTheError(t *testing.T) {
	dir := t.TempDir()
	const boom = "CODEX_BOOM_e5f6"
	lines := []string{`{"type":"task_started"}`, `{"type":"error","message":"codex blew up"}`}
	stub := writeStderrStub(t, lines, boom, "", 0)

	_, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
	if err == nil {
		t.Fatal("Run returned nil err; want an error for a terminal event reporting failure")
	}
	if !strings.Contains(err.Error(), boom) {
		t.Errorf("error %q does not carry codex's stderr marker %q", err.Error(), boom)
	}
}
