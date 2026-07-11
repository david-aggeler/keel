package vscode_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/vscode"
)

// DHF-TEST: keel/requirement-23
func TestRunEventProtocolIsConsumerNeutralAndTerminalGuaranteed(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		"starting external producer",
		`{"event":"run_started","run_id":"run-1"}`,
		`{"event":"heartbeat","message":"tick"}`,
	}, "\n"))

	var events []vscode.RunEvent
	var logs []string
	exit := vscode.NormalizeRunEvents(input, errors.New("signal: killed"), func(event vscode.RunEvent) {
		events = append(events, event)
	}, func(message string) {
		logs = append(logs, message)
	})
	if exit == 0 {
		t.Fatal("producer crash without a terminal event returned exit code 0")
	}
	if len(events) == 0 || events[len(events)-1].Event != "run_finished" {
		t.Fatalf("stream did not end with run_finished: %+v", events)
	}

	var sawLeadingNoise, sawUnknownAsOutput bool
	for _, event := range events {
		if event.Event == "heartbeat" {
			t.Fatal("unknown producer event passed through instead of rendering as output")
		}
		if event.Event == "output" && strings.Contains(event.Message, "starting external producer") {
			sawLeadingNoise = true
		}
		if event.Event == "output" && strings.Contains(event.Message, "heartbeat") {
			sawUnknownAsOutput = true
		}
	}
	if !sawLeadingNoise {
		t.Fatal("leading non-JSON producer output was not rendered as output")
	}
	if !sawUnknownAsOutput {
		t.Fatal("unknown producer event was not rendered as output")
	}
	if strings.Join(logs, "\n") == "" || !strings.Contains(strings.Join(logs, "\n"), "heartbeat") {
		t.Fatalf("unknown producer event was not logged: %q", logs)
	}
}

// DHF-TEST: keel/requirement-23
func TestRunAttributionStampsRunAndWorkspace(t *testing.T) {
	attr := vscode.RunAttribution{
		ConsumerID: "vscode",
		RunID:      "run-42",
		Node:       "cr-38",
	}
	if !attr.Complete() {
		t.Fatalf("attribution triple is incomplete: %+v", attr)
	}
	stamped := attr.Stamp(vscode.RunEvent{Event: "passed", TestID: "go::root"})
	if stamped.RunID != "run-42" {
		t.Fatalf("RunID = %q, want run-42", stamped.RunID)
	}
	if stamped.Workspace != "cr-38" {
		t.Fatalf("Workspace = %q, want cr-38", stamped.Workspace)
	}
	if line := attr.LogLine(); !strings.Contains(line, "vscode") || !strings.Contains(line, "run-42") || !strings.Contains(line, "cr-38") {
		t.Fatalf("attribution log line misses part of the triple: %q", line)
	}
}
