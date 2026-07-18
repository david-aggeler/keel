package vscode

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// DHF-TEST: keel/requirement-23
func TestVitestReportResultMapping(t *testing.T) {
	var events []RunEvent
	write := func(event RunEvent) {
		events = append(events, event)
	}
	EmitVitestReportResult("vitest::test::web/src/lib/thing.test.ts::fails", VitestJSONReport{
		Success:        false,
		NumTotalTests:  1,
		NumFailedTests: 1,
		TestResults: []VitestTestResult{{
			AssertionResults: []VitestAssertionResult{{
				FullName:        "thing fails",
				Status:          "failed",
				FailureMessages: []string{"expected true to be false"},
			}},
		}},
	}, write, time.Now())

	failed := assertRunEvent(t, events, "failed", "vitest::test::web/src/lib/thing.test.ts::fails")
	if !strings.Contains(failed.Message, "expected true") {
		t.Fatalf("failed message = %q", failed.Message)
	}

	file, ok := ParseVitestItemID("vitest::test::web/src/lib/thing.test.ts::fails")
	if !ok || file != "web/src/lib/thing.test.ts" {
		t.Fatalf("parse vitest id = %q, %v", file, ok)
	}
	file, ok = ParseVitestItemID("vitest::root")
	if !ok || file != "" {
		t.Fatalf("parse vitest root = %q, %v", file, ok)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-23
func TestVitestFailureMessageFallsBackThroughSpecificSources(t *testing.T) {
	tests := []struct {
		name   string
		report VitestJSONReport
		want   string
	}{
		{
			name: "failed assertion full name",
			report: VitestJSONReport{TestResults: []VitestTestResult{{
				AssertionResults: []VitestAssertionResult{{Status: "failed", FullName: "suite fails clearly"}},
			}}},
			want: "suite fails clearly",
		},
		{
			name: "file message",
			report: VitestJSONReport{TestResults: []VitestTestResult{{
				Message:          "file load failed",
				AssertionResults: []VitestAssertionResult{{Status: "passed", FullName: "ignored"}},
			}}},
			want: "file load failed",
		},
		{
			name:   "generic",
			report: VitestJSONReport{},
			want:   "Vitest failed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := VitestFailureMessage(tc.report); got != tc.want {
				t.Fatalf("VitestFailureMessage = %q, want %q", got, tc.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-23
func TestVitestReportResultSkippedAndPassedBranches(t *testing.T) {
	for _, tc := range []struct {
		name   string
		id     string
		report VitestJSONReport
		event  string
	}{
		{name: "skipped", id: "vitest::file::pending.test.ts", report: VitestJSONReport{NumTotalTests: 2, NumPendingTests: 2}, event: "skipped"},
		{name: "passed", id: "vitest::file::ok.test.ts", report: VitestJSONReport{Success: true, NumTotalTests: 1}, event: "passed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []RunEvent
			EmitVitestReportResult(tc.id, tc.report, func(event RunEvent) {
				events = append(events, event)
			}, time.Now())
			assertRunEvent(t, events, tc.event, tc.id)
		})
	}
}

// DHF-TEST: keel/requirement-23
func TestVitestReportDetailsMapToFileSuiteAndTestItems(t *testing.T) {
	repo := t.TempDir()
	var events []RunEvent
	write := func(event RunEvent) {
		events = append(events, event)
	}
	EmitVitestReportDetails(repo, VitestJSONReport{
		Success:       true,
		NumTotalTests: 1,
		TestResults: []VitestTestResult{{
			Name:   filepath.Join(repo, "web", "src", "lib", "thing.test.ts"),
			Status: "passed",
			AssertionResults: []VitestAssertionResult{{
				AncestorTitles: []string{"thing"},
				Status:         "passed",
				Title:          "works",
				Duration:       17,
			}},
		}},
	}, write)

	assertRunEvent(t, events, "passed", "vitest::file::web/src/lib/thing.test.ts")
	suite := assertRunEvent(t, events, "passed", "vitest::suite::web/src/lib/thing.test.ts::thing")
	if suite.DurationMS != 17 {
		t.Fatalf("suite duration = %d", suite.DurationMS)
	}
	test := assertRunEvent(t, events, "passed", "vitest::test::web/src/lib/thing.test.ts::works")
	if test.DurationMS != 17 {
		t.Fatalf("test duration = %d", test.DurationMS)
	}
}
