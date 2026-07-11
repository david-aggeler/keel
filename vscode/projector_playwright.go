package vscode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
var playwrightProgressLinePattern = regexp.MustCompile(`\[[0-9]+/[0-9]+\]\s+\[([^\]]+)\]\s+›\s+(.+\.(?:spec|test)\.tsx?):[0-9]+:[0-9]+\s+›\s+(.+)$`)
var playwrightListResultLinePattern = regexp.MustCompile(`^([✓✘×-])\s+[0-9]+\s+\[([^\]]+)\]\s+›\s+(.+\.(?:spec|test)\.tsx?):[0-9]+:[0-9]+\s+›\s+(.+)\s+\(([^)]+)\)$`)

// PlaywrightJSONReport is the top-level shape of `playwright test --reporter=json`.
type PlaywrightJSONReport struct {
	Stats  PlaywrightStats   `json:"stats"`
	Suites []PlaywrightSuite `json:"suites"`
	Errors []PlaywrightError `json:"errors"`
}

// PlaywrightStats is the run-level tally in a Playwright report.
type PlaywrightStats struct {
	Expected   int `json:"expected"`
	Skipped    int `json:"skipped"`
	Unexpected int `json:"unexpected"`
	Flaky      int `json:"flaky"`
}

// PlaywrightSuite is a (possibly nested) suite node in a Playwright report.
type PlaywrightSuite struct {
	Title  string            `json:"title"`
	File   string            `json:"file"`
	Specs  []PlaywrightSpec  `json:"specs"`
	Suites []PlaywrightSuite `json:"suites"`
}

// PlaywrightSpec is a single spec (test title) within a suite.
type PlaywrightSpec struct {
	Title string           `json:"title"`
	Tests []PlaywrightTest `json:"tests"`
}

// PlaywrightTest is a per-project execution of a spec.
type PlaywrightTest struct {
	ProjectName string             `json:"projectName"`
	Status      string             `json:"status"`
	Results     []PlaywrightResult `json:"results"`
}

// PlaywrightResult is a single attempt's result for a test.
type PlaywrightResult struct {
	Status      string                  `json:"status"`
	Duration    int64                   `json:"duration"`
	Error       *PlaywrightError        `json:"error"`
	Errors      []PlaywrightError       `json:"errors"`
	Attachments []PlaywrightAttachment  `json:"attachments"`
	Stdout      []PlaywrightOutputEntry `json:"stdout"`
	Stderr      []PlaywrightOutputEntry `json:"stderr"`
}

// PlaywrightError is a Playwright error with an optional source location.
type PlaywrightError struct {
	Message  string              `json:"message"`
	Location *PlaywrightLocation `json:"location"`
}

// PlaywrightLocation is a source position reported by Playwright.
type PlaywrightLocation struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// PlaywrightAttachment is a run byproduct (trace, screenshot, video, report).
type PlaywrightAttachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Path        string `json:"path"`
}

// PlaywrightOutputEntry is one captured stdout/stderr chunk.
type PlaywrightOutputEntry struct {
	Text string `json:"text"`
}

// PlaywrightProgressObserver watches a Playwright child process's live `list`
// reporter output and emits test_started plus live leaf results, de-duplicating
// against the final JSON-report projection.
type PlaywrightProgressObserver struct {
	Project string
	write   RunEventWriter
	started map[string]bool
	final   map[string]bool
}

// NewPlaywrightProgressObserver constructs a progress observer for one project.
//
// DHF-REQ: keel/requirement-23
func NewPlaywrightProgressObserver(project string, write RunEventWriter) *PlaywrightProgressObserver {
	return &PlaywrightProgressObserver{
		Project: project,
		write:   write,
		started: map[string]bool{},
		final:   map[string]bool{},
	}
}

// Observe consumes one line of child output on a named stream, emitting
// test_started / live leaf results parsed from the Playwright list reporter.
//
// DHF-REQ: keel/requirement-23
func (o *PlaywrightProgressObserver) Observe(stream, text string) {
	if !strings.HasSuffix(stream, "_stdout") && stream != "stdout" {
		return
	}
	if id, ok := playwrightStartedTestIDFromLine(o.Project, text); ok && !o.started[id] {
		o.started[id] = true
		o.write(RunEvent{Event: "test_started", TestID: id})
	}
	if event, ok := playwrightLiveResultEventFromLine(o.Project, text); ok && !o.final[event.TestID] {
		o.final[event.TestID] = true
		o.write(event)
	}
}

// FinalEventTrackingWriter wraps a writer so terminal leaf events for this
// project are recorded, suppressing a final passed/skipped that a live result
// already reported.
//
// DHF-REQ: keel/requirement-23
func (o *PlaywrightProgressObserver) FinalEventTrackingWriter(write RunEventWriter) RunEventWriter {
	return func(event RunEvent) {
		if strings.HasPrefix(event.TestID, "playwright::test::"+o.Project+"::") && IsTerminalRunEvent(event.Event) {
			if o.final[event.TestID] && (event.Event == "passed" || event.Event == "skipped") {
				return
			}
			o.final[event.TestID] = true
		}
		write(event)
	}
}

