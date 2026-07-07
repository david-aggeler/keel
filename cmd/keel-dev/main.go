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
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	logging "github.com/david-aggeler/keel/log"
)

const usage = `keel-dev — keel's development CLI

Usage:
  keel-dev [flags] <verb> [args]

Verbs:
  ci                 Run the verification gate: gofmt, go build, go vet, go test.
  release vX.Y.Z     Cut a release: preflight (clean tree + green ci) -> annotated
                     tag -> GitHub release -> anonymous go-get verification.
  verify vX.Y.Z      Re-verify (with retry) that an existing tag resolves
                     anonymously via the default Go toolchain. Tag-CI entrypoint.
  help               Show this help.

Flags:
  --json             Emit machine-readable JSON logs instead of the human console.
  -v, --verbose      Include debug-level detail (child stdout, per-step timing).

Record operations (issue, CR, requirement) are not handled here: use
openbrain-client from PATH.`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses global flags, builds the keel/log logger, and dispatches the verb.
// It returns the process exit code. Kept separate from main so tests can drive
// the whole CLI surface.
func run(argv []string) int {
	var (
		jsonMode bool
		verbose  bool
		rest     []string
	)
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--json":
			jsonMode = true
		case "-v", "--verbose":
			verbose = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, usage)
			return 0
		default:
			rest = argv[i:]
			i = len(argv)
		}
	}

	if len(rest) == 0 {
		// Static help text is not run output; run output flows through keel/log.
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	logger := newLogger(jsonMode, level)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	verb, args := rest[0], rest[1:]
	switch verb {
	case "ci":
		return exitFor(logger, runCI(ctx, logger, "."))
	case "release":
		if len(args) != 1 {
			logger.Error("release requires exactly one argument: the semver tag (e.g. v0.1.0)")
			return 2
		}
		return exitFor(logger, runRelease(ctx, logger, ".", args[0]))
	case "verify":
		if len(args) != 1 {
			logger.Error("verify requires exactly one argument: the semver tag (e.g. v0.1.0)")
			return 2
		}
		return exitFor(logger, runVerify(ctx, logger, args[0]))
	case "help":
		fmt.Fprintln(os.Stderr, usage)
		return 0
	default:
		logger.Error("unknown verb", "verb", verb)
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
}

// newLogger builds keel-dev's logger from keel/log — the human console handler
// by default, or the G1 JSON handler when --json is set.
func newLogger(jsonMode bool, level slog.Level) *slog.Logger {
	cfg := logging.Config{Service: "keel-dev", Level: level, Writer: os.Stdout}
	if jsonMode {
		return logging.New(cfg)
	}
	return logging.NewConsole(cfg)
}

// exitFor maps a verb's error to a process exit code, logging the failure
// through keel/log so nothing is surfaced via a raw fmt fallback.
func exitFor(logger *slog.Logger, err error) int {
	if err != nil {
		logger.Error("keel-dev failed", "error", logging.RedactErr(err).Error())
		return 1
	}
	return 0
}
