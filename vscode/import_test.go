package vscode

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-23
func TestImportExternalRunDemotesHostileArtifactsToOutputWarnings(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "trace.zip")
	input := strings.NewReader(strings.Join([]string{
		`{"event":"run_started"}`,
		`{"event":"artifact","test_id":"go::root","artifact":{"name":"trace","uri":"https://example.invalid/trace.zip","kind":"trace"}}`,
		`{"event":"artifact","test_id":"go::root","artifact":{"name":"trace","uri":"` + (&url.URL{Scheme: "file", Path: outside}).String() + `","kind":"trace"}}`,
		`{"event":"run_finished","exit_code":0}`,
	}, "\n"))

	var events []RunEvent
	report := ImportExternalRun(workspace, input, nil, func(event RunEvent) {
		events = append(events, event)
	}, nil)
	if report.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", report.ExitCode)
	}
	if len(report.Warnings) != 2 {
		t.Fatalf("Warnings = %v, want scheme and outside-workspace warnings", report.Warnings)
	}
	var artifactCount, warningOutputCount int
	for _, event := range events {
		if event.Event == "artifact" {
			artifactCount++
		}
		if event.Event == "output" && strings.Contains(event.Message, "artifact.uri") {
			warningOutputCount++
		}
	}
	if artifactCount != 0 {
		t.Fatalf("hostile artifacts passed through: %+v", events)
	}
	if warningOutputCount != 2 {
		t.Fatalf("warning outputs = %d, want 2; events=%+v", warningOutputCount, events)
	}
}

// DHF-TEST: keel/requirement-23
func TestImportExternalRunContinuesPastOverCapLine(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"event":"run_started"}`,
		strings.Repeat("x", ImportMaxLineBytes+1),
		`{"event":"passed","test_id":"go::github.com/david-aggeler/keel/vscode::TestAfterOversized"}`,
		`{"event":"run_finished","exit_code":0}`,
	}, "\n"))

	var events []RunEvent
	report := ImportExternalRun(t.TempDir(), input, nil, func(event RunEvent) {
		events = append(events, event)
	}, nil)
	if report.TruncatedLines != 1 {
		t.Fatalf("TruncatedLines = %d, want 1", report.TruncatedLines)
	}
	if report.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 from the producer terminal after the oversized line", report.ExitCode)
	}
	var sawPassed bool
	for _, event := range events {
		if event.Event == "passed" && event.TestID == "go::github.com/david-aggeler/keel/vscode::TestAfterOversized" {
			sawPassed = true
		}
	}
	if !sawPassed {
		t.Fatalf("passed event after the oversized line was dropped: %+v", events)
	}
	if len(events) == 0 || events[len(events)-1].Event != "run_finished" {
		t.Fatalf("stream did not end with the producer run_finished: %+v", events)
	}
	last := events[len(events)-1]
	if last.ExitCode == nil || *last.ExitCode != 0 {
		t.Fatalf("terminal run_finished lost the producer exit code: %+v", last)
	}
}

// DHF-TEST: keel/requirement-23
func TestImportExternalRunAccountsForOverCapLine(t *testing.T) {
	tooLong := strings.NewReader(strings.Repeat("x", ImportMaxLineBytes+1))
	var events []RunEvent
	report := ImportExternalRun(t.TempDir(), tooLong, nil, func(event RunEvent) {
		events = append(events, event)
	}, nil)
	if report.TruncatedLines != 1 {
		t.Fatalf("TruncatedLines = %d, want 1", report.TruncatedLines)
	}
	if report.ExitCode == 0 {
		t.Fatal("truncated stream without producer terminal returned exit code 0")
	}
	if len(events) == 0 || events[len(events)-1].Event != "run_finished" {
		t.Fatalf("truncated stream did not get terminal run_finished: %+v", events)
	}
}
