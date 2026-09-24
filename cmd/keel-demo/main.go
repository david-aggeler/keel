// Command keel-demo is a first-party demonstration binary for visually
// comparing keel/log and keel/exec renderings across console modes.
//
// DHF-REQ: keel/requirement-26
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	keel "github.com/david-aggeler/keel"
	"github.com/david-aggeler/keel/cli"
	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/term"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	tree := commandTree()
	if err := tree.ValidateTree(); err != nil {
		return bootstrapFailure(err, 1)
	}
	// DHF-REQ: keel/requirement-172 — keel/cli serves every help output and
	// usage error itself; keel-demo only returns the code it reports.
	cfg, words, code, done := tree.Start(argv)
	if done {
		return code
	}
	logger, closeLogger, err := buildLogger(cfg)
	if err != nil {
		return bootstrapFailure(err, 1)
	}
	defer closeLogger()
	// A bare invocation dispatches to the root handler, the showcase.
	ctx := withMode(withLogger(context.Background(), logger), string(cfg.Mode))
	return exitCodeFor(logger, tree.Dispatch(ctx, words))
}

// bootstrapFailure reports a failure that precedes the logger — an invalid
// command tree or a logger that cannot be built — on stderr, and returns code.
// It is the one place keel-demo writes a diagnostic without keel/log, because
// at that point there is no logger.
//
// DHF-REQ: keel/requirement-164
func bootstrapFailure(err error, code int) int {
	_, _ = io.WriteString(os.Stderr, "keel-demo: "+err.Error()+"\n")
	return code
}

// newPayloadStream is the stdout writer handed to a verb that declares a
// payload. keel/log never writes here: every log record goes to stderr through
// the CLI profile, so a consumer reading stdout sees the result and nothing else.
//
// DHF-REQ: keel/requirement-164
func newPayloadStream() io.Writer {
	return os.Stdout
}

// payloadHandler is a verb handler that writes a result. payload is the
// declared stdout stream; the logger in ctx carries every diagnostic.
type payloadHandler func(ctx context.Context, args []string, payload io.Writer) error

// declarePayload is how a keel-demo verb states that it writes to stdout. A
// verb built without it has no stdout writer to reach for.
//
// DHF-REQ: keel/requirement-164
func declarePayload(h payloadHandler) cli.Handler {
	return func(ctx context.Context, args []string) error {
		return h(ctx, args, newPayloadStream())
	}
}

type loggerKey struct{}

type modeKey struct{}

func withMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, modeKey{}, mode)
}

// modeFrom returns the invocation's console mode name, or "" when none was
// threaded.
func modeFrom(ctx context.Context) string {
	mode, _ := ctx.Value(modeKey{}).(string)
	return mode
}

