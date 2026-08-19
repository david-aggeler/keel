package claude

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	logging "github.com/david-aggeler/keel/log"
)

// TestRun_InvokesClaudeWithPromptAndStreamingJSONMode proves the invocation
// shape: the argv carries -p immediately followed by the prompt, and selects
// claude's line-per-event streaming-JSON output mode.
//
// DHF-TEST: keel/requirement-135
func TestRun_InvokesClaudeWithPromptAndStreamingJSONMode(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stub := writeArgvStub(t, fixtureResult, 0, argvFile)

	if _, err := Run(context.Background(), Request{Prompt: "greet me", Bin: stub}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	argvBytes, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(argvBytes)), "\n")

	var promptFound bool
	for i, arg := range argv {
		if arg == "-p" {
			if i+1 >= len(argv) {
				t.Fatalf("-p at end of argv: %v", argv)
			}
			if argv[i+1] != "greet me" {
				t.Fatalf("argv after -p = %q, want the prompt; argv=%v", argv[i+1], argv)
			}
			promptFound = true
		}
	}
	if !promptFound {
		t.Fatalf("argv carries no -p prompt flag: %v", argv)
	}
	if joined := strings.Join(argv, " "); !strings.Contains(joined, "--output-format stream-json") {
		t.Fatalf("argv = %q, want claude's streaming-JSON output mode", joined)
	}
}

// TestRun_EmptyPromptRejectsBeforeSpawningProcess proves the empty-prompt check
// happens before any child is spawned: the configured stub would create a
// marker file the instant it ran, and that marker must never appear.
//
// DHF-TEST: keel/requirement-135
func TestRun_EmptyPromptRejectsBeforeSpawningProcess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran.marker")
	stubPath := filepath.Join(dir, "claude-marker-stub")
	script := "#!/bin/sh\ntouch '" + marker + "'\nexit 0\n"
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Request{Prompt: "", Bin: stubPath})
	if err == nil {
		t.Fatal("Run returned nil err for an empty prompt; want a non-nil error")
	}
	if res != nil {
		t.Errorf("Run returned non-nil Result %+v for an empty prompt; want nil", res)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("marker file exists; the claude process was spawned despite the empty prompt")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("stat marker: %v", statErr)
	}
}

// TestRun_DeliversEventsBeforeProcessExit proves the adapter decodes each event
// line as it arrives rather than buffering the whole run.
//
// The stub emits one assistant event, then blocks until a sentinel file
// exists; the injected logger creates that sentinel the first time it sees a
// "claude progress" record. The process can therefore only exit if the adapter
// made that event observable while the child was still running. A buffering
// adapter would never emit the record, the stub would run to its own cap and
// exit non-zero, and this test would fail.
//
// DHF-TEST: keel/requirement-135
func TestRun_DeliversEventsBeforeProcessExit(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "release.sentinel")
	stubPath := filepath.Join(dir, "claude-blocking-stub")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"mid-flight\"}]}}'\n" +
		"i=0\n" +
		"while [ ! -f '" + sentinel + "' ]; do\n" +
		"  i=$((i+1))\n" +
		"  [ \"$i\" -gt 300 ] && exit 7\n" +
		"  sleep 0.05\n" +
		"done\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"is_error\":false,\"result\":\"done\",\"num_turns\":1,\"usage\":{}}'\n" +
		"exit 0\n"
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Request{
		Prompt:  "x",
		Bin:     stubPath,
		Timeout: 30 * time.Second,
		Logger:  &sentinelLogger{sentinel: sentinel},
	})
	if err != nil {
		t.Fatalf("Run: %v (the stub never saw the sentinel — the adapter buffered instead of streaming)", err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("len(Events) = %d, want 2: %+v", len(res.Events), res.Events)
	}
	if res.Events[0].Type != "assistant" {
		t.Errorf("Events[0].Type = %q, want %q", res.Events[0].Type, "assistant")
	}
}

// sentinelLogger creates a sentinel file the first time it records a claude
// progress line, so a stub blocked on that file can only proceed once the
// adapter has decoded and surfaced a mid-flight event.
type sentinelLogger struct {
	sentinel string
	done     bool
}

func (l *sentinelLogger) Info(msg string, _ ...any) {
	if l.done || msg != "claude progress" {
		return
	}
	l.done = true
	_ = os.WriteFile(l.sentinel, []byte("go"), 0o600)
}

func (l *sentinelLogger) Debug(string, ...any)                        {}
func (l *sentinelLogger) Error(string, ...any)                        {}
func (l *sentinelLogger) InfoContext(context.Context, string, ...any) {}

// TestRun_ResultSurfacesTerminalEventFieldsAndVerbatimEvent proves the result
// event is the terminal event: its reported text, turn count, duration, cost
// and token usage reach the typed Result fields, and the event itself is kept
// verbatim on Raw for anything the Result does not model.
//
// DHF-TEST: keel/requirement-135
func TestRun_ResultSurfacesTerminalEventFieldsAndVerbatimEvent(t *testing.T) {
	const terminal = `{"type":"result","is_error":false,"result":"done","num_turns":4,"duration_ms":12345,` +
		`"total_cost_usd":0.0123,"session_id":"sess-abc","usage":{"input_tokens":42,"output_tokens":17,` +
		`"cache_creation_input_tokens":1000,"cache_read_input_tokens":9000}}`
	stub := writeStub(t, terminal, 0)

	res, err := Run(context.Background(), Request{Prompt: "x", Bin: stub})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "done" || res.NumTurns != 4 || res.DurationMS != 12345 || res.CostUSD != 0.0123 {
		t.Errorf("Result = %+v, want the terminal event's reported fields", res)
	}
	if res.Usage.TotalInput() != 42+1000+9000 || res.Usage.OutputTokens != 17 {
		t.Errorf("Usage = %+v, want the terminal event's token accounting", res.Usage)
	}
	// session_id is not modelled on Result; the verbatim event is how a caller
	// reaches it, so Raw must be the event as emitted.
	if string(res.Raw) != terminal {
		t.Errorf("Raw = %s\nwant the verbatim terminal event:\n%s", res.Raw, terminal)
	}
	if res.Final == nil || string(res.Final.Raw) != terminal {
		t.Errorf("Final = %+v, want the verbatim terminal event inside Events", res.Final)
	}
}

// TestRun_ProgressDetailIsTrimmedAtOneHundredSixtyCharacters proves the
// claude-specific presentation choice pinned by keel/ac-532: progress detail
// curated from a claude event and emitted to the log is trimmed at 160
// characters. No other adapter inherits this trim.
//
// DHF-TEST: keel/requirement-135
func TestRun_ProgressDetailIsTrimmedAtOneHundredSixtyCharacters(t *testing.T) {
	long := strings.Repeat("y", 200)
	stream := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"` + long + `"}]}}`,
		`{"type":"result","is_error":false,"result":"done","num_turns":1,"usage":{}}`,
	}, "\n")
	stub := writeStub(t, stream, 0)

	var logBuf bytes.Buffer
	logger, err := logging.New(logging.Config{
		Service:          "claudecli-trim-test",
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           &logBuf,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := Run(context.Background(), Request{Prompt: "x", Bin: stub, Logger: logger}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	records := parseJSONLogRecords(t, logBuf.String())
	progress := findLogRecord(t, records, "msg", "claude progress")
	detail, ok := progress["detail"].(string)
	if !ok {
		t.Fatalf("claude progress detail = %#v, want string", progress["detail"])
	}
	if got := strings.Count(detail, "y"); got != 160 {
		t.Fatalf("progress detail carries %d characters of the 200-character event text, want 160", got)
	}
}
