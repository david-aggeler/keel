package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	logging "github.com/david-aggeler/keel/log"
)

// writeStreamStub writes an executable stub standing in for the real codex
// binary. The stub:
//   - records the exact argv it was invoked with (one arg per line) into
//     argvFile,
//   - reads all of stdin and records the byte count into stdinLenFile,
//   - emits a multi-line JSONL event stream on stdout (one JSON object per
//     line, in order),
//   - exits with exitCode.
//
// Mirrors claudecli_test.go's writeStub technique, but emits a streaming
// multi-line stream rather than a single result blob.
//
// DHF-REQ: keel/requirement-7
// This scripted-stub approach is what keeps the suite deterministic and
// codex-free: every non-smoke test drives this stub instead of a real codex
// binary, so the unit suite needs no live codex install and never flakes on a
// live model's output.
func writeStreamStub(t *testing.T, argvFile, stdinLenFile string, lines []string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-stub")

	script := "#!/bin/sh\n"
	// Record argv, one entry per line. "$@" is exactly the args the adapter
	// passed to exec.CommandContext — the binary path is $0 and is NOT in
	// "$@", so this list has no leading binary-path token to skip.
	script += "for a in \"$@\"; do echo \"$a\" >> " + shquote(argvFile) + "; done\n"
	// Drain stdin and record its byte length. With stdin from /dev/null this
	// must be 0.
	script += "wc -c < /dev/stdin | tr -d ' \\n' > " + shquote(stdinLenFile) + "\n"
	// Emit the JSONL stream, one object per line, in order.
	for _, ln := range lines {
		script += "printf '%s\\n' " + shquote(ln) + "\n"
	}
	script += "exit " + itoa(exitCode) + "\n"

	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// shquote single-quotes a string for safe use in the /bin/sh stub.
func shquote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

var streamLines = []string{
	`{"type":"task_started"}`,
	`{"type":"agent_message","text":"hi"}`,
	`{"type":"result","text":"done"}`,
}

// TestRun_SpawnsCodexExecWithStreamingJSONAndNoStdin proves the wrapper
// invokes `codex exec` with a streaming-JSON output flag and feeds the codex
// process no stdin (stdin from /dev/null -> 0 bytes read).
//
// DHF-TEST: keel/requirement-7
func TestRun_SpawnsCodexExecWithStreamingJSONAndNoStdin(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stdinLenFile := filepath.Join(dir, "stdinlen.txt")
	stub := writeStreamStub(t, argvFile, stdinLenFile, streamLines, 0)

	const prompt = "greet me"
	_, err := Run(context.Background(), Request{Prompt: prompt, Dir: dir, Bin: stub})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	argvBytes, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv file: %v", err)
	}
	// The stub records exactly "$@" — i.e. the args the adapter passed to
	// exec.CommandContext, with the binary path ($0) NOT included. So argv
	// here IS the full adapter-supplied argv, with no token to drop.
	argv := strings.Split(strings.TrimSpace(string(argvBytes)), "\n")

	if len(argv) == 0 {
		t.Fatalf("argv is empty")
	}

	// Contract: the `exec` subcommand must be first. The adapter must NOT
	// inject any leading top-level flag (e.g. `--cd`) before `exec` —
	// worktree/--cd rooting is a different unit's responsibility and must not
	// appear in this argv.
	if argv[0] != "exec" {
		t.Errorf("argv[0] = %q, want %q (no leading top-level flag before exec): %v", argv[0], "exec", argv)
	}

	// Contract: the streaming-JSON flag must be present as exactly "--json"
	// (not merely an arg containing the substring "json").
	var sawJSON bool
	for _, a := range argv {
		if a == "--json" {
			sawJSON = true
			break
		}
	}
	if !sawJSON {
		t.Errorf("argv missing exact %q flag: %v", "--json", argv)
	}

	// Contract: the prompt is the final positional arg.
	if last := argv[len(argv)-1]; last != prompt {
		t.Errorf("last argv = %q, want prompt %q: %v", last, prompt, argv)
	}

	stdinLenBytes, err := os.ReadFile(stdinLenFile)
	if err != nil {
		t.Fatalf("read stdin-len file: %v", err)
	}
	if got := strings.TrimSpace(string(stdinLenBytes)); got != "0" {
		t.Errorf("codex received stdin of %s bytes; want 0 (stdin must be /dev/null)", got)
	}
}

