package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-173 (keel/ac-744)
func TestSetupUserConvergesAndVerifiesPinnedPnpm(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "scripts", "setup_user.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		`corepack enable --install-directory "${HOME_DIR}/.local/bin"`,
		`corepack prepare "pnpm@${PNPM_VERSION}" --activate`,
		`resolved_pnpm="$(command -v pnpm`,
		`reported_pnpm_version="$(pnpm --version`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("setup_user.sh missing %q", want)
		}
	}
}

// DHF-TEST: keel/requirement-173 (keel/ac-744)
func TestSetupUserPnpmConvergenceAndMismatchAreObservable(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, reported string
		wantOK         bool
	}{{"converges", "12.4.2", true}, {"mismatch", "11.0.9", false}} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			stubDir := filepath.Join(home, "stubs")
			if err := os.MkdirAll(stubDir, 0o755); err != nil {
				t.Fatal(err)
			}
			corepack := `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == enable ]]; then
  mkdir -p "$3"
  printf '%s\n' '#!/usr/bin/env bash' 'if [[ "${1:-}" == "--version" ]]; then echo "${COREPACK_PNPM_VERSION}"; fi' >"$3/pnpm"
  chmod +x "$3/pnpm"
fi
`
			if err := os.WriteFile(filepath.Join(stubDir, "corepack"), []byte(corepack), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", filepath.Join(root, "scripts", "setup_user.sh"))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "HOME="+home, "TARGET_USER="+os.Getenv("USER"), "COREPACK_PNPM_VERSION="+tc.reported, "PATH="+stubDir+":/usr/bin:/bin")
			output, runErr := cmd.CombinedOutput()
			if tc.wantOK && runErr != nil {
				t.Fatalf("setup_user.sh: %v\n%s", runErr, output)
			}
			if !tc.wantOK && (runErr == nil || !strings.Contains(string(output), "path=") || !strings.Contains(string(output), "reported="+tc.reported)) {
				t.Fatalf("mismatch result err=%v\n%s", runErr, output)
			}
		})
	}
}

// DHF-TEST: keel/requirement-173 (keel/ac-741, keel/ac-745)
func TestSetupAsRootProvisionsVSIXHostClosureAtPinnedNodeMajor(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "scripts", "setup_as_root.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{`setup_${NODE_MAJOR}.x`, `require_node_major "$NODE_MAJOR"`, `apt-get install -y`, "xvfb"} {
		if !strings.Contains(text, want) {
			t.Errorf("setup_as_root.sh missing %q", want)
		}
	}
	installed := aptInstallPackages(text)
	seen := map[string]bool{}
	for _, resource := range vsixCIRuntimeLibraryResources {
		if seen[resource.packageName] {
			continue
		}
		seen[resource.packageName] = true
		if !installed[resource.packageName] {
			t.Errorf("setup_as_root.sh does not provision %s", resource.packageName)
		}
	}
}

// DHF-TEST: keel/requirement-173 (keel/ac-745)
func TestNodeMajorMismatchNamesInstalledAndExpectedVersions(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	stubDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubDir, "node"), []byte("#!/bin/sh\necho v23.9.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", `source "$1"; require_node_major 24`, "test", filepath.Join(root, "scripts", "bootstrap_versions.sh"))
	cmd.Env = append(os.Environ(), "PATH="+stubDir+":/usr/bin:/bin")
	output, runErr := cmd.CombinedOutput()
	if runErr == nil || !strings.Contains(string(output), "installed=23 expected=24") {
		t.Fatalf("require_node_major err=%v\n%s", runErr, output)
	}
}
