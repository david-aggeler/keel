// Command keel-demo is a first-party demonstration binary for visually
// comparing keel/log and keel/exec renderings across console modes.
//
// DHF-REQ: keel/requirement-26
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

const version = "demo"

type cliConfig struct {
	mode string
	help bool
	args []string
}

type usageError string

func (e usageError) Error() string { return string(e) }

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	cfg, err := parseArgs(argv)
	if err != nil {
		logger, closeLogger := buildLogger("human")
		defer closeLogger()
		logger.Error("keel-demo failed", "error", err.Error())
		return 2
	}

	logger, closeLogger := buildLogger(cfg.mode)
	defer closeLogger()

	if cfg.help {
		return showHelp(logger, cfg)
	}
	if len(cfg.args) > 0 {
		return exitFor(logger, usageError(fmt.Sprintf("unknown keel-demo command %q", cfg.args[0])))
	}
	return exitFor(logger, runShowcase(context.Background(), logger, cfg.mode))
}

func parseArgs(argv []string) (cliConfig, error) {
	cfg := cliConfig{mode: "human"}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "--mode":
			if i+1 >= len(argv) {
				return cfg, usageError("--mode requires one of: human, ai, json")
			}
			i++
			cfg.mode = argv[i]
		case "-h", "--help", "help":
			cfg.help = true
		default:
			if strings.HasPrefix(arg, "-") {
				return cfg, usageError(fmt.Sprintf("unknown flag %q", arg))
			}
			cfg.args = append(cfg.args, arg)
		}
	}
	if _, err := consoleForMode(cfg.mode); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func buildLogger(mode string) (*logging.Logger, func()) {
	console, err := consoleForMode(mode)
	if err != nil {
		console = logging.ConsolePlain
	}
	logDir := ".logs"
	if root, err := findModuleRoot("."); err == nil {
		logDir = filepath.Join(root, ".logs")
	}
	logger := logging.New(logging.Config{
		Service:         "keel-demo",
		Level:           slog.LevelDebug,
		Console:         console,
		Writer:          os.Stdout,
		TextDir:         logDir,
		JSONLDir:        logDir,
		PerRun:          true,
		ConsoleOmitKeys: []string{"service"},
	})
	return logger, func() { _ = logger.Close() }
}

func consoleForMode(mode string) (logging.Console, error) {
	switch strings.ToLower(mode) {
	case "human":
		return logging.ConsolePlain, nil
	case "ai":
		return logging.ConsoleSparseAI, nil
	case "json":
		return logging.ConsoleJSON, nil
	default:
		return "", fmt.Errorf("unknown --mode %q: expected human, ai, or json", mode)
	}
}

// DHF-REQ: keel/requirement-26
func showHelp(logger *logging.Logger, cfg cliConfig) int {
	slogLogger := logger.Slog()
	if len(cfg.args) > 0 && cfg.args[0] == "workflow" {
		logging.Header(slogLogger, "keel-demo workflow help", version)
		logging.Section(slogLogger, "workflow")
		logging.Fields(slogLogger, []logging.FieldRow{
			{Label: "workflow inspect", Value: "preview a captured run tree"},
			{Label: "workflow replay", Value: "replay a saved demo transcript"},
		})
		slogLogger.Info("nested command help",
			"event_type", "help",
			"command", "workflow",
			"subcommands", []string{"inspect", "replay"},
			"mode", cfg.mode,
		)
		return 0
	}

	logging.Header(slogLogger, "keel-demo help", version)
	logging.Section(slogLogger, "commands")
	logging.Fields(slogLogger, []logging.FieldRow{
		{Label: "keel-demo", Value: "run the log and exec showcase"},
		{Label: "workflow", Value: "parent command with nested help"},
		{Label: "workflow inspect", Value: "preview a captured run tree"},
		{Label: "workflow replay", Value: "replay a saved demo transcript"},
	})
	slogLogger.Info("top-level command help",
		"event_type", "help",
		"command", "keel-demo",
		"subcommands", []string{"workflow", "workflow inspect", "workflow replay"},
		"mode", cfg.mode,
	)
	return 0
}

// DHF-REQ: keel/requirement-26
func runShowcase(ctx context.Context, logger *logging.Logger, mode string) error {
	slogLogger := logger.Slog()
	logging.Header(slogLogger, "keel-demo showcase", version)
	logging.Section(slogLogger, "presentation surfaces")
	logging.Field(slogLogger, "mode", mode)
	logging.Fields(slogLogger, []logging.FieldRow{
		{Label: "surface_count", Value: 9},
		{Label: "secret", Value: "Bearer demo-secret-token"},
	})
	logger.Event("demo_step", "starting demo step", "mode", mode)

	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program:       "sh",
		Args:          []string{"-c", "printf 'child stdout line\\n'; printf 'child stderr line\\n' >&2", "demo-secret-token"},
		Logger:        logger,
		SensitiveArgs: map[int]bool{2: true},
	})
	if err != nil {
		return err
	}
	result, waitErr := proc.Wait()
	if waitErr != nil {
		return waitErr
	}
	logger.Event("demo_success", "subprocess completed",
		"stdout_bytes", len(result.Stdout),
		"stderr_bytes", len(result.Stderr),
	)
	logging.Emit(slogLogger, "demo_metric",
		slog.String("mode", mode),
		slog.Int("surface_count", 9),
	)

	startLine := logger.RunLogLine() + 1
	opErr := &logging.OperationalError{
		Op:        "keel-demo",
		Message:   "structured failure",
		Err:       errors.New("demo failure with Bearer demo-secret-token"),
		Task:      "showcase",
		LogFile:   logger.RunLogPath(),
		StartLine: startLine,
		ExitCode:  4,
		Hint:      fmt.Sprintf("inspect %s from line %d", logger.RunLogPath(), startLine),
		Metadata: map[string]any{
			"mode":          mode,
			"secret_detail": "Bearer demo-secret-token",
		},
	}
	logger.Event("demo_failed", "structured failure", slog.Any("err", opErr))
	return opErr
}

func exitFor(logger *logging.Logger, err error) int {
	if err == nil {
		return 0
	}
	if logger == nil {
		logger, closeLogger := buildLogger("human")
		defer closeLogger()
		return exitFor(logger, err)
	}
	var usage usageError
	if errors.As(err, &usage) {
		logger.Error("keel-demo failed", "error", err.Error())
		return 2
	}
	var opErr *logging.OperationalError
	if errors.As(err, &opErr) {
		logger.Error("keel-demo failed", slog.Any("err", opErr))
		if opErr.ExitCode != 0 {
			return opErr.ExitCode
		}
		return 1
	}
	logger.Error("keel-demo failed", "error", logging.RedactErr(err).Error())
	return 1
}

func findModuleRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	start := dir
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			if declaresKeel(string(data)) {
				return dir, nil
			}
			return "", fmt.Errorf("go.mod at %s does not declare module github.com/david-aggeler/keel", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found walking up from %s", start)
		}
		dir = parent
	}
}

func declaresKeel(gomod string) bool {
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")) == "github.com/david-aggeler/keel"
		}
	}
	return false
}
