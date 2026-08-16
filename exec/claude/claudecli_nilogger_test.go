package claude_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	claude "github.com/david-aggeler/keel/exec/claude"
)

// writeProgressStub writes an executable stub standing in for the real claude
// binary. It emits one progress event followed by the terminal result event, so
// a nil-logger run exercises both the adapter's Run path and its per-line
// progress path.
func writeProgressStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-stub")
	const progress = `{"type":"assistant","message":{"text":"working on it"}}`
	const result = `{"type":"result","is_error":false,"result":"done","num_turns":1}`
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '" + progress + "'\n" +
		"printf '%s\\n' '" + result + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// DHF-TEST: keel/requirement-122
func TestRunWithNilLoggerWritesNothingToTheDefaultSink(t *testing.T) {
	var captured bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	res, err := claude.Run(context.Background(), claude.Request{
		Prompt: "greet me",
		Bin:    writeProgressStub(t),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.Text != "done" {
		t.Fatalf("Run parsed Text = %q; want %q — the stub did not drive the progress path", res.Text, "done")
	}

	if got := captured.String(); got != "" {
		t.Fatalf("a Request with no Logger wrote %d bytes to the process-wide default sink; want none:\n%s", len(got), got)
	}
}
