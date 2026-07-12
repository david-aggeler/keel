package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// writeStub writes an executable stub that emits the given stdout and exits
// with the given code. Used in place of the real claude binary.
func writeStub(t *testing.T, stdout string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-stub")
	script := "#!/bin/sh\n"
	if stdout != "" {
		script += "cat <<'STUBEOF'\n" + stdout + "\nSTUBEOF\n"
	}
	script += "exit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeArgvStub(t *testing.T, stdout string, exitCode int, argvFile string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-stub")
	script := "#!/bin/sh\n"
	script += "printf '%s\\n' \"$@\" > " + shellQuote(argvFile) + "\n"
	if stdout != "" {
		script += "cat <<'STUBEOF'\n" + stdout + "\nSTUBEOF\n"
	}
	script += "exit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

const fixtureResult = `{"type":"result","is_error":false,"result":"HELLO-WORLD-SKILL-ACTIVATED v2","num_turns":4,"duration_ms":12345,"total_cost_usd":0.0123,"usage":{"input_tokens":42,"output_tokens":17,"cache_creation_input_tokens":1000,"cache_read_input_tokens":9000}}`

// TestRun_ParsesResultEvent proves the wrapper parses the result event into
// typed fields: text, turns, duration, cost, and all four usage counters.
func TestRun_ParsesResultEvent(t *testing.T) {
	stub := writeStub(t, fixtureResult, 0)

	res, err := Run(context.Background(), Request{Prompt: "greet me", Bin: stub})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "HELLO-WORLD-SKILL-ACTIVATED v2" {
		t.Errorf("Text = %q", res.Text)
	}
	if res.NumTurns != 4 || res.DurationMS != 12345 || res.CostUSD != 0.0123 {
		t.Errorf("metrics = turns=%d dur=%d cost=%f", res.NumTurns, res.DurationMS, res.CostUSD)
	}
	if res.Usage.InputTokens != 42 || res.Usage.OutputTokens != 17 ||
		res.Usage.CacheCreationInputTokens != 1000 || res.Usage.CacheReadInputTokens != 9000 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if got := res.Usage.TotalInput(); got != 42+1000+9000 {
		t.Errorf("TotalInput = %d", got)
	}
	if res.IsError {
		t.Error("IsError must be false")
	}
}

