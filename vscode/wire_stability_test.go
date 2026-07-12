package vscode

import (
	"encoding/json"
	"testing"
	"time"
)

// TestRunEventWireByteStability is the byte-stability conformance guard for the
// neutral run-event JSONL wire (keel/requirement-23, keel/requirement-23):
// the field tags and omitempty behavior of RunEvent are the contract the VS
// Code extension and headless tests decode, and they must stay byte-for-byte
// stable across every consumer of the extracted engine. Each case pins the
// exact JSON encoding of a representative event; together they exercise every
// wire field. A change to any tag, field order, or omitempty rule breaks a
// golden here before it can reach a consumer.
//
// DHF-TEST: keel/requirement-23
func TestRunEventWireByteStability(t *testing.T) {
	fixed := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	live := true
	exitZero := 0
	exitOne := 1

	cases := []struct {
		name  string
		event RunEvent
		want  string
	}{
		{
			name:  "run_started",
			event: RunEvent{Version: 1, Event: "run_started", Time: fixed, RunID: "20260711T120000Z", Source: "vscode", Requested: []RunRequest{{ID: "keel::lane::test-fast", Label: "test-fast"}}, Message: "VS Code test run started"},
			want:  `{"version":1,"event":"run_started","time":"2026-07-11T12:00:00Z","run_id":"20260711T120000Z","source":"vscode","requested":[{"id":"keel::lane::test-fast","label":"test-fast"}],"message":"VS Code test run started"}`,
		},
		{
			name:  "test_started",
			event: RunEvent{Version: 1, Event: "test_started", Time: fixed, Workspace: "cr-8", Live: &live, TestID: "keel::lane::test-fast"},
			want:  `{"version":1,"event":"test_started","time":"2026-07-11T12:00:00Z","workspace":"cr-8","live":true,"test_id":"keel::lane::test-fast"}`,
		},
		{
			name:  "passed",
			event: RunEvent{Version: 1, Event: "passed", Time: fixed, TestID: "go::root", DurationMS: 1200},
			want:  `{"version":1,"event":"passed","time":"2026-07-11T12:00:00Z","test_id":"go::root","duration_ms":1200}`,
		},
		{
			name:  "failed_with_location",
			event: RunEvent{Version: 1, Event: "failed", Time: fixed, TestID: "go::test::pkg::TestX", Message: "boom", DurationMS: 5, Location: &RunLocation{URI: "file:///x_test.go", Line: 41, Column: 2}},
			want:  `{"version":1,"event":"failed","time":"2026-07-11T12:00:00Z","test_id":"go::test::pkg::TestX","message":"boom","duration_ms":5,"location":{"uri":"file:///x_test.go","line":41,"column":2}}`,
		},
		{
			name:  "artifact",
			event: RunEvent{Version: 1, Event: "artifact", Time: fixed, TestID: "keel::lane::e2e-mock", Artifact: &RunArtifact{Name: "trace", URI: "file:///trace.zip", Kind: "report"}},
			want:  `{"version":1,"event":"artifact","time":"2026-07-11T12:00:00Z","test_id":"keel::lane::e2e-mock","artifact":{"name":"trace","uri":"file:///trace.zip","kind":"report"}}`,
		},
		{
			name:  "run_finished_zero",
			event: RunEvent{Version: 1, Event: "run_finished", Time: fixed, ExitCode: &exitZero},
			want:  `{"version":1,"event":"run_finished","time":"2026-07-11T12:00:00Z","exit_code":0}`,
		},
		{
			name:  "run_finished_nonzero",
			event: RunEvent{Version: 1, Event: "run_finished", Time: fixed, Message: "lane blocked", ExitCode: &exitOne},
			want:  `{"version":1,"event":"run_finished","time":"2026-07-11T12:00:00Z","message":"lane blocked","exit_code":1}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.event)
			if err != nil {
				t.Fatalf("marshal %s: %v", tc.name, err)
			}
			if string(got) != tc.want {
				t.Errorf("run-event wire drift for %s:\n got: %s\nwant: %s", tc.name, got, tc.want)
			}
		})
	}
}

// TestRunLockWireByteStability pins the run-lock file wire format, the second
// half of the byte-stable contract shared with the extension.
//
// DHF-TEST: keel/requirement-23
func TestRunLockWireByteStability(t *testing.T) {
	lock := RunLockFile{PID: 4242, CreatedAt: "2026-07-11T12:00:00Z", IDs: []string{"keel::lane::test-fast"}, Token: "20260711T120000Z"}
	want := `{"pid":4242,"created_at":"2026-07-11T12:00:00Z","ids":["keel::lane::test-fast"],"token":"20260711T120000Z"}`
	got, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("marshal run-lock: %v", err)
	}
	if string(got) != want {
		t.Errorf("run-lock wire drift:\n got: %s\nwant: %s", got, want)
	}
}
