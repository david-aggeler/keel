package exec_test

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

// runChild runs script under sh with the given logger and adapter, and returns
// the JSON console records it produced.
func runChild(t *testing.T, cfg logging.Config, classify func(stream, line string) procexec.LineClass, script string) []map[string]any {
	t.Helper()
	var logBuf bytes.Buffer
	cfg.Console = logging.ConsoleJSON
	cfg.Writer = &logBuf
	logger := mustLogger(t, cfg)
	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:   logger,
		Program:  "sh",
		Args:     []string{"-c", script},
		Classify: classify,
	})
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	_, _ = proc.Wait()
	return parseJSONLogRecords(t, logBuf.String())
}

func recordsWithEvent(records []map[string]any, event string) []map[string]any {
	var out []map[string]any
	for _, rec := range records {
		if rec["event_type"] == event {
			out = append(out, rec)
		}
	}
	return out
}

// DHF-TEST: keel/requirement-24
func TestChildOutputClassifiesAtOneConfiguredLevelOnBothStreamsWithStreamAttribute(t *testing.T) {
	// keel/ac-712: with no level set anywhere, both streams land at the one
	// default (Debug); neither stream earns a level of its own.
	records := runChild(t, logging.Config{ConsoleVerbosity: slog.LevelDebug}, nil,
		"echo out-line; echo err-line 1>&2")
	outputs := recordsWithEvent(records, "process_output")
	if len(outputs) != 2 {
		t.Fatalf("process_output records = %#v, want one per stream", outputs)
	}
	byData := map[string]map[string]any{}
	for _, rec := range outputs {
		byData[rec["data"].(string)] = rec
	}
	for data, stream := range map[string]string{"out-line": "stdout", "err-line": "stderr"} {
		rec, ok := byData[data]
		if !ok {
			t.Fatalf("no process_output record for %q in %#v", data, outputs)
		}
		if rec["level"] != "DEBUG" {
			t.Fatalf("%s record level = %#v, want DEBUG (the default child-output level)", stream, rec["level"])
		}
		if rec["stream"] != stream {
			t.Fatalf("record for %q stream = %#v, want %q", data, rec["stream"], stream)
		}
	}
}

// DHF-TEST: keel/requirement-24
func TestChildOutputLevelIsStreamIndependentForAPlainSlogLogger(t *testing.T) {
	// keel/ac-712 through a caller logger that carries no configured level: the
	// default still applies to both streams alike.
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:  logger,
		Program: "sh",
		Args:    []string{"-c", "echo a; echo b 1>&2"},
	})
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	for _, rec := range recordsWithEvent(parseJSONLogRecords(t, logBuf.String()), "process_output") {
		if rec["level"] != "DEBUG" {
			t.Fatalf("process_output %#v level = %#v, want DEBUG on both streams", rec["stream"], rec["level"])
		}
	}
}

// DHF-TEST: keel/requirement-24
func TestFailingChildEndsAtErrorWithOutputTailReplayedAtDefaultFloor(t *testing.T) {
	// keel/ac-713: the default console floor (zero ConsoleVerbosity) hides the
	// Debug per-line records, so the failure reason must reach it through the
	// Error END record and the replayed tail.
	records := runChild(t, logging.Config{}, nil,
		"echo out-before; echo err-reason 1>&2; exit 3")

	for _, rec := range recordsWithEvent(records, "process_output") {
		t.Fatalf("per-line process_output reached the default Info floor: %#v", rec)
	}
	ends := recordsWithEvent(records, "process_end")
	if len(ends) != 1 {
		t.Fatalf("process_end records = %#v, want exactly one", ends)
	}
	if ends[0]["level"] != "ERROR" {
		t.Fatalf("process_end level = %#v, want ERROR for exit 3", ends[0]["level"])
	}
	if got, _ := ends[0]["exit_code"].(float64); got != 3 {
		t.Fatalf("process_end exit_code = %#v, want 3", ends[0]["exit_code"])
	}

	tail := recordsWithEvent(records, "process_output_tail")
	got := map[string]string{}
	for _, rec := range tail {
		if rec["level"] != "ERROR" {
			t.Fatalf("tail record level = %#v, want ERROR (visible at the default floor)", rec["level"])
		}
		got[rec["data"].(string)], _ = rec["stream"].(string)
	}
	if got["out-before"] != "stdout" || got["err-reason"] != "stderr" {
		t.Fatalf("replayed tail = %v, want both lines with their stream", got)
	}
}

