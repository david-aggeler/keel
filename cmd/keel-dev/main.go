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

	keel "github.com/david-aggeler/keel"
	"github.com/david-aggeler/keel/cli"
	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/term"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses flags (position-independent), builds the keel/log logger, and
// dispatches the verb. It returns the process exit code. Kept separate from
// main so tests can drive the whole CLI surface.
func run(argv []string) int {
	tree := commandTree()
	// DHF-REQ: keel/requirement-172 — keel/cli serves every help output and
	// usage error itself; keel-dev only returns the code it reports.
	cfg, words, code, done := tree.Start(argv)
	if done {
		return code
	}

	// Every verb operates on the keel module root, never on whatever directory
	// keel-dev happens to be invoked from. Resolved before the logger so the
	// .logs sinks anchor at the root too.
	root, err := findModuleRoot(".")
	if err != nil {
		return exitFor(newLogger(cfg), err)
	}

	// DHF-REQ: keel/requirement-164 — every verb's console sink is stderr; a
	// verb reaches stdout only through the payload it declares.
	logger, closeSinks, err := buildLogger(cfg, filepath.Join(root, ".logs"))
	if err != nil {
		return exitFor(newLogger(cfg), err)
	}
	defer closeSinks()

	// DHF-REQ: keel/requirement-11 — human-mode banner + build identity through
	// keel/log's own presentation surface (Header, LogBuildIdentity).
	if !cfg.NoHeader {
		logger.Header("keel-dev "+words[0], versionString())
		logger.LogBuildIdentity(versionString(), "")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slogLogger := logger.Slog()
	return exitFor(slogLogger, dispatchKeelDev(withRunState(ctx, slogLogger, logger, root), tree, words))
}

// newPayloadStream is the single allowlisted payload writer — the only
// non-logger os.Stdout reference the no-raw-stdout-stream lint admits. A verb
// receives it only by declaring a payload (declarePayload); it carries the VS
// Code test-bridge JSONL stream and the worktree verbs' result lines.
//
// DHF-REQ: keel/requirement-38, keel/requirement-114, keel/requirement-164
func newPayloadStream() io.Writer {
	return os.Stdout
}

// buildLogger builds keel-dev's three-sink logger from keel/log:
//
//  1. console on stderr — human by default; sparse-AI or JSON via --mode;
//  2. daily human-readable .log under logDir;
//  3. per-run JSON Lines .jsonl under logDir.
//
// The returned closer releases both file handlers; call it once at exit.
// DHF-REQ: keel/requirement-11, keel/requirement-19, keel/requirement-25, keel/requirement-29
func buildLogger(rt cli.RuntimeConfig, logDir string) (*logging.Logger, func(), error) {
	return openRunLogger(loggerConfig(rt), logDir)
}

// openRunLogger adds both .logs file sinks under logDir to cfg and builds the
// logger. buildLogger is its only production caller; tests reach it to swap
// the console writer while keeping keel-dev's real config.
func openRunLogger(cfg logging.Config, logDir string) (*logging.Logger, func(), error) {
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
func newLogger(rt cli.RuntimeConfig) *slog.Logger {
	return consoleLogger(loggerConfig(rt))
}

// consoleLogger builds a console-only logger from cfg, falling back to a plain
// slog text handler on cfg's writer if keel/log rejects the config.
func consoleLogger(cfg logging.Config) *slog.Logger {
	logger, err := logging.New(cfg)
	if err != nil {
		return slog.New(slog.NewTextHandler(cfg.Writer, nil))
	}
	return logger.Slog()
}

// DHF-REQ: keel/requirement-110
func versionString() string {
	return keel.Version()
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

// loggerConfig is keel-dev's base logger config for one invocation's global
// flags, amended from the keel/log CLI profile: the console is stderr, stdout
// is left to the payload a verb declares. The service attr is suppressed on the human console only (keel/log
// ConsoleOmitKeys, keel/issue-3) — a single-service CLI repeating
// service=keel-dev per line is noise. JSON mode and both .logs file sinks keep
// the field.
//
// The operator policy flags set the console floor (-q, -v) and the color
// policy (--color, --plain). File sinks keep keel/log's own verbosity: quiet
// is a console floor, never a sink switch.
//
// DHF-REQ: keel/requirement-164, keel/requirement-166
func loggerConfig(rt cli.RuntimeConfig) logging.Config {
	console, _ := consoleForMode(string(rt.Mode))
	color := rt.EffectiveColor()
	cfg := logging.CLIProfile("keel-dev")
	cfg.Console = console
	cfg.ConsoleVerbosity = rt.ConsoleLevel(slog.LevelInfo)
	cfg.ForceColor = color == term.ColorAlways
	cfg.DisableColor = color == term.ColorNever
	cfg.ConsoleOmitKeys = []string{"service"}
	return cfg
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
