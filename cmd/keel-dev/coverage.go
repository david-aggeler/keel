package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// coverageFloorPercent is the ci gate's total-statement-coverage floor
// (keel/ac-37). Raising it is a one-constant change; the owner's stated target
// is ~90% — ratchet as the suite grows.
const coverageFloorPercent = 85.0

// runTestWithCoverage is the ci test step: it runs the full suite with a
// coverage profile, surfaces the per-package results live through keel/log,
// computes the total statement coverage, and fails the gate below the floor.
//
// DHF-REQ: keel/requirement-11 (keel/ac-37)
func runTestWithCoverage(ctx context.Context, logger *slog.Logger, dir string) error {
	tmp, err := os.MkdirTemp("", "keel-cover-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	profile := filepath.Join(tmp, "cover.out")

	if err := runCmd(ctx, logger, dir, "go", "test", "./...", "-coverprofile="+profile, "-covermode=atomic", "-coverpkg=./..."); err != nil {
		return err
	}

	funcOut, stderr, err := capture(ctx, logger, dir, "go", "tool", "cover", "-func="+profile)
	if err != nil {
		return fmt.Errorf("go tool cover: %w: %s", err, strings.TrimSpace(stderr))
	}
	total, err := parseCoverageTotal(funcOut)
	if err != nil {
		return err
	}

	logger.Info("total statement coverage", "percent", total, "floor", coverageFloorPercent)
	if total < coverageFloorPercent {
		return fmt.Errorf("total statement coverage %.1f%% is below the %.1f%% floor (keel/ac-37)", total, coverageFloorPercent)
	}
	return nil
}

// parseCoverageTotal extracts the percentage from `go tool cover -func` output,
// whose last data line reads: "total:  (statements)  NN.N%".
func parseCoverageTotal(funcOutput string) (float64, error) {
	for _, line := range strings.Split(funcOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "total:" {
			continue
		}
		pct := strings.TrimSuffix(fields[len(fields)-1], "%")
		total, err := strconv.ParseFloat(pct, 64)
		if err != nil {
			return 0, fmt.Errorf("parse coverage total %q: %w", pct, err)
		}
		return total, nil
	}
	return 0, fmt.Errorf("no total: line in cover -func output")
}
