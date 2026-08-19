package exec_test

import (
	"strings"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
)

// TestDecideCLIOutcome_SucceedsOnlyWhenBothFactsAreClean proves the shared
// contract treats a run as successful exactly when the child exited zero and
// the wrapped CLI's terminal event reported no failure.
//
// DHF-TEST: keel/requirement-134
func TestDecideCLIOutcome_SucceedsOnlyWhenBothFactsAreClean(t *testing.T) {
	got := procexec.DecideCLIOutcome(0, false)
	if got.Failed {
		t.Fatalf("DecideCLIOutcome(0, false) = %+v, want a non-failed outcome", got)
	}
	if got.ExitCodeFailed || got.TerminalEventFailed {
		t.Fatalf("DecideCLIOutcome(0, false) = %+v, want neither fact flagged", got)
	}
	if got.Reason != "" {
		t.Fatalf("Reason = %q, want empty on a successful run", got.Reason)
	}
}

// TestDecideCLIOutcome_NonZeroExitFails proves a non-zero exit alone fails the
// run, whatever the terminal event reported.
//
// DHF-TEST: keel/requirement-134
func TestDecideCLIOutcome_NonZeroExitFails(t *testing.T) {
	got := procexec.DecideCLIOutcome(2, false)
	if !got.Failed || !got.ExitCodeFailed {
		t.Fatalf("DecideCLIOutcome(2, false) = %+v, want a failed outcome flagged on the exit code", got)
	}
	if got.TerminalEventFailed {
		t.Fatalf("DecideCLIOutcome(2, false) = %+v, want the terminal-event fact clean", got)
	}
	if !strings.Contains(got.Reason, "2") {
		t.Fatalf("Reason = %q, want it to name the exit code", got.Reason)
	}
}

// TestDecideCLIOutcome_FailingTerminalEventFailsOnAZeroExit proves the mirror
// clause: a terminal event reporting failure fails a run that exited zero, so
// a clean exit code cannot mask the CLI's own verdict.
//
// DHF-TEST: keel/requirement-134
func TestDecideCLIOutcome_FailingTerminalEventFailsOnAZeroExit(t *testing.T) {
	got := procexec.DecideCLIOutcome(0, true)
	if !got.Failed || !got.TerminalEventFailed {
		t.Fatalf("DecideCLIOutcome(0, true) = %+v, want a failed outcome flagged on the terminal event", got)
	}
	if got.ExitCodeFailed {
		t.Fatalf("DecideCLIOutcome(0, true) = %+v, want the exit-code fact clean", got)
	}
	if got.Reason == "" {
		t.Fatal("Reason is empty, want it to state the terminal-event verdict")
	}
}

// TestDecideCLIOutcome_ReportsBothFactsWhenBothFail proves neither fact masks
// the other: when both fire, the outcome records both and the reason names
// both.
//
// DHF-TEST: keel/requirement-134
func TestDecideCLIOutcome_ReportsBothFactsWhenBothFail(t *testing.T) {
	got := procexec.DecideCLIOutcome(9, true)
	if !got.Failed || !got.ExitCodeFailed || !got.TerminalEventFailed {
		t.Fatalf("DecideCLIOutcome(9, true) = %+v, want both facts flagged", got)
	}
	if !strings.Contains(got.Reason, "9") {
		t.Fatalf("Reason = %q, want it to name the exit code alongside the terminal-event verdict", got.Reason)
	}
}

// TestDecideCLIOutcome_UnknownExitCodeFails proves the sentinel exit code
// keel/exec reports for a child that never produced a status (-1, e.g. killed
// by a signal) is treated as a failure rather than read as "not non-zero".
//
// DHF-TEST: keel/requirement-134
func TestDecideCLIOutcome_UnknownExitCodeFails(t *testing.T) {
	got := procexec.DecideCLIOutcome(-1, false)
	if !got.Failed || !got.ExitCodeFailed {
		t.Fatalf("DecideCLIOutcome(-1, false) = %+v, want a failed outcome", got)
	}
}
