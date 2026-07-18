package vscode

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// DHF-TEST: keel/requirement-23
func TestPlaywrightReportResultMapping(t *testing.T) {
	repo := t.TempDir()
	var events []RunEvent
	write := func(event RunEvent) {
		events = append(events, event)
	}
	EmitPlaywrightReportResult(repo, "playwright::test::e2e-mock::web/tests/e2e/login.spec.ts::logs-in", "e2e-mock", PlaywrightJSONReport{
		Stats: PlaywrightStats{Unexpected: 1},
		Suites: []PlaywrightSuite{{
			Specs: []PlaywrightSpec{{
				Title: "logs in",
				Tests: []PlaywrightTest{{
					ProjectName: "e2e-mock",
					Status:      "unexpected",
					Results: []PlaywrightResult{{
						Status:   "failed",
						Duration: 123,
						Error: &PlaywrightError{
							Message: "expected dashboard",
							Location: &PlaywrightLocation{
								File:   "web/tests/e2e/login.spec.ts",
								Line:   17,
								Column: 5,
							},
						},
						Attachments: []PlaywrightAttachment{
							{Name: "trace", Path: "/tmp/trace.zip", ContentType: "application/zip"},
							{Name: "screenshot", Path: "/tmp/failure.png", ContentType: "image/png"},
						},
					}},
				}},
			}},
		}},
	}, nil, write, time.Now())

	trace := assertRunEventWithArtifactName(t, events, "playwright::test::e2e-mock::web/tests/e2e/login.spec.ts::logs-in", "trace")
	if trace.Artifact == nil || trace.Artifact.Kind != "trace" {
		t.Fatalf("trace artifact = %#v", trace)
	}
	screenshot := assertRunEventWithArtifactName(t, events, "playwright::test::e2e-mock::web/tests/e2e/login.spec.ts::logs-in", "screenshot")
	if screenshot.Artifact == nil || screenshot.Artifact.Kind != "screenshot" {
		t.Fatalf("screenshot artifact = %#v", screenshot)
	}
	failed := assertRunEvent(t, events, "failed", "playwright::test::e2e-mock::web/tests/e2e/login.spec.ts::logs-in")
	if !strings.Contains(failed.Message, "expected dashboard") || failed.Location == nil || failed.Location.Line != 16 {
		t.Fatalf("failed event = %#v", failed)
	}
	if failed.Location.URI != filepath.Join(repo, "web", "tests", "e2e", "login.spec.ts") {
		t.Fatalf("failed uri = %q", failed.Location.URI)
	}

	selection, ok := ParsePlaywrightItemID("playwright::test::e2e-mock::web/tests/e2e/login.spec.ts::logs-in")
	if !ok || selection.Project != "e2e-mock" || selection.File != "web/tests/e2e/login.spec.ts" {
		t.Fatalf("selection = %#v, %v", selection, ok)
	}
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightReportResultFailsOnTopLevelErrors(t *testing.T) {
	repo := t.TempDir()
	var events []RunEvent
	write := func(event RunEvent) {
		events = append(events, event)
	}
	EmitPlaywrightReportResult(repo, "playwright::project::e2e-mock", "e2e-mock", PlaywrightJSONReport{
		Stats: PlaywrightStats{Expected: 0, Unexpected: 0},
		Errors: []PlaywrightError{{
			Message: "global setup failed",
			Location: &PlaywrightLocation{
				File: "web/tests/e2e/global.setup.ts",
				Line: 9,
			},
		}},
	}, errors.New("exit status 1"), write, time.Now())

	failed := assertRunEvent(t, events, "failed", "playwright::project::e2e-mock")
	if !strings.Contains(failed.Message, "global setup failed") || failed.Location == nil || failed.Location.Line != 8 {
		t.Fatalf("failed event = %#v", failed)
	}
	assertNoRunEvent(t, events, "passed", "playwright::project::e2e-mock")
}

// DHF-TEST: keel/requirement-11, keel/requirement-23
func TestPlaywrightReportResultSkippedRunErrorFallbackAndArtifactKinds(t *testing.T) {
	t.Run("all skipped", func(t *testing.T) {
		var events []RunEvent
		EmitPlaywrightReportResult(t.TempDir(), "playwright::project::e2e", "e2e", PlaywrightJSONReport{
			Stats: PlaywrightStats{Expected: 0, Skipped: 2, Unexpected: 0},
		}, nil, func(event RunEvent) {
			events = append(events, event)
		}, time.Now())
		skipped := assertRunEvent(t, events, "skipped", "playwright::project::e2e")
		if !strings.Contains(skipped.Message, "all selected tests skipped") {
			t.Fatalf("skipped message = %q", skipped.Message)
		}
	})

	t.Run("run error fallback", func(t *testing.T) {
		var events []RunEvent
		EmitPlaywrightReportResult(t.TempDir(), "playwright::project::e2e", "e2e", PlaywrightJSONReport{
			Stats: PlaywrightStats{Unexpected: 1},
		}, errors.New("playwright exited 1"), func(event RunEvent) {
			events = append(events, event)
		}, time.Now())
		failed := assertRunEvent(t, events, "failed", "playwright::project::e2e")
		if failed.Message != "playwright exited 1" {
			t.Fatalf("failed message = %q, want run error fallback", failed.Message)
		}
	})

	kinds := map[string]PlaywrightAttachment{
		"video":  {Name: "movie", Path: "movie.webm", ContentType: "video/webm"},
		"report": {Name: "html", Path: "report.html", ContentType: "text/html"},
		"other":  {Name: "log", Path: "stdout.txt", ContentType: "text/plain"},
	}
	for want, attachment := range kinds {
		if got := playwrightArtifactKind(attachment); got != want {
			t.Fatalf("playwrightArtifactKind(%+v) = %q, want %q", attachment, got, want)
		}
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-23
func TestPlaywrightReportResultOutputsPassingAndReportFileErrors(t *testing.T) {
	repo := t.TempDir()
	var events []RunEvent
	EmitPlaywrightReportResult(repo, "playwright::project::e2e", "e2e", PlaywrightJSONReport{
		Stats: PlaywrightStats{Expected: 1},
		Suites: []PlaywrightSuite{{
			Specs: []PlaywrightSpec{{
				Title: "passes",
				Tests: []PlaywrightTest{{
					ProjectName: "e2e",
					Results: []PlaywrightResult{{
						Status:   "passed",
						Duration: 33,
						Stdout:   []PlaywrightOutputEntry{{Text: "stdout line\n"}},
						Stderr:   []PlaywrightOutputEntry{{Text: "stderr line\r\n"}, {Text: "   "}},
						Attachments: []PlaywrightAttachment{
							{Name: "empty"},
							{Name: "report", Path: filepath.Join(repo, "report.html"), ContentType: "text/html"},
						},
					}},
				}},
			}},
		}},
	}, nil, func(event RunEvent) {
		events = append(events, event)
	}, time.Now())

	assertRunEvent(t, events, "output", "playwright::project::e2e")
	report := assertRunEventWithArtifactName(t, events, "playwright::project::e2e", "report")
	if report.Artifact == nil || report.Artifact.Kind != "report" {
		t.Fatalf("report artifact = %+v, want report kind", report.Artifact)
	}
	passed := assertRunEvent(t, events, "passed", "playwright::project::e2e")
	if passed.DurationMS != 33 {
		t.Fatalf("passed duration = %d, want summed result duration", passed.DurationMS)
	}

	if err := EmitPlaywrightReportDetailsFromFile(repo, "e2e", "", func(RunEvent) {}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("empty report path err = %v, want missing path", err)
	}
	badPath := filepath.Join(repo, "bad.json")
	if err := os.WriteFile(badPath, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EmitPlaywrightReportDetailsFromFile(repo, "e2e", badPath, func(RunEvent) {}); err == nil {
		t.Fatal("bad Playwright report JSON returned nil error")
	}
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightReportDetailsMapToProjectFileAndTestItems(t *testing.T) {
	repo := t.TempDir()
	var events []RunEvent
	write := func(event RunEvent) {
		events = append(events, event)
	}
	EmitPlaywrightReportDetails(repo, "e2e-mock", PlaywrightJSONReport{
		Suites: []PlaywrightSuite{{
			File: "web/tests/e2e/login.spec.ts",
			Specs: []PlaywrightSpec{{
				Title: "logs in",
				Tests: []PlaywrightTest{{
					ProjectName: "e2e-mock",
					Results: []PlaywrightResult{{
						Status:   "passed",
						Duration: 45,
					}},
				}},
			}},
		}},
	}, write)

	test := assertRunEvent(t, events, "passed", "playwright::test::e2e-mock::web/tests/e2e/login.spec.ts::logs-in")
	if test.DurationMS != 45 {
		t.Fatalf("test duration = %d", test.DurationMS)
	}
	file := assertRunEvent(t, events, "passed", "playwright::file::e2e-mock::web/tests/e2e/login.spec.ts")
	if file.DurationMS != 45 {
		t.Fatalf("file duration = %d", file.DurationMS)
	}
	project := assertRunEvent(t, events, "passed", "playwright::project::e2e-mock")
	if project.DurationMS != 45 {
		t.Fatalf("project duration = %d", project.DurationMS)
	}
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightReportDetailsPreservesConsumerRelativeReportPaths(t *testing.T) {
	repo := t.TempDir()
	var events []RunEvent
	write := func(event RunEvent) {
		events = append(events, event)
	}
	EmitPlaywrightReportDetails(repo, "e2e-mock", PlaywrightJSONReport{
		Suites: []PlaywrightSuite{{
			File: "login.spec.ts",
			Specs: []PlaywrightSpec{{
				Title: "logs in",
				Tests: []PlaywrightTest{{
					ProjectName: "e2e-mock",
					Results: []PlaywrightResult{{
						Status:   "passed",
						Duration: 45,
					}},
				}},
			}},
		}},
	}, write)

	assertRunEvent(t, events, "passed", "playwright::test::e2e-mock::login.spec.ts::logs-in")
	assertRunEvent(t, events, "passed", "playwright::file::e2e-mock::login.spec.ts")
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightStartedTestIDFromLine(t *testing.T) {
	line := "\x1b[1A\x1b[2K[37/38] [e2e-emulator] › tests/e2e/vapps-lifecycle-live.spec.ts:45:1 › @no-mock vApp lifecycle changes state and exposes VM busy status during add"
	got, ok := playwrightStartedTestIDFromLine("e2e-emulator", line)
	if !ok {
		t.Fatalf("line did not parse")
	}
	want := "playwright::test::e2e-emulator::tests/e2e/vapps-lifecycle-live.spec.ts::no-mock-vapp-lifecycle-changes-state-and-exposes-vm-busy-status-during-add"
	if got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
	if _, ok := playwrightStartedTestIDFromLine("e2e-real", line); ok {
		t.Fatalf("line for e2e-emulator matched e2e-real")
	}
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightProgressObserverEmitsStartedOnce(t *testing.T) {
	var events []RunEvent
	observer := NewPlaywrightProgressObserver("e2e-mock", func(event RunEvent) {
		events = append(events, event)
	})
	line := "[1/2] [e2e-mock] › tests/e2e/login-success.spec.ts:11:1 › admin sign-in lands on the dashboard with the user name visible"

	observer.Observe("playwright-e2e-mock_stdout", line)
	observer.Observe("playwright-e2e-mock_stdout", line)
	observer.Observe("playwright-e2e-mock_stderr", line)

	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Event != "test_started" {
		t.Fatalf("event = %#v", events[0])
	}
	want := "playwright::test::e2e-mock::tests/e2e/login-success.spec.ts::admin-sign-in-lands-on-the-dashboard-with-the-user-name-visible"
	if events[0].TestID != want {
		t.Fatalf("test id = %q, want %q", events[0].TestID, want)
	}
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightProgressObserverEmitsLivePassedFromListReporter(t *testing.T) {
	var events []RunEvent
	observer := NewPlaywrightProgressObserver("e2e-mock", func(event RunEvent) {
		events = append(events, event)
	})
	line := "  ✓  2 [e2e-mock] › tests/e2e/shell-renders.spec.ts:16:3 › unauthenticated landing › login form mounts with email + password fields and a submit button (724ms)"

	observer.Observe("playwright-e2e-mock_stdout", line)
	observer.Observe("playwright-e2e-mock_stdout", line)

	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	wantID := "playwright::test::e2e-mock::tests/e2e/shell-renders.spec.ts::login-form-mounts-with-email-password-fields-and-a-submit-button"
	if events[0].Event != "passed" || events[0].TestID != wantID || events[0].DurationMS != 724 {
		t.Fatalf("event = %#v", events[0])
	}
}

// TestPlaywrightReporterFormatStable captures the exact shapes
// of progress and result lines as emitted by Playwright's `list`
// reporter at v1.49. Closes Story 27.24 AC10. If a Playwright upgrade
// changes any of these formats — separator, project bracket, file path
// shape, duration suffix — this test fails LOUDLY with the captured
// vs. current output, instead of the live-progress fanout silently
// going dark while the JSON-report fallback masks the regression.
//
// DHF-TEST: keel/requirement-23
func TestPlaywrightReporterFormatStable(t *testing.T) {
	cases := []struct {
		name string
		line string
		want RunEvent
	}{
		{
			name: "progress line",
			line: "[1/38] [e2e-mock] › tests/e2e/login-success.spec.ts:11:1 › admin sign-in lands on the dashboard with the user name visible",
			want: RunEvent{
				Event:  "test_started",
				TestID: "playwright::test::e2e-mock::tests/e2e/login-success.spec.ts::admin-sign-in-lands-on-the-dashboard-with-the-user-name-visible",
			},
		},
		{
			name: "result line ms",
			line: "  ✓  2 [e2e-mock] › tests/e2e/shell-renders.spec.ts:16:3 › login form mounts (724ms)",
			want: RunEvent{
				Event:      "passed",
				TestID:     "playwright::test::e2e-mock::tests/e2e/shell-renders.spec.ts::login-form-mounts",
				DurationMS: 724,
			},
		},
		{
			name: "result line seconds",
			line: "✓  1 [e2e-mock] › tests/e2e/visual-themes.spec.ts:56:3 › dark theme captures all views (9.8s)",
			want: RunEvent{
				Event:      "passed",
				TestID:     "playwright::test::e2e-mock::tests/e2e/visual-themes.spec.ts::dark-theme-captures-all-views",
				DurationMS: 9800,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got RunEvent
			if tc.want.Event == "test_started" {
				id, parsed := playwrightStartedTestIDFromLine("e2e-mock", tc.line)
				if !parsed {
					t.Fatalf("playwright list-reporter progress format changed; line %q no longer matches playwrightProgressLinePattern", tc.line)
				}
				got = RunEvent{Event: "test_started", TestID: id}
			} else {
				event, ok := playwrightLiveResultEventFromLine("e2e-mock", tc.line)
				if !ok {
					t.Fatalf("playwright list-reporter result format changed; line %q no longer matches playwrightListResultLinePattern", tc.line)
				}
				got = event
			}
			if got.Event != tc.want.Event || got.TestID != tc.want.TestID || got.DurationMS != tc.want.DurationMS {
				t.Fatalf("event mismatch:\n  got:  %+v\n  want: %+v\n  line: %q", got, tc.want, tc.line)
			}
		})
	}
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightLiveResultParsesSecondDurations(t *testing.T) {
	line := "✓  1 [e2e-mock] › tests/e2e/visual-themes.spec.ts:56:3 › @no-emulator @no-real dark — captures all views (9.8s)"
	event, ok := playwrightLiveResultEventFromLine("e2e-mock", line)
	if !ok {
		t.Fatalf("line did not parse")
	}
	if event.Event != "passed" || event.DurationMS != 9800 {
		t.Fatalf("event = %#v", event)
	}
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightFinalReportSkipsDuplicateLivePassedEvents(t *testing.T) {
	var events []RunEvent
	observer := NewPlaywrightProgressObserver("e2e-mock", func(event RunEvent) {
		events = append(events, event)
	})
	live := RunEvent{
		Event:      "passed",
		TestID:     "playwright::test::e2e-mock::web/tests/e2e/login.spec.ts::logs-in",
		DurationMS: 12,
	}
	observer.final[live.TestID] = true

	observer.FinalEventTrackingWriter(func(event RunEvent) {
		events = append(events, event)
	})(live)
	observer.FinalEventTrackingWriter(func(event RunEvent) {
		events = append(events, event)
	})(RunEvent{
		Event:      "passed",
		TestID:     "playwright::file::e2e-mock::web/tests/e2e/login.spec.ts",
		DurationMS: 12,
	})

	if len(events) != 1 || events[0].TestID != "playwright::file::e2e-mock::web/tests/e2e/login.spec.ts" {
		t.Fatalf("events = %#v", events)
	}
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightProgressObserverSkipsStartedItemsMissingFinalReportEvents(t *testing.T) {
	var events []RunEvent
	observer := NewPlaywrightProgressObserver("e2e-mock", func(event RunEvent) {
		events = append(events, event)
	})
	startedID := "playwright::test::e2e-mock::web/tests/e2e/login.spec.ts::logs-in"
	observer.started[startedID] = true

	observer.FinalEventTrackingWriter(func(event RunEvent) {
		events = append(events, event)
	})(RunEvent{
		Event:  "passed",
		TestID: "playwright::test::e2e-mock::web/tests/e2e/other.spec.ts::passes",
	})
	observer.EmitUnreportedStartedAsSkipped()

	assertRunEvent(t, events, "skipped", startedID)
	assertRunEvent(t, events, "passed", "playwright::test::e2e-mock::web/tests/e2e/other.spec.ts::passes")
}

// DHF-TEST: keel/requirement-23
func TestPlaywrightLaneReportFileDrivesLeafDurations(t *testing.T) {
	repo := t.TempDir()
	reportPath := filepath.Join(t.TempDir(), "playwright-report.json")
	report := PlaywrightJSONReport{
		Stats: PlaywrightStats{Expected: 1},
		Suites: []PlaywrightSuite{{
			File: "web/tests/e2e/login-success.spec.ts",
			Specs: []PlaywrightSpec{{
				Title: "admin sign-in lands on the dashboard with the user name visible",
				Tests: []PlaywrightTest{{
					ProjectName: "e2e-mock",
					Results: []PlaywrightResult{{
						Status:   "passed",
						Duration: 321,
					}},
				}},
			}},
		}},
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	var events []RunEvent
	err = EmitPlaywrightReportDetailsFromFile(repo, "e2e-mock", reportPath, func(event RunEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}

	testID := "playwright::test::e2e-mock::web/tests/e2e/login-success.spec.ts::admin-sign-in-lands-on-the-dashboard-with-the-user-name-visible"
	test := assertRunEvent(t, events, "passed", testID)
	if test.DurationMS != 321 {
		t.Fatalf("test duration = %d, want 321", test.DurationMS)
	}
}
