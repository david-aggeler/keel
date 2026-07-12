package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
	logging "github.com/david-aggeler/keel/log"
)

// DHF-TEST: keel/requirement-26
func TestKeelDemoRunsEveryModeAndSurfacesLogAndExecFeatures(t *testing.T) {
	for _, mode := range []string{"human", "ai", "json"} {
		t.Run(mode, func(t *testing.T) {
			out, exitCode := runDemo(t, "--mode", mode)
			if exitCode != 4 {
				t.Fatalf("keel-demo exit code = %d, want 4\noutput:\n%s", exitCode, out)
			}

			for _, want := range []string{
				"keel-demo showcase",
				"presentation surfaces",
				"mode",
				"surface_count",
				"demo_step",
				"demo_success",
				"demo_failed",
				"process start",
				"process output",
				"stdout",
				"stderr",
				"[REDACTED]",
				"structured failure",
				"log_file",
				"start_line",
				"exit_code",
				"hint",
				"demo_metric",
				"metric",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("output for --mode %s missing %q\noutput:\n%s", mode, want, out)
				}
			}
			if strings.Contains(out, "demo-secret-token") {
				t.Fatalf("output for --mode %s leaked the raw secret\noutput:\n%s", mode, out)
			}

			if mode == "json" {
				assertEveryLineIsJSON(t, out)
			}
			if mode == "ai" {
				assertSparseAIEvents(t, out)
			}
		})
	}
}

// DHF-TEST: keel/requirement-26, keel/requirement-28
func TestKeelDemoHelpTreeRendersTopLevelAndNestedPerMode(t *testing.T) {
	for _, mode := range []string{"human", "ai", "json"} {
		t.Run(mode, func(t *testing.T) {
			top, exitCode := runDemo(t, "--mode", mode, "--help")
			if exitCode != 0 {
				t.Fatalf("top-level help exit code = %d, want 0\noutput:\n%s", exitCode, top)
			}
			nested, exitCode := runDemo(t, "--mode", mode, "workflow", "--help")
			if exitCode != 0 {
				t.Fatalf("nested help exit code = %d, want 0\noutput:\n%s", exitCode, nested)
			}

			for _, want := range []string{"keel-demo", "workflow", "inspect", "replay"} {
				if !strings.Contains(top, want) {
					t.Fatalf("top-level help for --mode %s missing %q\noutput:\n%s", mode, want, top)
				}
			}
			for _, want := range []string{"workflow", "inspect", "replay"} {
				if !strings.Contains(nested, want) {
					t.Fatalf("nested help for --mode %s missing %q\noutput:\n%s", mode, want, nested)
				}
			}

			if mode == "human" {
				for _, notWant := range []string{"INFO", "====", `"event_type":"help"`, `"level":"INFO"`} {
					if strings.Contains(top, notWant) || strings.Contains(nested, notWant) {
						t.Fatalf("human help used log rendering marker %q\ntop:\n%s\nnested:\n%s", notWant, top, nested)
					}
				}
			}
			if mode == "json" {
				assertEveryLineIsJSON(t, top)
				assertEveryLineIsJSON(t, nested)
			}
			if mode == "ai" {
				assertSparseAIEvents(t, top)
				assertSparseAIEvents(t, nested)
			}
		})
	}
}

