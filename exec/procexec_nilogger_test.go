package exec_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
)

// DHF-TEST: keel/requirement-122
func TestProcessStartWithNilLoggerWritesNothingToTheDefaultSink(t *testing.T) {
	var captured bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Program: "sh",
		Args:    []string{"-c", "printf 'child stdout\n'; printf 'child stderr\n' >&2"},
	})
	if err != nil {
		t.Fatalf("ProcessStart returned error: %v", err)
	}
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	if got := captured.String(); got != "" {
		t.Fatalf("a Request with no Logger wrote %d bytes to the process-wide default sink; want none:\n%s", len(got), got)
	}
}
