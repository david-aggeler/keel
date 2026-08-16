package codex_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	codex "github.com/david-aggeler/keel/exec/codex"
)

// writeProgressStub writes an executable stub standing in for the real codex
// binary. It emits one progress-bearing event followed by the terminal result
// event, so a nil-logger run exercises the adapter's own progress path.
func writeProgressStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-stub")
	const progress = `{"type":"item.completed","text":"working on it"}`
	const result = `{"type":"result","text":"done"}`
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

	res, err := codex.Run(context.Background(), codex.Request{
		Prompt: "greet me",
		Bin:    writeProgressStub(t),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("Run decoded %d events; want 2 — the stub did not drive the progress path", len(res.Events))
	}

	if got := captured.String(); got != "" {
		t.Fatalf("a Request with no Logger wrote %d bytes to the process-wide default sink; want none:\n%s", len(got), got)
	}
}