// TestRun_DeliversEachEventToOnEventInOrder proves OnEvent is invoked once per
// emitted JSONL line, in arrival order, with the decoded type sequence
// [task_started, agent_message, result].
//
// DHF-TEST: keel/requirement-7
func TestRun_DeliversEachEventToOnEventInOrder(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stdinLenFile := filepath.Join(dir, "stdinlen.txt")
	stub := writeStreamStub(t, argvFile, stdinLenFile, streamLines, 0)

	var gotTypes []string
	_, err := Run(context.Background(), Request{
		Prompt: "x",
		Dir:    dir,
		Bin:    stub,
		OnEvent: func(e Event) {
			gotTypes = append(gotTypes, e.Type)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"task_started", "agent_message", "result"}
	if len(gotTypes) != len(want) {
		t.Fatalf("OnEvent called %d times, want %d: %v", len(gotTypes), len(want), gotTypes)
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Errorf("event[%d].Type = %q, want %q", i, gotTypes[i], want[i])
		}
	}
}

// DHF-TEST: openbrain/requirement-591
func TestRun_CapturesThreadStartedThreadID(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stdinLenFile := filepath.Join(dir, "stdinlen.txt")
	stub := writeStreamStub(t, argvFile, stdinLenFile, []string{
		`{"type":"thread.started","thread_id":"0199a213-codex-thread"}`,
		`{"type":"result","text":"done"}`,
	}, 0)

	res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ThreadID != "0199a213-codex-thread" {
		t.Fatalf("ThreadID = %q, want %q", res.ThreadID, "0199a213-codex-thread")
	}
}

// DHF-TEST: openbrain/requirement-591
func TestRun_LeavesThreadIDEmptyWhenThreadStartedAbsent(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stdinLenFile := filepath.Join(dir, "stdinlen.txt")
	stub := writeStreamStub(t, argvFile, stdinLenFile, []string{
		`{"type":"result","text":"done"}`,
	}, 0)

	res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ThreadID != "" {
		t.Fatalf("ThreadID = %q, want empty", res.ThreadID)
	}
}

// TestRun_UsesSharedProcessLifecycleLogging proves codex launches through the
// shared ProcessStart facility: the public Run call still curates codex JSONL
// events for OnEvent, while process start/end lifecycle records are emitted
// uniformly by the process facility.
//
// DHF-TEST: keel/requirement-2, openbrain/requirement-602
func TestRun_UsesSharedProcessLifecycleLogging(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stdinLenFile := filepath.Join(dir, "stdinlen.txt")
	stub := writeStreamStub(t, argvFile, stdinLenFile, realExecJSONLines, 0)

	var logBuf bytes.Buffer
	logger := logging.New(logging.Config{Service: "codexcli-test", Writer: &logBuf, Level: slog.LevelDebug})
	var gotTypes []string
	res, err := Run(context.Background(), Request{
		Prompt: "inspect repository",
		Dir:    dir,
		Bin:    stub,
		Logger: logger,
		OnEvent: func(e Event) {
			gotTypes = append(gotTypes, e.Type)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if got := strings.Join(gotTypes, ","); got != "thread.started,turn.started,command_execution,agent_message,turn.completed" {
		t.Fatalf("OnEvent types = %s, want curated codex JSONL event types", got)
	}

	records := parseJSONLogRecords(t, logBuf.String())
	start := findLogRecord(t, records, "event_type", "process_start")
	end := findLogRecord(t, records, "event_type", "process_end")
	output := findLogRecordWithFields(t, records, map[string]string{
		"event_type": "process_output",
		"stream":     "stdout",
	})

	commandLine, ok := start["command_line"].(string)
	if !ok {
		t.Fatalf("process_start command_line = %#v, want string", start["command_line"])
	}
	if !strings.Contains(commandLine, "exec --json") || !strings.Contains(commandLine, "inspect repository") {
		t.Fatalf("process_start command_line = %q, want full codex exec --json command", commandLine)
	}
	if got, ok := start["working_dir"].(string); !ok || got != dir {
		t.Fatalf("process_start working_dir = %#v, want %q", start["working_dir"], dir)
	}
	if got, ok := end["exit_code"].(float64); !ok || got != 0 {
		t.Fatalf("process_end exit_code = %#v, want 0", end["exit_code"])
	}
	if data, ok := output["data"].(string); !ok || !strings.Contains(data, `"thread.started"`) {
		t.Fatalf("process_output data = %#v, want raw codex stdout to remain observable", output["data"])
	}
	if got, ok := output["level"].(string); !ok || got != "debug" {
		t.Fatalf("codex stdout process_output level = %#v, want debug", output["level"])
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

func findLogRecordWithFields(t *testing.T, records []map[string]any, fields map[string]string) map[string]any {
	t.Helper()
	for _, record := range records {
		matches := true
		for key, value := range fields {
			if got, ok := record[key].(string); !ok || got != value {
				matches = false
				break
			}
		}
		if matches {
			return record
		}
	}
	t.Fatalf("missing log record with fields %#v in %#v", fields, records)
	return nil
}

// realExecJSONLines is the documented `codex exec --json` stdout stream — the
// thread/item events format, verbatim from the codex CLI manual's `codex exec
// --json` reference sample (top-level type = thread.*/turn.*/item.*; the
// semantic type is nested under "item"). This is the format codexcli actually
// reads — distinct from the rollout-file event_msg/payload envelope that
// misled CR-392. See gold issue-244.
var realExecJSONLines = []string{
	`{"type":"thread.started","thread_id":"0199a213-81c0-7800-8aa1-bbab2a035a53"}`,
	`{"type":"turn.started"}`,
	`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"bash -lc ls","status":"in_progress"}}`,
	`{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Repo contains docs, sdk, and examples directories."}}`,
	`{"type":"turn.completed","usage":{"input_tokens":24763,"output_tokens":122}}`,
}

// TestRun_ResolvesSemanticTypeFromExecJSONStream proves Event.Type is the nested
// item.type for the real codex exec --json thread/item framing — the top-level
// "type" (item.started/item.completed) is the wrapper and must NOT leak through;
// non-item frames (thread.started/turn.*) keep their top-level type. This is the
// regression guard for issue-244: before the fix Event.Type was the wrapper, so
// every curated-progress consumer matched nothing.
//
// DHF-TEST: openbrain/requirement-32
func TestRun_ResolvesSemanticTypeFromExecJSONStream(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stdinLenFile := filepath.Join(dir, "stdinlen.txt")
	stub := writeStreamStub(t, argvFile, stdinLenFile, realExecJSONLines, 0)

	var gotTypes []string
	_, err := Run(context.Background(), Request{
		Prompt:  "x",
		Dir:     dir,
		Bin:     stub,
		OnEvent: func(e Event) { gotTypes = append(gotTypes, e.Type) },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"thread.started", "turn.started", "command_execution", "agent_message", "turn.completed"}
	if len(gotTypes) != len(want) {
		t.Fatalf("OnEvent called %d times, want %d: %v", len(gotTypes), len(want), gotTypes)
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Errorf("event[%d].Type = %q, want semantic %q (item wrapper leaked?)", i, gotTypes[i], want[i])
		}
	}
}

// TestDecodeEventType_RobustAcrossEnvelopes proves the decoder also unwraps the
// rollout/persistence (event_msg/payload) and older (msg) envelopes, and passes
// a flat top-level type through — so codexcli tolerates codex format drift.
//
// DHF-TEST: openbrain/requirement-32
func TestDecodeEventType_RobustAcrossEnvelopes(t *testing.T) {
	cases := map[string]string{
		`{"type":"item.completed","item":{"type":"reasoning","text":"hi"}}`: "reasoning",
		`{"type":"event_msg","payload":{"type":"agent_message"}}`:           "agent_message",
		`{"id":"0","msg":{"type":"agent_reasoning"}}`:                       "agent_reasoning",
		`{"type":"agent_message","text":"flat stub shape"}`:                 "agent_message",
		`{"type":"error","message":"boom"}`:                                 "error",
	}
	for line, want := range cases {
		if got := decodeEventType([]byte(line)); got != want {
			t.Errorf("decodeEventType(%s) = %q, want %q", line, got, want)
		}
	}
}

// TestVersion_RunsBinVersionAndTrims proves Version shells out to `<bin>
// --version` and returns the trimmed output. openbrain-client logs this at
// startup (owner request / requirement-32 observability).
//
// DHF-TEST: openbrain/requirement-32
func TestVersion_RunsBinVersionAndTrims(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "codex-ver")
	// Echo a version line only when invoked as `--version`, with trailing space
	// and newline to prove trimming.
	script := "#!/bin/sh\n[ \"$1\" = \"--version\" ] && printf 'codex-cli 0.140.0  \\n' || exit 2\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Version(context.Background(), stub)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got != "codex-cli 0.140.0" {
		t.Errorf("Version = %q, want %q (trimmed)", got, "codex-cli 0.140.0")
	}
}

// TestRun_ResultCarriesAllEventsFinalAndExitCode proves the returned Result
// collects every decoded event in order, exposes the terminal event as Final,
// reports ExitCode 0, and preserves each Event.Raw as the verbatim JSONL line.
//
// DHF-TEST: keel/requirement-7
func TestRun_ResultCarriesAllEventsFinalAndExitCode(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stdinLenFile := filepath.Join(dir, "stdinlen.txt")
	stub := writeStreamStub(t, argvFile, stdinLenFile, streamLines, 0)

	res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(res.Events) != 3 {
		t.Fatalf("len(Events) = %d, want 3: %+v", len(res.Events), res.Events)
	}
	if res.Final == nil {
		t.Fatal("Final is nil; want terminal event")
	}
	if res.Final.Type != "result" {
		t.Errorf("Final.Type = %q, want %q", res.Final.Type, "result")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}

	// Each Event.Raw must be the verbatim emitted line, in order.
	for i, want := range streamLines {
		if got := strings.TrimSpace(string(res.Events[i].Raw)); got != want {
			t.Errorf("Events[%d].Raw = %q, want %q", i, got, want)
		}
	}
}

// writeBlockingStub writes an executable stub that proves true mid-flight
// streaming. It:
//   - emits the first JSONL line and flushes it,
//   - then blocks, polling for a sentinel file (path supplied via the
//     CODEX_STUB_SENTINEL env var) in a tight loop with a hard ~4s cap,
//   - on seeing the sentinel, emits the remaining lines and exits 0,
//   - if the cap elapses without the sentinel, exits 7 (non-zero) so a broken
//     (buffered) adapter fails fast rather than hanging CI.
//
// The first line must reach the adapter (and thus OnEvent) BEFORE the process
// exits, because the process will not exit until OnEvent has created the
// sentinel. A buffered impl that only replays to OnEvent after cmd.Wait() can
// never create the sentinel in time → the stub caps out and exits 7.
func writeBlockingStub(t *testing.T, first string, rest []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-blocking-stub")

	script := "#!/bin/sh\n"
	// Emit and flush the first line immediately.
	script += "printf '%s\\n' " + shquote(first) + "\n"
	// Poll for the sentinel. ~4s cap at 0.05s per iteration => 80 iterations.
	script += "i=0\n"
	script += "while [ ! -f \"$CODEX_STUB_SENTINEL\" ]; do\n"
	script += "  i=$((i+1))\n"
	script += "  if [ \"$i\" -ge 80 ]; then exit 7; fi\n"
	script += "  sleep 0.05\n"
	script += "done\n"
	// Sentinel appeared (OnEvent fired mid-flight): emit the rest and exit 0.
	for _, ln := range rest {
		script += "printf '%s\\n' " + shquote(ln) + "\n"
	}
	script += "exit 0\n"

	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeStderrStub writes an executable stub that emits the given JSONL lines on
// stdout (in order), writes stderrText to stderr verbatim, optionally touches a
// marker file to prove the process ran, and exits with exitCode. It is the
// error-path counterpart to writeStreamStub: it lets a test drive a non-zero
// exit with controllable stdout/stderr.
//
// If markerFile is non-empty the stub creates it as its FIRST action — so a test
// can assert process-did-not-run by checking the marker's absence.
func writeStderrStub(t *testing.T, lines []string, stderrText, markerFile string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-stderr-stub")

	script := "#!/bin/sh\n"
	if markerFile != "" {
		script += "echo ran > " + shquote(markerFile) + "\n"
	}
	for _, ln := range lines {
		script += "printf '%s\\n' " + shquote(ln) + "\n"
	}
	if stderrText != "" {
		script += "printf '%s\\n' " + shquote(stderrText) + " 1>&2\n"
	}
	script += "exit " + itoa(exitCode) + "\n"

	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRun_NonZeroExitWithEventsReturnsResultWithExitCodeAndNoError proves a
// non-zero codex exit is NOT swallowed when stdout still produced events: Run
// returns a non-nil Result (err nil, per codexcli.go's documented contract),
// the parsed events are populated, Final is the terminal event, and the
// process's non-zero exit code is observable as Result.ExitCode.
//
// DHF-TEST: keel/requirement-7
func TestRun_NonZeroExitWithEventsReturnsResultWithExitCodeAndNoError(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	stdinLenFile := filepath.Join(dir, "stdinlen.txt")
	// Same valid JSONL stream, but the process exits 2.
	stub := writeStreamStub(t, argvFile, stdinLenFile, streamLines, 2)

	res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
	// Contract (codexcli.go line 151-164): events parsed → (res, nil) even on
	// non-zero exit. The exit code is carried on the result, not raised as an
	// error.
	if err != nil {
		t.Fatalf("Run returned err %v; want nil when events were parsed despite non-zero exit", err)
	}
	if res == nil {
		t.Fatal("Run returned nil Result; want non-nil so ExitCode/events are inspectable")
	}
	if res.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2 (non-zero exit must be observable on the result)", res.ExitCode)
	}
	if len(res.Events) != len(streamLines) {
		t.Fatalf("len(Events) = %d, want %d: %+v", len(res.Events), len(streamLines), res.Events)
	}
	if res.Final == nil {
		t.Fatal("Final is nil; want terminal event populated despite non-zero exit")
	}
	if res.Final.Type != "result" {
		t.Errorf("Final.Type = %q, want %q", res.Final.Type, "result")
	}
}

// TestRun_EmptyPromptReturnsErrBeforeSpawningProcess proves an empty prompt is
// rejected before any process is spawned: Run returns a nil Result and a
// non-nil error, and the configured Bin stub never runs (its marker file is
// never created).
//
// DHF-TEST: keel/requirement-7
func TestRun_EmptyPromptReturnsErrBeforeSpawningProcess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran.marker")
	// This stub would create the marker the instant it executes. If empty-prompt
	// rejection happens before spawn, the marker must never appear.
	stub := writeStderrStub(t, streamLines, "", marker, 0)

	res, err := Run(context.Background(), Request{Prompt: "", Dir: dir, Bin: stub})
	if err == nil {
		t.Fatal("Run returned nil err for empty prompt; want a non-nil error")
	}
	if res != nil {
		t.Errorf("Run returned non-nil Result %+v for empty prompt; want nil", res)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("marker file exists; the codex process was spawned despite the empty prompt — rejection must happen before spawn")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("stat marker: %v", statErr)
	}
}

// TestRun_NonZeroExitWithNoEventsReturnsErrCarryingStderr proves that when codex
// exits non-zero having produced no parseable events on stdout, Run returns a
// nil Result and an error whose message carries the codex stderr — so the
// failure cause is not lost.
//
// DHF-TEST: keel/requirement-7
func TestRun_NonZeroExitWithNoEventsReturnsErrCarryingStderr(t *testing.T) {
	dir := t.TempDir()
	const boom = "CODEX_BOOM_a1b2"
	// Empty stdout (no lines), distinctive stderr, non-zero exit.
	stub := writeStderrStub(t, nil, boom, "", 3)

	res, err := Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
	if err == nil {
		t.Fatal("Run returned nil err; want an error for non-zero exit with no events")
	}
	if res != nil {
		t.Errorf("Run returned non-nil Result %+v; want nil when no events were parsed", res)
	}
	if !strings.Contains(err.Error(), boom) {
		t.Errorf("error %q does not contain stderr marker %q; stderr was dropped", err.Error(), boom)
	}
}

// TestRun_StreamsEventsBeforeProcessExit proves events reach OnEvent BEFORE the
// codex process exits — i.e. the adapter streams stdout line-by-line as the
// process runs rather than buffering all stdout and replaying after cmd.Wait().
//
// The stub emits the first event, then blocks until a sentinel file exists.
// OnEvent creates that sentinel on the first event it receives. Therefore the
// process can only exit if OnEvent fired while the process was still running.
// A buffered adapter would not invoke OnEvent until after the process exited —
// but the process can't exit until OnEvent runs, so the stub would hit its cap
// and exit 7, failing this test. Passing requires true mid-flight streaming.
//
// DHF-TEST: keel/requirement-7
func TestRun_StreamsEventsBeforeProcessExit(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "release.sentinel")
	stub := writeBlockingStub(t,
		`{"type":"task_started"}`,
		[]string{`{"type":"result"}`},
	)

	t.Setenv("CODEX_STUB_SENTINEL", sentinel)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var gotTypes []string
	res, err := Run(ctx, Request{
		Prompt:  "x",
		Dir:     dir,
		Bin:     stub,
		Timeout: 5 * time.Second,
		OnEvent: func(e Event) {
			gotTypes = append(gotTypes, e.Type)
			// On the first event, release the stub by creating the sentinel.
			// If this never runs before exit, the stub caps out and exits 7.
			if len(gotTypes) == 1 {
				if err := os.WriteFile(sentinel, []byte("go"), 0o644); err != nil {
					t.Errorf("write sentinel: %v", err)
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("Run: %v (stub exited before OnEvent released it — adapter buffered instead of streaming)", err)
	}

	want := []string{"task_started", "result"}
	if len(res.Events) != len(want) {
		t.Fatalf("len(Events) = %d, want %d: %+v", len(res.Events), len(want), res.Events)
	}
	for i := range want {
		if res.Events[i].Type != want[i] {
			t.Errorf("Events[%d].Type = %q, want %q", i, res.Events[i].Type, want[i])
		}
	}
	if res.Final == nil {
		t.Fatal("Final is nil; want terminal event")
	}
	if res.Final.Type != "result" {
		t.Errorf("Final.Type = %q, want %q", res.Final.Type, "result")
	}
}

// writeRawStub writes an executable /bin/sh stub from a verbatim script body.
// Unlike writeStreamStub it imposes no structure: the caller supplies the exact
// shell that must run. Used by the failure-mode tests that need to emit a
// partial stream then either block past a deadline or emit an over-long line.
func writeRawStub(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-raw-stub")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRun_DeadlineExceededReturnsTimeoutError proves that when a run is killed
// by its own Timeout, Run does NOT report a clean exit: it returns a non-nil
// error wrapping context.DeadlineExceeded AND a non-nil Result carrying the
// events that streamed before the kill.
//
// Why this matters: a timed-out/killed codex must be distinguishable from a
// codex that finished cleanly. The cr-294 runner makes retry decisions off
// Run's error — if a deadline kill were swallowed as (res, nil), the runner
// would treat a half-finished, killed process as a successful complete run and
// never retry it.
//
// DHF-TEST: keel/requirement-7
func TestRun_DeadlineExceededReturnsTimeoutError(t *testing.T) {
	dir := t.TempDir()
	// Emit one valid event, flush it, then sleep well past the Timeout so the
	// process is killed by the context deadline rather than exiting on its own.
	stub := writeRawStub(t, "printf '%s\\n' "+shquote(`{"type":"task_started"}`)+"\nsleep 10\n")

	res, err := Run(context.Background(), Request{
		Prompt:  "x",
		Dir:     dir,
		Bin:     stub,
		Timeout: 200 * time.Millisecond,
	})

	if err == nil {
		t.Fatal("Run returned nil err; want an error wrapping context.DeadlineExceeded for a timed-out run")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v; want errors.Is(err, context.DeadlineExceeded) — a deadline kill must be distinguishable from a clean exit", err)
	}
	if res == nil {
		t.Fatal("Run returned nil Result; want a non-nil Result carrying the partial events that streamed before the kill")
	}
	if len(res.Events) < 1 {
		t.Errorf("len(res.Events) = %d; want >= 1 (the event streamed before the kill must be preserved)", len(res.Events))
	}
}

// TestRun_TruncatedOverLongLineReturnsError proves that when the stdout scanner
// fails on an over-long line (a single line past the 4 MiB buffer cap ->
// bufio.ErrTooLong), Run does NOT report a clean exit: it returns a non-nil
// error wrapping bufio.ErrTooLong AND a non-nil Result holding the events
// parsed before the bad line.
//
// Why this matters: a stream cut short — over-long line, broken pipe — must not
// be reported as a successful complete run. If it were, Result.Final would name
// a non-terminal event as the final one, making Final a lie for the runner.
//
// DHF-TEST: keel/requirement-7
func TestRun_TruncatedOverLongLineReturnsError(t *testing.T) {
	dir := t.TempDir()
	// One valid event, then a single line of > 4 MiB with NO trailing newline,
	// then a clean exit 0 — so the deadline is provably NOT the cause; only the
	// scanner's buffer-cap overflow can produce the error.
	body := "printf '%s\\n' " + shquote(`{"type":"task_started"}`) + "\n" +
		"head -c 5000000 /dev/zero | tr '\\0' x\n" +
		"exit 0\n"
	stub := writeRawStub(t, body)

	// Run in a goroutine behind a watchdog. An impl that stops draining stdout
	// once the scanner trips ErrTooLong deadlocks: the stub blocks writing the
	// 5 MiB line into a full pipe and cmd.Wait() never returns. That deadlock is
	// itself a contract violation — surface it as a clean FAIL, not a hung suite.
	type out struct {
		res *Result
		err error
	}
	done := make(chan out, 1)
	go func() {
		res, err := Run(context.Background(), Request{
			Prompt:  "x",
			Dir:     dir,
			Bin:     stub,
			Timeout: 5 * time.Second,
		})
		done <- out{res, err}
	}()

	var res *Result
	var err error
	select {
	case o := <-done:
		res, err = o.res, o.err
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not return within 20s — an over-long line must surface bufio.ErrTooLong promptly, not deadlock on an undrained stdout pipe")
	}

	if err == nil {
		t.Fatal("Run returned nil err; want an error wrapping bufio.ErrTooLong for a truncated/over-long stream")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("err = %v; want errors.Is(err, bufio.ErrTooLong) — a cut-short stream must not be reported as a clean run", err)
	}
	if res == nil {
		t.Fatal("Run returned nil Result; want a non-nil Result holding the events parsed before the over-long line")
	}
	var sawFirst bool
	for _, ev := range res.Events {
		if ev.Type == "task_started" {
			sawFirst = true
			break
		}
	}
	if !sawFirst {
		t.Errorf("res.Events = %+v; want the first valid event (task_started) parsed before the bad line", res.Events)
	}
}

// TestRun_EnvExportsAssignmentsToChild proves Request.Env assignments reach the
// codex child's environment and that a key in Env overrides the inherited
// value — the mechanism by which openbrain-client exports a provisioned
// TMPDIR/GOTMPDIR so the merge-gate CGO link does not OOM on a small /tmp
// (openbrain/requirement-181). The stub records its own TMPDIR/GOTMPDIR env
// into files the test then asserts on.
//
// DHF-TEST: openbrain/requirement-181
func TestRun_EnvExportsAssignmentsToChild(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "child-tmpdir.txt")
	gotmpFile := filepath.Join(dir, "child-gotmpdir.txt")

	// Stub records the env values it actually saw.
	body := "printf '%s' \"$TMPDIR\" > " + shquote(tmpFile) + "\n" +
		"printf '%s' \"$GOTMPDIR\" > " + shquote(gotmpFile) + "\n" +
		"printf '%s\\n' " + shquote(`{"type":"result"}`) + "\n" +
		"exit 0\n"
	stub := writeRawStub(t, body)

	// Seed an inherited TMPDIR the child would see WITHOUT Env, so the test
	// proves Env overrides the inherited value, not merely that it is present.
	t.Setenv("TMPDIR", filepath.Join(dir, "inherited-should-be-overridden"))

	want := filepath.Join(dir, "provisioned")
	_, err := Run(context.Background(), Request{
		Prompt: "x",
		Dir:    dir,
		Bin:    stub,
		Env:    []string{"TMPDIR=" + want, "GOTMPDIR=" + want},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	gotTmp, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("read child TMPDIR: %v", err)
	}
	if string(gotTmp) != want {
		t.Errorf("child TMPDIR = %q, want %q (Env must override the inherited TMPDIR)", string(gotTmp), want)
	}
	gotGoTmp, err := os.ReadFile(gotmpFile)
	if err != nil {
		t.Fatalf("read child GOTMPDIR: %v", err)
	}
	if string(gotGoTmp) != want {
		t.Errorf("child GOTMPDIR = %q, want %q", string(gotGoTmp), want)
	}
}

// TestRun_LiveSmoke drives the REAL codex binary end-to-end and proves the
// wrapper spawns it, streams its actual `exec --json` stdout, and surfaces a
// terminal event — proving wiring and parsing against a live codex, NOT any
// specific model output.
//
// This is the clause-3 live-smoke half of requirement-50. It is guarded behind
// CODEXCLI_LIVE_SMOKE: CI never sets that env var, so the test always SKIPs in
// CI and the suite never depends on a real codex binary (or network) being
// present. Run it locally with CODEXCLI_LIVE_SMOKE=1 against an installed codex.
//
// Assertions are deliberately lenient on content: a live model's exact reply
// varies run to run, so the smoke proves the wrapper drives a real codex and
// parses its real `--json` stream, not that any particular text came back.
//
// DHF-TEST: keel/requirement-7
func TestRun_LiveSmoke(t *testing.T) {
	if os.Getenv("CODEXCLI_LIVE_SMOKE") == "" {
		t.Skip("set CODEXCLI_LIVE_SMOKE=1 to run the live codex smoke test")
	}

	var events int
	// Bin empty → "codex" resolved via PATH (the real binary).
	res, err := Run(context.Background(), Request{
		Prompt:  "Reply with exactly the word: PONG",
		Timeout: 60 * time.Second,
		OnEvent: func(Event) { events++ },
	})
	if err != nil {
		t.Fatalf("Run against real codex: %v", err)
	}
	if res == nil {
		t.Fatal("Run returned nil Result")
	}
	if events == 0 {
		t.Error("OnEvent never fired; want at least one event from real codex")
	}
	if res.Final == nil {
		t.Error("Final is nil; want a terminal event from real codex")
	}
}