func withLogger(ctx context.Context, logger *logging.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// loggerFrom returns the invocation's logger. A verb dispatched without one is
// a wiring defect and fails rather than dropping its diagnostics.
func loggerFrom(ctx context.Context) (*logging.Logger, error) {
	if logger, ok := ctx.Value(loggerKey{}).(*logging.Logger); ok && logger != nil {
		return logger, nil
	}
	return nil, errors.New("keel-demo: verb dispatched without a logger")
}

// DHF-REQ: keel/requirement-28, keel/requirement-57, keel/requirement-108, keel/requirement-111
func commandTree() *cli.CommandSpec {
	var inspectFormat string
	var replaySpeed string
	tree := &cli.CommandSpec{
		Name: "keel-demo",
		Config: cli.Config{
			Program:      "keel-demo",
			Version:      versionString(),
			RootSummary:  "keel-demo runs the log and exec showcase.",
			Usage:        "keel-demo [--mode human|ai|json]",
			HelpUsage:    "keel-demo help [command]",
			CommandUsage: "keel-demo <command> --help",
			// DHF-REQ: keel/requirement-100 — the showcase is the root's own
			// action, so --help-json lists the root as invocable.
			RootHandler: handleShowcase,
			// keel/cli owns and renders the shared global flags and the --mode
			// output-mode description (keel/requirement-101); keel-demo declares no
			// additional globals, so GlobalFlags/ModeHelp are left empty.
			Trailing: "Workflow subcommands: workflow inspect, workflow replay. Run keel-demo help workflow for nested command details.",
		},
		Subcommands: []*cli.CommandSpec{
			{
				Name:  "workflow",
				Short: "Parent command with nested help.",
				Subcommands: []*cli.CommandSpec{
					{
						Name:        "inspect",
						Use:         "workflow inspect [--format text|json] <run-id>",
						Short:       "Preview a captured run tree.",
						Positionals: []cli.PositionalSpec{{Name: "run-id", Min: 1, Max: 1}},
						Flags: []cli.FlagSpec{
							{Name: "format", Value: "text|json", Default: "text", Enum: []string{"text", "json"}, Short: "Preview format.", StringTarget: &inspectFormat},
						},
						Handler: declarePayload(handleWorkflowInspect(&inspectFormat)),
					},
					{
						Name:        "replay",
						Use:         "workflow replay [--speed normal|fast] <transcript>",
						Short:       "Replay a saved demo transcript.",
						Positionals: []cli.PositionalSpec{{Name: "transcript", Min: 1, Max: 1}},
						Flags: []cli.FlagSpec{
							{Name: "speed", Value: "normal|fast", Default: "normal", Enum: []string{"normal", "fast"}, Short: "Replay pacing.", StringTarget: &replaySpeed},
						},
						Handler: declarePayload(handleWorkflowReplay(&replaySpeed)),
					},
				},
			},
		},
	}
	tree.InheritConfig()
	return tree
}

// handleShowcase runs the showcase for a bare keel-demo invocation.
func handleShowcase(ctx context.Context, _ []string) error {
	logger, err := loggerFrom(ctx)
	if err != nil {
		return err
	}
	return runShowcase(ctx, logger, modeFrom(ctx))
}

// handleWorkflowInspect logs what it previews and writes the result line to
// the declared payload stream — two output kinds, two paths.
//
// DHF-REQ: keel/requirement-108, keel/requirement-164
func handleWorkflowInspect(format *string) payloadHandler {
	return func(ctx context.Context, args []string, payload io.Writer) error {
		logger, err := loggerFrom(ctx)
		if err != nil {
			return err
		}
		logger.Event("workflow_inspect", "previewing captured run", "run_id", args[0], "format", *format)
		_, err = io.WriteString(payload, fmt.Sprintf("workflow inspect run_id=%s format=%s\n", args[0], *format))
		return err
	}
}

// handleWorkflowReplay logs what it replays and writes the result line to the
// declared payload stream.
//
// DHF-REQ: keel/requirement-108, keel/requirement-164
func handleWorkflowReplay(speed *string) payloadHandler {
	return func(ctx context.Context, args []string, payload io.Writer) error {
		logger, err := loggerFrom(ctx)
		if err != nil {
			return err
		}
		logger.Event("workflow_replay", "replaying transcript", "transcript", args[0], "speed", *speed)
		_, err = io.WriteString(payload, fmt.Sprintf("workflow replay transcript=%s speed=%s\n", args[0], *speed))
		return err
	}
}

// DHF-REQ: keel/requirement-29
func buildLogger(rt cli.RuntimeConfig) (*logging.Logger, func(), error) {
	return newLogger(loggerConfig(rt))
}

func newLogger(cfg logging.Config) (*logging.Logger, func(), error) {
	logger, err := logging.New(cfg)
	if err != nil {
		return nil, nil, err
	}
	return logger, func() { _ = logger.Close() }, nil
}

// loggerConfig is keel-demo's three-sink logger config for one invocation's
// global flags, amended from the keel/log CLI profile: diagnostics on stderr,
// stdout left to the payload a verb declares. The showcase defaults the console
// to Debug so every rendering is visible; -q raises that floor to Warn on the
// console only, and --color and --plain set the color policy. File sinks keep
// keel/log's own verbosity.
//
// DHF-REQ: keel/requirement-29, keel/requirement-164, keel/requirement-166
func loggerConfig(rt cli.RuntimeConfig) logging.Config {
	color := rt.EffectiveColor()
	cfg := logging.CLIProfile("keel-demo")
	cfg.ConsoleVerbosity = rt.ConsoleLevel(slog.LevelDebug)
	cfg.Console = consoleForSharedMode(rt.Mode)
	cfg.TextDir = ".logs"
	cfg.JSONLDir = ".logs"
	cfg.PerRun = true
	cfg.ForceColor = color == term.ColorAlways
	cfg.DisableColor = color == term.ColorNever
	cfg.ConsoleOmitKeys = []string{"service"}
	return cfg
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
	logger.Header("keel-demo showcase", versionString())
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

// DHF-REQ: keel/requirement-110
func versionString() string {
	return keel.Version()
}

func exitCodeFor(logger *logging.Logger, err error) int {
	if err == nil {
		return 0
	}
	if logger == nil {
		logger, closeLogger, buildErr := buildLogger(cli.RuntimeConfig{Mode: cli.ModeHuman})
		if buildErr != nil {
			return bootstrapFailure(buildErr, 1)
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
