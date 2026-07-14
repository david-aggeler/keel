package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/david-aggeler/keel/cli"
)

const vsixCoverageFloorPercent = 76.3

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

// DHF-REQ: keel/requirement-40, keel/requirement-76
func runVSIXGate(ctx context.Context, logger *slog.Logger, dir string) error {
	for _, tool := range []string{"node", "pnpm", "xvfb-run"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("keel-dev vsix ci: required tool %q not found on PATH", tool)
		}
	}
	if err := runStep(ctx, logger, dir, step{
		name:    "vsix:ci",
		program: "pnpm",
		args:    []string{"--dir", filepath.Join(dir, "vsix"), "run", "ci"},
	}); err != nil {
		return err
	}
	if err := evaluateVSIXCoverageSummary(logger, filepath.Join(dir, "vsix", ".vscode-test", "coverage", "coverage-summary.json")); err != nil {
		return err
	}
	return runStep(ctx, logger, dir, step{
		name:    "vsix:e2e-packaged",
		program: "pnpm",
		args:    []string{"--dir", filepath.Join(dir, "vsix"), "run", "test:e2e:packaged"},
	})
}

// DHF-REQ: keel/requirement-79
func evaluateVSIXCoverageSummary(logger *slog.Logger, path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("vsix coverage summary %s: %w", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("parse vsix coverage summary %s: %w", path, err)
	}
	for name := range raw {
		if name == "total" {
			continue
		}
		slash := filepath.ToSlash(name)
		if strings.HasPrefix(slash, "src/test/") || strings.Contains(slash, "/src/test/") {
			return fmt.Errorf("vsix coverage summary includes excluded test fixture %s", name)
		}
	}

	var summary struct {
		Total struct {
			Statements struct {
				Pct *float64 `json:"pct"`
			} `json:"statements"`
		} `json:"total"`
	}
	if err := json.Unmarshal(body, &summary); err != nil {
		return fmt.Errorf("parse vsix coverage total %s: %w", path, err)
	}
	if summary.Total.Statements.Pct == nil {
		return fmt.Errorf("vsix coverage summary %s has no total statement coverage", path)
	}
	total := *summary.Total.Statements.Pct
	logger.Info("total statement coverage", "percent", total, "floor", vsixCoverageFloorPercent)
	if total < vsixCoverageFloorPercent {
		return fmt.Errorf("total statement coverage %.1f%% is below the %.1f%% floor (keel/requirement-79)", total, vsixCoverageFloorPercent)
	}
	return nil
}
