// Command keel-dev is keel's single first-party CLI: the deterministic
// verification gate (`ci`) and the scripted release loop (`release`).
//
// keel-dev is also keel's first consumer — every line of run output flows
// through keel/log and every subprocess it launches goes through keel/exec,
// so the library's own ergonomics are felt on every invocation.
//
// DHF-REQ: keel/requirement-11
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/david-aggeler/keel/cli"
	logging "github.com/david-aggeler/keel/log"
)

// version is stamped via -ldflags "-X main.version=vX.Y.Z"; "dev" otherwise.
// The git commit is resolved from build info by keel/log.
var version = ""

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses flags (position-independent), builds the keel/log logger, and
// dispatches the verb. It returns the process exit code. Kept separate from
// main so tests can drive the whole CLI surface.
func run(argv []string) int {
	tree := commandTree()
	cfg, words, err := cli.ParseGlobalConfig(argv)
	mode := string(cfg.Mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keel-dev: "+err.Error())
		fmt.Fprintln(os.Stderr)
		printUsage(tree)
		return 2
	}

	if cfg.Version {
		fmt.Fprintln(os.Stdout, versionString())
		return 0
	}
	if cfg.Help && len(words) == 0 {
		printUsage(tree)
		return 0
	}
	if len(words) > 0 && words[0] == "help" {
		tree.RenderTopicHelp(os.Stderr, words[1:])
		return 0
	}
	if cfg.Help {
		tree.RenderTopicHelp(os.Stderr, words)
		return 0
	}
	if len(words) == 0 {
		printUsage(tree)
		return 2
	}

	// Every verb operates on the keel module root, never on whatever directory
	// keel-dev happens to be invoked from. Resolved before the logger so the
	// .logs sinks anchor at the root too.
	root, err := findModuleRoot(".")
	if err != nil {
		return exitFor(newLogger(mode, slog.LevelInfo, os.Stdout), err)
	}

	level := slog.LevelInfo
	if cfg.Verbose {
		level = slog.LevelDebug
	}
	// DHF-REQ: keel/requirement-38 — vscode verbs keep stdout pure protocol:
	// the console sink routes to stderr while both file sinks stay on.
	consoleWriter := io.Writer(os.Stdout)
	if len(words) > 0 && words[0] == "vscode" {
		consoleWriter = os.Stderr
	}
	logger, closeSinks, err := buildLogger(mode, level, filepath.Join(root, ".logs"), consoleWriter)
	if err != nil {
		return exitFor(newLogger(mode, level, os.Stdout), err)
	}
	defer closeSinks()

	// DHF-REQ: keel/requirement-11 — human-mode banner + build identity through
	// keel/log's own presentation surface (Header, LogBuildIdentity).
	if !cfg.NoHeader {
		logger.Header("keel-dev "+words[0], version)
		logger.LogBuildIdentity(version, "")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slogLogger := logger.Slog()
	return exitFor(slogLogger, tree.Dispatch(withRunState(ctx, slogLogger, logger, root), words))
}

// printUsage writes the static help text. Help is documentation, not run
// output; run output (gate progress, results, errors) flows through keel/log.
func printUsage(tree *cli.CommandSpec) {
	tree.RenderRootHelp(os.Stderr)
}

// newProtocolStream is the single allowlisted VS Code protocol JSONL writer —
// the only non-logger os.Stdout reference the no-raw-stdout-stream lint admits.
//
// DHF-REQ: keel/requirement-38
func newProtocolStream() io.Writer {
	return os.Stdout
}

// buildLogger builds keel-dev's three-sink logger from keel/log:
//
//  1. console on stdout — human by default; sparse-AI or JSON via --mode;
//  2. daily human-readable .log under logDir;
//  3. per-run JSON Lines .jsonl under logDir.
//
// The returned closer releases both file handlers; call it once at exit.
// DHF-REQ: keel/requirement-11, keel/requirement-19, keel/requirement-25, keel/requirement-29
func buildLogger(mode string, level slog.Leveler, logDir string, writer io.Writer) (*logging.Logger, func(), error) {
	cfg := loggerConfig(level)
	cfg.Console, _ = consoleForMode(mode)
	cfg.Writer = writer
	cfg.TextDir = logDir
	cfg.JSONLDir = logDir
	cfg.PerRun = true
	logger, err := logging.New(cfg)
	if err != nil {
		return nil, nil, err
	}
	return logger, func() { _ = logger.Close() }, nil
}

// newLogger builds a console-only keel/log logger (bootstrap path, before the
// module root — and thus the .logs directory — is known).
func newLogger(mode string, level slog.Leveler, writer io.Writer) *slog.Logger {
	cfg := loggerConfig(level)
	cfg.Console, _ = consoleForMode(mode)
	cfg.Writer = writer
	logger, err := logging.New(cfg)
	if err != nil {
		return slog.New(slog.NewTextHandler(writer, nil))
	}
	return logger.Slog()
}

// DHF-REQ: keel/requirement-25
func consoleForMode(mode string) (logging.Console, error) {
	parsed, err := cli.ParseMode(mode)
	if err != nil {
		return "", err
	}
	switch parsed {
	case cli.ModeHuman:
		return logging.ConsolePlain, nil
	case cli.ModeAI:
		return logging.ConsoleSparseAI, nil
	case cli.ModeJSON:
		return logging.ConsoleJSON, nil
	default:
		return "", fmt.Errorf("unknown --mode %q: expected human, ai, or json", mode)
	}
}

// loggerConfig is keel-dev's base logger config. The service attr is
// suppressed on the human console only (keel/log ConsoleOmitKeys, keel/issue-3)
// — a single-service CLI repeating service=keel-dev per line is noise. JSON
// mode and both .logs file sinks keep the field.
func loggerConfig(level slog.Leveler) logging.Config {
	return logging.Config{
		Service:          "keel-dev",
		ConsoleVerbosity: level,
		Writer:           os.Stdout,
		ConsoleOmitKeys:  []string{"service"},
	}
}

// exitFor maps a verb's error to a process exit code, logging the failure
// through keel/log so nothing is surfaced via a raw fmt fallback.
//
// DHF-REQ: keel/requirement-18
func exitFor(logger *slog.Logger, err error) int {
	if err != nil {
		var usage cli.UsageError
		if errors.As(err, &usage) {
			logger.Error("keel-dev failed", "error", logging.RedactErr(err).Error())
			return usage.ExitCode()
		}
		var opErr *logging.OperationalError
		if errors.As(err, &opErr) {
			logger.Error("keel-dev failed", slog.Any("err", opErr))
			if opErr.ExitCode != 0 {
				return opErr.ExitCode
			}
		} else {
			logger.Error("keel-dev failed", "error", logging.RedactErr(err).Error())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}