// TestRun_EmptyPrompt proves the wrapper rejects an empty prompt before
// spawning anything.
func TestRun_EmptyPrompt(t *testing.T) {
	if _, err := Run(context.Background(), Request{}); err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

// TestRun_NonZeroExitEmptyStdout proves a failed spawn with no JSON output
// returns an error carrying stderr context, not a nil-deref or empty Result.
func TestRun_NonZeroExitEmptyStdout(t *testing.T) {
	stub := writeStub(t, "", 3)

	res, err := Run(context.Background(), Request{Prompt: "x", Bin: stub})
	if err == nil {
		t.Fatal("expected error for non-zero exit with empty stdout")
	}
	if res != nil {
		t.Errorf("expected nil result, got %+v", res)
	}
}

// TestRun_ErrorResultParses proves an is_error result event still parses into
// a Result (no error from Run — the CLI reported a structured failure).
func TestRun_ErrorResultParses(t *testing.T) {
	stub := writeStub(t, `{"type":"result","is_error":true,"result":"Error: Reached max turns (3)","num_turns":3,"usage":{}}`, 1)

	res, err := Run(context.Background(), Request{Prompt: "x", Bin: stub})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.IsError {
		t.Error("IsError must be true")
	}
	if res.Text == "" {
		t.Error("Text must carry the error message")
	}
}

// DHF-TEST: openbrain/requirement-615
func TestRun_SkipPermissionsOmitAllowedTools(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stub := writeArgvStub(t, fixtureResult, 0, argvFile)

	_, err := Run(context.Background(), Request{
		Prompt:          "x",
		Bin:             stub,
		SkipPermissions: true,
		AllowedTools:    []string{"Bash", "mcp__gold__get_change_request"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	argvBytes, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(argvBytes)), "\n")
	var skipCount, allowedCount int
	for _, arg := range argv {
		switch arg {
		case "--dangerously-skip-permissions":
			skipCount++
		case "--allowedTools":
			allowedCount++
		}
	}
	if skipCount != 1 {
		t.Fatalf("skip permissions flag count = %d, want 1; argv=%v", skipCount, argv)
	}
	if allowedCount != 0 {
		t.Fatalf("--allowedTools flag count = %d, want 0 when skip mode is selected; argv=%v", allowedCount, argv)
	}
}

// DHF-TEST: openbrain/requirement-615
func TestRun_AllowedToolsPathPreservedWhenSkipDisabled(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stub := writeArgvStub(t, fixtureResult, 0, argvFile)

	_, err := Run(context.Background(), Request{
		Prompt:       "x",
		Bin:          stub,
		AllowedTools: []string{"Bash", "mcp__gold__get_change_request"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	argvBytes, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(argvBytes)), "\n")
	var allowedFlags []string
	for i, arg := range argv {
		if arg == "--dangerously-skip-permissions" {
			t.Fatalf("argv contains skip flag when SkipPermissions is false: %v", argv)
		}
		if arg == "--allowedTools" {
			if i+1 >= len(argv) {
				t.Fatalf("--allowedTools at end of argv: %v", argv)
			}
			allowedFlags = append(allowedFlags, argv[i+1])
		}
	}
	want := []string{"Bash", "mcp__gold__get_change_request"}
	if strings.Join(allowedFlags, "\n") != strings.Join(want, "\n") {
		t.Fatalf("allowed tools = %v, want %v; argv=%v", allowedFlags, want, argv)
	}
}

// DHF-TEST: keel/requirement-2
func TestRun_UsesProcessStartWithClaudeStreamAdapterAndPreservesResult(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stream := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Inspecting repository status."}]}}`,
		`{"type":"result","is_error":false,"result":"done","num_turns":2,"duration_ms":3456,"total_cost_usd":0.0042,"usage":{"input_tokens":5,"output_tokens":7,"cache_creation_input_tokens":11,"cache_read_input_tokens":13}}`,
	}, "\n")
	stub := writeArgvStub(t, stream, 0, argvFile)

	var logBuf bytes.Buffer
	logger, err := logging.New(logging.Config{
		Service:          "claudecli-test",
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           &logBuf,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := Run(context.Background(), Request{
		Prompt: "summarize the branch",
		Dir:    dir,
		Bin:    stub,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "done" || res.NumTurns != 2 || res.DurationMS != 3456 || res.CostUSD != 0.0042 {
		t.Fatalf("Result = %+v, want parsed final result event", res)
	}
	if res.Usage.TotalInput() != 5+11+13 {
		t.Fatalf("Usage.TotalInput = %d, want all input token classes", res.Usage.TotalInput())
	}

	argvBytes, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(argvBytes)), "\n")
	if got := strings.Join(argv, " "); !strings.Contains(got, "--output-format stream-json") || !strings.Contains(got, "--verbose") {
		t.Fatalf("argv = %q, want claude stream-json verbose mode", got)
	}

	records := parseJSONLogRecords(t, logBuf.String())
	start := findLogRecord(t, records, "event_type", "process_start")
	end := findLogRecord(t, records, "event_type", "process_end")
	progress := findLogRecord(t, records, "msg", "claude progress")

	commandLine, ok := start["command_line"].(string)
	if !ok {
		t.Fatalf("process_start command_line = %#v, want string", start["command_line"])
	}
	if !strings.Contains(commandLine, "--output-format stream-json") || !strings.Contains(commandLine, "summarize the branch") {
		t.Fatalf("process_start command_line = %q, want full claude stream command", commandLine)
	}
	if got, ok := start["working_dir"].(string); !ok || got != dir {
		t.Fatalf("process_start working_dir = %#v, want %q", start["working_dir"], dir)
	}
	if got, ok := end["exit_code"].(float64); !ok || got != 0 {
		t.Fatalf("process_end exit_code = %#v, want 0", end["exit_code"])
	}
	if got, ok := progress["detail"].(string); !ok || !strings.Contains(got, "Inspecting repository status.") {
		t.Fatalf("claude progress detail = %#v, want curated assistant text", progress["detail"])
	}
}

func parseJSONLogRecords(t *testing.T, logs string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("unmarshal log line %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func findLogRecord(t *testing.T, records []map[string]any, key string, value string) map[string]any {
	t.Helper()
	for _, record := range records {
		if got, ok := record[key].(string); ok && got == value {
			return record
		}
	}
	t.Fatalf("missing log record with %s=%q in %#v", key, value, records)
	return nil
}