// DHF-TEST: keel/requirement-24
func TestSucceedingChildEndsAtInfoWithoutTailReplay(t *testing.T) {
	// keel/ac-713 control: only the exit status promotes; a clean exit that
	// wrote to stderr stays quiet.
	records := runChild(t, logging.Config{ConsoleVerbosity: slog.LevelDebug}, nil,
		"echo chatter 1>&2; exit 0")
	ends := recordsWithEvent(records, "process_end")
	if len(ends) != 1 || ends[0]["level"] != "INFO" {
		t.Fatalf("process_end = %#v, want one INFO record for exit 0", ends)
	}
	if tail := recordsWithEvent(records, "process_output_tail"); len(tail) != 0 {
		t.Fatalf("tail replayed for a successful child: %#v", tail)
	}
	for _, rec := range records {
		if rec["level"] == "ERROR" {
			t.Fatalf("successful child produced an ERROR record: %#v", rec)
		}
	}
}

// DHF-TEST: keel/requirement-24
func TestFailureTailKeepsOnlyTheLastLines(t *testing.T) {
	records := runChild(t, logging.Config{}, nil,
		"i=0; while [ $i -lt 100 ]; do echo line-$i; i=$((i+1)); done; exit 1")
	tail := recordsWithEvent(records, "process_output_tail")
	if len(tail) != procexec.FailureTailLines {
		t.Fatalf("tail records = %d, want %d", len(tail), procexec.FailureTailLines)
	}
	if last := tail[len(tail)-1]["data"]; last != "line-99" {
		t.Fatalf("last tail record = %#v, want line-99", last)
	}
	if first := tail[0]["data"]; first != "line-"+strconv.Itoa(100-procexec.FailureTailLines) {
		t.Fatalf("first tail record = %#v, want the oldest retained line", first)
	}
}

// levelTokenAdapter is a per-process-type adapter for a child whose format is
// "LEVEL=<slog level> <message>".
func levelTokenAdapter(_ string, line string) procexec.LineClass {
	token, _, ok := strings.Cut(line, " ")
	if !ok || !strings.HasPrefix(token, "LEVEL=") {
		return procexec.LineClass{}
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.TrimPrefix(token, "LEVEL="))); err != nil {
		return procexec.LineClass{}
	}
	return procexec.LineClass{Declared: level}
}

// DHF-TEST: keel/requirement-24
func TestAdapterDeclaredLevelIsCarriedInItsOwnFieldAndAbsentWhenUndeclared(t *testing.T) {
	// keel/ac-714.
	records := runChild(t, logging.Config{ConsoleVerbosity: slog.LevelDebug}, levelTokenAdapter,
		"echo 'LEVEL=WARN disk nearly full' 1>&2; echo 'no level here' 1>&2")
	byData := map[string]map[string]any{}
	for _, rec := range recordsWithEvent(records, "process_output") {
		byData[rec["data"].(string)] = rec
	}
	declared, ok := byData["LEVEL=WARN disk nearly full"]
	if !ok {
		t.Fatalf("no record for the declaring line: %#v", byData)
	}
	if declared["declared_level"] != "WARN" {
		t.Fatalf("declared_level = %#v, want WARN", declared["declared_level"])
	}
	undeclared, ok := byData["no level here"]
	if !ok {
		t.Fatalf("no record for the line without a level: %#v", byData)
	}
	if v, present := undeclared["declared_level"]; present {
		t.Fatalf("declared_level = %#v on a line without a level, want the field absent", v)
	}
	// The declared level is carried, never promoted into the record's severity.
	if declared["level"] != "DEBUG" || undeclared["level"] != "DEBUG" {
		t.Fatalf("record levels = %#v/%#v, want both at the configured DEBUG", declared["level"], undeclared["level"])
	}
}

// DHF-TEST: keel/requirement-24
func TestDeclaredLevelIsAbsentWithoutAnAdapter(t *testing.T) {
	records := runChild(t, logging.Config{ConsoleVerbosity: slog.LevelDebug}, nil,
		"echo 'LEVEL=ERROR looks declared' 1>&2")
	for _, rec := range recordsWithEvent(records, "process_output") {
		if v, present := rec["declared_level"]; present {
			t.Fatalf("declared_level = %#v with no adapter, want absent: core keel/exec must not infer", v)
		}
	}
}

// DHF-TEST: keel/requirement-24
func TestFailureTailCarriesDeclaredLevel(t *testing.T) {
	records := runChild(t, logging.Config{}, levelTokenAdapter,
		"echo 'LEVEL=ERROR boom' 1>&2; echo plain 1>&2; exit 2")
	byData := map[string]map[string]any{}
	for _, rec := range recordsWithEvent(records, "process_output_tail") {
		byData[rec["data"].(string)] = rec
	}
	if byData["LEVEL=ERROR boom"]["declared_level"] != "ERROR" {
		t.Fatalf("tail record for declaring line = %#v, want declared_level ERROR", byData["LEVEL=ERROR boom"])
	}
	if _, present := byData["plain"]["declared_level"]; present {
		t.Fatalf("tail record for plain line = %#v, want declared_level absent", byData["plain"])
	}
}