// EmitUnreportedStartedAsSkipped emits a skipped event for every item that
// started but never received a leaf result in the JSON report.
//
// DHF-REQ: keel/requirement-23
func (o *PlaywrightProgressObserver) EmitUnreportedStartedAsSkipped() {
	ids := make([]string, 0, len(o.started))
	for id := range o.started {
		if !o.final[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		o.final[id] = true
		o.write(RunEvent{
			Event:   "skipped",
			TestID:  id,
			Message: "Playwright started this item but did not include a leaf result in the JSON report",
		})
	}
}

func playwrightStartedTestIDFromLine(project, line string) (string, bool) {
	clean := strings.TrimSpace(ansiEscapePattern.ReplaceAllString(line, ""))
	match := playwrightProgressLinePattern.FindStringSubmatch(clean)
	if match == nil {
		return "", false
	}
	lineProject := match[1]
	if project != "" && lineProject != project {
		return "", false
	}
	file := filepath.ToSlash(match[2])
	title := match[3]
	if idx := strings.LastIndex(title, " › "); idx >= 0 {
		title = title[idx+len(" › "):]
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", false
	}
	return TSItemID("playwright", "test", lineProject, file, StableTitleSlug(title)), true
}

func playwrightLiveResultEventFromLine(project, line string) (RunEvent, bool) {
	clean := strings.TrimSpace(ansiEscapePattern.ReplaceAllString(line, ""))
	match := playwrightListResultLinePattern.FindStringSubmatch(clean)
	if match == nil {
		return RunEvent{}, false
	}
	eventName := ""
	switch match[1] {
	case "✓":
		eventName = "passed"
	case "-":
		eventName = "skipped"
	default:
		return RunEvent{}, false
	}
	lineProject := match[2]
	if project != "" && lineProject != project {
		return RunEvent{}, false
	}
	file := filepath.ToSlash(match[3])
	title := match[4]
	if idx := strings.LastIndex(title, " › "); idx >= 0 {
		title = title[idx+len(" › "):]
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return RunEvent{}, false
	}
	duration, ok := ParsePlaywrightListDurationMS(match[5])
	if !ok {
		return RunEvent{}, false
	}
	return RunEvent{
		Event:      eventName,
		TestID:     TSItemID("playwright", "test", lineProject, file, StableTitleSlug(title)),
		DurationMS: duration,
	}, true
}

// NormalizePlaywrightReportPath resolves a Playwright-reported spec path to a
// module-relative path without imposing any consumer-specific source layout.
//
// DHF-REQ: keel/requirement-23
func NormalizePlaywrightReportPath(repo, path string) string {
	return NormalizeReportPath(repo, path)
}

// EmitPlaywrightReportResult projects the top-level pass/fail/skip outcome of a
// Playwright report onto the selected item id, surfacing captured output and
// attachments as run events.
//
// DHF-REQ: keel/requirement-23
func EmitPlaywrightReportResult(repo, id, project string, report PlaywrightJSONReport, runErr error, write RunEventWriter, start time.Time) {
	results := collectPlaywrightResults(project, report.Suites)
	for _, result := range results {
		for _, out := range result.Stdout {
			if strings.TrimSpace(out.Text) != "" {
				write(RunEvent{Event: "output", TestID: id, Message: strings.TrimRight(out.Text, "\r\n")})
			}
		}
		for _, out := range result.Stderr {
			if strings.TrimSpace(out.Text) != "" {
				write(RunEvent{Event: "output", TestID: id, Message: strings.TrimRight(out.Text, "\r\n")})
			}
		}
		for _, attachment := range result.Attachments {
			if attachment.Path == "" {
				continue
			}
			write(RunEvent{
				Event:   "artifact",
				TestID:  id,
				Message: fmt.Sprintf("%s: %s", attachment.Name, attachment.Path),
				Artifact: &RunArtifact{
					Name: attachment.Name,
					URI:  attachment.Path,
					Kind: playwrightArtifactKind(attachment),
				},
			})
		}
	}
	duration := playwrightDurationMillis(results, start)
	if report.Stats.Unexpected == 0 && len(report.Errors) == 0 && runErr == nil {
		if report.Stats.Skipped > 0 && report.Stats.Expected == 0 {
			write(RunEvent{Event: "skipped", TestID: id, Message: "Playwright reported all selected tests skipped", DurationMS: duration})
			return
		}
		write(RunEvent{Event: "passed", TestID: id, DurationMS: duration})
		return
	}
	message, location := playwrightFailureDiagnostic(repo, report, results)
	if message == "Playwright failed" && runErr != nil {
		message = runErr.Error()
	}
	write(RunEvent{Event: "failed", TestID: id, Message: message, Location: location, DurationMS: duration})
}

// EmitPlaywrightReportDetails projects a Playwright report into per-test,
// per-file, and per-project run events.
//
// DHF-REQ: keel/requirement-23
func EmitPlaywrightReportDetails(repo, project string, report PlaywrightJSONReport, write RunEventWriter) {
	projectID := "playwright::project::" + project
	projectEvent := ""
	var projectDuration int64
	fileRollups := map[string]RunEvent{}
	var walk func([]PlaywrightSuite, string)
	walk = func(suites []PlaywrightSuite, inheritedFile string) {
		for _, suite := range suites {
			file := inheritedFile
			if suite.File != "" {
				file = NormalizePlaywrightReportPath(repo, suite.File)
			}
			for _, spec := range suite.Specs {
				testID := TSItemID("playwright", "test", project, file, StableTitleSlug(spec.Title))
				event := ""
				var duration int64
				seen := false
				var message string
				var location *RunLocation
				for _, test := range spec.Tests {
					if project != "" && test.ProjectName != "" && test.ProjectName != project {
						continue
					}
					for _, result := range test.Results {
						seen = true
						resultEvent := StatusEventName(result.Status)
						event = MergedStatusEvent(event, resultEvent)
						duration += result.Duration
						if resultEvent == "failed" && message == "" {
							message, location = playwrightFailureDiagnostic(repo, PlaywrightJSONReport{}, []PlaywrightResult{result})
						}
					}
				}
				if file != "" && seen {
					write(RunEvent{Event: event, TestID: testID, Message: message, Location: location, DurationMS: duration})
					fileID := TSItemID("playwright", "file", project, file, "")
					rollup := fileRollups[fileID]
					rollup.TestID = fileID
					rollup.Event = MergedStatusEvent(rollup.Event, event)
					rollup.DurationMS += duration
					fileRollups[fileID] = rollup
					projectEvent = MergedStatusEvent(projectEvent, event)
					projectDuration += duration
				}
			}
			walk(suite.Suites, file)
		}
	}
	walk(report.Suites, "")
	for _, rollup := range fileRollups {
		write(rollup)
	}
	if len(report.Errors) > 0 {
		message, location := playwrightFailureDiagnostic(repo, report, nil)
		write(RunEvent{Event: "failed", TestID: projectID, Message: message, Location: location, DurationMS: projectDuration})
		return
	}
	if projectEvent != "" {
		write(RunEvent{Event: projectEvent, TestID: projectID, DurationMS: projectDuration})
	}
}

// EmitPlaywrightReportDetailsFromFile reads a Playwright JSON report from disk
// and projects its detail events.
//
// DHF-REQ: keel/requirement-23
func EmitPlaywrightReportDetailsFromFile(repo, project, path string, write RunEventWriter) error {
	if path == "" {
		return errors.New("missing Playwright JSON report path")
	}
	body, err := os.ReadFile(path) //nolint:gosec // path is generated by the devtool under the repo-local log directory.
	if err != nil {
		return err
	}
	var report PlaywrightJSONReport
	if err := json.Unmarshal(body, &report); err != nil {
		return err
	}
	EmitPlaywrightReportDetails(repo, project, report, write)
	return nil
}

func collectPlaywrightResults(project string, suites []PlaywrightSuite) []PlaywrightResult {
	var results []PlaywrightResult
	var walk func([]PlaywrightSuite)
	walk = func(suites []PlaywrightSuite) {
		for _, suite := range suites {
			for _, spec := range suite.Specs {
				for _, test := range spec.Tests {
					if project != "" && test.ProjectName != "" && test.ProjectName != project {
						continue
					}
					results = append(results, test.Results...)
				}
			}
			walk(suite.Suites)
		}
	}
	walk(suites)
	return results
}

func playwrightFailureDiagnostic(repo string, report PlaywrightJSONReport, results []PlaywrightResult) (string, *RunLocation) {
	for _, result := range results {
		if result.Error != nil && result.Error.Message != "" {
			return result.Error.Message, normalizePlaywrightLocation(repo, result.Error.Location)
		}
		for _, err := range result.Errors {
			if err.Message != "" {
				return err.Message, normalizePlaywrightLocation(repo, err.Location)
			}
		}
	}
	for _, err := range report.Errors {
		if err.Message != "" {
			return err.Message, normalizePlaywrightLocation(repo, err.Location)
		}
	}
	return "Playwright failed", nil
}

func normalizePlaywrightLocation(repo string, location *PlaywrightLocation) *RunLocation {
	if location == nil {
		return nil
	}
	uri := location.File
	if uri != "" && !filepath.IsAbs(uri) {
		uri = filepath.Join(repo, filepath.FromSlash(uri))
	}
	return &RunLocation{
		URI:    uri,
		Line:   max(location.Line-1, 0),
		Column: max(location.Column-1, 0),
	}
}

func playwrightDurationMillis(results []PlaywrightResult, start time.Time) int64 {
	var total int64
	for _, result := range results {
		total += result.Duration
	}
	if total > 0 {
		return total
	}
	return time.Since(start).Milliseconds()
}

func playwrightArtifactKind(attachment PlaywrightAttachment) string {
	switch {
	case strings.Contains(attachment.ContentType, "zip") || strings.HasSuffix(attachment.Path, ".zip"):
		return "trace"
	case strings.HasPrefix(attachment.ContentType, "image/"):
		return "screenshot"
	case strings.HasPrefix(attachment.ContentType, "video/"):
		return "video"
	case strings.HasSuffix(attachment.Path, ".html"):
		return "report"
	default:
		return "other"
	}
}
