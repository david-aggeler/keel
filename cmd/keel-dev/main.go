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
	"strings"
	"syscall"

	logging "github.com/david-aggeler/keel/log"
)

// version is stamped via -ldflags "-X main.version=vX.Y.Z"; "dev" otherwise.
// The git commit is resolved from build info by keel/log's ResolveGitCommit.
var version = ""

const usage = `keel-dev — keel's development CLI

Usage:
  keel-dev <verb> [args] [flags]

Verbs:
  ci                 Run the verification gate: gofmt, build, vet, lint, test.
  release vX.Y.Z     Cut a release: preflight (clean tree + green ci) -> annotated
                     tag -> GitHub release -> anonymous go-get verification.
  verify vX.Y.Z      Re-verify (with retry) that an existing tag resolves
                     anonymously via the default Go toolchain. Tag-CI entrypoint.
  help               Show this help.

Flags (accepted before or after the verb):
  --mode MODE        Console mode: human, ai, or json. Default: human.
  -v, --verbose      Include debug-level detail (child stdout, per-step timing).`

type usageError string

func (e usageError) Error() string { return string(e) }

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses flags (position-independent), builds the keel/log logger, and
// dispatches the verb. It returns the process exit code. Kept separate from
// main so tests can drive the whole CLI surface.
func run(argv []string) int {
	var (
		mode    = "human"
		verbose bool
		words   []string
	)
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "--mode":
			if i+1 >= len(argv) {
				fmt.Fprintln(os.Stderr, "keel-dev: --mode requires one of: human, ai, json")
				fmt.Fprintln(os.Stderr)
				printUsage()
				return 2
			}
			i++
			mode = argv[i]
		case "-v", "--verbose":
			verbose = true
		case "-h", "--help":
			printUsage()
			return 0
		default:
			if len(arg) > 1 && arg[0] == '-' {
				// Unknown flags are an error, never silently dropped: a gate
				// tool that ignores what it was asked to do is worse than one
				// that refuses.
				fmt.Fprintf(os.Stderr, "keel-dev: unknown flag %q\n\n", arg)
				printUsage()
				return 2
			}
			words = append(words, arg)
		}
	}

	if _, err := consoleForMode(mode); err != nil {
		fmt.Fprintf(os.Stderr, "keel-dev: %v\n\n", err)
		printUsage()
		return 2
	}

	if len(words) == 0 {
		printUsage()
		return 2
	}

	verb, args := words[0], words[1:]
	if verb == "help" {
		printUsage()
		return 0
	}

	// Every verb operates on the keel module root, never on whatever directory
	// keel-dev happens to be invoked from. Resolved before the logger so the
	// .logs sinks anchor at the root too.
	root, err := findModuleRoot(".")
	if err != nil {
		return exitFor(newLogger(mode, slog.LevelInfo, os.Stdout), err)
	}

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	logger, closeSinks := buildLogger(mode, level, filepath.Join(root, ".logs"))
	defer closeSinks()
	slogLogger := logger.Slog()

	// DHF-REQ: keel/requirement-11 — human-mode banner + build identity through
	// keel/log's own presentation surface (Header, LogBuildIdentity).
	logging.Header(slogLogger, "keel-dev "+verb, version)
	logging.LogBuildIdentity(slogLogger, version, "")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch verb {
	case "ci":
		if len(args) != 0 {
			slogLogger.Error("ci takes no arguments", "got", fmt.Sprintf("%q", args))
			return 2
		}
		return exitFor(slogLogger, runCIWithRunLog(ctx, slogLogger, logger, root))
	case "release":
		if len(args) != 1 {
			slogLogger.Error("release requires exactly one argument: the semver tag (e.g. v0.1.0)")
			return 2
		}
		return exitFor(slogLogger, runRelease(ctx, slogLogger, root, args[0]))
	case "verify":
		if len(args) != 1 {
			slogLogger.Error("verify requires exactly one argument: the semver tag (e.g. v0.1.0)")
			return 2
		}
		return exitFor(slogLogger, runVerify(ctx, slogLogger, args[0]))
	default:
		slogLogger.Error("unknown verb", "verb", verb)
		printUsage()
		return 2
	}
}

// printUsage writes the static help text. Help is documentation, not run
// output; run output (gate progress, results, errors) flows through keel/log.
func printUsage() {
	fmt.Fprintln(os.Stderr, usage)
}

// buildLogger builds keel-dev's three-sink logger from keel/log:
//
//  1. console on stdout — human by default; sparse-AI or JSON via --mode;
//  2. daily human-readable .log under logDir;
//  3. per-run JSON Lines .jsonl under logDir.
//
// The returned closer releases both file handlers; call it once at exit.
// File-sink open failures degrade to console-only (a gate that cannot write
// its own log file should still gate) — the failure is reported on the logger.
//
// DHF-REQ: keel/requirement-11, keel/requirement-19, keel/requirement-25
func buildLogger(mode string, level slog.Leveler, logDir string) (*logging.Logger, func()) {
	cfg := loggerConfig(level)
	cfg.Console, _ = consoleForMode(mode)
	cfg.TextDir = logDir
	cfg.JSONLDir = logDir
	cfg.PerRun = true
	logger := logging.New(cfg)
	return logger, func() { _ = logger.Close() }
}

// newLogger builds a console-only keel/log logger (bootstrap path, before the
// module root — and thus the .logs directory — is known).
func newLogger(mode string, level slog.Leveler, writer io.Writer) *slog.Logger {
	cfg := loggerConfig(level)
	cfg.Console, _ = consoleForMode(mode)
	cfg.Writer = writer
	return logging.New(cfg).Slog()
}

// DHF-REQ: keel/requirement-25
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

// loggerConfig is keel-dev's base logger config. The service attr is
// suppressed on the human console only (keel/log ConsoleOmitKeys, keel/issue-3)
// — a single-service CLI repeating service=keel-dev per line is noise. JSON
// mode and both .logs file sinks keep the field.
func loggerConfig(level slog.Leveler) logging.Config {
	return logging.Config{
		Service:         "keel-dev",
		Level:           level,
		Writer:          os.Stdout,
		ConsoleOmitKeys: []string{"service"},
	}
}

// exitFor maps a verb's error to a process exit code, logging the failure
// through keel/log so nothing is surfaced via a raw fmt fallback.
//
// DHF-REQ: keel/requirement-18
func exitFor(logger *slog.Logger, err error) int {
	if err != nil {
		var usage usageError
		if errors.As(err, &usage) {
			logger.Error("keel-dev failed", "error", logging.RedactErr(err).Error())
			return 2
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
