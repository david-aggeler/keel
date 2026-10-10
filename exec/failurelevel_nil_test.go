package exec_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

// typedNilFailureLevel is a non-nil slog.Leveler interface holding a nil
// pointer. A plain "!= nil" check passes it; calling Level on it panics.
var typedNilFailureLevel slog.Leveler = (*slog.LevelVar)(nil)

// DHF-TEST: keel/requirement-24 (keel/ac-799)
func TestTypedNilFailureLevelReportsFailedExitAtError(t *testing.T) {
	start, end := runLifecycle(t, procexec.Request{
		Program:      "sh",
		Args:         []string{"-c", "echo boom >&2; exit 5"},
		FailureLevel: typedNilFailureLevel,
	})
	if end["process_id"] != start["process_id"] {
		t.Fatalf("process_end process_id = %#v, want %#v", end["process_id"], start["process_id"])
	}
	if end["level"] != "ERROR" {
		t.Errorf("process_end level = %#v, want %q (same as FailureLevel unset)", end["level"], "ERROR")
	}
}

// DHF-TEST: keel/requirement-24 (keel/ac-799)
func TestTypedNilFailureLevelReportsStartFailureAtError(t *testing.T) {
	var logBuf bytes.Buffer
	logger := mustLogger(t, logging.Config{
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           &logBuf,
	})
	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:       logger,
		Program:      "/nonexistent",
		FailureLevel: typedNilFailureLevel,
	})
	if err == nil {
		t.Fatalf("ProcessStart(/nonexistent) error = nil, process = %#v", proc)
	}
	records := parseJSONLogRecords(t, logBuf.String())
	starts := recordsWithEvent(records, "process_start")
	if len(starts) != 1 {
		t.Fatalf("process_start records = %d, want 1: %#v", len(starts), records)
	}
	var ends []map[string]any
	for _, rec := range recordsWithEvent(records, "process_end") {
		if rec["process_id"] == starts[0]["process_id"] {
			ends = append(ends, rec)
		}
	}
	if len(ends) != 1 {
		t.Fatalf("process_end records for process_id %v = %d, want 1: %#v", starts[0]["process_id"], len(ends), records)
	}
	if ends[0]["level"] != "ERROR" {
		t.Errorf("process_end level = %#v, want %q (same as FailureLevel unset)", ends[0]["level"], "ERROR")
	}
}
