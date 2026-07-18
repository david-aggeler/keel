package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
)

func TestFindModuleRoot(t *testing.T) {
	// From a subdirectory of a keel-shaped module, the root is found.
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	sub := filepath.Join(root, "cmd", "keel-dev")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findModuleRoot(sub)
	if err != nil {
		t.Fatalf("findModuleRoot(%s) = %v", sub, err)
	}
	if got != root {
		t.Fatalf("findModuleRoot = %q, want %q", got, root)
	}

	// A foreign module is refused, not silently gated.
	foreign := t.TempDir()
	writeFile(t, foreign, "go.mod", "module example.com/other\n\ngo 1.25\n")
	if _, err := findModuleRoot(foreign); err == nil {
		t.Fatal("foreign module should be refused, got nil")
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-57
func TestRunDirectVersionHelpAndNoCommandBranches(t *testing.T) {
	oldVersion := version
	version = "v1.2.3"
	t.Cleanup(func() { version = oldVersion })

	stdout, stderr := captureProcessStreams(t, func() {
		if code := run([]string{"--version"}); code != 0 {
			t.Fatalf("run --version exit = %d, want 0", code)
		}
	})
	if strings.TrimSpace(stdout) != "v1.2.3" || stderr != "" {
		t.Fatalf("run --version stdout=%q stderr=%q", stdout, stderr)
	}

	stdout, stderr = captureProcessStreams(t, func() {
		if code := run([]string{"--help-all"}); code != 0 {
			t.Fatalf("run --help-all exit = %d, want 0", code)
		}
	})
	if stdout != "" || !strings.Contains(stderr, "test-bridge tests run") {
		t.Fatalf("run --help-all stdout=%q stderr=%q, want help on stderr", stdout, stderr)
	}

	stdout, stderr = captureProcessStreams(t, func() {
		if code := run(nil); code != 2 {
			t.Fatalf("run nil exit = %d, want 2", code)
		}
	})
	if stdout != "" || !strings.Contains(stderr, "Usage:") {
		t.Fatalf("run nil stdout=%q stderr=%q, want usage on stderr", stdout, stderr)
	}
}

// DHF-TEST: keel/requirement-11
func TestRunDirectCIDispatchesThroughLoggerAndGate(t *testing.T) {
	callsFile := stubTools(t, false, false)
	root := moduleFixture(t)
	t.Chdir(root)

	stdout, stderr := captureProcessStreams(t, func() {
		if code := run([]string{"--no-header", "ci"}); code != 0 {
			t.Fatalf("run ci exit = %d, want 0\ncalls:\n%s", code, calls(t, callsFile))
		}
	})
	if !strings.Contains(stdout, "ci gate green") {
		t.Fatalf("run ci stdout missing success log:\n%s", stdout)
	}
	if stderr != "" {
		t.Fatalf("run ci stderr = %q, want empty", stderr)
	}
	got := calls(t, callsFile)
	for _, want := range []string{"go build ./...", "go test ./...", "go tool cover"} {
		if !strings.Contains(got, want) {
			t.Fatalf("run ci calls missing %q:\n%s", want, got)
		}
	}
}

func TestLintNoStdlibLog(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, dir, "bad.go", "package p\n\nimport \"log\"\n\nvar _ = log.Default\n")
	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-stdlib-log") {
		t.Fatalf("stdlib log import should fail lint, got %v", err)
	}

	// log/slog is allowed.
	writeFile(t, dir, "bad.go", "package p\n\nimport \"log/slog\"\n\nvar _ = slog.Default\n")
	if err := runLint(dir); err != nil {
		t.Fatalf("log/slog should pass lint, got %v", err)
	}
}

func TestLintNoRawFmtOutput(t *testing.T) {
	dir := t.TempDir()
	keeldev := filepath.Join(dir, "cmd", "keel-dev")
	if err := os.MkdirAll(keeldev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, keeldev, "out.go",
		"package main\n\nimport \"fmt\"\n\nfunc x() { fmt.Println(\"run output\") }\n")
	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-raw-fmt-output") {
		t.Fatalf("raw fmt output in keel-dev should fail lint, got %v", err)
	}

	// fmt.Sprintf constructs a value — allowed.
	writeFile(t, keeldev, "out.go",
		"package main\n\nimport \"fmt\"\n\nvar _ = fmt.Sprintf(\"x\")\n")
	if err := runLint(dir); err != nil {
		t.Fatalf("fmt.Sprintf should pass lint, got %v", err)
	}
}

