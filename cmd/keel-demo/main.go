// Command keel-demo is a first-party demonstration binary for visually
// comparing keel/log and keel/exec renderings across console modes.
//
// DHF-REQ: keel/requirement-26
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/david-aggeler/keel/cli"
	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

const version = "demo"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	tree := commandTree()
	cfg, words, err := cli.ParseGlobalConfig(argv)
	mode := cfg.Mode
	if err != nil {
		fmt.Fprintln(os.Stderr, "keel-demo: "+err.Error())
		fmt.Fprintln(os.Stderr)
		tree.RenderRootHelp(os.Stderr)
		return 2
	}
	if len(words) > 0 && words[0] == "help" {
		return renderHelp(tree, mode, words[1:])
	}
	if cfg.Help {
		return renderHelp(tree, mode, words)
	}
	logger, closeLogger, err := buildLogger(mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keel-demo: "+err.Error())
		return 1
	}
	defer closeLogger()
	if len(words) == 0 {
		return exitCodeFor(logger, runShowcase(context.Background(), logger, string(mode)))
	}
	return exitCodeFor(logger, tree.Dispatch(context.Background(), words))
}

// DHF-REQ: keel/requirement-28
func commandTree() *cli.CommandSpec {
	tree := &cli.CommandSpec{
		Name: "keel-demo",
		Config: cli.Config{
			Program:      "keel-demo",
			RootSummary:  "keel-demo runs the log and exec showcase.",
			Usage:        "keel-demo [--mode human|ai|json]",
			HelpUsage:    "keel-demo help [command]",
			CommandUsage: "keel-demo <command> --help",
			GlobalFlags: []cli.FlagSpec{
				{Name: "mode", Value: "human|ai|json", Default: "human", Short: "Console mode."},
			},
			ModeHelp: []string{
				"human renders plain console output.",
				"ai emits sparse AI-readable records.",
				"json emits full JSON log records.",
			},
			Trailing: "Workflow subcommands: workflow inspect, workflow replay. Run keel-demo help workflow for nested command details.",
		},
		Subcommands: []*cli.CommandSpec{
			{
				Name:  "workflow",
				Short: "Parent command with nested help.",
				Subcommands: []*cli.CommandSpec{
					{Name: "inspect", Use: "workflow inspect", Short: "Preview a captured run tree."},
					{Name: "replay", Use: "workflow replay", Short: "Replay a saved demo transcript."},
				},
			},
		},
	}
	tree.InheritConfig()
	return tree
}

// DHF-REQ: keel/requirement-28
func renderHelp(tree *cli.CommandSpec, mode cli.Mode, path []string) int {
	var help bytes.Buffer
	tree.RenderTopicHelp(&help, path)
	if mode == cli.ModeHuman {
		fmt.Fprint(os.Stdout, help.String())
		return 0
	}
	logger, closeLogger, err := buildLogger(mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keel-demo: "+err.Error())
		return 1
	}
	defer closeLogger()
	command := "keel-demo"
	if len(path) > 0 {
		command += " " + strings.Join(path, " ")
	}
	logger.Event("help", "keel-demo help", "command", command, "help", help.String(), "mode", string(mode))
	return 0
}

// DHF-REQ: keel/requirement-29
func buildLogger(mode cli.Mode) (*logging.Logger, func(), error) {
	logger, err := logging.New(logging.Config{
		Service:         "keel-demo",
		Level:           slog.LevelDebug,
		Console:         consoleForSharedMode(mode),
		Writer:          os.Stdout,
		TextDir:         ".logs",
		JSONLDir:        ".logs",
		PerRun:          true,
		ConsoleOmitKeys: []string{"service"},
	})
	if err != nil {
		return nil, nil, err
	}
	return logger, func() { _ = logger.Close() }, nil
}

func consoleForSharedMode(mode cli.Mode) logging.Console {
	switch mode {
	case cli.ModeHuman:
		return logging.ConsolePlain
	case cli.ModeAI:
		return logging.ConsoleSparseAI
	case cli.ModeJSON:
		return logging.ConsoleJSON
	default:
		return logging.ConsolePlain
	}
}

// DHF-REQ: keel/requirement-26
func runShowcase(ctx context.Context, logger *logging.Logger, mode string) error {
	logger.Header("keel-demo showcase", version)
	logger.Section("presentation surfaces")
	logger.Field("mode", mode)
	logger.Fields([]logging.FieldRow{
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
	logger.Emit("demo_metric",
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

func exitCodeFor(logger *logging.Logger, err error) int {
	if err == nil {
		return 0
	}
	if logger == nil {
		logger, closeLogger, buildErr := buildLogger(cli.ModeHuman)
		if buildErr != nil {
			fmt.Fprintln(os.Stderr, "keel-demo: "+buildErr.Error())
			return 1
		}
		defer closeLogger()
		return exitCodeFor(logger, err)
	}
	// DHF-REQ: keel/requirement-18
	var usage cli.UsageError
	if errors.As(err, &usage) {
		logger.Error("keel-demo failed", "error", logging.RedactErr(err).Error())
		return usage.ExitCode()
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
