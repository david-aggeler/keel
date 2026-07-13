package main

import (
	"context"
	"io"
	"log/slog"

	"github.com/david-aggeler/keel/cli"
	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/testbridge"
)

type runState struct {
	logger   *slog.Logger
	runLog   *logging.Logger
	root     string
	protocol io.Writer
}

type runStateKey struct{}

func withRunState(ctx context.Context, logger *slog.Logger, runLog *logging.Logger, root string) context.Context {
	return withRunStateProtocol(ctx, logger, runLog, root, newProtocolStream())
}

func withRunStateProtocol(ctx context.Context, logger *slog.Logger, runLog *logging.Logger, root string, protocol io.Writer) context.Context {
	state := runState{logger: logger, runLog: runLog, root: root, protocol: protocol}
	ctx = context.WithValue(ctx, runStateKey{}, state)
	return testbridge.WithRuntime(ctx, testbridge.Runtime{
		Root:     root,
		Protocol: protocol,
		Log:      logger,
		RunID:    newVSCodeRunID,
	})
}

func stateFrom(ctx context.Context) runState {
	state, _ := ctx.Value(runStateKey{}).(runState)
	return state
}

// DHF-REQ: keel/requirement-21, keel/requirement-57
func commandTree() *cli.CommandSpec {
	tree := &cli.CommandSpec{
		Name: "keel-dev",
		Config: cli.Config{
			Program:      "keel-dev",
			RootSummary:  "keel-dev is keel's development CLI.",
			Usage:        "keel-dev [--mode human|ai|json] [--no-header] [-v|--verbose] <command> [args]",
			HelpUsage:    "keel-dev help [command]",
			CommandUsage: "keel-dev <command> --help",
			GlobalFlags: []cli.FlagSpec{
				{Name: "mode", Value: "human|ai|json", Default: "human", Short: "Console mode."},
				{Name: "no-header", Short: "Suppress the run header for machine protocol consumers."},
				{Name: "verbose", Short: "Include debug-level detail."},
				{Name: "help-all", Short: "Print root help plus every command topic and exit."},
				{Name: "version", Short: "Print version and exit."},
			},
			ModeHelp: []string{
				"human renders plain console output.",
				"ai emits sparse AI-readable records.",
				"json emits full JSON log records.",
			},
			Trailing: "Run keel-dev help <command> for command details.",
		},
		Subcommands: []*cli.CommandSpec{
			{Name: "ci", Use: "ci", Short: "Run the verification gate: gofmt, build, vet, lint, test.", Handler: handleCI},
			{Name: "release", Use: "release vX.Y.Z", Short: "Cut a release after a clean preflight.", Handler: handleRelease},
			{Name: "verify", Use: "verify vX.Y.Z", Short: "Re-verify anonymous module fetch for an existing tag.", Handler: handleVerify},
			testBridgeCommandSpec(),
			vscodeCommandSpec(),
			vsixCommandSpec(),
		},
	}
	tree.InheritConfig()
	return tree
}

func handleCI(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	if len(args) != 0 {
		return cli.NewUsageError("ci takes no arguments: got %q", args)
	}
	return runCIWithRunLog(ctx, state.logger, state.runLog, state.root)
}

func handleRelease(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	if len(args) != 1 {
		return cli.NewUsageError("release requires exactly one argument: the semver tag (e.g. v0.1.0)")
	}
	return runRelease(ctx, state.logger, state.root, args[0])
}

func handleVerify(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	if len(args) != 1 {
		return cli.NewUsageError("verify requires exactly one argument: the semver tag (e.g. v0.1.0)")
	}
	return runVerify(ctx, state.logger, args[0])
}

func versionString() string {
	if version == "" {
		return "dev"
	}
	return version
}
