package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-174 (keel/ac-739, keel/ac-740, keel/ac-742, keel/ac-746, keel/ac-747, keel/ac-748)
func TestRunLintRejectsInvalidHostToolchainPinBlock(t *testing.T) {
	tests := []struct {
		name string
		edit func(string) string
		want string
	}{
		{"missing directive", func(s string) string {
			return strings.Replace(s, "GO_VERSION=1.26.6 # pin: go pinned", "GO_VERSION=1.26.6", 1)
		}, "line 2"},
		{"unknown class", func(s string) string {
			return strings.Replace(s, "# pin: gh system -- distro package", "# pin: gh mystery -- distro package", 1)
		}, "unknown class"},
		{"missing reason", func(s string) string {
			return strings.Replace(s, "# pin: gh system -- distro package", "# pin: gh system", 1)
		}, "requires a -- reason"},
		{"missing inventory", func(s string) string { return strings.Replace(s, "# pin: gh system -- distro package\n", "", 1) }, "tool \"gh\" is undeclared"},
		{"wrong assignment", func(s string) string { return strings.Replace(s, "PNPM_VERSION=12.4.2", "TYPO_VERSION=12.4.2", 1) }, "must use variable PNPM_VERSION"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := validPinFixture(t)
			path := filepath.Join(root, "scripts", "setup_user.sh")
			body, _ := os.ReadFile(path)
			if err := os.WriteFile(path, []byte(tc.edit(string(body))), 0o755); err != nil {
				t.Fatal(err)
			}
			err := runLint(root, lintFixtureFiles(t, root))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runLint error = %v, want %q", err, tc.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-173 (keel/ac-741)
func TestRunLintRequiresPackagesInAptInstallCommand(t *testing.T) {
	root := validPinFixture(t)
	path := filepath.Join(root, "scripts", "setup_as_root.sh")
	body, _ := os.ReadFile(path)
	changed := strings.Replace(string(body), " libnss3", "", 1) + "# libnss3 mentioned but not installed\n"
	if err := os.WriteFile(path, []byte(changed), 0o755); err != nil {
		t.Fatal(err)
	}
	err := runLint(root, lintFixtureFiles(t, root))
	if err == nil || !strings.Contains(err.Error(), `does not install package "libnss3"`) {
		t.Fatalf("runLint error = %v", err)
	}
}

// DHF-TEST: keel/requirement-174 (keel/ac-740, keel/ac-742, keel/ac-748)
func TestRunLintRejectsHostToolchainPinCopiesThatDrift(t *testing.T) {
	tests := []struct{ file, old, replacement, want string }{
		{"keel-dev.yaml", "version: v2.12.2", "version: v2.11.0", "golangci-lint"},
		{"vsix/package.json", `"packageManager":"pnpm@12.4.2"`, `"packageManager":"pnpm@11.0.9"`, "pnpm@12.4.2"},
		{"scripts/install_go.sh", "GO_VERSION=1.26.6", "GO_VERSION=1.25.0", "install_go.sh"},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			root := validPinFixture(t)
			path := filepath.Join(root, filepath.FromSlash(tc.file))
			body, _ := os.ReadFile(path)
			if err := os.WriteFile(path, []byte(strings.Replace(string(body), tc.old, tc.replacement, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			err := runLint(root, lintFixtureFiles(t, root))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runLint error = %v, want %q", err, tc.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-174 (keel/ac-743)
func TestSetupUserPinBlockRemainsReadableByOpenbrain(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "scripts", "setup_user.sh"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^\s*([A-Z][A-Z0-9_]*)_VERSION="?([^"\s]+)"?`)
	pins, violations, err := parseHostToolPins(filepath.Join(root, "scripts", "setup_user.sh"))
	if err != nil || len(violations) != 0 {
		t.Fatalf("parse pin block: violations=%v err=%v", violations, err)
	}
	want := map[string]bool{"GOLANGCI_LINT": true, "GOVULNCHECK": true, "GOFUMPT": true, "SHFMT": true, "DEADCODE": true, "GITLEAKS": true, "CSPELL": true}
	tools := map[string]string{"GOLANGCI_LINT": "golangci-lint", "GOVULNCHECK": "govulncheck", "GOFUMPT": "gofumpt", "SHFMT": "shfmt", "DEADCODE": "deadcode", "GITLEAKS": "gitleaks", "CSPELL": "cspell"}
	seen := map[string]int{}
	for _, match := range re.FindAllStringSubmatch(string(body), -1) {
		if want[match[1]] {
			seen[match[1]]++
			if got := match[2]; got != pins[tools[match[1]]].version {
				t.Errorf("%s captured %q, pin block has %q", match[1], got, pins[tools[match[1]]].version)
			}
		}
	}
	for stem := range want {
		if seen[stem] != 1 {
			t.Errorf("%s matches = %d, want 1", stem, seen[stem])
		}
	}
}

func validPinFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"scripts", "vsix"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, "scripts/setup_user.sh", `# pin-block: begin
GO_VERSION=1.26.6 # pin: go pinned
NODE_MAJOR=24 # pin: node pinned
PNPM_VERSION=12.4.2 # pin: pnpm pinned
CSPELL_VERSION=10.0.1 # pin: cspell pinned
GOLANGCI_LINT_VERSION=v2.12.2 # pin: golangci-lint pinned
GOVULNCHECK_VERSION=v1.7.0 # pin: govulncheck pinned
GOFUMPT_VERSION=v0.7.0 # pin: gofumpt pinned
SHFMT_VERSION=v3.13.1 # pin: shfmt pinned
DEADCODE_VERSION=v0.28.0 # pin: deadcode pinned
GITLEAKS_VERSION=v8.30.1 # pin: gitleaks pinned
# pin: xvfb-run system -- apt package xvfb
# pin: shellcheck system -- apt package
# pin: just system -- apt package
# pin: gh system -- distro package
# pin: gopls float -- editor tool
# pin-block: end
`)
	rootScript := "NODE_MAJOR=24\nEXPECTED_SHELLCHECK_VERSION=0.10.0\napt-get install -y xvfb"
	seenPackages := map[string]bool{}
	for _, resource := range vsixCIRuntimeLibraryResources {
		if !seenPackages[resource.packageName] {
			rootScript += " " + resource.packageName
			seenPackages[resource.packageName] = true
		}
	}
	writeFile(t, root, "scripts/setup_as_root.sh", rootScript+"\n")
	writeFile(t, root, "scripts/install_go.sh", "GO_VERSION=1.26.6\n")
	writeFile(t, root, "vsix/package.json", `{"packageManager":"pnpm@12.4.2"}`)
	writeFile(t, root, "keel-dev.yaml", `gate:
  excludes: [docs/handoffs/**]
tools:
  pins:
    - name: golangci-lint
      version_args: [--version]
      want: 2.12.2
      install: {method: go, package: example/tool, version: v2.12.2}
`)
	return root
}
