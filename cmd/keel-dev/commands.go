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

// DHF-REQ: keel/requirement-21, keel/requirement-57, keel/requirement-65, keel/requirement-107, keel/requirement-111, keel/requirement-114
func commandTree() *cli.CommandSpec {
	tree := &cli.CommandSpec{
		Name: "keel-dev",
		Config: cli.Config{
			Program:      "keel-dev",
			Version:      versionString(),
			RootSummary:  "keel-dev is keel's development CLI.",
			Usage:        "keel-dev [--mode human|ai|json] [--no-header] [-v|--verbose] <command> [args]",
			HelpUsage:    "keel-dev help [command]",
			CommandUsage: "keel-dev <command> --help",
			// keel/cli owns and renders the shared global flags and the --mode
			// output-mode description (keel/requirement-101); keel-dev declares no
			// additional globals, so GlobalFlags/ModeHelp are left empty.
			Trailing: "Run keel-dev help <command> for command details.",
		},
		Subcommands: []*cli.CommandSpec{
			{Name: "ci", Use: "ci", Short: "Run the verification gate: gofmt, build, vet, lint, test.", Positionals: []cli.PositionalSpec{{Name: "args", Min: 0, Max: 0}}, Handler: handleCI},
			gateCommandSpec(),
			{Name: "release", Use: "release vX.Y.Z", Short: "Cut a release after a clean preflight.", Positionals: []cli.PositionalSpec{{Name: "version", Min: 1, Max: 1}}, Handler: handleRelease},
			{Name: "verify", Use: "verify vX.Y.Z", Short: "Re-verify anonymous module fetch for an existing tag.", Positionals: []cli.PositionalSpec{{Name: "version", Min: 1, Max: 1}}, Handler: handleVerify},
			testBridgeCommandSpec(),
			vsixCommandSpec(),
			worktreeCommandSpec(),
		},
	}
	tree.InheritConfig()
	return tree
}

func handleCI(ctx context.Context, _ []string) error {
	state := stateFrom(ctx)
	return runCIWithRunLog(ctx, state.logger, state.runLog, state.root)
}

// gateCommandSpec declares one leaf command per gate stage, derived from the
// battery itself, so the reported command surface and the stage set a run
// executes cannot drift apart (keel/ac-544).
//
// The stages hang off their own namespace rather than under `ci` because
// keel/requirement-106 admits no node that both does work and holds children:
// `ci` keeps the bare battery every transition_gates declaration resolves to
// (keel/ac-542), and `gate <stage>` addresses one stage of it.
//
// DHF-REQ: keel/requirement-136 (keel/ac-541, keel/ac-544)
func gateCommandSpec() *cli.CommandSpec {
	names := gateStageNames()
	stages := make([]*cli.CommandSpec, 0, len(names))
	for _, name := range names {
		stages = append(stages, &cli.CommandSpec{
			Name:        name,
			Use:         "gate " + name,
			Short:       "Run the " + name + " gate stage on its own.",
			Positionals: []cli.PositionalSpec{{Name: "args", Min: 0, Max: 0}},
			Handler:     handleGateStage(name),
		})
	}
	return &cli.CommandSpec{
		Name:        "gate",
		Short:       "Run one stage of the verification gate on its own.",
		Subcommands: stages,
	}
}

func handleGateStage(name string) cli.Handler {
	return func(ctx context.Context, _ []string) error {
		state := stateFrom(ctx)
		return runGateStage(ctx, state.logger, state.runLog, state.root, name)
	}
}

func handleRelease(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	return runRelease(ctx, state.logger, state.root, args[0])
}

func handleVerify(ctx context.Context, args []string) error {
	state := stateFrom(ctx)
	return runVerify(ctx, state.logger, args[0])
}