// DHF-TEST: keel/requirement-24
func TestAdapterLevelOverridesConfiguredLevelPerProcessType(t *testing.T) {
	// keel/requirement-24: a caller that knows better overrides per process type.
	override := func(stream, line string) procexec.LineClass {
		if stream == "stderr" && line == "known-bad" {
			return procexec.LineClass{Level: slog.LevelWarn}
		}
		return procexec.LineClass{}
	}
	records := runChild(t, logging.Config{ConsoleVerbosity: slog.LevelDebug, ChildOutputLevel: slog.LevelInfo}, override,
		"echo known-bad 1>&2; echo other 1>&2")
	levels := map[string]any{}
	for _, rec := range recordsWithEvent(records, "process_output") {
		levels[rec["data"].(string)] = rec["level"]
	}
	if levels["known-bad"] != "WARN" || levels["other"] != "INFO" {
		t.Fatalf("levels = %v, want known-bad WARN (override) and other INFO (configured)", levels)
	}
}

// DHF-TEST: keel/requirement-24
func TestFailureLevelOverridesTheNonZeroExitSeverityPerProcessType(t *testing.T) {
	// A caller whose child answers with its exit status (a probe) states the
	// severity that answer deserves; nothing is promoted to Error.
	var logBuf bytes.Buffer
	logger := mustLogger(t, logging.Config{Console: logging.ConsoleJSON, ConsoleVerbosity: slog.LevelDebug, Writer: &logBuf})
	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:       logger,
		Program:      "sh",
		Args:         []string{"-c", "echo not-found 1>&2; exit 1"},
		FailureLevel: slog.LevelDebug,
	})
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	_, _ = proc.Wait()
	records := parseJSONLogRecords(t, logBuf.String())
	for _, rec := range records {
		if rec["level"] == "ERROR" {
			t.Fatalf("record at ERROR despite FailureLevel Debug: %#v", rec)
		}
	}
	ends := recordsWithEvent(records, "process_end")
	if len(ends) != 1 || ends[0]["level"] != "DEBUG" {
		t.Fatalf("process_end = %#v, want one DEBUG record", ends)
	}
	if got, _ := ends[0]["exit_code"].(float64); got != 1 {
		t.Fatalf("process_end exit_code = %#v, want 1", ends[0]["exit_code"])
	}
	if tail := recordsWithEvent(records, "process_output_tail"); len(tail) != 1 || tail[0]["level"] != "DEBUG" {
		t.Fatalf("tail = %#v, want the one line at DEBUG", tail)
	}
}

// warnErrorAdapter raises the line "warn-line" to Warn and "error-line" to
// Error and leaves every other line unclassified.
func warnErrorAdapter(_ string, line string) procexec.LineClass {
	switch line {
	case "warn-line":
		return procexec.LineClass{Level: slog.LevelWarn}
	case "error-line":
		return procexec.LineClass{Level: slog.LevelError}
	}
	return procexec.LineClass{}
}

// DHF-TEST: keel/requirement-171, keel/ac-734
func TestChildOutputAtWarnOrHigherNamesItsProgramAndLowerLinesDoNot(t *testing.T) {
	records := runChild(t, logging.Config{ConsoleVerbosity: slog.LevelDebug}, warnErrorAdapter,
		"echo warn-line 1>&2; echo error-line 1>&2; echo plain-line")
	byData := map[string]map[string]any{}
	for _, rec := range recordsWithEvent(records, "process_output") {
		byData[rec["data"].(string)] = rec
	}
	for data, level := range map[string]string{"warn-line": "WARN", "error-line": "ERROR"} {
		rec, ok := byData[data]
		if !ok {
			t.Fatalf("no process_output record for %q in %#v", data, byData)
		}
		if rec["level"] != level {
			t.Fatalf("%q level = %#v, want %s", data, rec["level"], level)
		}
		if got, present := rec["program"]; !present || got != "sh" {
			t.Fatalf("%q record program = %#v (present %v), want %q", data, got, present, "sh")
		}
	}
	plain, ok := byData["plain-line"]
	if !ok {
		t.Fatalf("no process_output record for the unclassified line in %#v", byData)
	}
	if plain["level"] != "DEBUG" {
		t.Fatalf("unclassified line level = %#v, want DEBUG", plain["level"])
	}
	if v, present := plain["program"]; present {
		t.Fatalf("unclassified Debug line carries program = %#v, want the key absent", v)
	}
}

