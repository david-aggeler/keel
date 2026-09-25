package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type hostToolPin struct {
	class   string
	version string
}

var pinDirectiveRE = regexp.MustCompile(`# pin: ([a-z0-9-]+) ([a-z]+)(?: -- (.+))?\s*$`)
var pinAssignmentRE = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)=(?:"([^"]+)"|([^\s]+))`)

// scanHostToolchainPins holds the bootstrap, gate config, and VSIX package
// manager to the one machine-readable pin block in setup_user.sh.
//
// DHF-REQ: keel/requirement-173, keel/requirement-174
func scanHostToolchainPins(root string) ([]string, error) {
	pins, violations, err := parseHostToolPins(filepath.Join(root, "scripts", "setup_user.sh"))
	if err != nil {
		return nil, err
	}
	required := []string{"go", "node", "pnpm", "xvfb-run", "cspell", "golangci-lint", "govulncheck", "gofumpt", "shfmt", "deadcode", "gitleaks", "shellcheck", "just", "gh", "gopls"}
	for _, tool := range required {
		if _, ok := pins[tool]; !ok {
			violations = append(violations, fmt.Sprintf("  host-toolchain-pins: tool %q is undeclared in scripts/setup_user.sh", tool))
		}
	}

	configPath := filepath.Join(root, keelDevConfigFile)
	if _, statErr := os.Stat(configPath); statErr == nil {
		cfg, loadErr := loadKeelDevConfigFile(configPath, true)
		if loadErr != nil {
			return nil, loadErr
		}
		for _, configured := range cfg.Tools.Pins {
			pin, ok := pins[configured.Name]
			if !ok {
				violations = append(violations, fmt.Sprintf("  host-toolchain-pins: keel-dev.yaml tool %q pin-block version is undeclared", configured.Name))
				continue
			}
			want := configured.Want
			if configured.Install.Method == toolInstallGo {
				want = configured.Install.Version
			}
			if trimVersion(want) != trimVersion(pin.version) {
				violations = append(violations, fmt.Sprintf("  host-toolchain-pins: tool %q keel-dev.yaml version %q differs from pin-block version %q", configured.Name, want, pin.version))
			}
		}
	}

	violations = append(violations, checkPinnedLiteral(root, "scripts/install_go.sh", "go", `(?m)^GO_VERSION="?([^"\s]+)`, pins)...)
	violations = append(violations, checkPinnedLiteral(root, "scripts/setup_as_root.sh", "node", `(?m)^NODE_MAJOR="?([^"\s]+)`, pins)...)
	violations = append(violations, checkPinnedLiteral(root, "scripts/setup_as_root.sh", "shellcheck", `(?m)^EXPECTED_SHELLCHECK_VERSION="?([^"\s]+)`, pins)...)

	packageJSON := filepath.Join(root, "vsix", "package.json")
	if body, readErr := os.ReadFile(packageJSON); readErr == nil {
		var pkg struct {
			PackageManager string `json:"packageManager"`
		}
		if jsonErr := json.Unmarshal(body, &pkg); jsonErr != nil {
			return nil, fmt.Errorf("host-toolchain-pins: parse %s: %w", packageJSON, jsonErr)
		}
		expected := "pnpm@" + pins["pnpm"].version
		if pkg.PackageManager != expected {
			violations = append(violations, fmt.Sprintf("  host-toolchain-pins: vsix/package.json packageManager: expected %q, found %q", expected, pkg.PackageManager))
		}
	}

	rootScript, readErr := os.ReadFile(filepath.Join(root, "scripts", "setup_as_root.sh"))
	if readErr == nil {
		text := string(rootScript)
		for _, tool := range vsixCIToolResources {
			if _, ok := pins[tool]; !ok {
				violations = append(violations, fmt.Sprintf("  host-toolchain-pins: VSIX tool %q has no pin-block directive", tool))
			}
		}
		packages := map[string]bool{"xvfb": true}
		for _, resource := range vsixCIRuntimeLibraryResources {
			packages[resource.packageName] = true
		}
		for pkg := range packages {
			if !regexp.MustCompile(`(^|[[:space:]])` + regexp.QuoteMeta(pkg) + `([[:space:]\\]|$)`).MatchString(text) {
				violations = append(violations, fmt.Sprintf("  host-toolchain-pins: scripts/setup_as_root.sh does not install package %q", pkg))
			}
		}
	}
	sort.Strings(violations)
	return violations, nil
}

func parseHostToolPins(path string) (map[string]hostToolPin, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	pins := map[string]hostToolPin{}
	var violations []string
	inBlock := false
	scanner := bufio.NewScanner(file)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "# pin-block: begin" {
			inBlock = true
			continue
		}
		if strings.TrimSpace(line) == "# pin-block: end" {
			inBlock = false
			continue
		}
		if !inBlock || strings.TrimSpace(line) == "" {
			continue
		}
		match := pinDirectiveRE.FindStringSubmatch(line)
		if match == nil {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				violations = append(violations, fmt.Sprintf("  host-toolchain-pins: scripts/setup_user.sh line %d has no valid directive: %s", lineNo, strings.TrimSpace(line)))
			}
			continue
		}
		tool, class, reason := match[1], match[2], strings.TrimSpace(match[3])
		if class != "pinned" && class != "system" && class != "float" {
			violations = append(violations, fmt.Sprintf("  host-toolchain-pins: tool %q has unknown class %q", tool, class))
			continue
		}
		if (class == "system" || class == "float") && reason == "" {
			violations = append(violations, fmt.Sprintf("  host-toolchain-pins: tool %q class %s requires a -- reason", tool, class))
		}
		version := ""
		if class == "pinned" {
			assignment := pinAssignmentRE.FindStringSubmatch(strings.TrimSpace(line))
			if assignment == nil {
				violations = append(violations, fmt.Sprintf("  host-toolchain-pins: pinned tool %q line %d has no version assignment", tool, lineNo))
			} else if assignment[2] != "" {
				version = assignment[2]
			} else {
				version = assignment[3]
			}
		}
		if _, duplicate := pins[tool]; duplicate {
			violations = append(violations, fmt.Sprintf("  host-toolchain-pins: tool %q has more than one directive", tool))
		} else {
			pins[tool] = hostToolPin{class: class, version: version}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return pins, violations, nil
}

func checkPinnedLiteral(root, name, tool, pattern string, pins map[string]hostToolPin) []string {
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return nil
	}
	match := regexp.MustCompile(pattern).FindStringSubmatch(string(body))
	if len(match) < 2 || trimVersion(match[1]) != trimVersion(pins[tool].version) {
		found := "missing"
		if len(match) >= 2 {
			found = match[1]
		}
		return []string{fmt.Sprintf("  host-toolchain-pins: %s tool %q version %q differs from pin-block version %q", name, tool, found, pins[tool].version)}
	}
	return nil
}

func trimVersion(version string) string { return strings.TrimPrefix(version, "v") }
