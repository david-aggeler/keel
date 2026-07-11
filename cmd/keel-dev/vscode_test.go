package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/vscode"
)

// DHF-TEST: keel/requirement-35, keel/requirement-37
func TestVSCodeRunBlockedLaneUsesEngineProtocol(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	writeFile(t, root, "main_test.go", "package p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n")

	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", "keel::lane::test-fast")

	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::test-fast"})
	if err == nil {
		t.Fatal("blocked lane returned nil error; want non-zero terminal failure")
	}

	events := decodeRunEvents(t, protocol.String())
	if len(events) < 3 {
		t.Fatalf("events = %+v, want run_started, failed, run_finished", events)
	}
	if events[0].Event != "run_started" {
		t.Fatalf("first event = %+v, want run_started", events[0])
	}
	if events[1].Event != "failed" || !strings.Contains(events[1].Message, "lane blocked:") {
		t.Fatalf("second event = %+v, want engine lane blocked failure", events[1])
	}
	if events[len(events)-1].Event != "run_finished" {
		t.Fatalf("last event = %+v, want run_finished", events[len(events)-1])
	}
	if events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode == 0 {
		t.Fatalf("terminal event = %+v, want non-zero exit_code", events[len(events)-1])
	}
	for _, event := range events {
		if event.RunID == "" || event.Workspace == "" || event.Version != 1 || event.Time.IsZero() {
			t.Fatalf("event missing production stamp: %+v", event)
		}
	}
}

func contextWithVSCodeTestState(root string, protocol io.Writer) context.Context {
	return withRunStateProtocol(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, root, protocol)
}

func decodeRunEvents(t *testing.T, raw string) []vscode.RunEvent {
	t.Helper()
	var events []vscode.RunEvent
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event vscode.RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid run-event JSON line %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func TestVSCodeDiscoveryAndPlanExposeKeelLaneSet(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")

	var discover bytes.Buffer
	if err := writeVSCodeDiscovery(root, &discover); err != nil {
		t.Fatalf("writeVSCodeDiscovery: %v", err)
	}
	var doc vscode.DiscoveryDocument
	if err := json.Unmarshal(discover.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, discover.String())
	}
	for _, want := range []string{"keel::lane::lint", "keel::lane::test-fast", "keel::lane::test-coverage"} {
		if !discoveryHasLane(doc, want) {
			t.Fatalf("discovery missing lane %q: %+v", want, doc.Items)
		}
	}

	var plan bytes.Buffer
	if err := writeVSCodePlan(root, []string{"keel::lane::test-fast"}, &plan); err != nil {
		t.Fatalf("writeVSCodePlan: %v", err)
	}
	var setup vscode.SetupPlan
	if err := json.Unmarshal(plan.Bytes(), &setup); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, plan.String())
	}
	if len(setup.Checks) == 0 {
		t.Fatalf("plan checks empty; want keel prereqs")
	}
}