// DHF-TEST: keel/requirement-15
func TestLintNoRawFmtOutputScansLibrarySurface(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	for _, sub := range []string{"log", "exec"} {
		pkgDir := filepath.Join(dir, sub)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, pkgDir, "out.go",
			"package "+strings.ReplaceAll(sub, "-", "")+"\n\nimport \"fmt\"\n\nfunc x() { fmt.Println(\"diagnostic bypass\") }\n")
		err := runLint(dir)
		if err == nil || !strings.Contains(err.Error(), "no-raw-fmt-output") || !strings.Contains(err.Error(), sub+"/out.go") {
			t.Fatalf("raw fmt output in %s should fail lint with package path, got %v", sub, err)
		}
		writeFile(t, pkgDir, "out.go", "package "+strings.ReplaceAll(sub, "-", "")+"\n\nfunc x() {}\n")
	}
}

// TestLintNoRawStdoutStream proves the ac-36 policy flags os.Stdout/os.Stderr
// references outside the main.go allowlist and names the offending function.
func TestLintNoRawStdoutStream(t *testing.T) {
	dir := t.TempDir()
	keeldev := filepath.Join(dir, "cmd", "keel-dev")
	if err := os.MkdirAll(keeldev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, keeldev, "stream.go",
		"package main\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\nfunc handOff() io.Writer { return os.Stdout }\n")
	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-raw-stdout-stream") || !strings.Contains(err.Error(), "handOff") {
		t.Fatalf("raw stdout handoff should fail lint naming the function, got %v", err)
	}

	// The same reference inside an allowlisted main.go function passes.
	writeFile(t, keeldev, "stream.go",
		"package main\n\nimport \"io\"\n\nvar _ io.Writer\n")
	writeFile(t, keeldev, "main.go",
		"package main\n\nimport (\n\t\"io\"\n\t\"os\"\n)\n\nfunc newLogger() io.Writer { return os.Stdout }\n")
	if err := runLint(dir); err != nil {
		t.Fatalf("allowlisted os.Stdout in newLogger should pass, got %v", err)
	}
}

// DHF-TEST: keel/requirement-77
func TestLintRejectsRetiredDesiredStateVocabulary(t *testing.T) {
	dir := t.TempDir()
	keeldev := filepath.Join(dir, "cmd", "keel-dev")
	if err := os.MkdirAll(keeldev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	retiredType := string([]byte{83, 101, 116, 117, 112, 80, 108, 97, 110})
	writeFile(t, keeldev, "desired_state.go", "package main\n\ntype "+retiredType+" struct{}\n")

	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-retired-desired-state-vocabulary") || !strings.Contains(err.Error(), filepath.Join("cmd", "keel-dev", "desired_state.go")) {
		t.Fatalf("retired desired-state vocabulary should fail lint naming the file, got %v", err)
	}
}

// DHF-TEST: keel/requirement-77
func TestLintRejectsRetiredDesiredStateVocabularyInVSIXJavaScript(t *testing.T) {
	dir := t.TempDir()
	fixtures := filepath.Join(dir, "vsix", "src", "test", "fixtures")
	if err := os.MkdirAll(fixtures, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	retiredWireLabel := string([]byte{115, 101, 116, 117, 112, 45, 112, 108, 97, 110})
	writeFile(t, fixtures, "fake-adapter.js", "const documentType = '"+retiredWireLabel+"';\n")

	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-retired-desired-state-vocabulary") || !strings.Contains(err.Error(), filepath.Join("vsix", "src", "test", "fixtures", "fake-adapter.js")) {
		t.Fatalf("retired desired-state vocabulary in VSIX JavaScript should fail lint naming the file, got %v", err)
	}
}

// DHF-TEST: keel/requirement-77
func TestLintRejectsRetiredDesiredStateVocabularyInVSIXTypeScriptTests(t *testing.T) {
	dir := t.TempDir()
	suite := filepath.Join(dir, "vsix", "src", "test", "suite")
	if err := os.MkdirAll(suite, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	retiredType := string([]byte{83, 101, 116, 117, 112, 80, 108, 97, 110})
	writeFile(t, suite, "extension.test.ts", "const doc = { kind: '"+retiredType+"' };\n")

	err := runLint(dir)
	if err == nil || !strings.Contains(err.Error(), "no-retired-desired-state-vocabulary") || !strings.Contains(err.Error(), filepath.Join("vsix", "src", "test", "suite", "extension.test.ts")) {
		t.Fatalf("retired desired-state vocabulary in VSIX TypeScript test file should fail lint naming the file, got %v", err)
	}
}

// TestCoverageFloorFails proves the ac-37 gate rejects a total below the floor.
func TestCoverageFloorFails(t *testing.T) {
	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "tool cover") echo "total:	(statements)	10.0%" ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runTestWithCoverage(context.Background(), discardLogger(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "below the 90.0% floor") {
		t.Fatalf("want coverage-floor failure, got %v", err)
	}
}

// DHF-TEST: keel/requirement-12
func TestCoverageUsesAllPackageDenominator(t *testing.T) {
	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2" in
  "tool cover") echo "total:	(statements)	92.0%" ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := runTestWithCoverage(context.Background(), discardLogger(), t.TempDir()); err != nil {
		t.Fatalf("coverage run failed: %v", err)
	}
	got := calls(t, callsFile)
	if !strings.Contains(got, "-coverpkg=./...") {
		t.Fatalf("coverage gate must use all-package denominator; calls:\n%s", got)
	}
}

