package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/david-aggeler/keel/cli"
)

const vsixCoverageFloorPercent = 76.3
const vsixSupportPolicyRel = "vsix/SUPPORTED_VSCODE.md"

var vsixHeldDependencyBaselines = []struct {
	name    string
	current string
}{
	{name: "@types/vscode", current: "1.125.0"},
	{name: "@types/node", current: "26.2.0"},
	{name: "typescript", current: "7.0.2"},
}

func vsixCommandSpec() *cli.CommandSpec {
	return &cli.CommandSpec{
		Name:        "vsix",
		Use:         "vsix ci",
		Short:       "Run Keel Test Bridge VSIX checks.",
		Positionals: []cli.PositionalSpec{{Name: "verb", Min: 1, Max: 1}},
		Handler:     handleVSIXGate,
	}
}

// DHF-REQ: keel/requirement-40
func handleVSIXGate(ctx context.Context, args []string) error {
	if args[0] != "ci" {
		return cli.NewUsageError("unknown vsix command %q\nusage: keel-dev vsix ci", args[0])
	}
	state := stateFrom(ctx)
	return runVSIXGate(ctx, state.logger, state.root)
}

// DHF-REQ: keel/requirement-40, keel/requirement-76, keel/requirement-90
func runVSIXGate(ctx context.Context, logger *slog.Logger, dir string) error {
	for _, tool := range []string{"node", "pnpm", "xvfb-run"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("keel-dev vsix ci: required tool %q not found on PATH", tool)
		}
	}
	if err := validateVSIXProtocolDrift(dir); err != nil {
		return err
	}
	if err := validateVSIXSupportPolicy(dir); err != nil {
		return err
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

// DHF-REQ: keel/requirement-119
func validateVSIXSupportPolicy(dir string) error {
	policyPath := filepath.Join(dir, filepath.FromSlash(vsixSupportPolicyRel))
	policy, err := os.ReadFile(policyPath)
	if err != nil {
		return fmt.Errorf("keel-dev vsix policy: read %s: %w", policyPath, err)
	}
	policyText := string(policy)
	declaredMinimum, ok := policyLineValue(policyText, "Minimum supported VS Code:")
	if !ok {
		return fmt.Errorf("keel-dev vsix policy: %s missing Minimum supported VS Code", policyPath)
	}
	if _, ok := policyLineValue(policyText, "Reason:"); !ok {
		return fmt.Errorf("keel-dev vsix policy: %s missing Reason", policyPath)
	}
	nodeMajorRaw, ok := policyLineValue(policyText, "VS Code runtime Node major:")
	if !ok {
		return fmt.Errorf("keel-dev vsix policy: %s missing VS Code runtime Node major", policyPath)
	}
	nodeMajor, err := strconv.Atoi(nodeMajorRaw)
	if err != nil || nodeMajor < 1 {
		return fmt.Errorf("keel-dev vsix policy: VS Code runtime Node major %q is not a positive integer", nodeMajorRaw)
	}
	engineFloor, err := parseCaretSemver("Minimum supported VS Code", declaredMinimum)
	if err != nil {
		return err
	}

	manifestPath := filepath.Join(dir, "vsix", "package.json")
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("keel-dev vsix policy: read %s: %w", manifestPath, err)
	}
	var manifest struct {
		Engines struct {
			VSCode string `json:"vscode"`
		} `json:"engines"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return fmt.Errorf("keel-dev vsix policy: parse %s: %w", manifestPath, err)
	}
	if manifest.Engines.VSCode != declaredMinimum {
		return fmt.Errorf("keel-dev vsix policy: engines.vscode %q does not match declared minimum %q", manifest.Engines.VSCode, declaredMinimum)
	}
	typesVSCode, err := parsePackageSemver("@types/vscode", manifest.DevDependencies["@types/vscode"])
	if err != nil {
		return err
	}
	if compareSemver(typesVSCode, engineFloor) > 0 {
		return fmt.Errorf("keel-dev vsix policy: @types/vscode %q is above declared engine minimum %q", manifest.DevDependencies["@types/vscode"], declaredMinimum)
	}
	typesNode, err := parsePackageSemver("@types/node", manifest.DevDependencies["@types/node"])
	if err != nil {
		return err
	}
	if typesNode.major > nodeMajor {
		return fmt.Errorf("keel-dev vsix policy: @types/node %q is above VS Code runtime Node major %d", manifest.DevDependencies["@types/node"], nodeMajor)
	}
	if err := validateVSIXDependencyHoldNotes(policyText, manifest.DevDependencies); err != nil {
		return err
	}
	return validateVSIXRuntimeNodeCitation(policyPath, policyText, declaredMinimum, nodeMajor)
}

var (
	electronVersionPattern = regexp.MustCompile(`(?i)\belectron\s+v?\d+\.\d+\.\d+\b`)
	nodeVersionPattern     = regexp.MustCompile(`(?i)\bnode(?:\.js)?\s+v?(\d+)\.\d+\.\d+\b`)
)

// validateVSIXRuntimeNodeCitation keeps the runtime Node major derivable from outside
// this repository. Without it the value the @types/node ceiling is checked against is a
// hand-edited line, and the cheapest way to clear that ceiling is to move the line —
// which is what keel/issue-147 records happening.
//
// DHF-REQ: keel/requirement-119 (keel/ac-466)
func validateVSIXRuntimeNodeCitation(policyPath, policyText, declaredMinimum string, nodeMajor int) error {
	citation, ok := policyLineValue(policyText, "VS Code runtime Node major source:")
	if !ok {
		return fmt.Errorf("keel-dev vsix policy: %s missing VS Code runtime Node major source", policyPath)
	}
	release := strings.TrimPrefix(declaredMinimum, "^")
	if !strings.Contains(citation, release) {
		return fmt.Errorf("keel-dev vsix policy: VS Code runtime Node major source %q does not name the declared VS Code release %s", citation, release)
	}
	if !electronVersionPattern.MatchString(citation) {
		return fmt.Errorf("keel-dev vsix policy: VS Code runtime Node major source %q does not name an Electron version", citation)
	}
	match := nodeVersionPattern.FindStringSubmatch(citation)
	if match == nil {
		return fmt.Errorf("keel-dev vsix policy: VS Code runtime Node major source %q does not name a Node version", citation)
	}
	citedMajor, err := strconv.Atoi(match[1])
	if err != nil {
		return fmt.Errorf("keel-dev vsix policy: VS Code runtime Node major source %q has an unreadable Node version", citation)
	}
	if citedMajor != nodeMajor {
		return fmt.Errorf("keel-dev vsix policy: VS Code runtime Node major source cites Node %d, which does not match the declared VS Code runtime Node major %d", citedMajor, nodeMajor)
	}
	if !strings.Contains(citation, "https://") {
		return fmt.Errorf("keel-dev vsix policy: VS Code runtime Node major source %q does not cite an external source URL", citation)
	}
	return nil
}

// DHF-REQ: keel/requirement-119 (keel/ac-459)
func validateVSIXDependencyHoldNotes(policyText string, devDependencies map[string]string) error {
	notes := dependencyHoldNoteBlocks(policyText)
	for _, dependency := range vsixHeldDependencyBaselines {
		declared, ok := devDependencies[dependency.name]
		if !ok {
			continue
		}
		declaredVersion, err := parsePackageSemver(dependency.name, declared)
		if err != nil {
			return err
		}
		currentVersion, err := parseSemver(dependency.name+" current release", dependency.current)
		if err != nil {
			return err
		}
		if compareSemver(declaredVersion, currentVersion) >= 0 {
			continue
		}
		if len(notes) == 0 {
			return fmt.Errorf("keel-dev vsix policy: Dependency hold notes missing for held dependency %s", dependency.name)
		}
		note, ok := notes[dependency.name]
		if !ok {
			return fmt.Errorf("keel-dev vsix policy: Dependency hold notes missing entry for held dependency %s", dependency.name)
		}
		installed := strings.TrimPrefix(strings.TrimSpace(declared), "^")
		if !strings.Contains(note, "`"+installed+"`") {
			return fmt.Errorf("keel-dev vsix policy: Dependency hold note for %s does not state installed version %s", dependency.name, installed)
		}
		if !strings.Contains(note, "current: `"+dependency.current+"`") {
			return fmt.Errorf("keel-dev vsix policy: Dependency hold note for %s does not state current release %s", dependency.name, dependency.current)
		}
		if !strings.Contains(strings.ToLower(note), "reason:") {
			return fmt.Errorf("keel-dev vsix policy: Dependency hold note for %s missing reason", dependency.name)
		}
		if !strings.Contains(strings.ToLower(note), "release condition:") {
			return fmt.Errorf("keel-dev vsix policy: Dependency hold note for %s missing release condition", dependency.name)
		}
	}
	return nil
}

func dependencyHoldNoteBlocks(policyText string) map[string]string {
	notes := map[string]string{}
	var currentName string
	var currentLines []string
	inSection := false
	flush := func() {
		if currentName == "" {
			return
		}
		notes[currentName] = strings.Join(currentLines, "\n")
		currentName = ""
		currentLines = nil
	}
	for _, line := range strings.Split(policyText, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inSection {
			inSection = trimmed == "Dependency hold notes:"
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			break
		}
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			for _, dependency := range vsixHeldDependencyBaselines {
				if strings.Contains(trimmed, "`"+dependency.name+"`") {
					currentName = dependency.name
					currentLines = append(currentLines, trimmed)
					break
				}
			}
			continue
		}
		if currentName != "" {
			currentLines = append(currentLines, trimmed)
		}
	}
	flush()
	return notes
}

func policyLineValue(body, prefix string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			return value, value != ""
		}
	}
	return "", false
}

type simpleSemver struct {
	major int
	minor int
	patch int
}

func parseCaretSemver(field, value string) (simpleSemver, error) {
	if !strings.HasPrefix(value, "^") {
		return simpleSemver{}, fmt.Errorf("keel-dev vsix policy: %s %q must use a caret semver floor", field, value)
	}
	return parseSemver(field, strings.TrimPrefix(value, "^"))
}

func parsePackageSemver(name, value string) (simpleSemver, error) {
	if value == "" {
		return simpleSemver{}, fmt.Errorf("keel-dev vsix policy: missing %s devDependency", name)
	}
	value = strings.TrimPrefix(strings.TrimSpace(value), "^")
	return parseSemver(name, value)
}

func parseSemver(field, value string) (simpleSemver, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return simpleSemver{}, fmt.Errorf("keel-dev vsix policy: %s version %q is not major.minor.patch", field, value)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return simpleSemver{}, fmt.Errorf("keel-dev vsix policy: %s version %q has invalid major", field, value)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return simpleSemver{}, fmt.Errorf("keel-dev vsix policy: %s version %q has invalid minor", field, value)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return simpleSemver{}, fmt.Errorf("keel-dev vsix policy: %s version %q has invalid patch", field, value)
	}
	return simpleSemver{major: major, minor: minor, patch: patch}, nil
}

func compareSemver(a, b simpleSemver) int {
	switch {
	case a.major != b.major:
		return a.major - b.major
	case a.minor != b.minor:
		return a.minor - b.minor
	default:
		return a.patch - b.patch
	}
}
