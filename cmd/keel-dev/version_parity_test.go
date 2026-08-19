package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// writeVersionFiles lays down the two committed carriers of the one-version
// invariant. An empty string means "do not write this file at all", so a
// checkout that carries neither can be expressed as well as a skewed one.
func writeVersionFiles(t *testing.T, dir, versionFile, manifestVersion string) {
	t.Helper()
	if versionFile != "" {
		writeFile(t, dir, "VERSION", versionFile+"\n")
	}
	if manifestVersion != "" {
		if err := os.MkdirAll(filepath.Join(dir, "vsix"), 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := "{\n  \"name\": \"keel-test-bridge\",\n  \"version\": \"" + manifestVersion + "\",\n  \"engines\": {\n    \"vscode\": \"^1.90.0\"\n  }\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "vsix", "package.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// alignFixtureVersions stamps a module fixture's VSIX manifest to that
// fixture's VERSION. moduleFixture ships the two deliberately skewed, because
// the pre-stamp skew is what makes the release verb's stamping step observable;
// a test that runs the bare gate over the fixture is not testing that, and the
// version-parity stage rightly refuses a skewed checkout.
func alignFixtureVersions(t *testing.T, dir string) {
	t.Helper()
	version, err := readVersionFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		t.Fatalf("read fixture VERSION: %v", err)
	}
	if err := stampVSIXPackageVersion(filepath.Join(dir, "vsix", "package.json"), version); err != nil {
		t.Fatalf("stamp fixture manifest: %v", err)
	}
}

// DHF-TEST: keel/requirement-141 (keel/ac-569)
func TestVersionParityStageRedsTheGateOnASkewedCheckout(t *testing.T) {
	requireTool(t, "git")

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	writeModule(t, dir)
	writeVersionFiles(t, dir, "0.9.0", "0.8.1")

	err := runCI(context.Background(), discardLogger(), dir)
	if err == nil {
		t.Fatal("a checkout whose VERSION and vsix/package.json disagree must fail ci, got nil")
	}
	var opErr *logging.OperationalError
	if !errors.As(err, &opErr) {
		t.Fatalf("ci error type = %T, want OperationalError: %v", err, err)
	}
	if opErr.Task != "ci:version-parity" {
		t.Fatalf("failing gate task = %q, want ci:version-parity — the skew must red the parity stage, not a later one", opErr.Task)
	}
}

// DHF-TEST: keel/requirement-141 (keel/ac-570)
func TestVersionParityFailureNamesBothPathsAndBothValues(t *testing.T) {
	dir := t.TempDir()
	writeVersionFiles(t, dir, "0.9.0", "0.8.1")

	err := runGateStage(context.Background(), discardLogger(), nil, dir, "version-parity")
	if err == nil {
		t.Fatal("the parity stage must fail over a skewed pair, got nil")
	}
	for _, token := range []string{"VERSION", "vsix/package.json", "0.9.0", "0.8.1"} {
		if !strings.Contains(err.Error(), token) {
			t.Fatalf("parity failure text is missing %q; a reader must not need a second command to see what disagrees.\ngot: %s", token, err.Error())
		}
	}
}

// DHF-TEST: keel/requirement-141 (keel/ac-571)
func TestVersionParityStageIsAddressableThroughTheGateCommandTree(t *testing.T) {
	if !slices.Contains(gateStageNames(), "version-parity") {
		t.Fatalf("battery stages = %v, want version-parity among them", gateStageNames())
	}

	gate := commandSpecByPath(commandTree(), "gate")
	if gate == nil {
		t.Fatal("keel-dev declares no gate namespace")
	}
	var found bool
	for _, sub := range gate.Subcommands {
		if sub.Name != "version-parity" {
			continue
		}
		found = true
		if sub.Handler == nil {
			t.Fatal("gate version-parity is listed but has no handler — listed and not invocable")
		}
	}
	if !found {
		t.Fatal("keel-dev gate declares no version-parity subcommand")
	}

	dir := t.TempDir()
	writeVersionFiles(t, dir, "0.8.1", "0.8.1")
	logger, cap := testLogger("keel-dev")
	if err := runGateStage(context.Background(), logger, nil, dir, "version-parity"); err != nil {
		t.Fatalf("version-parity should pass over an agreeing pair: %v", err)
	}
	if got := gateStartedNames(cap.AllJSON()); !stringSliceEqual(got, []string{"version-parity"}) {
		t.Fatalf("stages started = %v, want only [version-parity]", got)
	}
}

// DHF-TEST: keel/requirement-141 (keel/ac-569, keel/ac-572)
func TestVersionParityStageVerdictFollowsTheTwoFiles(t *testing.T) {
	cases := []struct {
		name        string
		versionFile string
		manifest    string
		wantErr     bool
	}{
		{name: "agreeing pair is green", versionFile: "0.8.1", manifest: "0.8.1", wantErr: false},
		{name: "differing patch is red", versionFile: "0.8.1", manifest: "0.8.0", wantErr: true},
		{name: "differing major is red", versionFile: "1.0.0", manifest: "0.8.1", wantErr: true},
		{name: "leading v on the manifest is still a difference", versionFile: "0.8.1", manifest: "v0.8.1", wantErr: true},
		{name: "neither file present leaves nothing to gate", versionFile: "", manifest: "", wantErr: false},
		{name: "VERSION without a manifest is red", versionFile: "0.8.1", manifest: "", wantErr: true},
		{name: "manifest without a VERSION is red", versionFile: "", manifest: "0.8.1", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeVersionFiles(t, dir, tc.versionFile, tc.manifest)

			err := runGateStage(context.Background(), discardLogger(), nil, dir, "version-parity")
			if tc.wantErr && err == nil {
				t.Fatalf("VERSION=%q manifest=%q must red the parity stage, got nil", tc.versionFile, tc.manifest)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("VERSION=%q manifest=%q must leave the parity stage green, got %v", tc.versionFile, tc.manifest, err)
			}
		})
	}
}

// DHF-TEST: keel/requirement-141 (keel/ac-569)
func TestVersionParityRunsAheadOfTheExpensiveStages(t *testing.T) {
	names := gateStageNames()
	parity := slices.Index(names, "version-parity")
	if parity < 0 {
		t.Fatalf("battery stages = %v, want version-parity among them", names)
	}
	// The whole value of the stage is failing before minutes of work are spent,
	// so it must precede every stage that compiles, spawns a pinned tool, or runs
	// the suite.
	for _, later := range []string{"build", "vet", "tool-pins", "golangci-lint", "govulncheck", "test"} {
		at := slices.Index(names, later)
		if at < 0 {
			t.Fatalf("battery stages = %v, want %q among them", names, later)
		}
		if parity > at {
			t.Fatalf("version-parity runs at %d, after %q at %d — the parity verdict must land before the expensive stages", parity, later, at)
		}
	}
}
