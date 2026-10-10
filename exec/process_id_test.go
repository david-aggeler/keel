package exec_test

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"sync"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

// lockedBuffer serializes writes from concurrent ProcessStart calls that share
// one logger.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runDebugChild runs script under sh with a Debug-floor JSON logger, so every
// record of the child, per-line output included, reaches the parsed set.
func runDebugChild(t *testing.T, script string) []map[string]any {
	t.Helper()
	var logBuf bytes.Buffer
	logger := mustLogger(t, logging.Config{
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           &logBuf,
	})
	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:  logger,
		Program: "sh",
		Args:    []string{"-c", script},
	})
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	_, _ = proc.Wait()
	return parseJSONLogRecords(t, logBuf.String())
}

var processIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// DHF-TEST: keel/requirement-182 (keel/ac-784)
func TestProcessIDIsSixteenLowercaseHexCharacters(t *testing.T) {
	records := runDebugChild(t, "echo hello")
	starts := recordsWithEvent(records, "process_start")
	if len(starts) != 1 {
		t.Fatalf("process_start records = %#v, want one", starts)
	}
	id, _ := starts[0]["process_id"].(string)
	if !processIDPattern.MatchString(id) {
		t.Fatalf("process_start process_id = %#v, want a match for %s", starts[0]["process_id"], processIDPattern)
	}
}

// DHF-TEST: keel/requirement-182 (keel/ac-785)
func TestEveryProcessStartCallGetsADistinctProcessID(t *testing.T) {
	var logBuf lockedBuffer
	logger := mustLogger(t, logging.Config{Console: logging.ConsoleJSON, Writer: &logBuf})
	run := func() {
		proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
			Logger: logger, Program: "sh", Args: []string{"-c", "true"},
		})
		if err != nil {
			t.Errorf("ProcessStart: %v", err)
			return
		}
		_, _ = proc.Wait()
	}
	const sequential, concurrent = 8, 16
	for range sequential {
		run()
	}
	var wg sync.WaitGroup
	for range concurrent {
		wg.Go(run)
	}
	wg.Wait()

	starts := recordsWithEvent(parseJSONLogRecords(t, logBuf.String()), "process_start")
	if len(starts) != sequential+concurrent {
		t.Fatalf("process_start records = %d, want %d", len(starts), sequential+concurrent)
	}
	seen := map[string]bool{}
	for _, rec := range starts {
		id, _ := rec["process_id"].(string)
		if id == "" {
			t.Fatalf("process_start without process_id: %#v", rec)
		}
		if seen[id] {
			t.Fatalf("process_id %q repeated across ProcessStart calls", id)
		}
		seen[id] = true
	}
}

// assertOneProcessID fails unless every record carries the same non-empty
// process_id, and returns it.
func assertOneProcessID(t *testing.T, records []map[string]any) string {
	t.Helper()
	var want string
	for _, rec := range records {
		id, _ := rec["process_id"].(string)
		if id == "" {
			t.Fatalf("record %q has no process_id: %#v", rec["event_type"], rec)
		}
		if want == "" {
			want = id
		}
		if id != want {
			t.Fatalf("record %q process_id = %q, want %q (one id per child)", rec["event_type"], id, want)
		}
	}
	return want
}

// requireEvents fails unless records holds at least one record per event.
func requireEvents(t *testing.T, records []map[string]any, events ...string) {
	t.Helper()
	for _, event := range events {
		if len(recordsWithEvent(records, event)) == 0 {
			t.Fatalf("no %s record in %#v", event, records)
		}
	}
}

// DHF-TEST: keel/requirement-182 (keel/ac-786)
func TestEveryRecordOfASuccessfulChildCarriesTheSameProcessID(t *testing.T) {
	records := runDebugChild(t, "echo out-one; echo err-one 1>&2; echo out-two")
	requireEvents(t, records, "process_start", "process_output", "process_end")
	if n := len(recordsWithEvent(records, "process_output")); n != 3 {
		t.Fatalf("process_output records = %d, want 3", n)
	}
	assertOneProcessID(t, records)
}

