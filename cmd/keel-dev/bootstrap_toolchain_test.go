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
			for _, name := range []string{"cat", "chmod", "dirname", "grep", "head", "mkdir", "touch", "whoami"} {
				realPath, lookErr := exec.LookPath(name)
				if lookErr != nil {
					t.Fatal(lookErr)
				}
				if err := os.Symlink(realPath, filepath.Join(stubDir, name)); err != nil {
					t.Fatal(err)
				}
			}
			corepack := `#!/bin/bash
set -eu
if [[ "$1" == enable ]]; then
  mkdir -p "$3"
  printf '%s\n' '#!/bin/bash' 'if [[ "${1:-}" == "--version" ]]; then echo "${COREPACK_PNPM_VERSION}"; fi' >"$3/pnpm"
  chmod +x "$3/pnpm"
fi
`
			if err := os.WriteFile(filepath.Join(stubDir, "corepack"), []byte(corepack), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/bash", filepath.Join(root, "scripts", "setup_user.sh"))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "HOME="+home, "TARGET_USER="+os.Getenv("USER"), "COREPACK_PNPM_VERSION="+tc.reported, "PATH="+stubDir)
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

// DHF-TEST: keel/requirement-173 (keel/ac-745)
func TestInstallPinnedNodeUsesNodeSourceAndVerifiesMajor(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "calls.log")
	writeExecutable := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(stubDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable("curl", "#!/bin/sh\nprintf 'curl %s\\n' \"$*\" >>\"$BOOTSTRAP_CALL_LOG\"\n")
	writeExecutable("apt-get", "#!/bin/sh\nprintf 'apt-get %s\\n' \"$*\" >>\"$BOOTSTRAP_CALL_LOG\"\n")
	writeExecutable("node", "#!/bin/sh\necho v24.19.0\n")
	cmd := exec.Command("/bin/bash", "-c", `source "$1"; install_pinned_node 24`, "test", filepath.Join(root, "scripts", "bootstrap_versions.sh"))
	cmd.Env = append(os.Environ(), "BOOTSTRAP_CALL_LOG="+logPath, "PATH="+stubDir+":/usr/bin:/bin")
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("install_pinned_node: %v\n%s", runErr, output)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"https://deb.nodesource.com/setup_24.x", "apt-get install -y nodejs"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("bootstrap calls missing %q:\n%s", want, calls)
		}
	}
	if !strings.Contains(string(output), "expected major 24") {
		t.Errorf("bootstrap output did not verify node major:\n%s", output)
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
	for _, want := range []string{`install_pinned_node "$NODE_MAJOR"`, `apt-get install -y`, "xvfb"} {
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
