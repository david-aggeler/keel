package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shellStartupEnvKeys name the startup files a non-interactive shell sources
// before its command: bash reads $BASH_ENV, a POSIX sh reads $ENV. A host that
// exports BASH_ENV=~/.profile would otherwise rebuild PATH ahead of a test's
// stub binaries (keel/issue-261).
var shellStartupEnvKeys = []string{"BASH_ENV", "ENV"}

// TestMain removes the shell startup files from the test process, so every
// shell a cmd/keel-dev test spawns — including ones added later — starts
// without them.
//
// DHF-REQ: keel/requirement-178
func TestMain(m *testing.M) {
	for _, key := range shellStartupEnvKeys {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

// hermeticShellEnv returns the process environment without the shell startup
// files and without any key that overrides replaces, followed by overrides
// ("KEY=value"). Tests that set cmd.Env build it here instead of appending to
// os.Environ().
//
// DHF-REQ: keel/requirement-178
func hermeticShellEnv(overrides ...string) []string {
	drop := map[string]bool{}
	for _, key := range shellStartupEnvKeys {
		drop[key] = true
	}
	for _, kv := range overrides {
		drop[strings.SplitN(kv, "=", 2)[0]] = true
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		if !drop[strings.SplitN(kv, "=", 2)[0]] {
			env = append(env, kv)
		}
	}
	return append(env, overrides...)
}

// DHF-TEST: keel/requirement-178 (keel/ac-770)
func TestPackageEnvHasNoShellStartupFile(t *testing.T) {
	for _, key := range shellStartupEnvKeys {
		if v, ok := os.LookupEnv(key); ok {
			t.Errorf("%s=%q is set in the test process; TestMain must unset it", key, v)
		}
	}
}

// DHF-TEST: keel/requirement-178 (keel/ac-768)
func TestHermeticShellEnvDefeatsHostileBashEnv(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	stubDir := t.TempDir()
	hostileDir := t.TempDir()
	for dir, version := range map[string]string{stubDir: "v23.9.0", hostileDir: "v24.0.0"} {
		if err := os.WriteFile(filepath.Join(dir, "node"), []byte("#!/bin/sh\necho "+version+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The startup file a host profile would be: it rebuilds PATH so the
	// "real" node wins over the test's stub.
	startup := filepath.Join(t.TempDir(), "profile")
	if err := os.WriteFile(startup, []byte("export PATH="+hostileDir+":$PATH\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BASH_ENV", startup)
	t.Setenv("ENV", startup)

	cmd := exec.Command("bash", "-c", `source "$1"; require_node_major 24`, "test", filepath.Join(root, "scripts", "bootstrap_versions.sh"))
	cmd.Env = hermeticShellEnv("PATH=" + stubDir + ":/usr/bin:/bin")
	output, runErr := cmd.CombinedOutput()
	if runErr == nil || !strings.Contains(string(output), "installed=23 expected=24") {
		t.Fatalf("hostile BASH_ENV reached the shell: err=%v\n%s", runErr, output)
	}
}
