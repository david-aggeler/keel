package exec_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

func mustLogger(t *testing.T, cfg logging.Config) *logging.Logger {
	t.Helper()
	logger, err := logging.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return logger
}

// DHF-TEST: openbrain/requirement-565
func TestProcessStartPlainCommandStreamsCapturesAndReturnsExitCode(t *testing.T) {
	var streamedOut bytes.Buffer
	var streamedErr bytes.Buffer

	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Program: "sh",
		Args: []string{
			"-c",
			"printf 'first stdout\n'; printf 'first stderr\n' >&2; exit 7",
		},
		Stdout: &streamedOut,
		Stderr: &streamedErr,
	})
	if err != nil {
		t.Fatalf("ProcessStart returned error: %v", err)
	}

	result, err := proc.Wait()
	if err == nil {
		t.Fatal("Wait returned nil error for exit code 7")
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
	if got := result.Stdout; !strings.Contains(got, "first stdout\n") {
		t.Fatalf("captured stdout = %q, want command stdout", got)
	}
	if got := result.Stderr; !strings.Contains(got, "first stderr\n") {
		t.Fatalf("captured stderr = %q, want command stderr", got)
	}
	if got := streamedOut.String(); got != result.Stdout {
		t.Fatalf("streamed stdout = %q, captured stdout = %q", got, result.Stdout)
	}
	if got := streamedErr.String(); got != result.Stderr {
		t.Fatalf("streamed stderr = %q, captured stderr = %q", got, result.Stderr)
	}
}

// DHF-TEST: keel/requirement-1, keel/requirement-20, openbrain/requirement-602
func TestProcessStartLogsStructuredLifecycleAndRedactsSensitiveArgs(t *testing.T) {
	var logBuf bytes.Buffer
	logger := mustLogger(t, logging.Config{
		Service: "procexec-test",
		Level:   slog.LevelDebug,
		Console: logging.ConsoleJSON,
		Writer:  &logBuf,
	})
	longArg := strings.Repeat("long-visible-argument-", 12)
	secret := "super-secret-token"

	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:  logger,
		Program: "sh",
		Args: []string{
			"-c",
			"printf 'during stdout\n'; printf 'during stderr\n' >&2",
			longArg,
			secret,
		},
		SensitiveArgs: map[int]bool{3: true},
	})
	if err != nil {
		t.Fatalf("ProcessStart returned error: %v", err)
	}
	result, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}

	records := parseJSONLogRecords(t, logBuf.String())
	start := findLogRecord(t, records, "event_type", "process_start")
	during := findLogRecordWithFields(t, records, map[string]string{
		"event_type": "process_output",
		"stream":     "stdout",
	})
	stderr := findLogRecordWithFields(t, records, map[string]string{
		"event_type": "process_output",
		"stream":     "stderr",
	})
	end := findLogRecord(t, records, "event_type", "process_end")

	commandLine, ok := start["command_line"].(string)
	if !ok {
		t.Fatalf("process_start command_line = %#v, want string", start["command_line"])
	}
	if !strings.Contains(commandLine, "sh -c") || !strings.Contains(commandLine, longArg) {
		t.Fatalf("process_start command_line = %q, want full untruncated command", commandLine)
	}
	if strings.Contains(commandLine, "...") {
		t.Fatalf("process_start command_line = %q, must not be abbreviated", commandLine)
	}
	if strings.Contains(commandLine, secret) {
		t.Fatalf("process_start command_line leaked sensitive arg: %q", commandLine)
	}
	if got, ok := start["working_dir"].(string); !ok || got == "" {
		t.Fatalf("process_start working_dir = %#v, want non-empty cwd", start["working_dir"])
	}

	if got, ok := during["stream"].(string); !ok || got != "stdout" {
		t.Fatalf("process_output stream = %#v, want stdout", during["stream"])
	}
	if got, ok := during["data"].(string); !ok || got != "during stdout" {
		t.Fatalf("process_output data = %#v, want streamed stdout", during["data"])
	}
	if got, ok := during["level"].(string); !ok || got != "DEBUG" {
		t.Fatalf("stdout process_output level = %#v, want DEBUG", during["level"])
	}
	if got, ok := stderr["level"].(string); !ok || got != "ERROR" {
		t.Fatalf("stderr process_output level = %#v, want ERROR", stderr["level"])
	}
	if got, ok := end["exit_code"].(float64); !ok || got != 0 {
		t.Fatalf("process_end exit_code = %#v, want 0", end["exit_code"])
	}
	if got, ok := end["elapsed_ms"].(float64); !ok || got < 0 {
		t.Fatalf("process_end elapsed_ms = %#v, want non-negative duration", end["elapsed_ms"])
	}
}