// DHF-TEST: keel/requirement-182 (keel/ac-787)
func TestEveryRecordOfAFailedChildIncludingTheTailCarriesTheSameProcessID(t *testing.T) {
	records := runDebugChild(t, "echo out-one; echo err-one 1>&2; exit 4")
	requireEvents(t, records, "process_start", "process_output", "process_output_tail", "process_end")
	if n := len(recordsWithEvent(records, "process_output_tail")); n != 2 {
		t.Fatalf("process_output_tail records = %d, want 2", n)
	}
	assertOneProcessID(t, records)
}

// recordedCall is one record a leveled fake logger received through Log.
type recordedCall struct {
	level slog.Level
	msg   string
	attrs map[string]any
}

// levelRecorder is a processLogger with a leveled Log method. Its Debug and
// Error methods fail the test: keel/exec must route every leveled record
// through Log when the logger has it. Info and InfoContext carry the fixed-Info
// lifecycle records and are recorded at Info.
type levelRecorder struct {
	t     *testing.T
	mu    sync.Mutex
	calls []recordedCall
}

func (r *levelRecorder) Log(_ context.Context, level slog.Level, msg string, args ...any) {
	r.record(level, msg, args)
}

func (r *levelRecorder) InfoContext(_ context.Context, msg string, args ...any) {
	r.record(slog.LevelInfo, msg, args)
}

func (r *levelRecorder) Debug(msg string, _ ...any)   { r.t.Errorf("Debug(%q) bypassed Log", msg) }
func (r *levelRecorder) Info(msg string, args ...any) { r.record(slog.LevelInfo, msg, args) }
func (r *levelRecorder) Error(msg string, _ ...any)   { r.t.Errorf("Error(%q) bypassed Log", msg) }

func (r *levelRecorder) record(level slog.Level, msg string, args []any) {
	attrs := map[string]any{}
	for i := 0; i+1 < len(args); i += 2 {
		key, _ := args[i].(string)
		attrs[key] = args[i+1]
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCall{level: level, msg: msg, attrs: attrs})
}

// DHF-TEST: keel/requirement-182 (keel/ac-788)
func TestProcessIDStampingLeavesEveryRecordLevelUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name    string
		script  string
		failure slog.Leveler
		want    map[string]slog.Level
	}{
		{"success", "echo out; echo err 1>&2", nil, map[string]slog.Level{
			"process_start": slog.LevelInfo, "process_output": slog.LevelDebug, "process_end": slog.LevelInfo,
		}},
		{"failure", "echo out; exit 2", nil, map[string]slog.Level{
			"process_start": slog.LevelInfo, "process_output": slog.LevelDebug,
			"process_output_tail": slog.LevelError, "process_end": slog.LevelError,
		}},
		{"failure-level-override", "echo out; exit 2", slog.LevelWarn, map[string]slog.Level{
			"process_start": slog.LevelInfo, "process_output": slog.LevelDebug,
			"process_output_tail": slog.LevelWarn, "process_end": slog.LevelWarn,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &levelRecorder{t: t}
			proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
				Logger: rec, Program: "sh", Args: []string{"-c", tc.script}, FailureLevel: tc.failure,
			})
			if err != nil {
				t.Fatalf("ProcessStart: %v", err)
			}
			_, _ = proc.Wait()
			got := map[string]bool{}
			for _, call := range rec.calls {
				event, _ := call.attrs["event_type"].(string)
				want, ok := tc.want[event]
				if !ok {
					t.Fatalf("unexpected record %q: %#v", event, call)
				}
				if call.level != want {
					t.Errorf("%s level = %v, want %v", event, call.level, want)
				}
				if id, _ := call.attrs["process_id"].(string); !processIDPattern.MatchString(id) {
					t.Errorf("%s process_id = %#v, want a 16-hex id", event, call.attrs["process_id"])
				}
				got[event] = true
			}
			for event := range tc.want {
				if !got[event] {
					t.Errorf("no %s record received", event)
				}
			}
		})
	}
}
