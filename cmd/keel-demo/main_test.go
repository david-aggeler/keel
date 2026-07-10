package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

// DHF-TEST: keel/requirement-26
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
func TestShowHelpDirectCoversTopLevelAndNestedModes(t *testing.T) {
	for _, mode := range []string{"human", "ai", "json"} {
		t.Run(mode, func(t *testing.T) {
			var top bytes.Buffer
			topLogger := testLogger(t, mode, &top)
			if code := showHelp(topLogger, cliConfig{mode: mode, help: true}); code != 0 {
				t.Fatalf("showHelp top-level returned %d, want 0", code)
			}
			if !strings.Contains(top.String(), "workflow inspect") {
				t.Fatalf("top-level help missing nested command:\n%s", top.String())
			}

			var nested bytes.Buffer
			nestedLogger := testLogger(t, mode, &nested)
			if code := showHelp(nestedLogger, cliConfig{mode: mode, help: true, args: []string{"workflow"}}); code != 0 {
				t.Fatalf("showHelp nested returned %d, want 0", code)
			}
			if !strings.Contains(nested.String(), "workflow replay") {
				t.Fatalf("nested help missing subcommand:\n%s", nested.String())
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

func TestParseArgsAndExitMapping(t *testing.T) {
	cfg, err := parseArgs([]string{"--mode", "json", "workflow", "--help"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cfg.mode != "json" || !cfg.help || len(cfg.args) != 1 || cfg.args[0] != "workflow" {
		t.Fatalf("parseArgs returned unexpected config: %+v", cfg)
	}
	if _, err := parseArgs([]string{"--mode", "bogus"}); err == nil {
		t.Fatal("parseArgs accepted an unknown mode")
	}
	if _, err := parseArgs([]string{"--unknown"}); err == nil {
		t.Fatal("parseArgs accepted an unknown flag")
	}

	var out bytes.Buffer
	logger := testLogger(t, "ai", &out)
	if code := exitFor(logger, nil); code != 0 {
		t.Fatalf("exitFor(nil) = %d, want 0", code)
	}
	if code := exitFor(logger, usageError("bad usage")); code != 2 {
		t.Fatalf("exitFor(usageError) = %d, want 2", code)
	}
	if code := exitFor(logger, errors.New("plain failure")); code != 1 {
		t.Fatalf("exitFor(generic) = %d, want 1", code)
	}
}

func TestModuleRootHelpers(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot returned error: %v", err)
	}
	if !filepath.IsAbs(root) {
		t.Fatalf("findModuleRoot returned non-absolute root %q", root)
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("findModuleRoot returned root without readable go.mod %q: %v", root, err)
	}
	if !declaresKeel(string(data)) {
		t.Fatalf("findModuleRoot returned non-keel module root %q", root)
	}
	if !declaresKeel("module github.com/david-aggeler/keel\n") {
		t.Fatal("declaresKeel rejected the keel module")
	}
	if declaresKeel("module example.com/other\n") {
		t.Fatal("declaresKeel accepted a foreign module")
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
	console, err := consoleForMode(mode)
	if err != nil {
		t.Fatalf("consoleForMode(%q): %v", mode, err)
	}
	logger := logging.New(logging.Config{
		Service:  "keel-demo-test",
		Level:    slog.LevelDebug,
		Console:  console,
		Writer:   out,
		TextDir:  t.TempDir(),
		JSONLDir: t.TempDir(),
		PerRun:   true,
	})
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
