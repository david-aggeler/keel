package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeExecStub writes an executable script named name into dir that prints
// stdout and exits with exitCode, then returns dir. Used to give tests a
// deterministic stand-in for an external gate tool on a scrubbed PATH.
func writeExecStub(t *testing.T, dir, name, stdout string, exitCode int) {
	t.Helper()
	script := "#!/bin/sh\n"
	if stdout != "" {
		script += "printf '%s\\n' " + shellSingleQuote(stdout) + "\n"
	}
	script += "exit " + itoaStub(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func itoaStub(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

// scrubPATH points PATH at a single fresh temp dir, so only stubs written there
// resolve. Returns that dir.
func scrubPATH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	return dir
}

// TestVerifyToolPin_Match: the pinned version substring appears in the probe.
func TestVerifyToolPin_Match(t *testing.T) {
	dir := scrubPATH(t)
	writeExecStub(t, dir, "faketool", "faketool version v9.9.9 built ok", 0)

	pin := toolPin{name: "faketool", versionArgs: []string{"--version"}, want: "v9.9.9"}
	if err := verifyToolPin(context.Background(), discardLogger(), pin); err != nil {
		t.Fatalf("matching version should verify, got %v", err)
	}
}

// TestVerifyToolPin_VersionMismatch: a present tool at the wrong version fails
// loud, and the error names the tool and the expected version.
func TestVerifyToolPin_VersionMismatch(t *testing.T) {
	dir := scrubPATH(t)
	writeExecStub(t, dir, "faketool", "faketool version v1.0.0", 0)

	pin := toolPin{name: "faketool", versionArgs: []string{"--version"}, want: "v9.9.9"}
	err := verifyToolPin(context.Background(), discardLogger(), pin)
	if err == nil {
		t.Fatal("version mismatch should fail, got nil")
	}
	if !strings.Contains(err.Error(), "faketool") || !strings.Contains(err.Error(), "v9.9.9") {
		t.Fatalf("mismatch error must name tool and want-version: %v", err)
	}
}

// TestVerifyToolPin_MissingTool: an absent tool fails loud, naming the tool and
// the pinned version — never a silent skip (keel/ac-42).
func TestVerifyToolPin_MissingTool(t *testing.T) {
	scrubPATH(t) // no stub written: the tool is absent

	pin := toolPin{name: "definitely-absent-tool", versionArgs: []string{"--version"}, want: "v2.3.4"}
	err := verifyToolPin(context.Background(), discardLogger(), pin)
	if err == nil {
		t.Fatal("missing tool should fail, got nil")
	}
	if !strings.Contains(err.Error(), "definitely-absent-tool") || !strings.Contains(err.Error(), "v2.3.4") {
		t.Fatalf("missing-tool error must name tool and want-version: %v", err)
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Fatalf("missing-tool error should point at PATH: %v", err)
	}
}

// TestVerifyToolPin_PresenceOnly: a presence-only pin (empty want) passes when
// the binary exists and fails loud when it does not.
func TestVerifyToolPin_PresenceOnly(t *testing.T) {
	dir := scrubPATH(t)
	pin := toolPin{name: "presencetool"}

	if err := verifyToolPin(context.Background(), discardLogger(), pin); err == nil {
		t.Fatal("presence-only pin should fail when binary is absent")
	}

	writeExecStub(t, dir, "presencetool", "", 0)
	if err := verifyToolPin(context.Background(), discardLogger(), pin); err != nil {
		t.Fatalf("presence-only pin should pass when binary exists, got %v", err)
	}
}

// TestRunStepToolGate_Missing: a subprocess step whose pinned tool is absent
// fails via the version gate before it ever spawns the tool.
func TestRunStepToolGate_Missing(t *testing.T) {
	scrubPATH(t)
	// deadcode is a real pinnedTools entry; on a scrubbed PATH it is absent.
	err := runStep(context.Background(), discardLogger(), ".", step{
		name: "deadcode", tool: "deadcode", program: "deadcode", args: []string{"./..."}, advisory: true,
	})
	if err == nil {
		t.Fatal("advisory step must still fail when its pinned tool is missing")
	}
	if !strings.Contains(err.Error(), "deadcode") {
		t.Fatalf("error should name the missing tool: %v", err)
	}
}

// TestRunStepToolGate_Unregistered: a step naming a tool with no pin entry is a
// programming error and fails loud.
func TestRunStepToolGate_Unregistered(t *testing.T) {
	err := runStep(context.Background(), discardLogger(), ".", step{
		name: "bogus", tool: "no-such-pin", program: "true",
	})
	if err == nil || !strings.Contains(err.Error(), "no version pin registered") {
		t.Fatalf("unregistered tool should fail loud, got %v", err)
	}
}

// TestRunStepAdvisory_IgnoresFailure: an advisory step whose subprocess exits
// non-zero still returns nil — findings are reported, the gate is unaffected
// (keel/ac-41).
func TestRunStepAdvisory_IgnoresFailure(t *testing.T) {
	dir := t.TempDir()
	writeExecStub(t, dir, "reporter", "found: some unreachable func", 1)
	stub := filepath.Join(dir, "reporter")

	if err := runStep(context.Background(), discardLogger(), ".", step{
		name: "advisory-probe", program: stub, advisory: true,
	}); err != nil {
		t.Fatalf("advisory step must not fail the gate on non-zero exit, got %v", err)
	}
}

// TestRunStepNonAdvisory_FailsOnNonZero is the control: the same non-zero exit
// without advisory does fail the step.
func TestRunStepNonAdvisory_FailsOnNonZero(t *testing.T) {
	dir := t.TempDir()
	writeExecStub(t, dir, "reporter", "boom", 1)
	stub := filepath.Join(dir, "reporter")

	if err := runStep(context.Background(), discardLogger(), ".", step{
		name: "blocking-probe", program: stub,
	}); err == nil {
		t.Fatal("non-advisory step must fail on non-zero exit")
	}
}

// TestCspellStep_FailsOnMisspelling is the anti-vacuous-pass guard: with the
// repo's committed cspell.json, a file containing a word that is in no
// dictionary MUST fail the cspell step. This proves the spell-check step
// actually evaluates rules — a rule-less or empty config could never make this
// fail. The misspelled token is assembled at runtime so the committed test
// source carries no unknown word for the real gate's cspell run to flag.
func TestCspellStep_FailsOnMisspelling(t *testing.T) {
	requireTool(t, "cspell")

	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	config := filepath.Join(root, "cspell.json")

	// A nonsense consonant run, built from runes so no unknown word literal
	// appears in this source file.
	bad := string([]rune{'z', 'q', 'x', 'v', 'w', 'k', 'j', 'b', 'f'})
	fixtureDir := t.TempDir()
	fixture := filepath.Join(fixtureDir, "bad.md")
	if err := os.WriteFile(fixture, []byte("# heading\n\nThe word "+bad+" is not real.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// --root anchors cspell at the fixture dir (it only checks files under its
	// root) while --config supplies the repo's committed rulebook.
	err = runStep(context.Background(), discardLogger(), root, step{
		name:    "cspell-selftest",
		program: "cspell",
		args:    []string{"--no-progress", "--root", fixtureDir, "--config", config, fixture},
	})
	if err == nil {
		t.Fatal("cspell must fail on an unknown word — the config is not evaluating rules")
	}

	// Control: a clean file with only dictionary words passes, proving the
	// failure above is the misspelling and not a broken invocation.
	clean := filepath.Join(fixtureDir, "clean.md")
	if err := os.WriteFile(clean, []byte("# heading\n\nThe keel gate runs cspell.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runStep(context.Background(), discardLogger(), root, step{
		name:    "cspell-selftest-clean",
		program: "cspell",
		args:    []string{"--no-progress", "--root", fixtureDir, "--config", config, clean},
	}); err != nil {
		t.Fatalf("cspell must pass on a clean file, got %v", err)
	}
}

// TestGitleaksStep_DetectsSecret is the ac-45 detection proof: a planted file
// with a recognizable secret must make the gitleaks step fail non-zero. Run
// with --no-git so gitleaks scans the temp dir as plain files (no repo needed);
// exit code 1 on a finding is what fails the gate.
func TestGitleaksStep_DetectsSecret(t *testing.T) {
	requireTool(t, "gitleaks")

	dir := t.TempDir()
	// A canonical AWS example key pair — inert test data, but gitleaks' default
	// ruleset flags it. Assembled from rune slices so this committed source file
	// carries no scannable secret-shaped token for the gate's own cspell/gitleaks
	// passes to trip over.
	keyID := string([]rune{'A', 'K', 'I', 'A', 'I', 'O', 'S', 'F', 'O', 'D', 'N', 'N', '7', 'E', 'X', 'A', 'M', 'P', 'L', 'E'})
	keySecret := string([]rune{'w', 'J', 'a', 'l', 'r', 'X', 'U', 't', 'n', 'F', 'E', 'M', 'I', '/', 'K', '7', 'M', 'D', 'E', 'N', 'G', '/', 'b', 'P', 'x', 'R', 'f', 'i', 'C', 'Y', 'E', 'M', 'P', 'L', 'E', 'K', 'E', 'Y', 'x', 'x'})
	content := "aws_access_key_id = \"" + keyID + "\"\n" +
		"aws_secret_access_key = \"" + keySecret + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "leak.conf"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runStep(context.Background(), discardLogger(), dir, step{
		name:    "gitleaks-selftest",
		program: "gitleaks",
		args:    []string{"detect", "--no-git", "--no-banner", "--redact", "--source", dir},
	})
	if err == nil {
		t.Fatal("gitleaks must fail on a planted secret — the gate is not detecting")
	}
}

// TestGitleaksPinPresenceOnly guards that gitleaks is registered as a
// presence-only pin (no version probe): go install does not stamp its version.
func TestGitleaksPinPresenceOnly(t *testing.T) {
	pin, ok := pinnedTools["gitleaks"]
	if !ok {
		t.Fatal("gitleaks must be registered in pinnedTools")
	}
	if pin.want != "" || len(pin.versionArgs) != 0 {
		t.Fatalf("gitleaks pin must be presence-only, got want=%q versionArgs=%v", pin.want, pin.versionArgs)
	}
}

// TestCiStepsHasStaticBattery asserts the gate wiring includes every pinned
// static tool and marks deadcode advisory, so a refactor cannot silently drop a
// step.
func TestCiStepsHasStaticBattery(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	byName := map[string]step{}
	for _, s := range ciSteps(root) {
		byName[s.name] = s
	}
	for _, want := range []string{"golangci-lint", "govulncheck", "cspell", "gitleaks", "shellcheck", "shfmt", "deadcode"} {
		s, ok := byName[want]
		if !ok {
			t.Errorf("ci gate is missing the %q step", want)
			continue
		}
		if s.tool == "" {
			t.Errorf("step %q must be version-pinned (tool unset)", want)
		}
	}
	if !byName["deadcode"].advisory {
		t.Error("deadcode step must be advisory")
	}
	if byName["golangci-lint"].advisory {
		t.Error("golangci-lint must be blocking, not advisory")
	}
}
