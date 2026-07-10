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
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
  --json             Emit machine-readable JSON logs instead of the human console.
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
		jsonMode bool
		verbose  bool
		words    []string
	)
	for _, arg := range argv {
		switch arg {
		case "--json":
			jsonMode = true
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
		return exitFor(newLogger(jsonMode, slog.LevelInfo), err)
	}

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	logger, closeSinks := buildLogger(jsonMode, level, filepath.Join(root, ".logs"))
	defer closeSinks()

	// DHF-REQ: keel/requirement-11 — human-mode banner + build identity through
	// keel/log's own presentation surface (Header, LogBuildIdentity).
	logging.Header(logger, "keel-dev "+verb, version)
	logging.LogBuildIdentity(logger, version, "")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch verb {
	case "ci":
		if len(args) != 0 {
			logger.Error("ci takes no arguments", "got", fmt.Sprintf("%q", args))
			return 2
		}
		return exitFor(logger, runCI(ctx, logger, root))
	case "release":
		if len(args) != 1 {
			logger.Error("release requires exactly one argument: the semver tag (e.g. v0.1.0)")
			return 2
		}
		return exitFor(logger, runRelease(ctx, logger, root, args[0]))
	case "verify":
		if len(args) != 1 {
			logger.Error("verify requires exactly one argument: the semver tag (e.g. v0.1.0)")
			return 2
		}
		return exitFor(logger, runVerify(ctx, logger, args[0]))
	default:
		logger.Error("unknown verb", "verb", verb)
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
//  1. console on stdout — human handler by default, G1 JSON with --json;
//  2. daily human-readable .log under logDir;
//  3. daily JSON Lines .jsonl under logDir.
//
// The returned closer releases both file handlers; call it once at exit.
// File-sink open failures degrade to console-only (a gate that cannot write
// its own log file should still gate) — the failure is reported on the logger.
//
// DHF-REQ: keel/requirement-11
func buildLogger(jsonMode bool, level slog.Leveler, logDir string) (*slog.Logger, func()) {
	cfg := loggerConfig(level)
	if jsonMode {
		cfg.Console = logging.ConsoleJSON
	} else {
		cfg.Console = logging.ConsolePlain
	}
	cfg.TextDir = logDir
	cfg.JSONLDir = logDir
	logger := logging.New(cfg)
	return logger.Slog(), func() { _ = logger.Close() }
}

// newLogger builds a console-only keel/log logger (bootstrap path, before the
// module root — and thus the .logs directory — is known).
func newLogger(jsonMode bool, level slog.Leveler) *slog.Logger {
	cfg := loggerConfig(level)
	if jsonMode {
		cfg.Console = logging.ConsoleJSON
	} else {
		cfg.Console = logging.ConsolePlain
	}
	return logging.New(cfg).Slog()
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
		logger.Error("keel-dev failed", "error", logging.RedactErr(err).Error())
		var usage usageError
		if errors.As(err, &usage) {
			return 2
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}
