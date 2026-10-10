package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeScanOnceFixture lays out a keel module root with several test-bearing
// packages and a lanes file of more than two lanes with Go members, so every
// lane-state consumer (Go member expansion, root member, covers aliases) is
// exercised in one invocation.
func writeScanOnceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, pkg := range []struct{ dir, name string }{
		{"exec", "exec"},
		{filepath.Join("exec", "codex"), "codex"},
		{"log", "log"},
		{"cli", "cli"},
	} {
		if err := os.MkdirAll(filepath.Join(root, pkg.dir), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, root, filepath.Join(pkg.dir, pkg.name+"_test.go"),
			"package "+pkg.name+"\n\nimport \"testing\"\n\nfunc Test_runs(t *testing.T) {}\n")
	}
	if err := os.MkdirAll(filepath.Join(root, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), `{
  "version": 1,
  "lanes": [
    {"id": "core", "label": "core", "members": [{"go": "./exec/..."}, {"go": "./log/..."}]},
    {"id": "cli", "label": "cli", "members": [{"go": "./cli/..."}]},
    {"id": "all", "label": "all", "members": [{"lane": "core"}, {"root": "go"}]}
  ]
}
`)
	return root
}

// goTestPackageScanCount reads the scan seam for one module root.
func goTestPackageScanCount(root string) int {
	goTestPackageScans.Lock()
	defer goTestPackageScans.Unlock()
	total := 0
	for key, n := range goTestPackageScans.byRoot {
		if filepath.Clean(key) == filepath.Clean(root) {
			total += n
		}
	}
	return total
}

// DHF-TEST: keel/requirement-66
func TestTestBridgeInvocationScansGoTestPackagesAtMostOnce(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"discover", []string{"test-bridge", "discover", "--format", "json"}},
		{"run file lane", []string{"test-bridge", "run", "--id", "keel::lane::cli"}},
		{"run desired-state row", []string{"test-bridge", "run", "--id", "keel::desired-state::keel-module-root"}},
		{"detect-lanes", []string{"test-bridge", "run", "--id", vscodeMaintenanceDetectLanes}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeScanOnceFixture(t)
			var protocol bytes.Buffer
			if err := commandTree().Dispatch(contextWithVSCodeTestState(root, &protocol), tc.args); err != nil {
				t.Fatalf("dispatch %v: %v\n%s", tc.args, err, protocol.String())
			}
			// Positive control: the seam observes the invocation's scan, so a
			// zero would mean the seam is blind, not that the scan is shared.
			got := goTestPackageScanCount(root)
			if got < 1 {
				t.Fatalf("%s: scan seam observed %d scans; want the invocation's scan to be counted", tc.name, got)
			}
			if got > 1 {
				t.Fatalf("%s: Go test-package scan ran %d times in one invocation; want at most 1 (keel/ac-800)", tc.name, got)
			}
		})
	}
}

// DHF-TEST: keel/requirement-66
func TestDiscoveryWithSharedPackageIndexMatchesScanPerCall(t *testing.T) {
	root := writeScanOnceFixture(t)
	// A nil index is the scan-per-call behavior: every consumer walks the
	// module itself, as keel-dev did before the shared index.
	perCall, err := buildVSCodeDiscoveryFrom(root, nil)
	if err != nil {
		t.Fatalf("scan-per-call discovery: %v", err)
	}
	if got := goTestPackageScanCount(root); got < 2 {
		t.Fatalf("scan-per-call discovery scanned %d times; want one scan per consumer (>= 2)", got)
	}
	shared, err := (keelTestBridge{}).Discover(contextWithVSCodeTestState(root, &bytes.Buffer{}))
	if err != nil {
		t.Fatalf("bridge discover: %v", err)
	}
	// The covers tree of each lane must be present, or both documents could
	// agree on an empty lane expansion.
	for _, id := range []string{
		"keel::lane::core::covers::go--pkg--exec-codex",
		"keel::lane::core::covers::go--test--log--test-runs",
		"keel::lane::cli::covers::go--pkg--cli",
		"keel::lane::all::covers::go--pkg--log",
		"keel::lane::all::covers::go--root",
	} {
		if _, ok := discoveryItemByID(shared, id); !ok {
			t.Fatalf("shared-index discovery missing %s", id)
		}
	}
	perCall.GeneratedAt, shared.GeneratedAt = time.Time{}, time.Time{}
	want, err := json.Marshal(perCall)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(shared)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("shared-index discovery differs from scan-per-call discovery\n got: %s\nwant: %s", got, want)
	}
}
