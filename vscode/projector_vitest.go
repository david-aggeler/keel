package vscode

import (
	"fmt"
	"time"
)

// VitestJSONReport is the top-level shape of `vitest run --reporter=json`.
type VitestJSONReport struct {
	Success         bool               `json:"success"`
	NumTotalTests   int                `json:"numTotalTests"`
	NumPassedTests  int                `json:"numPassedTests"`
	NumFailedTests  int                `json:"numFailedTests"`
	NumPendingTests int                `json:"numPendingTests"`
	TestResults     []VitestTestResult `json:"testResults"`
}

// VitestTestResult is a single test file's result within a vitest report.
type VitestTestResult struct {
	Status           string                  `json:"status"`
	Message          string                  `json:"message"`
	Name             string                  `json:"name"`
	AssertionResults []VitestAssertionResult `json:"assertionResults"`
}

// VitestAssertionResult is a single assertion (leaf test) within a vitest file.
type VitestAssertionResult struct {
	FullName        string   `json:"fullName"`
	AncestorTitles  []string `json:"ancestorTitles"`
	Status          string   `json:"status"`
	Title           string   `json:"title"`
	Duration        float64  `json:"duration"`
	FailureMessages []string `json:"failureMessages"`
}

// EmitVitestReportResult projects the top-level pass/fail/skip outcome of a
// vitest report onto the selected item id.
//
// DHF-REQ: keel/requirement-23
func EmitVitestReportResult(id string, report VitestJSONReport, write RunEventWriter, start time.Time) {
	duration := VitestDurationMillis(report, start)
	switch {
	case report.NumTotalTests > 0 && report.NumPendingTests == report.NumTotalTests:
		write(RunEvent{Event: "skipped", TestID: id, Message: "Vitest reported all selected tests pending", DurationMS: duration})
	case report.Success:
		write(RunEvent{Event: "passed", TestID: id, DurationMS: duration})
	default:
		write(RunEvent{Event: "failed", TestID: id, Message: VitestFailureMessage(report), DurationMS: duration})
	}
}

// EmitVitestReportDetails projects a vitest report into per-file, per-suite,
// and per-test run events.
//
// DHF-REQ: keel/requirement-23
func EmitVitestReportDetails(repo string, report VitestJSONReport, write RunEventWriter) {
	for _, result := range report.TestResults {
		rel := NormalizeReportPath(repo, result.Name)
		if rel == "" {
			continue
		}
		fileID := TSItemID("vitest", "file", "", rel, "")
		fileDuration := VitestFileDurationMillis(result)
		write(RunEvent{Event: StatusEventName(result.Status), TestID: fileID, Message: result.Message, DurationMS: fileDuration})

		suiteRollups := map[string]RunEvent{}
		titleSeen := map[string]int{}
		for _, assertion := range result.AssertionResults {
			slug := StableTitleSlug(assertion.Title)
			titleSeen[slug]++
			if titleSeen[slug] > 1 {
				slug = fmt.Sprintf("%s-%d", slug, titleSeen[slug])
			}
			testID := TSItemID("vitest", "test", "", rel, slug)
			event := StatusEventName(assertion.Status)
			message := ""
			if len(assertion.FailureMessages) > 0 {
				message = assertion.FailureMessages[0]
			}
			duration := FloatDurationMillis(assertion.Duration)
			write(RunEvent{Event: event, TestID: testID, Message: message, DurationMS: duration})

			for _, ancestor := range assertion.AncestorTitles {
				suiteID := TSItemID("vitest", "suite", "", rel, StableTitleSlug(ancestor))
				rollup := suiteRollups[suiteID]
				rollup.TestID = suiteID
				rollup.Event = MergedStatusEvent(rollup.Event, event)
				rollup.DurationMS += duration
				suiteRollups[suiteID] = rollup
			}
		}
		for _, rollup := range suiteRollups {
			write(rollup)
		}
	}
}

// VitestFileDurationMillis sums the assertion durations in a vitest file result.
func VitestFileDurationMillis(result VitestTestResult) int64 {
	var total int64
	for _, assertion := range result.AssertionResults {
		total += FloatDurationMillis(assertion.Duration)
	}
	return total
}

// VitestDurationMillis sums every assertion duration in a vitest report,
// falling back to wall-clock elapsed when the report carries no durations.
func VitestDurationMillis(report VitestJSONReport, start time.Time) int64 {
	var total float64
	for _, result := range report.TestResults {
		for _, assertion := range result.AssertionResults {
			total += assertion.Duration
		}
	}
	if total > 0 {
		return FloatDurationMillis(total)
	}
	return time.Since(start).Milliseconds()
}

// VitestFailureMessage extracts the most specific failure message from a vitest
// report.
func VitestFailureMessage(report VitestJSONReport) string {
	for _, result := range report.TestResults {
		for _, assertion := range result.AssertionResults {
			if assertion.Status != "failed" {
				continue
			}
			if len(assertion.FailureMessages) > 0 {
				return assertion.FailureMessages[0]
			}
			if assertion.FullName != "" {
				return assertion.FullName
			}
		}
		if result.Message != "" {
			return result.Message
		}
	}
	return "Vitest failed"
}
