package exec

import (
	"context"
	"testing"
)

// capturedLine is one record emitted by captureWriter, reduced to the fields
// this white-box test asserts on.
type capturedLine struct {
	level string
	data  string
}

// recordingProcessLogger implements processLogger and captures the level and
// "data" field of each process-output record.
type recordingProcessLogger struct {
	lines []capturedLine
}

func (l *recordingProcessLogger) record(level, msg string, args ...any) {
	if msg != "process output" {
		return
	}
	for i := 0; i+1 < len(args); i += 2 {
		if key, _ := args[i].(string); key == "data" {
			data, _ := args[i+1].(string)
			l.lines = append(l.lines, capturedLine{level: level, data: data})
		}
	}
}

func (l *recordingProcessLogger) Debug(msg string, args ...any) { l.record("DEBUG", msg, args...) }
func (l *recordingProcessLogger) Error(msg string, args ...any) { l.record("ERROR", msg, args...) }
func (l *recordingProcessLogger) Info(msg string, args ...any)  {}
func (l *recordingProcessLogger) InfoContext(_ context.Context, msg string, args ...any) {
}

// DHF-TEST: keel/requirement-24
func TestCaptureWriterBuffersLinesAcrossSplitWrites(t *testing.T) {
	logger := &recordingProcessLogger{}
	w := &captureWriter{logger: logger, streamName: "stderr"}

	// A single logical line "error\n" delivered as two separate writes must
	// produce ONE record, not two fragments.
	mustWrite(t, w, "err")
	mustWrite(t, w, "or\n")
	// A blank line spanning the boundary is dropped.
	mustWrite(t, w, "\n")
	// A complete line plus the start of the next, split across the boundary.
	mustWrite(t, w, "second line\nthird ")
	mustWrite(t, w, "line\n")
	// A trailing unterminated fragment is only emitted on flush.
	mustWrite(t, w, "final no newline")

	// "error", "second line", "third line" are complete; "final no newline" is
	// still a pending fragment and must NOT be logged until flush.
	if got := len(logger.lines); got != 3 {
		t.Fatalf("before flush: %d records %#v, want 3 (trailing fragment must not be logged early)", got, logger.lines)
	}

	w.flush()

	want := []string{"error", "second line", "third line", "final no newline"}
	if len(logger.lines) != len(want) {
		t.Fatalf("records = %#v, want %v", logger.lines, want)
	}
	for i, expect := range want {
		if logger.lines[i].data != expect {
			t.Fatalf("record[%d].data = %q, want %q (all: %#v)", i, logger.lines[i].data, expect, logger.lines)
		}
		if logger.lines[i].level != "ERROR" {
			t.Fatalf("record[%d].level = %q, want ERROR (stderr)", i, logger.lines[i].level)
		}
	}
}

// DHF-TEST: keel/requirement-24
func TestCaptureWriterFlushIsIdempotentAndClearsPending(t *testing.T) {
	logger := &recordingProcessLogger{}
	w := &captureWriter{logger: logger, streamName: "stdout"}

	mustWrite(t, w, "only partial")
	w.flush()
	w.flush() // second flush must be a no-op

	if len(logger.lines) != 1 {
		t.Fatalf("records = %#v, want exactly 1 after double flush", logger.lines)
	}
	if logger.lines[0].data != "only partial" || logger.lines[0].level != "DEBUG" {
		t.Fatalf("record = %#v, want stdout DEBUG %q", logger.lines[0], "only partial")
	}
}

func mustWrite(t *testing.T, w *captureWriter, s string) {
	t.Helper()
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatalf("Write(%q) returned error: %v", s, err)
	}
}
