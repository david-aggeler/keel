package main

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
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

	pin := pathToolPin("faketool", "v9.9.9")
	if _, err := resolveOne(t, pin); err != nil {
		t.Fatalf("matching version should verify, got %v", err)
	}
}

// TestVerifyToolPin_VersionMismatch: a present tool at the wrong version fails
// loud, and the error names the tool and the expected version.
func TestVerifyToolPin_VersionMismatch(t *testing.T) {
	dir := scrubPATH(t)
	writeExecStub(t, dir, "faketool", "faketool version v1.0.0", 0)

	pin := pathToolPin("faketool", "v9.9.9")
	_, err := resolveOne(t, pin)
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

	pin := pathToolPin("definitely-absent-tool", "v2.3.4")
	_, err := resolveOne(t, pin)
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
	pin := toolPin{name: "presencetool", install: toolInstall{method: toolInstallPath}}

	if _, err := resolveOne(t, pin); err == nil {
		t.Fatal("presence-only pin should fail when binary is absent")
	}

	writeExecStub(t, dir, "presencetool", "", 0)
	if _, err := resolveOne(t, pin); err != nil {
		t.Fatalf("presence-only pin should pass when binary exists, got %v", err)
	}
}

// TestRunStepToolGate_Missing: a subprocess step whose pinned tool is absent
// fails via the version gate before it ever spawns the tool.
func TestRunStepToolGate_Missing(t *testing.T) {
	t.Setenv(toolCacheEnv, t.TempDir())
	scrubPATH(t)
	// deadcode is a real keel-dev config pin; on a scrubbed PATH it is absent.
	err := runStep(context.Background(), discardLogger(), ".", step{
		name: "deadcode", tool: "deadcode", program: "deadcode", args: []string{"-test", "./..."}, advisory: true,
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

// DHF-TEST: keel/requirement-17, keel/requirement-24, keel/requirement-25
func TestRunStepQuietStderrReclassifiesKnownBenignToolProgress(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' 'CSpell: Files checked: 55, Issues found: 0 in 0 files.' >&2\n"
	tool := filepath.Join(dir, "quiet-tool")
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	logger, cap := testLogger("keel-dev")
	if err := runStep(context.Background(), logger, ".", step{
		name: "cspell", program: tool, quietStderr: true,
	}); err != nil {
		t.Fatalf("quiet stderr step should pass, got %v", err)
	}

	var sawReclassified bool
	for _, rec := range cap.AllJSON() {
		if rec["event_type"] == "process_output" && rec["stream"] == "stderr" && rec["data"] == "CSpell: Files checked: 55, Issues found: 0 in 0 files." {
			if rec["level"] == "ERROR" {
				t.Fatalf("quiet stderr progress surfaced at ERROR: %#v", rec)
			}
			if rec["level"] == "DEBUG" {
				sawReclassified = true
			}
		}
	}
	if !sawReclassified {
		t.Fatalf("did not find reclassified stderr progress record; records=%#v", cap.AllJSON())
	}
}

// DHF-TEST: keel/requirement-12, keel/requirement-8
func TestStaticDetectionSelfTestsDoNotSkip(t *testing.T) {
	src, err := parser.ParseFile(token.NewFileSet(), "tools_test.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TestCspellStep_FailsOnMisspelling", "TestGitleaksStep_DetectsSecret"} {
		fn := testFunc(src, name)
		if fn == nil {
			t.Fatalf("%s not found", name)
		}
		if callsHelper(fn, "require"+"Tool") {
			t.Fatalf("%s must be non-skippable; do not call requireTool", name)
		}
		if callsMethod(fn, "Skip") || callsMethod(fn, "Skipf") || callsMethod(fn, "SkipNow") {
			t.Fatalf("%s must be non-skippable; do not call t.Skip", name)
		}
	}
}

func testFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func callsHelper(fn *ast.FuncDecl, name string) bool {
	var found bool
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok && ident.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

func callsMethod(fn *ast.FuncDecl, name string) bool {
	var found bool
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestCspellStep_FailsOnMisspelling is the anti-vacuous-pass guard: with the
// repo's committed cspell.json, a file containing a word that is in no
// dictionary MUST fail the cspell step. This proves the spell-check step
// actually evaluates rules — a rule-less or empty config could never make this
// fail. The misspelled token is assembled at runtime so the committed test
// source carries no unknown word for the real gate's cspell run to flag.
func TestCspellStep_FailsOnMisspelling(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	config := filepath.Join(root, "cspell.json")

	// A nonsense consonant run, built from runes so no unknown word literal
	// appears in this source file.
	bad := string([]rune{'z', 'q', 'x', 'v', 'w', 'k', 'j', 'b', 'f'})
	fixtureDir, err := os.MkdirTemp(root, "cspell-selftest-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(fixtureDir)
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

// TestCspellStepFailure_NamesDictionaryAndRegistrationAction proves keel/ac-502:
// when the gate's cspell stage reds on an unknown word, its failure does not
// stop at the offending token. It also names the committed dictionary that
// declares the repo's spelling convention and states the action that registers
// a deliberate coinage, so the author learns the rule from the failure instead
// of by search.
//
// The assertions are anchored on facts rather than on a golden copy of the
// message: the named dictionary must be the one cspell.json actually loads and
// must be git-tracked, and the named action — appending the exact word to that
// dictionary — must be the one that turns the red run green. A test pinned to
// the exact wording would only prove that a constant equals itself, and would
// red on every future rewording.
//
// DHF-TEST: keel/requirement-130
func TestCspellStepFailure_NamesDictionaryAndRegistrationAction(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	config := filepath.Join(root, "cspell.json")

	// The expected dictionary is read from the committed config here, not from
	// the production helper under test, so the two derivations stay independent.
	dict := addWordsDictionary(t, config)
	if _, err := os.Stat(filepath.Join(root, dict)); err != nil {
		t.Fatalf("dictionary named by cspell.json is not in the tree: %v", err)
	}
	mustRun(t, root, "git", "ls-files", "--error-unmatch", dict)

	// A nonsense consonant run, built from runes so no unknown word literal
	// appears in this source file for the real gate's own cspell run to flag.
	bad := string([]rune{'z', 'q', 'x', 'v', 'w', 'k', 'j', 'b', 'f'})
	fixtureDir, err := os.MkdirTemp(root, "cspell-remedy-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(fixtureDir)
	fixture := filepath.Join(fixtureDir, "bad.md")
	if err := os.WriteFile(fixture, []byte("# heading\n\nThe word "+bad+" is not real.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The step under test is the gate's own cspell step, re-aimed at the fixture:
	// --root anchors cspell there while --config supplies the committed rulebook.
	// Pin resolution is dropped (the pinned-version probe is tool-pins' subject,
	// not this test's) but every other field, remedy included, is the gate's.
	s := stepByName(t, ciSteps(context.Background(), discardLogger(), root), "cspell")
	s.tool = ""
	s.resolver = nil
	s.args = []string{"--no-progress", "--root", fixtureDir, "--config", config, fixture}

	logger, capture := testLogger("keel-dev")
	err = runStep(context.Background(), logger, root, s)
	if err == nil {
		t.Fatal("cspell step must fail on an unknown word")
	}
	failure := err.Error()

	if !strings.Contains(failure, dict) {
		t.Errorf("failure must name the dictionary %q, got: %s", dict, failure)
	}
	if !strings.Contains(strings.ToLower(failure), "add") {
		t.Errorf("failure must state the registration action, got: %s", failure)
	}

	// "In addition to the offending word and its location": the stage's own
	// report still reaches the operator through keel/log.
	logged := capture.buf.String()
	if !strings.Contains(logged, bad) || !strings.Contains(logged, "bad.md") {
		t.Errorf("stage output must still carry the offending word and its location, got: %s", logged)
	}

	// The named action is the one that works: register the exact word in a copy
	// of the dictionary and the same fixture passes. This is the leg that keeps
	// the remedy honest — a message naming a file that registration does not fix
	// would still be wrong.
	registered := t.TempDir()
	copyFileForTest(t, config, filepath.Join(registered, "cspell.json"))
	copiedDict := filepath.Join(registered, filepath.FromSlash(dict))
	if err := os.MkdirAll(filepath.Dir(copiedDict), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFileForTest(t, filepath.Join(root, dict), copiedDict)
	words, err := os.ReadFile(copiedDict)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copiedDict, append(words, []byte("\n"+bad+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	copyFileForTest(t, fixture, filepath.Join(registered, "bad.md"))

	s.args = []string{
		"--no-progress", "--root", registered,
		"--config", filepath.Join(registered, "cspell.json"),
		filepath.Join(registered, "bad.md"),
	}
	if err := runStep(context.Background(), discardLogger(), root, s); err != nil {
		t.Fatalf("registering the word in %s must make the stage green, got %v", dict, err)
	}
}

// addWordsDictionary returns the repo-relative path of the writable dictionary
// declared by the given cspell config — the test's own reading of the config,
// independent of the production helper it checks.
func addWordsDictionary(t *testing.T, config string) string {
	t.Helper()
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatalf("read cspell config: %v", err)
	}
	var parsed struct {
		DictionaryDefinitions []struct {
			Path     string `json:"path"`
			AddWords bool   `json:"addWords"`
		} `json:"dictionaryDefinitions"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse cspell config: %v", err)
	}
	for _, def := range parsed.DictionaryDefinitions {
		if def.AddWords && def.Path != "" {
			return filepath.ToSlash(filepath.Clean(def.Path))
		}
	}
	t.Fatalf("cspell config %s declares no writable dictionary", config)
	return ""
}

func copyFileForTest(t *testing.T, src, dst string) {
	t.Helper()
	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// TestGitleaksStep_DetectsSecret is the ac-45 detection proof: a planted file
// with a recognizable secret must make the gitleaks step fail non-zero. Run
// with --no-git so gitleaks scans the temp dir as plain files (no repo needed);
// exit code 1 on a finding is what fails the gate.
func TestGitleaksStep_DetectsSecret(t *testing.T) {
	dir := t.TempDir()
	// An inert AWS-shaped key pair that gitleaks' default ruleset flags.
	// Deliberately NOT the canonical AWS documented example key: recent gitleaks
	// (>=8.21) allowlists that well-known example, so it no longer trips
	// detection — a different, non-example key id is what proves the gate works.
	// Assembled from rune slices so this committed source file carries no
	// scannable secret-shaped token for the gate's own cspell/gitleaks passes.
	keyID := string([]rune{'A', 'K', 'I', 'A', '3', 'M', '7', 'Q', 'K', '2', 'P', '9', 'R', 'J', 'T', 'Z', '5', 'W', 'Y', '4'})
	keySecret := string([]rune{'w', 'J', 'a', 'l', 'r', 'X', 'U', 't', 'n', 'F', 'E', 'M', 'I', '/', 'K', '7', 'M', 'D', 'E', 'N', 'G', '/', 'b', 'P', 'x', 'R', 'f', 'i', 'C', 'Y', 'z', '9', 'Q', '2', 'p', '4', 'R', 'f', 'i', 'C', 'a'})
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
	pin, ok := defaultKeelDevConfig().toolPins()["gitleaks"]
	if !ok {
		t.Fatal("gitleaks must be registered in keel-dev config")
	}
	if pin.want != "" || len(pin.versionArgs) != 0 {
		t.Fatalf("gitleaks pin must be presence-only, got want=%q versionArgs=%v", pin.want, pin.versionArgs)
	}
}

// DHF-TEST: keel/requirement-120 (keel/ac-460)
func TestModuleHygieneStepFailsOnUntidyGoMod(t *testing.T) {
	requireTool(t, "go")

	dir := moduleHygieneFixture(t)
	step := stepByName(t, ciSteps(context.Background(), discardLogger(), dir), "module-hygiene")

	err := runStep(context.Background(), discardLogger(), dir, step)
	if err == nil {
		t.Fatal("module-hygiene should fail when go mod tidy would change go.mod")
	}
	if !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("module-hygiene error should name go.mod, got %v", err)
	}
}

// DHF-TEST: keel/requirement-120 (keel/ac-461)
func TestModuleHygieneStepDoesNotMutateManifestFiles(t *testing.T) {
	requireTool(t, "go")

	tidy := moduleHygieneFixture(t)
	mustRun(t, tidy, "go", "mod", "tidy")
	beforeGoMod := mustReadFile(t, tidy, "go.mod")
	beforeGoSum := mustReadFile(t, tidy, "go.sum")
	if err := runStep(context.Background(), discardLogger(), tidy, stepByName(t, ciSteps(context.Background(), discardLogger(), tidy), "module-hygiene")); err != nil {
		t.Fatalf("module-hygiene should pass on a tidy module, got %v", err)
	}
	if got := mustReadFile(t, tidy, "go.mod"); got != beforeGoMod {
		t.Fatalf("module-hygiene mutated tidy go.mod:\n%s", got)
	}
	if got := mustReadFile(t, tidy, "go.sum"); got != beforeGoSum {
		t.Fatalf("module-hygiene mutated tidy go.sum:\n%s", got)
	}

	untidy := moduleHygieneFixture(t)
	beforeGoMod = mustReadFile(t, untidy, "go.mod")
	beforeGoSum = mustReadFile(t, untidy, "go.sum")
	if err := runStep(context.Background(), discardLogger(), untidy, stepByName(t, ciSteps(context.Background(), discardLogger(), untidy), "module-hygiene")); err == nil {
		t.Fatal("module-hygiene should fail on an untidy module")
	}
	if got := mustReadFile(t, untidy, "go.mod"); got != beforeGoMod {
		t.Fatalf("module-hygiene mutated untidy go.mod:\n%s", got)
	}
	if got := mustReadFile(t, untidy, "go.sum"); got != beforeGoSum {
		t.Fatalf("module-hygiene mutated untidy go.sum:\n%s", got)
	}
}

// DHF-TEST: keel/requirement-8 (keel/ac-464)
func TestModuleZipStepFailsOnTrackedMalformedPath(t *testing.T) {
	requireTool(t, "git")

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	writeFile(t, dir, "go.mod", "module example.com/main\n\ngo 1.25\n")
	writeFile(t, dir, keelDevConfigFile, "gate:\n  excludes:\n    - docs/auto-generated/**\n")
	badDir := filepath.Join(dir, "docs", "auto-generated", "sbom", "licenses", "go-.", "example.com", "dep")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join("docs", "auto-generated", "sbom", "licenses", "go-.", "example.com", "dep", "LICENSE")
	writeFile(t, dir, badPath, "license\n")
	mustRun(t, dir, "git", "add", "go.mod", keelDevConfigFile, badPath)

	err := runStep(context.Background(), discardLogger(), dir, stepByName(t, ciSteps(context.Background(), discardLogger(), dir), "module-zip"))
	if err == nil {
		t.Fatal("module-zip should fail on a tracked path rejected by module zip rules")
	}
	if !strings.Contains(err.Error(), filepath.ToSlash(badPath)) || !strings.Contains(err.Error(), "trailing dot") {
		t.Fatalf("module-zip error should name the malformed tracked path and reason, got %v", err)
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
	for _, s := range ciSteps(context.Background(), discardLogger(), root) {
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
		if !s.quietStderr {
			t.Errorf("step %q must quiet benign stderr progress", want)
		}
	}
	if !byName["deadcode"].advisory {
		t.Error("deadcode step must be advisory")
	}
	if byName["golangci-lint"].advisory {
		t.Error("golangci-lint must be blocking, not advisory")
	}
}

func moduleHygieneFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/main\n\ngo 1.25\n\nrequire example.com/dep v0.0.0 // indirect\n\nreplace example.com/dep => ./dep\n")
	writeFile(t, dir, "go.sum", "")
	writeFile(t, dir, "main.go", "package main\n\nimport _ \"example.com/dep\"\n")
	if err := os.MkdirAll(filepath.Join(dir, "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, filepath.Join("dep", "go.mod"), "module example.com/dep\n\ngo 1.25\n")
	writeFile(t, dir, filepath.Join("dep", "dep.go"), "package dep\n")
	return dir
}

// DHF-TEST: keel/requirement-85
func TestCiStepsFileSelectingGatesUseTrackedFiles(t *testing.T) {
	requireTool(t, "git")

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	writeFile(t, dir, ".gitignore", "ignored.go\nignored.md\n")
	writeFile(t, dir, "tracked.go", "package p\n")
	writeFile(t, dir, "tracked.md", "# tracked\n")
	writeFile(t, dir, "ignored.go", "package p\n\nvar    Bad = 1\n")
	writeFile(t, dir, "ignored.md", "zzzzzzzzzz\n")
	writeFile(t, dir, "untracked.go", "package p\n\nvar    Bad = 1\n")
	writeFile(t, dir, "untracked.md", "zzzzzzzzzz\n")
	mustRun(t, dir, "git", "add", "tracked.go", "tracked.md", ".gitignore")

	byName := map[string]step{}
	logger, cap := testLogger("keel-dev")
	for _, s := range ciSteps(context.Background(), logger, dir) {
		byName[s.name] = s
	}
	if !processLifecycleRecorded(cap.AllJSON(), "git", []string{"ls-files"}) {
		t.Fatalf("ciSteps must list tracked files through keel/exec lifecycle logging; records=%#v", cap.AllJSON())
	}

	gofmtStep, ok := byName["gofmt"]
	if !ok {
		t.Fatal("ci gate is missing gofmt")
	}
	if !stringSliceEqual(gofmtStep.args, []string{"-l", "tracked.go"}) {
		t.Fatalf("gofmt args = %v, want tracked Go file only", gofmtStep.args)
	}

	cspellStep, ok := byName["cspell"]
	if !ok {
		t.Fatal("ci gate is missing cspell")
	}
	if !stringSliceEqual(cspellStep.args, []string{"--no-progress", "tracked.go", "tracked.md"}) {
		t.Fatalf("cspell args = %v, want tracked Go/Markdown files only", cspellStep.args)
	}
}

// DHF-TEST: keel/requirement-85
func TestGofmtGateIgnoresUntrackedAndIgnoredFilesButFailsTrackedOffenders(t *testing.T) {
	requireTool(t, "git")
	requireTool(t, "gofmt")

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	writeFile(t, dir, ".gitignore", "ignored.go\n")
	writeFile(t, dir, "tracked.go", "package p\n")
	writeFile(t, dir, "ignored.go", "package p\n\nvar    Ignored = 1\n")
	writeFile(t, dir, "untracked.go", "package p\n\nvar    Untracked = 1\n")
	mustRun(t, dir, "git", "add", ".gitignore", "tracked.go")

	if err := runStep(context.Background(), discardLogger(), dir, stepByName(t, ciSteps(context.Background(), discardLogger(), dir), "gofmt")); err != nil {
		t.Fatalf("gofmt should ignore untracked/gitignored offenders, got %v", err)
	}

	writeFile(t, dir, "tracked_bad.go", "package p\n\nvar    Tracked = 1\n")
	mustRun(t, dir, "git", "add", "tracked_bad.go")
	err := runStep(context.Background(), discardLogger(), dir, stepByName(t, ciSteps(context.Background(), discardLogger(), dir), "gofmt"))
	if err == nil {
		t.Fatal("gofmt should fail on a tracked offender, got nil")
	}
	if !strings.Contains(err.Error(), "tracked_bad.go") {
		t.Fatalf("gofmt error should name tracked offender, got %v", err)
	}
}

// DHF-TEST: keel/requirement-85
func TestCspellGateIgnoresUntrackedAndIgnoredFilesButFailsTrackedOffenders(t *testing.T) {
	requireTool(t, "git")

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	writeFile(t, dir, ".gitignore", "ignored.md\n")
	writeFile(t, dir, "tracked.md", "# tracked\n")
	writeFile(t, dir, "ignored.md", "ignored spelling offender\n")
	writeFile(t, dir, "untracked.md", "untracked spelling offender\n")
	mustRun(t, dir, "git", "add", ".gitignore", "tracked.md")

	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cspell := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "10.0.1"
  exit 0
fi
for arg in "$@"; do
  case "$arg" in
    *ignored.md|*untracked.md)
      echo "unexpected untracked input: $arg"
      exit 2
      ;;
    *tracked_bad.md)
      echo "tracked spelling offender"
      exit 1
      ;;
  esac
done
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "cspell"), []byte(cspell), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := runStep(context.Background(), discardLogger(), dir, stepByName(t, ciSteps(context.Background(), discardLogger(), dir), "cspell")); err != nil {
		t.Fatalf("cspell should ignore untracked/gitignored offenders, got %v", err)
	}

	writeFile(t, dir, "tracked_bad.md", "tracked spelling offender\n")
	mustRun(t, dir, "git", "add", "tracked_bad.md")
	if err := runStep(context.Background(), discardLogger(), dir, stepByName(t, ciSteps(context.Background(), discardLogger(), dir), "cspell")); err == nil {
		t.Fatal("cspell should fail on a tracked offender, got nil")
	}
}

// DHF-TEST: keel/requirement-85 (keel/ac-435)
func TestCspellGateExcludesCommittedGatePathsWithoutToolIgnore(t *testing.T) {
	requireTool(t, "git")

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	writeFile(t, dir, keelDevConfigFile, "gate:\n  excludes:\n    - .claude/**\n")
	writeFile(t, dir, "cspell.json", "{\"version\":\"0.2\",\"language\":\"en-US\",\"words\":[]}\n")
	writeFile(t, dir, "tracked.md", "# tracked\n")
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, ".claude/skills/generated.md", "catalog spelling offender\n")
	mustRun(t, dir, "git", "add", keelDevConfigFile, "cspell.json", "tracked.md", ".claude/skills/generated.md")

	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cspell := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "10.0.1"
  exit 0
fi
for arg in "$@"; do
  case "$arg" in
    *.claude/skills/generated.md|.claude/skills/generated.md)
      echo "excluded catalog spelling offender reached cspell: $arg"
      exit 1
      ;;
  esac
done
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "cspell"), []byte(cspell), 0o755); err != nil {
		t.Fatal(err)
	}

	cspellStep := stepByName(t, ciSteps(context.Background(), discardLogger(), dir), "cspell")
	if !stringSliceEqual(cspellStep.args, []string{"--no-progress", "tracked.md"}) {
		t.Fatalf("cspell args = %v, want excluded .claude path absent", cspellStep.args)
	}
	if err := runStep(context.Background(), discardLogger(), dir, cspellStep); err != nil {
		t.Fatalf("cspell should not receive excluded tracked path, got %v", err)
	}
}

// DHF-TEST: keel/requirement-118 (keel/ac-451)
func TestCIStepsReuseLoadedConfigForToolPins(t *testing.T) {
	requireTool(t, "git")

	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	writeFile(t, dir, keelDevConfigFile, "gate:\n  excludes:\n    - .claude/**\ntools:\n  pins:\n    - name: cspell\n      version_args: [--version]\n      want: 10.0.1\n      install:\n        method: path\n")
	writeFile(t, dir, "tracked.md", "# tracked\n")
	mustRun(t, dir, "git", "add", keelDevConfigFile, "tracked.md")

	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cspell := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "10.0.1"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "cspell"), []byte(cspell), 0o755); err != nil {
		t.Fatal(err)
	}

	cspellStep := stepByName(t, ciSteps(context.Background(), discardLogger(), dir), "cspell")
	writeFile(t, dir, keelDevConfigFile, "gate:\n  excludes:\n    - .claude/**\ntools:\n  pins:\n    - name: deadcode\n      install:\n        method: path\n")

	if err := runStep(context.Background(), discardLogger(), dir, cspellStep); err != nil {
		t.Fatalf("cspell step reread mutated config instead of reusing the loaded pin: %v", err)
	}
}

func stepByName(t *testing.T, steps []step, name string) step {
	t.Helper()
	for _, s := range steps {
		if s.name == name {
			return s
		}
	}
	t.Fatalf("ci gate is missing %q", name)
	return step{}
}

func processLifecycleRecorded(records []map[string]any, program string, args []string) bool {
	wantCommand := strings.TrimSpace(program + " " + strings.Join(args, " "))
	var sawStart, sawEnd bool
	for _, rec := range records {
		switch rec["event_type"] {
		case "process_start":
			commandLine, _ := rec["command_line"].(string)
			if rec["program"] == program && commandLine == wantCommand {
				sawStart = true
			}
		case "process_end":
			sawEnd = true
		}
	}
	return sawStart && sawEnd
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
