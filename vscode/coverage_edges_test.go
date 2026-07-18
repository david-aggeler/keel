package vscode

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// DHF-TEST: keel/requirement-23, keel/requirement-34
func TestEventStamperValidEventAndInvalidValues(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	stamper := EventStamper{
		Now:       func() time.Time { return now },
		RunID:     "run-1",
		Source:    "vscode",
		Workspace: "cr-38",
	}
	got := stamper.Stamp(RunEvent{Event: "passed", TestID: "go::root", DurationMS: 7})
	if got.Version != 1 || !got.Time.Equal(now) || got.RunID != "run-1" || got.Source != "vscode" || got.Workspace != "cr-38" {
		t.Fatalf("stamped event missing invariant fields: %+v", got)
	}

	for _, tc := range []RunEvent{
		{Event: "passed", Artifact: &RunArtifact{Name: "trace", URI: "file:///tmp/trace.zip", Kind: "bogus"}},
		{Event: "not-a-real-event"},
	} {
		stamped := stamper.Stamp(tc)
		if stamped.Event != "output" {
			t.Fatalf("invalid event was not demoted to output: in=%+v out=%+v", tc, stamped)
		}
	}
	if stamped := (EventStamper{Now: func() time.Time { return now }, Source: "bad-source"}).Stamp(RunEvent{Event: "passed"}); stamped.Event != "output" || stamped.Source != "vscode" {
		t.Fatalf("invalid configured source was not demoted to schema-valid output: %+v", stamped)
	}
}

// DHF-TEST: keel/requirement-23
func TestGoSelectionFiltersAndResultIDs(t *testing.T) {
	fileSelection := GoSelection{Kind: "file", Pkg: "pkg", TestNames: []string{"TestA", "TestB"}}
	if !OutputBelongsToGoSelection(fileSelection, GoTestJSONEvent{Test: "TestA"}) {
		t.Fatal("file selection should include selected test output")
	}
	if OutputBelongsToGoSelection(fileSelection, GoTestJSONEvent{Test: "TestC"}) {
		t.Fatal("file selection should exclude unselected test output")
	}
	if !GoJSONResultBelongsToSelection(fileSelection, GoTestJSONEvent{Test: "TestB"}) {
		t.Fatal("file selection should include selected test result")
	}
	if !GoJSONResultBelongsToSelection(GoSelection{Kind: "root"}, GoTestJSONEvent{Test: "TestA"}) {
		t.Fatal("root selection should own leaf test results so they settle per id (requirement-71)")
	}

	id := GoRunEventTestID(GoSelection{Kind: "package", Pkg: "pkg"}, GoTestJSONEvent{Package: "example.org/mod/pkg"}, "go::pkg::pkg", "example.org/mod")
	if id != "go::pkg::pkg" {
		t.Fatalf("package aggregate id = %q", id)
	}
}

// DHF-TEST: keel/requirement-23
func TestSharedProjectorHelpersCoverFallbacks(t *testing.T) {
	if StatusEventName("pending") != "skipped" || StatusEventName("weird") != "failed" {
		t.Fatal("status fallback mapping drifted")
	}
	if MergedStatusEvent("skipped", "passed") != "passed" || MergedStatusEvent("passed", "skipped") != "passed" || MergedStatusEvent("errored", "failed") != "failed" {
		t.Fatal("merged status severity drifted")
	}
	if FloatDurationMillis(0) != 0 || FloatDurationMillis(1.2) != 2 {
		t.Fatal("duration rounding drifted")
	}
	if IsTerminalRunEvent("output") || !IsTerminalRunEvent("errored") {
		t.Fatal("terminal event classification drifted")
	}
	if !StringInSlice([]string{"a", "b"}, "b") || StringInSlice([]string{"a"}, "b") {
		t.Fatal("StringInSlice result drifted")
	}
	if StableTitleSlug("!!!") != "unnamed" {
		t.Fatal("empty title slug fallback drifted")
	}
}

// DHF-TEST: keel/requirement-34
func TestSchemaBytesRejectsUnknownAndMarshalJSONL(t *testing.T) {
	if _, err := SchemaBytes("unknown"); err == nil {
		t.Fatal("unknown schema name should fail")
	}
	line, err := MarshalRunEventJSONL(RunEvent{Version: 1, Event: "run_finished"})
	if err != nil {
		t.Fatalf("MarshalRunEventJSONL returned error: %v", err)
	}
	if !strings.HasSuffix(string(line), "\n") {
		t.Fatalf("JSONL line missing newline: %q", line)
	}
	var decoded RunEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(line))), &decoded); err != nil {
		t.Fatalf("JSONL line is not valid JSON: %v", err)
	}
}
