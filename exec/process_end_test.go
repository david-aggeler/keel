package exec_test

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

// runLifecycle runs req with a JSON logger and returns the single process
// start and process end records it produced.
func runLifecycle(t *testing.T, req procexec.Request) (start, end map[string]any) {
	t.Helper()
	var logBuf bytes.Buffer
	req.Logger = mustLogger(t, logging.Config{
		Service:          "procexec-test",
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           &logBuf,
	})
	proc, err := procexec.ProcessStart(context.Background(), req)
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	_, _ = proc.Wait()
	records := parseJSONLogRecords(t, logBuf.String())
	starts := recordsWithEvent(records, "process_start")
	ends := recordsWithEvent(records, "process_end")
	if len(starts) != 1 || len(ends) != 1 {
		t.Fatalf("start/end records = %d/%d, want 1/1: %#v", len(starts), len(ends), records)
	}
	return starts[0], ends[0]
}

var exitPaths = []struct {
	name string
	exit string
}{
	{"success", "exit 0"},
	{"failure", "exit 5"},
}

// DHF-TEST: keel/requirement-183 (keel/ac-789)
func TestProcessEndRepeatsProgramCommandLineWorkingDirAndProcessID(t *testing.T) {
	dir := t.TempDir()
	for _, path := range exitPaths {
		t.Run(path.name, func(t *testing.T) {
			start, end := runLifecycle(t, procexec.Request{
				Program: "sh",
				Args:    []string{"-c", "echo line; " + path.exit, "arg-one", "arg two"},
				Dir:     dir,
			})
			for _, key := range []string{"program", "command_line", "working_dir", "process_id"} {
				if start[key] == nil {
					t.Fatalf("process_start has no %s: %#v", key, start)
				}
				if end[key] != start[key] {
					t.Errorf("process_end %s = %#v, want %#v (process_start value)", key, end[key], start[key])
				}
			}
			if end["working_dir"] != dir {
				t.Errorf("process_end working_dir = %#v, want %q", end["working_dir"], dir)
			}
		})
	}
}

// DHF-TEST: keel/requirement-183 (keel/ac-790)
func TestProcessEndCommandLineKeepsSensitiveArgumentsRedacted(t *testing.T) {
	const secret = "end-record-secret-value"
	var logBuf bytes.Buffer
	logger := mustLogger(t, logging.Config{
		ConsoleVerbosity: slog.LevelDebug,
		Console:          logging.ConsoleJSON,
		Writer:           &logBuf,
	})
	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Logger:        logger,
		Program:       "sh",
		Args:          []string{"-c", "echo done", secret},
		SensitiveArgs: map[int]bool{2: true},
	})
	if err != nil {
		t.Fatalf("ProcessStart: %v", err)
	}
	_, _ = proc.Wait()
	logs := logBuf.String()
	records := parseJSONLogRecords(t, logs)
	start := recordsWithEvent(records, "process_start")[0]
	end := recordsWithEvent(records, "process_end")[0]
	startLine, _ := start["command_line"].(string)
	endLine, ok := end["command_line"].(string)
	if !ok || endLine != startLine {
		t.Fatalf("process_end command_line = %#v, want byte-identical %q", end["command_line"], startLine)
	}
	if strings.Contains(logs, secret) {
		t.Fatalf("a record contains the sensitive argument:\n%s", logs)
	}
}

// DHF-TEST: keel/requirement-183 (keel/ac-791)
func TestProcessEndCarriesStartedAtAsRFC3339NanoUTC(t *testing.T) {
	for _, path := range exitPaths {
		t.Run(path.name, func(t *testing.T) {
			before := time.Now()
			_, end := runLifecycle(t, procexec.Request{Program: "sh", Args: []string{"-c", path.exit}})
			raw, ok := end["started_at"].(string)
			if !ok {
				t.Fatalf("process_end started_at = %#v, want a string", end["started_at"])
			}
			startedAt, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				t.Fatalf("started_at %q does not parse as RFC 3339 nano: %v", raw, err)
			}
			if !strings.HasSuffix(raw, "Z") {
				t.Fatalf("started_at %q, want the UTC offset Z", raw)
			}
			endTS, err := time.Parse(time.RFC3339Nano, end["ts"].(string))
			if err != nil {
				t.Fatalf("process_end ts %q: %v", end["ts"], err)
			}
			if startedAt.Before(before.Truncate(0)) || startedAt.After(endTS) {
				t.Fatalf("started_at %s outside [%s, %s]", startedAt, before, endTS)
			}
		})
	}
}

// startCoreFields are the keel/log envelope keys every record carries; they
// describe the record, not the process, and are excluded from the superset.
var startCoreFields = map[string]bool{"ts": true, "level": true, "msg": true, "service": true, "event_type": true}

// DHF-TEST: keel/requirement-183 (keel/ac-792)
func TestProcessEndAttributeSetContainsEveryProcessStartAttribute(t *testing.T) {
	for _, path := range exitPaths {
		t.Run(path.name, func(t *testing.T) {
			start, end := runLifecycle(t, procexec.Request{
				Program: "sh",
				Args:    []string{"-c", "echo out; " + path.exit},
				Dir:     t.TempDir(),
			})
			for key, want := range start {
				if startCoreFields[key] {
					continue
				}
				got, ok := end[key]
				if !ok {
					t.Errorf("process_end lacks process_start key %q", key)
					continue
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("process_end %s = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}
