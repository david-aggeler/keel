package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/david-aggeler/keel/vscode"
)

// coverageFloorPercent is the ci gate's total-statement-coverage floor
// (keel/ac-37). Raising it is a one-constant change; the owner's stated target
// is ~90% — ratchet as the suite grows.
//
// DHF-REQ: keel/requirement-11
const coverageFloorPercent = 90.0

// runTestWithCoverage is the ci test step: it runs the git-tracked packages'
// suite with a coverage profile, surfaces the per-package results live through
// keel/log, computes the total statement coverage, and fails the gate below the
// floor.
//
// DHF-REQ: keel/requirement-11 (keel/ac-37), keel/requirement-85 (keel/ac-666)
func runTestWithCoverage(ctx context.Context, logger *slog.Logger, dir string) error {
	tmp, err := os.MkdirTemp("", "keel-cover-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	profile := filepath.Join(tmp, "cover.out")

	packages, err := trackedGoPackages(ctx, logger, dir)
	if err != nil {
		return err
	}
	if err := runCmd(ctx, logger, dir, "go", goTestCoverageArgs(packages, profile)...); err != nil {
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

// runVSCodeTestCoverage is the VS Code coverage-profile lane. It deliberately
// persists the profile under .logs so the run artifact survives handler return;
// runTestWithCoverage keeps using a temp profile for the ci gate. Both select
// packages through trackedGoPackages.
//
// DHF-REQ: keel/requirement-39, keel/requirement-85 (keel/ac-666)
func runVSCodeTestCoverage(ctx context.Context, logger *slog.Logger, root, runID string, maxOutputBytes int, writer vscode.RunEventWriter) error {
	coverRoot := filepath.Join(root, ".logs", "vscode-cover")
	if err := os.MkdirAll(coverRoot, 0o755); err != nil {
		return err
	}
	pruneOldVSCodeCoverageDirs(coverRoot, logger)

	runDir := filepath.Join(coverRoot, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	profile := filepath.Join(runDir, "cover.out")

	packages, err := trackedGoPackages(ctx, logger, root)
	if err != nil {
		return err
	}
	stdout, stderr, err := captureWithMaxOutput(ctx, logger, root, maxOutputBytes, "go", goTestCoverageArgs(packages, profile)...)
	emitVSCodeCoveragePackages(stdout, writer)
	if err != nil {
		return fmt.Errorf("go test coverage: %w: %s", err, strings.TrimSpace(stderr))
	}

	funcOut, stderr, err := captureWithMaxOutput(ctx, logger, root, maxOutputBytes, "go", "tool", "cover", "-func="+profile)
	if err != nil {
		return fmt.Errorf("go tool cover: %w: %s", err, strings.TrimSpace(stderr))
	}
	total, err := parseCoverageTotal(funcOut)
	if err != nil {
		return err
	}

	message := fmt.Sprintf("total statement coverage %.1f%%", total)
	logger.Info("total statement coverage", "percent", total, "floor", coverageFloorPercent)
	writer(vscode.RunEvent{Event: "output", TestID: vscodeLaneTestCoverage, Message: message})
	writer(vscode.RunEvent{
		Event:  "artifact",
		TestID: vscodeLaneTestCoverage,
		Artifact: &vscode.RunArtifact{
			Name: "coverage profile",
			URI:  (&url.URL{Scheme: "file", Path: profile}).String(),
			Kind: "coverage",
		},
	})
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

func emitVSCodeCoveragePackages(goTestOutput string, writer vscode.RunEventWriter) {
	for _, line := range strings.Split(goTestOutput, "\n") {
		pkg, duration, ok := parseGoTestPackageLine(line)
		if !ok {
			continue
		}
		writer(vscode.RunEvent{
			Event:      "passed",
			TestID:     "go::package::" + pkg,
			DurationMS: duration,
		})
	}
}

func parseGoTestPackageLine(line string) (string, int64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", 0, false
	}
	switch fields[0] {
	case "ok":
		if len(fields) < 3 {
			return "", 0, false
		}
		duration, err := time.ParseDuration(fields[2])
		if err != nil {
			return fields[1], 0, true
		}
		return fields[1], duration.Milliseconds(), true
	case "?":
		return fields[1], 0, true
	default:
		return "", 0, false
	}
}

func pruneOldVSCodeCoverageDirs(dir string, logger *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil && logger != nil {
			logger.Warn("remove old vscode coverage profile", "path", path, "error", err.Error())
		}
	}
}

// trackedGoPackages returns the ./-relative package patterns of every directory
// that holds a git-tracked, non-excluded .go file, sorted. It is the test
// stage's package selector: `./...` would also enumerate gitignored and
// untracked directories (scratchpad/), so a scratch package could fail a test
// or dilute the coverage denominator on a clean tracked tree (keel/issue-246).
// Directories the Go toolchain itself never treats as packages (testdata, and
// path segments starting with "." or "_") are skipped, matching `./...`.
//
// DHF-REQ: keel/requirement-85 (keel/ac-666)
func trackedGoPackages(ctx context.Context, logger *slog.Logger, dir string) ([]string, error) {
	cfg, err := loadKeelDevConfig(dir)
	if err != nil {
		return nil, err
	}
	files, err := trackedFilesWithExt(ctx, logger, dir, cfg.Gate.Excludes, ".go")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, file := range files {
		pkgDir := path.Dir(filepath.ToSlash(file))
		if !toolchainPackageDir(pkgDir) {
			continue
		}
		if pkgDir == "." {
			seen["."] = true
			continue
		}
		seen["./"+pkgDir] = true
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("keel-dev: no git-tracked Go package under %s", dir)
	}
	packages := make([]string, 0, len(seen))
	for pkg := range seen {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)
	return packages, nil
}

// toolchainPackageDir reports whether `go list ./...` would consider a
// repo-relative directory: it skips testdata and any segment starting with
// "." or "_".
func toolchainPackageDir(dir string) bool {
	if dir == "." {
		return true
	}
	for _, segment := range strings.Split(dir, "/") {
		if segment == "testdata" || strings.HasPrefix(segment, ".") || strings.HasPrefix(segment, "_") {
			return false
		}
	}
	return true
}

// goTestCoverageArgs builds the coverage run's argv: the tracked packages are
// both the packages under test and the -coverpkg denominator.
func goTestCoverageArgs(packages []string, profile string) []string {
	args := append([]string{"test"}, packages...)
	return append(args, "-coverprofile="+profile, "-covermode=atomic", "-coverpkg="+strings.Join(packages, ","))
}
