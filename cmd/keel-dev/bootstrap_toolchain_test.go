package main

import (
	"os"
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
	for _, want := range []string{`setup_${NODE_MAJOR}.x`, `installed_node_major`, `apt-get install -y`, "xvfb"} {
		if !strings.Contains(text, want) {
			t.Errorf("setup_as_root.sh missing %q", want)
		}
	}
	seen := map[string]bool{}
	for _, resource := range vsixCIRuntimeLibraryResources {
		if seen[resource.packageName] {
			continue
		}
		seen[resource.packageName] = true
		if !strings.Contains(text, resource.packageName) {
			t.Errorf("setup_as_root.sh does not provision %s", resource.packageName)
		}
	}
}