// DHF-TEST: keel/requirement-36
func TestVSCodeRunWritesStampedExternalRunStream(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", "keel::lane::test-fast")

	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::test-fast"})
	if err == nil {
		t.Fatal("blocked lane returned nil error; want non-zero")
	}
	protocolEvents := decodeRunEvents(t, protocol.String())
	runID := protocolEvents[0].RunID
	if runID == "" {
		t.Fatalf("run_started missing run id: %+v", protocolEvents[0])
	}

	streamPath := filepath.Join(root, ".devtools", "vscode-runs", runID+".jsonl")
	stream, readErr := os.ReadFile(streamPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	externalEvents := decodeRunEvents(t, string(stream))
	if len(externalEvents) != len(protocolEvents) {
		t.Fatalf("external stream events = %d, protocol events = %d\n%s", len(externalEvents), len(protocolEvents), stream)
	}
	for i, event := range externalEvents {
		if event.Version != 1 || event.Time.IsZero() || event.RunID != runID || event.Workspace == "" {
			t.Fatalf("external event %d missing stamp: %+v", i, event)
		}
	}
}

func discoveryHasLane(doc vscode.DiscoveryDocument, id string) bool {
	for _, item := range doc.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestVSCodeProtocolWriterIsOnlyStdoutAllowlistGrowth(t *testing.T) {
	dir := t.TempDir()
	keeldev := filepath.Join(dir, "cmd", "keel-dev")
	if err := os.MkdirAll(keeldev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, keeldev, "main.go",
		"package main\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\nfunc newLogger() io.Writer { return os.Stdout }\nfunc newProtocolStream() io.Writer { return os.Stdout }\n")
	if err := runLint(dir); err != nil {
		t.Fatalf("newProtocolStream should be the one protocol stdout allowlist entry: %v", err)
	}

	writeFile(t, keeldev, "main.go",
		"package main\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\nfunc newLogger() io.Writer { return os.Stdout }\nfunc newProtocolStream() io.Writer { return os.Stdout }\nfunc extraProtocolStream() io.Writer { return os.Stdout }\n")
	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "extraProtocolStream") {
		t.Fatalf("unexpected stdout allowlist growth should fail, got %v", err)
	}
}

// DHF-TEST: keel/requirement-38
func TestVSCodeRunKeepsStdoutProtocolAndConsoleOnStderr(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	t.Chdir(root)
	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", "keel::lane::test-fast")

	stdout, stderr := captureProcessStreams(t, func() {
		if code := run([]string{"--no-header", "-v", "vscode", "tests", "run", "--id", "keel::lane::test-fast"}); code == 0 {
			t.Fatal("blocked vscode run exit = 0, want non-zero")
		}
	})

	events := decodeRunEvents(t, stdout)
	if len(events) == 0 {
		t.Fatalf("stdout had no protocol events: %q", stdout)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var event vscode.RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("stdout contains non-protocol residue %q: %v\nstderr:\n%s", line, err, stderr)
		}
	}
	if strings.Contains(stdout, "keel-dev failed") || strings.Contains(stdout, "level=") {
		t.Fatalf("stdout contains console log residue:\n%s", stdout)
	}
	if !strings.Contains(stderr, "keel-dev failed") {
		t.Fatalf("stderr missing console failure log:\n%s", stderr)
	}
}

// DHF-TEST: keel/requirement-35
func TestVSCodeRunRefusesExistingLockAndReleasesOwnLock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(runDir, "run.lock")
	existing := `{"pid":12345,"created_at":"2026-07-11T00:00:00Z","ids":["keel::lane::test-fast"],"token":"foreign"}`
	if err := os.WriteFile(lockPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	var protocol bytes.Buffer
	err := handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::test-fast"})
	if err == nil {
		t.Fatal("run with existing lock returned nil error; want refusal")
	}
	got, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != existing {
		t.Fatalf("existing lock was modified:\n%s", got)
	}
	events := decodeRunEvents(t, protocol.String())
	if events[len(events)-1].Event != "run_finished" || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode == 0 {
		t.Fatalf("lock refusal did not emit non-zero terminal event: %+v", events)
	}
	if !strings.Contains(protocol.String(), "run lock") {
		t.Fatalf("lock refusal protocol did not name the run lock:\n%s", protocol.String())
	}

	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	protocol.Reset()
	t.Setenv("KEEL_VSCODE_DEMO_BLOCK", "keel::lane::test-fast")
	err = handleVSCodeTestsRun(contextWithVSCodeTestState(root, &protocol), []string{"--id", "keel::lane::test-fast"})
	if err == nil {
		t.Fatal("blocked lane returned nil error; want non-zero")
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("own run lock should be released, stat err=%v", err)
	}
}

func captureProcessStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	fn()
	if err := stdoutW.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrW.Close(); err != nil {
		t.Fatal(err)
	}
	stdoutBytes, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatal(err)
	}
	stderrBytes, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatal(err)
	}
	return string(stdoutBytes), string(stderrBytes)
}