// DHF-TEST: keel/requirement-26
func TestRunShowcaseDirectReturnsStructuredFailure(t *testing.T) {
	var out bytes.Buffer
	logger := testLogger(t, "ai", &out)

	err := runShowcase(context.Background(), logger, "ai")
	var opErr *logging.OperationalError
	if !errors.As(err, &opErr) {
		t.Fatalf("runShowcase error = %T, want OperationalError", err)
	}
	if opErr.ExitCode != 4 || opErr.LogFile == "" || opErr.StartLine == 0 {
		t.Fatalf("unexpected OperationalError detail: %+v", opErr)
	}
	rendered := out.String()
	for _, want := range []string{"demo_step", "demo_success", "demo_failed", "process_output", "demo_metric"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("showcase output missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "demo-secret-token") {
		t.Fatalf("showcase leaked raw secret:\n%s", rendered)
	}
}

// DHF-TEST: keel/requirement-28
func TestKeelDemoUsesSharedCLIForUsageErrors(t *testing.T) {
	out, code := runDemo(t, "--unknown")
	if code != 2 {
		t.Fatalf("unknown global flag exit = %d, want 2\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `unknown command "--unknown"`) && !strings.Contains(out, `unknown flag "--unknown"`) {
		t.Fatalf("unknown flag did not report a shared CLI usage error:\n%s", out)
	}
}

func TestExitCodeMapping(t *testing.T) {
	var out bytes.Buffer
	logger := testLogger(t, "ai", &out)
	if code := exitCodeFor(logger, nil); code != 0 {
		t.Fatalf("exitCodeFor(nil) = %d, want 0", code)
	}
	if code := exitCodeFor(logger, errors.New("plain failure")); code != 1 {
		t.Fatalf("exitCodeFor(generic) = %d, want 1", code)
	}
}

// DHF-TEST: keel/requirement-18
func TestExitCodeForRedactsUsageErrorBeforeInjectedHandlers(t *testing.T) {
	var out bytes.Buffer
	logger, err := logging.New(logging.Config{
		Service:  "keel-demo-test",
		Console:  logging.ConsoleNone,
		Handlers: []slog.Handler{slog.NewJSONHandler(&out, nil)},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	code := exitCodeFor(logger, cli.NewUsageError("bad token Bearer usage-error-token"))
	if code != 2 {
		t.Fatalf("exitCodeFor(usage) = %d, want 2", code)
	}
	rendered := out.String()
	if strings.Contains(rendered, "usage-error-token") {
		t.Fatalf("usage error leaked raw secret:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Bearer [REDACTED]") {
		t.Fatalf("usage error output missing redaction marker:\n%s", rendered)
	}
}

func runDemo(t *testing.T, args ...string) (string, int) {
	t.Helper()

	exe := filepath.Join(t.TempDir(), "keel-demo")
	build := exec.Command("go", "build", "-o", exe, ".")
	var buildOut bytes.Buffer
	build.Stdout = &buildOut
	build.Stderr = &buildOut
	if err := build.Run(); err != nil {
		t.Fatalf("go build failed: %v\noutput:\n%s", err, buildOut.String())
	}

	cmd := exec.Command(exe, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err == nil {
		return out.String(), 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return out.String(), exitErr.ExitCode()
	}
	t.Fatalf("keel-demo failed before process exit: %v\noutput:\n%s", err, out.String())
	return "", -1
}

func testLogger(t *testing.T, mode string, out *bytes.Buffer) *logging.Logger {
	t.Helper()
	parsed, err := cli.ParseMode(mode)
	if err != nil {
		t.Fatalf("ParseMode(%q): %v", mode, err)
	}
	logger, err := logging.New(logging.Config{
		Service:          "keel-demo-test",
		ConsoleVerbosity: slog.LevelDebug,
		Console:          consoleForSharedMode(parsed),
		Writer:           out,
		TextDir:          t.TempDir(),
		JSONLDir:         t.TempDir(),
		PerRun:           true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	return logger
}

func assertEveryLineIsJSON(t *testing.T, out string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("line is not JSON: %q\nerr: %v\noutput:\n%s", line, err, out)
		}
	}
}

func assertSparseAIEvents(t *testing.T, out string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var payload struct {
			Event  string         `json:"event"`
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("AI mode line is not JSON: %q\nerr: %v\noutput:\n%s", line, err, out)
		}
		if payload.Event == "" || payload.Fields == nil {
			t.Fatalf("AI mode line lacks sparse event shape: %q\noutput:\n%s", line, out)
		}
	}
}