// DHF-TEST: keel/requirement-171, keel/ac-734
func TestChildOutputAtConfiguredWarnLevelNamesItsProgram(t *testing.T) {
	// The tag follows the one resolved level: a consumer-configured child-output
	// level of Warn (keel/ac-711) tags an unclassified line, and an adapter that
	// lowers a line to Info leaves that line untagged.
	lower := func(_ string, line string) procexec.LineClass {
		if line == "info-line" {
			return procexec.LineClass{Level: slog.LevelInfo}
		}
		return procexec.LineClass{}
	}
	records := runChild(t, logging.Config{ConsoleVerbosity: slog.LevelDebug, ChildOutputLevel: slog.LevelWarn}, lower,
		"echo plain-line; echo info-line")
	byData := map[string]map[string]any{}
	for _, rec := range recordsWithEvent(records, "process_output") {
		byData[rec["data"].(string)] = rec
	}
	plain := byData["plain-line"]
	if plain["level"] != "WARN" || plain["program"] != "sh" {
		t.Fatalf("unclassified line at configured Warn = %#v, want level WARN and program sh", plain)
	}
	info := byData["info-line"]
	if info["level"] != "INFO" {
		t.Fatalf("lowered line level = %#v, want INFO", info["level"])
	}
	if v, present := info["program"]; present {
		t.Fatalf("Info line carries program = %#v, want the key absent", v)
	}
}

// runFailingChild runs script under sh with the given failure level (nil keeps
// the default) and returns its JSON console records.
func runFailingChild(t *testing.T, failure slog.Leveler, script string) []map[string]any {
	t.Helper()
	var logBuf bytes.Buffer
	logger := mustLogger(t, logging.Config{Console: logging.ConsoleJSON, ConsoleVerbosity: slog.LevelDebug, Writer: &logBuf})
	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:       logger,
		Program:      "sh",
		Args:         []string{"-c", script},
		FailureLevel: failure,
	})
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	_, _ = proc.Wait()
	return parseJSONLogRecords(t, logBuf.String())
}

// DHF-TEST: keel/requirement-171, keel/ac-734
func TestFailureTailAtDefaultLevelNamesItsProgram(t *testing.T) {
	// Under --quiet the Info process start record is hidden, so each Error tail
	// record must name the child by itself.
	tail := recordsWithEvent(runFailingChild(t, nil, "echo out-before; echo err-reason 1>&2; exit 3"), "process_output_tail")
	if len(tail) != 2 {
		t.Fatalf("tail records = %#v, want two", tail)
	}
	for _, rec := range tail {
		if rec["level"] != "ERROR" {
			t.Fatalf("tail record level = %#v, want ERROR", rec["level"])
		}
		if got, present := rec["program"]; !present || got != "sh" {
			t.Fatalf("tail record %#v program = %#v (present %v), want %q", rec["data"], got, present, "sh")
		}
	}
}

// DHF-TEST: keel/requirement-171, keel/ac-734
func TestFailureTailBelowWarnDoesNotNameItsProgram(t *testing.T) {
	tail := recordsWithEvent(runFailingChild(t, slog.LevelInfo, "echo not-found 1>&2; exit 1"), "process_output_tail")
	if len(tail) != 1 || tail[0]["level"] != "INFO" {
		t.Fatalf("tail = %#v, want the one line at INFO", tail)
	}
	if v, present := tail[0]["program"]; present {
		t.Fatalf("Info tail record carries program = %#v, want the key absent", v)
	}
}

// DHF-TEST: keel/requirement-171, keel/ac-733
func TestSuccessfulChildLogsStartAndEndAtInfoWithOutputAtDebugInBetween(t *testing.T) {
	records := runChild(t, logging.Config{ConsoleVerbosity: slog.LevelDebug}, nil,
		"echo out-line; echo err-line 1>&2; exit 0")
	type step struct{ event, level string }
	var got []step
	for _, rec := range records {
		ev, _ := rec["event_type"].(string)
		lvl, _ := rec["level"].(string)
		got = append(got, step{ev, lvl})
	}
	if len(got) != 4 {
		t.Fatalf("records = %#v, want start, two output lines, end", records)
	}
	if got[0] != (step{"process_start", "INFO"}) {
		t.Fatalf("first record = %+v, want process_start at INFO", got[0])
	}
	for _, s := range got[1:3] {
		if s != (step{"process_output", "DEBUG"}) {
			t.Fatalf("middle record = %+v, want process_output at DEBUG", s)
		}
	}
	if got[3] != (step{"process_end", "INFO"}) {
		t.Fatalf("last record = %+v, want process_end at INFO", got[3])
	}
}