func TestParseCoverageTotal(t *testing.T) {
	if _, err := parseCoverageTotal("garbage\n"); err == nil {
		t.Error("missing total line should error")
	}
	if _, err := parseCoverageTotal("total:\t(statements)\tnot-a-number%\n"); err == nil {
		t.Error("unparseable percentage should error")
	}
	got, err := parseCoverageTotal("foo 1%\ntotal:\t(statements)\t88.7%\n")
	if err != nil || got != 88.7 {
		t.Errorf("parse = %v, %v; want 88.7, nil", got, err)
	}
}

// DHF-TEST: keel/requirement-22
func TestLogCoreDependencyQuarantineRejectsOpenTelemetryReachability(t *testing.T) {
	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2 $3" in
  "list -deps ./log") printf '%s\n' "errors" "log/slog" "`+modulePath+`/log" "go.opentelemetry.io/otel/sdk/log" ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "log"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := runLogCoreDependencyQuarantine(context.Background(), discardLogger(), dir)
	if err == nil || !strings.Contains(err.Error(), "log core dependency quarantine failed") || !strings.Contains(err.Error(), "go.opentelemetry.io/otel/sdk/log") {
		t.Fatalf("want OTel dependency quarantine failure, got %v", err)
	}
}

// DHF-TEST: keel/requirement-22
func TestLogCoreDependencyQuarantineAllowsStdlibAndOwnModule(t *testing.T) {
	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
case "$1 $2 $3" in
  "list -deps ./log") printf '%s\n' "errors" "log/slog" "`+modulePath+`/log" ;;
esac
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "log"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := runLogCoreDependencyQuarantine(context.Background(), discardLogger(), dir); err != nil {
		t.Fatalf("stdlib and own-module deps should pass, got %v", err)
	}
}

// TestLintSelf holds keel's own tree to its own lint policies.
func TestLintSelf(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	if err := runLint(root); err != nil {
		t.Fatalf("keel fails its own lint:\n%v", err)
	}
}

// DHF-TEST: keel/requirement-27
func TestBuildToolingCoversAdmittedBinaries(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}

	justfile, err := os.ReadFile(filepath.Join(root, "Justfile"))
	if err != nil {
		t.Fatal(err)
	}
	setupRepo, err := os.ReadFile(filepath.Join(root, "scripts", "setup_repo.sh"))
	if err != nil {
		t.Fatal(err)
	}

	for _, src := range []struct {
		name string
		body string
	}{
		{name: "Justfile", body: string(justfile)},
		{name: "scripts/setup_repo.sh", body: string(setupRepo)},
	} {
		for _, want := range []string{
			"go build -o bin/keel-dev ./cmd/keel-dev",
			"go build -o bin/keel-demo ./cmd/keel-demo",
			"go build -o bin/keel-demo-dev ./cmd/keel-demo-dev",
		} {
			if !strings.Contains(src.body, want) {
				t.Fatalf("%s missing admitted binary build command %q", src.name, want)
			}
		}
	}
	for _, want := range []string{"./bin/keel-dev", "./bin/keel-demo", "./bin/keel-demo-dev"} {
		if !strings.Contains(string(setupRepo), want) {
			t.Fatalf("scripts/setup_repo.sh completion message missing %s", want)
		}
	}
}

func TestRunRejectsUnknownFlagAndExtraArgs(t *testing.T) {
	if code := run([]string{"ci", "--jsn"}); code != 2 {
		t.Fatalf("unknown flag should exit 2, got %d", code)
	}
	if code := run([]string{"ci", "--mode", "bogus"}); code != 2 {
		t.Fatalf("unknown mode should exit 2, got %d", code)
	}
	if code := run([]string{"ci", "extra"}); code != 2 {
		t.Fatalf("ci with an argument should exit 2, got %d", code)
	}
}

