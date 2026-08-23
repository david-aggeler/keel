package log_test

import (
	"bytes"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// consoleBody strips the leading "HH:MM:SS " timestamp from a single console
// line so the remainder — level tag, separator, body — can be compared byte for
// byte.
func consoleBody(t *testing.T, line string) string {
	t.Helper()
	const stamp = 8 // HH:MM:SS
	if len(line) < stamp+1 {
		t.Fatalf("console line too short to carry a timestamp: %q", line)
	}
	if !regexp.MustCompile(`^\d{2}:\d{2}:\d{2} `).MatchString(line) {
		t.Fatalf("console line does not start with HH:MM:SS and a space: %q", line)
	}
	return line[stamp+1:]
}

// textFileBody strips the leading "YYYY-MM-DD HH:MM:SS.mmm\t" timestamp field
// from a rolling-text-file line.
func textFileBody(t *testing.T, line string) string {
	t.Helper()
	idx := strings.IndexByte(line, '\t')
	if idx < 0 {
		t.Fatalf("text-file line carries no tab after the timestamp: %q", line)
	}
	return line[idx+1:]
}

// DHF-TEST: keel/requirement-148
// Covers keel/ac-611 (attrs-only console record) and keel/ac-613
// (message-bearing console line stays byte-identical).
func TestConsolePlainSeparatorIsOneGutterWithAndWithoutAMessage(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	newConsole := func(buf *bytes.Buffer) *logging.Logger {
		return mustNewLogger(t, logging.Config{
			Console:          logging.ConsolePlain,
			Service:          "cli",
			ConsoleVerbosity: slog.LevelDebug,
			Writer:           buf,
		})
	}

	var attrsOnly bytes.Buffer
	newConsole(&attrsOnly).Info("", "activity", "ci-extra-goconst", "target", "dtovalidate", "work", "started")
	got := consoleBody(t, strings.TrimSuffix(attrsOnly.String(), "\n"))
	const wantAttrsOnly = "INFO  service=cli activity=ci-extra-goconst target=dtovalidate work=started"
	if got != wantAttrsOnly {
		t.Fatalf("attrs-only ConsolePlain line:\n got %q\nwant %q", got, wantAttrsOnly)
	}

	var withMessage bytes.Buffer
	newConsole(&withMessage).Info("build identity", "version", "1.6.1.6505", "git_commit", "75f492758")
	got = consoleBody(t, strings.TrimSuffix(withMessage.String(), "\n"))
	const wantWithMessage = "INFO  build identity service=cli version=1.6.1.6505 git_commit=75f492758"
	if got != wantWithMessage {
		t.Fatalf("message-bearing ConsolePlain line:\n got %q\nwant %q", got, wantWithMessage)
	}
}

// DHF-TEST: keel/requirement-148
// Covers keel/ac-614 (attrs-only text-file line carries no space after the
// source tab) and the message-bearing counterpart on the same sink.
func TestHumanTextFileSeparatorIsOneTabWithAndWithoutAMessage(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	write := func(msg string, attrs ...any) string {
		t.Helper()
		dir := t.TempDir()
		var console bytes.Buffer
		logger := mustNewLogger(t, logging.Config{
			Console:          logging.ConsolePlain,
			Service:          "cli",
			ConsoleVerbosity: slog.LevelDebug,
			FileVerbosity:    slog.LevelDebug,
			Writer:           &console,
			TextDir:          dir,
		})
		logger.Info(msg, attrs...)
		if err := logger.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		body, err := os.ReadFile(logger.TextLogPath())
		if err != nil {
			t.Fatalf("read text log: %v", err)
		}
		lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
		return lines[len(lines)-1]
	}

	got := textFileBody(t, write("", "activity", "ci-extra-goconst", "work", "started"))
	wantAttrsOnly := "INFO\t" + padSource("cli") + "\tactivity=ci-extra-goconst work=started"
	if got != wantAttrsOnly {
		t.Fatalf("attrs-only text-file line:\n got %q\nwant %q", got, wantAttrsOnly)
	}

	got = textFileBody(t, write("build identity", "version", "1.6.1.6505"))
	wantWithMessage := "INFO\t" + padSource("cli") + "\tbuild identity version=1.6.1.6505"
	if got != wantWithMessage {
		t.Fatalf("message-bearing text-file line:\n got %q\nwant %q", got, wantWithMessage)
	}
}

// padSource mirrors the rolling-text-file handler's %-26s source column.
func padSource(source string) string {
	if len(source) >= 26 {
		return source
	}
	return source + strings.Repeat(" ", 26-len(source))
}

// DHF-TEST: keel/requirement-148
// Covers keel/ac-615 — the machine sinks assemble no text line, so an
// attrs-only record must reach them byte-unchanged.
func TestMachineSinksAreUnaffectedByTheSeparatorRule(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var sparse bytes.Buffer
	dir := t.TempDir()
	logger := mustNewLogger(t, logging.Config{
		Console:          logging.ConsoleSparseAI,
		Service:          "cli",
		ConsoleVerbosity: slog.LevelDebug,
		FileVerbosity:    slog.LevelDebug,
		Writer:           &sparse,
		JSONLDir:         dir,
	})
	logger.Info("", "activity", "ci-extra-goconst", "work", "started")
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sparseLine := strings.TrimSuffix(sparse.String(), "\n")
	const wantSparse = `{"level":"INFO","event":"log","message":"","fields":{"activity":"ci-extra-goconst","service":"cli","work":"started"}}`
	if sparseLine != wantSparse {
		t.Fatalf("sparse-AI console line:\n got %q\nwant %q", sparseLine, wantSparse)
	}

	jsonlBytes, err := os.ReadFile(logger.JSONLLogPath())
	if err != nil {
		t.Fatalf("read jsonl log: %v", err)
	}
	jsonlLine := normalizeJSONTime(strings.TrimSuffix(string(jsonlBytes), "\n"))
	const wantJSONL = `{"ts":"<t>","level":"INFO","msg":"","service":"cli","activity":"ci-extra-goconst","work":"started"}`
	if jsonlLine != wantJSONL {
		t.Fatalf("JSONL file-sink line:\n got %q\nwant %q", jsonlLine, wantJSONL)
	}
}

// jsonTimeField matches the JSONL sink's timestamp field so the varying instant
// can be normalized out of a byte-exact comparison.
var jsonTimeField = regexp.MustCompile(`"ts":"[^"]*"`)

func normalizeJSONTime(line string) string {
	return jsonTimeField.ReplaceAllString(line, `"ts":"<t>"`)
}
