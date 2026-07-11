package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"

	"github.com/david-aggeler/keel/cli"
)

func vsixCommandSpec() *cli.CommandSpec {
	return &cli.CommandSpec{
		Name:  "vsix",
		Use:   "vsix ci",
		Short: "Run Keel Test Bridge VSIX checks.",
		Subcommands: []*cli.CommandSpec{
			{Name: "ci", Use: "vsix ci", Short: "Run pnpm build, lint, and headless VSIX tests.", Handler: handleVSIXGate},
		},
	}
}

// DHF-REQ: keel/requirement-40
func handleVSIXGate(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return cli.NewUsageError("vsix ci takes no arguments: got %q", args)
	}
	state := stateFrom(ctx)
	return runVSIXGate(ctx, state.logger, state.root)
}

// DHF-REQ: keel/requirement-40
func runVSIXGate(ctx context.Context, logger *slog.Logger, dir string) error {
	for _, tool := range []string{"node", "pnpm", "xvfb-run"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("keel-dev vsix ci: required tool %q not found on PATH", tool)
		}
	}
	return runStep(ctx, logger, dir, step{
		name:    "vsix:ci",
		program: "pnpm",
		args:    []string{"--dir", filepath.Join(dir, "vsix"), "run", "ci"},
	})
}