// TestRunDispatch covers the verb dispatch and exit-code contract end to end
// (help, missing verb, unknown verb, arg-count refusals, and a verify run
// against the stub toolchain — flags accepted after the verb per ac-34).
func TestRunDispatch(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want int
	}{
		{"no args", nil, 2},
		{"help verb", []string{"help"}, 0},
		{"help flag", []string{"--help"}, 0},
		{"unknown verb", []string{"frobnicate"}, 2},
		{"release missing arg", []string{"release"}, 2},
		{"release two args", []string{"release", "v1.0.0", "v2.0.0"}, 2},
		{"verify missing arg", []string{"verify"}, 2},
		{"verify bad version", []string{"verify", "not-semver"}, 1},
	}
	for _, c := range cases {
		if got := run(c.argv); got != c.want {
			t.Errorf("%s: run(%v) = %d, want %d", c.name, c.argv, got, c.want)
		}
	}
}

// DHF-TEST: keel/requirement-21
func TestKeelDevUsesGeneratedCommandTreeHelp(t *testing.T) {
	tree := commandTree()

	for _, path := range [][]string{
		{"ci"},
		{"release"},
		{"verify"},
	} {
		var help bytes.Buffer
		tree.RenderTopicHelp(&help, path)
		got := help.String()
		if !strings.Contains(got, strings.Join(path, " ")+" commands:") {
			t.Fatalf("topic %v missing generated heading:\n%s", path, got)
		}
		if !strings.Contains(got, "keel-dev "+strings.Join(path, " ")) {
			t.Fatalf("topic %v missing generated usage:\n%s", path, got)
		}
	}

	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"const usage", "switch verb"} {
		if strings.Contains(string(src), forbidden) {
			t.Fatalf("keel-dev migrated help/dispatch must not contain %q", forbidden)
		}
	}
}

// DHF-TEST: keel/requirement-57
func TestKeelDevHelpAllRendersFullCommandTreeAndExitsZero(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "keel-dev")
	build := exec.Command("go", "build", "-o", exe, ".")
	var buildOut bytes.Buffer
	build.Stdout = &buildOut
	build.Stderr = &buildOut
	if err := build.Run(); err != nil {
		t.Fatalf("go build failed: %v\noutput:\n%s", err, buildOut.String())
	}

	cmd := exec.Command(exe, "--help-all")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("keel-dev --help-all failed: %v\noutput:\n%s", err, out.String())
	}

	got := out.String()
	for _, want := range []string{
		"keel-dev is keel's development CLI.",
		"--help-all",
		"ci commands:",
		"release commands:",
		"test-bridge commands:",
		"test-bridge tests commands:",
		"vsix commands:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("keel-dev --help-all missing %q\noutput:\n%s", want, got)
		}
	}
	if strings.Contains(got, "keel-dev ci\n\nkeel-dev ci") {
		t.Fatalf("keel-dev --help-all appears to duplicate ci help\noutput:\n%s", got)
	}
}

// DHF-TEST: keel/requirement-21
func TestVersionStringDefaultsAndUsesStampedVersion(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })

	version = ""
	if got := versionString(); got != "dev" {
		t.Fatalf("default version = %q, want dev", got)
	}
	version = "v1.2.3"
	if got := versionString(); got != "v1.2.3" {
		t.Fatalf("stamped version = %q, want v1.2.3", got)
	}
}

// DHF-TEST: keel/requirement-18
func TestExitForMapsUsageChildAndGenericErrors(t *testing.T) {
	if got := exitFor(discardLogger(), cli.NewUsageError("bad args")); got != 2 {
		t.Fatalf("usage error exit = %d, want 2", got)
	}

	err := exec.Command("sh", "-c", "exit 7").Run()
	if err == nil {
		t.Fatal("stub command succeeded; want exit error")
	}
	if got := exitFor(discardLogger(), err); got != 7 {
		t.Fatalf("child exit error exit = %d, want 7", got)
	}

	if got := exitFor(discardLogger(), os.ErrInvalid); got != 1 {
		t.Fatalf("generic error exit = %d, want 1", got)
	}
}

// TestRunVerifyVerbHappyPath drives the whole CLI surface (flag after verb,
// three-sink logger, module root resolution) with a stub go on PATH.
func TestRunVerifyVerbHappyPath(t *testing.T) {
	stubTools(t, false, false)
	if code := run([]string{"verify", "v9.9.9", "--mode", "json"}); code != 0 {
		t.Fatalf("verify verb = %d, want 0", code)
	}
}