// DHF-TEST: keel/requirement-24
func TestRequestLoggerContractIncludesErrorForStderrRouting(t *testing.T) {
	loggerField, ok := reflect.TypeOf(procexec.Request{}).FieldByName("Logger")
	if !ok {
		t.Fatal("Request.Logger field missing")
	}
	errorMethod, ok := loggerField.Type.MethodByName("Error")
	if !ok {
		t.Fatal("Request.Logger contract does not require Error; stderr process_output can bypass the caller logger")
	}
	if got := errorMethod.Type.String(); got != "func(string, ...interface {})" {
		t.Fatalf("Request.Logger Error method type = %s, want func(string, ...interface {})", got)
	}
}

// DHF-TEST: keel/requirement-1
func TestProcessStartHumanLifecycleShowsFullCommandWithoutEllipsis(t *testing.T) {
	var logBuf bytes.Buffer
	logger := mustLogger(t, logging.Config{Console: logging.ConsolePlain, Service: "procexec-test", Writer: &logBuf, DisableColor: true})
	longArg := strings.Repeat("visible-human-argument-", 12)

	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:  logger,
		Program: "sh",
		Args:    []string{"-c", "printf done", longArg},
	})
	if err != nil {
		t.Fatalf("ProcessStart returned error: %v", err)
	}
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, longArg) {
		t.Fatalf("human lifecycle output = %q, want full long argument", logged)
	}
	if strings.Contains(logged, "...") {
		t.Fatalf("human lifecycle output = %q, must not abbreviate the command", logged)
	}
	if !strings.Contains(logged, "INFO") || !strings.Contains(logged, "process start") || !strings.Contains(logged, "process end") {
		t.Fatalf("human lifecycle output = %q, want concise severity-tagged lifecycle lines", logged)
	}
}

// DHF-TEST: keel/requirement-24
func TestProcessStartLogsChildOutputAsCleanPerLineRecords(t *testing.T) {
	var logBuf bytes.Buffer
	logger := mustLogger(t, logging.Config{
		Service: "procexec-test",
		Level:   slog.LevelDebug,
		Console: logging.ConsoleJSON,
		Writer:  &logBuf,
	})

	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:  logger,
		Program: "sh",
		Args: []string{
			"-c",
			"printf 'out one\\n\\nout two'; printf 'err one\\n\\nerr two' >&2",
		},
	})
	if err != nil {
		t.Fatalf("ProcessStart returned error: %v", err)
	}
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	records := parseJSONLogRecords(t, logBuf.String())
	got := make(map[string][]string)
	for _, record := range records {
		if eventType, _ := record["event_type"].(string); eventType != "process_output" {
			continue
		}
		stream, _ := record["stream"].(string)
		data, _ := record["data"].(string)
		level, _ := record["level"].(string)
		if strings.Contains(data, "\n") {
			t.Fatalf("process_output data retained embedded newline: %#v", record)
		}
		if strings.TrimSpace(data) == "" {
			t.Fatalf("process_output logged empty/blank data: %#v", record)
		}
		if stream == "stdout" && level != "DEBUG" {
			t.Fatalf("stdout process_output level = %q, want DEBUG; record=%#v", level, record)
		}
		if stream == "stderr" && level != "ERROR" {
			t.Fatalf("stderr process_output level = %q, want ERROR; record=%#v", level, record)
		}
		got[stream] = append(got[stream], data)
	}
	if strings.Join(got["stdout"], "|") != "out one|out two" {
		t.Fatalf("stdout process_output records = %#v, want clean lines", got["stdout"])
	}
	if strings.Join(got["stderr"], "|") != "err one|err two" {
		t.Fatalf("stderr process_output records = %#v, want clean lines", got["stderr"])
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
