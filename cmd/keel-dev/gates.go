package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/david-aggeler/keel/cli"
	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
	zip "golang.org/x/mod/zip"
)

// step is one gate in the ci pipeline: either a labelled subprocess or an
// in-process check (fn). Subprocess steps may additionally judge their stdout
// (gofmt reports unformatted files on stdout while still exiting 0).
type step struct {
	name    string
	program string
	args    []string
	// maxOutputBytes, when positive, overrides keel/exec's default captured
	// stdout+stderr ceiling for this subprocess.
	maxOutputBytes int
	// stdoutFails, when set and returning a non-empty message for the captured
	// stdout, turns a zero-exit run into a failure carrying that message.
	stdoutFails func(stdout string) string
	// fn, when set, runs in-process instead of spawning a subprocess (used for
	// the compiled-in lint policies; keeps CI hermetic — no external lint binary).
	fn func(ctx context.Context, logger *slog.Logger, dir string) error
	// tool, when set, names an entry in the keel-dev config's tool pins whose
	// presence and exact version are verified before the subprocess runs
	// (keel/ac-42) — a missing or drifted external tool fails the gate loud,
	// never a silent skip.
	tool string
	// resolver resolves pinned tools to concrete binaries from the
	// already-loaded config. CI sets this when it builds the step list so one
	// gate process does not reread keel-dev.yaml between file selection and
	// subprocess execution, and so each tool is installed and probed once.
	resolver *toolResolver
	// advisory marks a step whose output is surfaced through keel/log but whose
	// failure (non-zero exit) never fails the gate (keel/ac-41: deadcode).
	advisory bool
	// quietStderr reclassifies only known-benign child stderr progress records
	// for noisy tools whose progress stream is not itself a failure signal.
	quietStderr bool
	// remedy, when set, is appended to this step's failure: the committed file
	// that declares the repo-local convention plus the action that satisfies it.
	// A stage enforcing a rule an author cannot infer from the language or the
	// toolchain must report the means of compliance next to the offense, so the
	// rule is learned from the failure instead of by search.
	// DHF-REQ: keel/requirement-130 (keel/ac-502)
	remedy string
}

type runLogLocator interface {
	RunLogPath() string
	RunLogLine() int
}

// ciSteps is the canonical gate definition: gofmt, build, vet, lint, test.
// Developers and the release preflight both run this exact sequence via
// `keel-dev ci`, so the gate lives in one place and never drifts between the
// local and release paths. keel runs no GitHub Actions CI; the local gate is
// the sole verification.
//
// The static-tool battery (golangci-lint, govulncheck, cspell, shellcheck,
// shfmt, advisory deadcode) is version-pinned via keel-dev.yaml and runs after
// the in-process checks. Each external tool is presence/version-verified before
// it runs (keel/ac-42) so a missing or drifted tool fails loud.
//
// DHF-REQ: keel/requirement-10, keel/requirement-11, keel/requirement-12, keel/requirement-107, keel/requirement-118 (keel/ac-451)
func ciSteps(ctx context.Context, logger *slog.Logger, dir string) []step {
	cfg, err := loadKeelDevConfig(dir)
	if err != nil {
		return []step{{name: "config", fn: func(context.Context, *slog.Logger, string) error { return err }}}
	}
	return ciStepsWithConfig(ctx, logger, dir, cfg)
}

// gateInputs are the per-checkout inputs the battery is built from: the file
// lists each file-selecting stage gates, the shell scripts, the cspell
// means-of-compliance text, and the tool pins. Collecting them is the only I/O
// in building the battery, so the stage inventory can be derived from the same
// builder with an empty gateInputs and no I/O at all (keel/ac-544). The stage
// set does not vary with these inputs — a stage with nothing to gate becomes a
// green no-op of the same name, never a missing command.
//
// DHF-REQ: keel/requirement-136 (keel/ac-544)
type gateInputs struct {
	scripts      []string
	gofmtFiles   []string
	gofmtErr     error
	cspellFiles  []string
	cspellErr    error
	cspellRemedy string
	lintFiles    []string
	lintErr      error
	pins         map[string]toolPin
}

func collectGateInputs(ctx context.Context, logger *slog.Logger, dir string, cfg keelDevConfig) gateInputs {
	in := gateInputs{cspellRemedy: cspellRemedy(dir), pins: cfg.toolPins()}
	// Shell scripts are enumerated up front so shellcheck/shfmt receive explicit
	// paths (no shell is involved to expand a glob). Sorted for stable output.
	in.scripts, _ = filepath.Glob(filepath.Join(dir, "scripts", "*.sh"))
	in.gofmtFiles, in.gofmtErr = trackedFilesWithExt(ctx, logger, dir, cfg.Gate.Excludes, ".go")
	in.cspellFiles, in.cspellErr = trackedFilesWithExt(ctx, logger, dir, cfg.Gate.Excludes, ".go", ".md")
	in.lintFiles, in.lintErr = trackedLintFiles(ctx, logger, dir, cfg.Gate.Excludes)
	return in
}

func ciStepsWithConfig(ctx context.Context, logger *slog.Logger, dir string, cfg keelDevConfig) []step {
	return ciStepsFrom(collectGateInputs(ctx, logger, dir, cfg))
}

// noopGateStage keeps a stage present, and green, when the checkout gives it
// nothing to gate. The stage's verdict is what it would have been had the stage
// been dropped; keeping the name means the declared command surface is the same
// in every checkout.
func noopGateStage(context.Context, *slog.Logger, string) error { return nil }

// ciStepsFrom builds the ordered battery from already-collected inputs. It is
// the single declaration of what stages exist and in which order they run: the
// bare `keel-dev ci` battery, a single named stage, and the declared command
// surface are all derived from this one slice.
func ciStepsFrom(in gateInputs) []step {
	scripts, gofmtFiles, gofmtListErr := in.scripts, in.gofmtFiles, in.gofmtErr
	cspellFiles, cspellListErr := in.cspellFiles, in.cspellErr
	lintFiles, lintListErr := in.lintFiles, in.lintErr
	gofmtStep := step{
		name:    "gofmt",
		program: "gofmt",
		args:    append([]string{"-l"}, gofmtFiles...),
		stdoutFails: func(out string) string {
			files := strings.TrimSpace(out)
			if files == "" {
				return ""
			}
			return "unformatted files:\n" + files
		},
	}
	if gofmtListErr != nil {
		gofmtStep = step{name: "gofmt", fn: func(context.Context, *slog.Logger, string) error { return gofmtListErr }}
	} else if len(gofmtFiles) == 0 {
		gofmtStep = step{name: "gofmt", fn: func(context.Context, *slog.Logger, string) error { return nil }}
	}
	cspellStep := step{
		name: "cspell", tool: "cspell", program: "cspell",
		args:        append([]string{"--no-progress"}, cspellFiles...),
		quietStderr: true,
		// Only the spelling verdict carries a remedy: the two fallbacks below fail
		// on file selection, which the dictionary cannot fix.
		remedy: in.cspellRemedy,
	}
	if cspellListErr != nil {
		cspellStep = step{name: "cspell", fn: func(context.Context, *slog.Logger, string) error { return cspellListErr }}
	} else if len(cspellFiles) == 0 {
		cspellStep = step{name: "cspell", fn: func(context.Context, *slog.Logger, string) error { return nil }}
	}
	lintStep := step{name: "lint", fn: func(_ context.Context, _ *slog.Logger, dir string) error {
		return runLint(dir, lintFiles)
	}}
	if lintListErr != nil {
		lintStep = step{name: "lint", fn: func(context.Context, *slog.Logger, string) error { return lintListErr }}
	}

	steps := []step{
		{name: "command-tree", fn: func(context.Context, *slog.Logger, string) error {
			return commandTree().ValidateTree()
		}},
		gofmtStep,
		{name: "build", program: "go", args: []string{"build", "./..."}},
		{name: "vet", program: "go", args: []string{"vet", "./..."}},
		lintStep,
		// DHF-REQ: keel/requirement-22
		{name: "log-core-deps", fn: runLogCoreDependencyQuarantine},
		{name: "module-hygiene", fn: runModuleHygiene},
		// This intentionally checks the git-tracked tree directly instead of the
		// gate.excludes-filtered file lists above: generated docs can be excluded
		// from prose/spell checks while still being part of the published module.
		// DHF-REQ: keel/requirement-8 (keel/ac-464)
		{name: "module-zip", fn: runModuleZipCheck},
		// Like module-zip, this reads the git-tracked tree directly: the SBOM
		// artifacts are excluded from the prose/spell file lists but are exactly
		// what keel publishes, and convergence is a property of the committed set.
		// DHF-REQ: keel/requirement-123 (keel/ac-495, keel/ac-496)
		{name: "sbom-convergence", fn: runSBOMConvergence},
		// --- static-tool battery (keel/requirement-12) ---
		// DHF-REQ: keel/requirement-12 (keel/ac-38)
		{name: "golangci-lint", tool: "golangci-lint", program: "golangci-lint", args: []string{"run", "./..."}, quietStderr: true},
		// DHF-REQ: keel/requirement-12 (keel/ac-39)
		{name: "govulncheck", tool: "govulncheck", program: "govulncheck", args: []string{"./..."}, quietStderr: true},
		// DHF-REQ: keel/requirement-12 (keel/ac-40)
		cspellStep,
		// gitleaks scans the git history + working tree for committed secrets and
		// exits non-zero on any finding (default --exit-code 1), so a leak fails
		// the gate. --no-banner keeps the log quiet; --redact prevents any matched
		// secret from being echoed through keel/log. The .gitleaks.toml at the
		// repo root (auto-loaded from the source path) supplies the ruleset +
		// keel's test-fixture allowlist. Version pin is enforced at install time
		// (presence-only here — see keel-dev.yaml), so this only fails loud if the
		// tool is missing (keel/ac-45).
		// DHF-REQ: keel/requirement-13 (keel/ac-45), keel/requirement-8
		{name: "gitleaks", tool: "gitleaks", program: "gitleaks", args: []string{"detect", "--no-banner", "--redact"}, quietStderr: true},
	}

	// DHF-REQ: keel/requirement-12 (keel/ac-43)
	shellcheckStep := step{name: "shellcheck", tool: "shellcheck", program: "shellcheck", args: scripts, quietStderr: true}
	// DHF-REQ: keel/requirement-12 (keel/ac-44)
	shfmtStep := step{
		name: "shfmt", tool: "shfmt", program: "shfmt",
		args:        append([]string{"-d"}, scripts...),
		quietStderr: true,
		stdoutFails: func(out string) string {
			diff := strings.TrimSpace(out)
			if diff == "" {
				return ""
			}
			return "shfmt found unformatted shell scripts:\n" + diff
		},
	}
	// A checkout with no shell scripts has nothing for either stage to gate. They
	// stay in the battery as green no-ops rather than disappearing, so the stage
	// set — and therefore the declared command surface — is checkout-independent
	// (keel/ac-544).
	if len(scripts) == 0 {
		shellcheckStep = step{name: "shellcheck", fn: noopGateStage}
		shfmtStep = step{name: "shfmt", fn: noopGateStage}
	}
	steps = append(steps, shellcheckStep, shfmtStep)

	// DHF-REQ: keel/requirement-12 (keel/ac-41) — advisory: reported, never fatal.
	// -test counts each package's tests as reachability roots (keel/issue-9): keel
	// is a library module with one binary (keel-dev), so log/ and exec/ public API
	// that keel-dev's main never calls — but the packages' own tests and external
	// consumers (vela, openbrain) do — is not genuinely dead. A function is reported
	// only when unused by main AND untested.
	steps = append(steps, step{name: "deadcode", tool: "deadcode", program: "deadcode", args: []string{"-test", "./..."}, advisory: true, quietStderr: true})

	// The coverage-floored test suite runs last: it is the most expensive step
	// and the fast static checks should fail before it does.
	steps = append(steps, step{name: "test", fn: runTestWithCoverage})

	resolver := newToolResolver(in.pins)
	attachToolResolver(steps, resolver)
	// The pin preflight runs before the first external tool: it resolves and
	// verifies EVERY pin in one pass, so a run enumerates all drifted or
	// un-installable tools at once instead of one per gate run (keel/issue-142).
	// DHF-REQ: keel/requirement-12 (keel/ac-42, keel/ac-465)
	tools := stepToolNames(steps)
	return insertStepBefore(steps, "golangci-lint", step{
		name: "tool-pins",
		fn: func(ctx context.Context, logger *slog.Logger, _ string) error {
			return resolver.verifyPins(ctx, logger, tools)
		},
	})
}

// gateStageNames returns the gate's stage names in battery order. It builds the
// battery from the same declaration a real run uses, with empty inputs and no
// I/O: the stage set is invariant over the checkout, so this is exactly the list
// a run executes. The declared command surface is derived from here, which is
// why there is no second list of stage names to maintain (keel/ac-544); the
// two-way match against a real run is pinned by
// TestGateStageInventoryMatchesTheRunningBatteryBothWays.
//
// DHF-REQ: keel/requirement-136 (keel/ac-544)
func gateStageNames() []string {
	steps := ciStepsFrom(gateInputs{})
	names := make([]string, 0, len(steps))
	for _, s := range steps {
		names = append(names, s.name)
	}
	return names
}

// selectGateStage returns the named stage out of an already-built battery, so a
// single stage is the very same step value the battery would have run — same
// arguments, same tool pin, same means-of-compliance text.
//
// DHF-REQ: keel/requirement-136 (keel/ac-543)
func selectGateStage(steps []step, name string) (step, bool) {
	for _, s := range steps {
		if s.name == name {
			return s, true
		}
	}
	return step{}, false
}

// cspellConfigFile is the committed rulebook the cspell stage evaluates.
const cspellConfigFile = "cspell.json"

// cspellRemedy composes the cspell stage's means-of-compliance text out of the
// committed cspell.json, so the failure names the dictionary the stage really
// loads instead of a hard-coded path that can drift away from it. An unreadable
// config, or one declaring no writable dictionary, yields no text: the stage's
// verdict is identical either way, and a guessed remedy is worse than none.
//
// DHF-REQ: keel/requirement-130 (keel/ac-502)
func cspellRemedy(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, cspellConfigFile))
	if err != nil {
		return ""
	}
	var cfg struct {
		Language              string `json:"language"`
		DictionaryDefinitions []struct {
			Path     string `json:"path"`
			AddWords bool   `json:"addWords"`
		} `json:"dictionaryDefinitions"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	for _, def := range cfg.DictionaryDefinitions {
		if !def.AddWords || def.Path == "" {
			continue
		}
		dict := filepath.ToSlash(filepath.Clean(def.Path))
		if cfg.Language == "" {
			return fmt.Sprintf("remedy: a deliberate coinage is registered, not reworded — add the exact word "+
				"on its own line in %s (the dictionary %s declares).", dict, cspellConfigFile)
		}
		return fmt.Sprintf("remedy: %s pins language %s for this repo, and a deliberate coinage is registered "+
			"rather than reworded — add the exact word on its own line in %s, or correct it to the %s spelling.",
			cspellConfigFile, cfg.Language, dict, cfg.Language)
	}
	return ""
}

// withRemedy appends a stage's means-of-compliance text to its failure, keeping
// the original error wrapped so exit-code extraction still reaches through it.
func withRemedy(err error, remedy string) error {
	if err == nil || remedy == "" {
		return err
	}
	return fmt.Errorf("%w\n%s", err, remedy)
}

// stepToolNames lists the pinned tools this step list will actually run, so the
// preflight verifies exactly those — no more, no less.
func stepToolNames(steps []step) []string {
	seen := map[string]bool{}
	var names []string
	for _, s := range steps {
		if s.tool != "" && !seen[s.tool] {
			seen[s.tool] = true
			names = append(names, s.tool)
		}
	}
	return names
}

func attachToolResolver(steps []step, resolver *toolResolver) {
	for i := range steps {
		if steps[i].tool != "" {
			steps[i].resolver = resolver
		}
	}
}

// insertStepBefore places s immediately before the named step, or appends it
// when that step is absent, so the pin preflight cannot silently vanish if the
// battery is reordered.
func insertStepBefore(steps []step, name string, s step) []step {
	for i := range steps {
		if steps[i].name == name {
			out := make([]step, 0, len(steps)+1)
			out = append(out, steps[:i]...)
			out = append(out, s)
			return append(out, steps[i:]...)
		}
	}
	return append(steps, s)
}

// DHF-REQ: keel/requirement-85 (keel/ac-454)
func trackedLintFiles(ctx context.Context, logger *slog.Logger, dir string, excludes gateExcludePatterns) ([]string, error) {
	return trackedFilesWithExt(ctx, logger, dir, excludes, ".go", ".js", ".ts", ".json")
}

// trackedFilesWithExt returns git-tracked, non-excluded repo-relative paths with
// the supplied extensions. The file-selecting gate steps use this as their input
// of record so untracked, gitignored, or keel-declared excluded scratch in the
// checkout cannot red the gate.
//
// DHF-REQ: keel/requirement-85 (keel/ac-435), keel/requirement-118 (keel/ac-451)
func trackedFilesWithExt(ctx context.Context, logger *slog.Logger, dir string, excludes gateExcludePatterns, exts ...string) ([]string, error) {
	tracked, err := listTrackedFiles(ctx, logger, dir)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(exts))
	for _, ext := range exts {
		want[ext] = true
	}
	var files []string
	for _, file := range tracked {
		if gatePathExcluded(file, excludes) || !want[filepath.Ext(file)] {
			continue
		}
		files = append(files, file)
	}
	return files, nil
}

// listTrackedFiles returns every git-tracked repo-relative path in dir, before
// any gate exclusion or extension filter. Steps that select by extension layer
// their filter on top; steps whose subject is an artifact set rather than a
// language (sbom-convergence) walk the raw list.
func listTrackedFiles(ctx context.Context, logger *slog.Logger, dir string) ([]string, error) {
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: "git",
		Args:    []string{"ls-files"},
		Dir:     dir,
		Logger:  logger,
	})
	if err != nil {
		return nil, fmt.Errorf("keel-dev: list git-tracked files: %w", err)
	}
	res, waitErr := proc.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("keel-dev: list git-tracked files: %w", waitErr)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("keel-dev: list git-tracked files: git ls-files exited %d", res.ExitCode)
	}
	var files []string
	for _, file := range strings.Split(res.Stdout, "\n") {
		if file = strings.TrimSpace(file); file != "" {
			files = append(files, file)
		}
	}
	return files, nil
}

type gateExcludePattern struct {
	raw             string
	recursivePrefix string
}

func gatePathExcluded(file string, patterns []gateExcludePattern) bool {
	file = filepath.ToSlash(file)
	for _, pattern := range patterns {
		if pattern.recursivePrefix != "" {
			if strings.HasPrefix(file, pattern.recursivePrefix) {
				return true
			}
			continue
		}
		matched, _ := path.Match(pattern.raw, file)
		if matched {
			return true
		}
	}
	return false
}

// runCI runs the verification gate in dir, fail-fast: the first failing step
// aborts and its error is returned. Every subprocess step is launched through
// keel/exec (START/END lifecycle logging) and every line of output flows
// through keel/log.
//
// DHF-REQ: keel/requirement-11
func runCI(ctx context.Context, logger *slog.Logger, dir string) error {
	return runCIWithRunLog(ctx, logger, nil, dir)
}

// runCIWithRunLog runs the CI gate and, when a per-run JSONL sink is available,
// wraps the first failing step in the structured OperationalError carrier.
//
// DHF-REQ: keel/requirement-18, keel/requirement-25, keel/requirement-118 (keel/ac-451)
func runCIWithRunLog(ctx context.Context, logger *slog.Logger, runLog runLogLocator, dir string) error {
	if runLogLogger, ok := runLog.(*logging.Logger); ok {
		runLogLogger.Section("ci")
	} else {
		logger.Info("ci", "banner", "section", "name", "ci")
	}
	// CR-74: the expected-red debt is printed on every gate run, never silent.
	if err := logExpectedRed(logger, dir); err != nil {
		return gateOperationalError("expected-red", "", 0, err)
	}
	cfg, err := loadKeelDevConfig(dir)
	if err != nil {
		return gateOperationalError("config", "", 0, err)
	}
	if err := runGateSteps(ctx, logger, runLog, dir, ciStepsWithConfig(ctx, logger, dir, cfg)); err != nil {
		return err
	}
	logger.Info("ci gate green")
	return nil
}

// runGateSteps runs an ordered slice of gate steps fail-fast. It is the single
// execution path for both the bare battery and one named stage: the per-stage
// records and the OperationalError carrier around the first failure are produced
// here, so a stage's verdict and failure output cannot differ between the two.
//
// DHF-REQ: keel/requirement-136 (keel/ac-542, keel/ac-543)
func runGateSteps(ctx context.Context, logger *slog.Logger, runLog runLogLocator, dir string, steps []step) error {
	for _, s := range steps {
		startLine := 0
		logFile := ""
		if runLog != nil {
			logFile = runLog.RunLogPath()
			if logFile != "" {
				startLine = runLog.RunLogLine() + 1
			}
		}
		logger.Info("gate started", "gate", s.name)
		if err := runStep(ctx, logger, dir, s); err != nil {
			return gateOperationalError(s.name, logFile, startLine, err)
		}
		logger.Info("gate passed", "gate", s.name)
	}
	return nil
}

// runGateStage runs exactly one stage of the gate, addressed by name, and no
// other. The stage is selected out of the battery the bare gate would have run,
// then executed through the shared path above, so clearing a stage alone clears
// it for the gate. An unknown name is refused naming the offending token — the
// stage list it could have been is part of the diagnostic.
//
// DHF-REQ: keel/requirement-136 (keel/ac-541, keel/ac-543)
func runGateStage(ctx context.Context, logger *slog.Logger, runLog runLogLocator, dir, name string) error {
	if runLogLogger, ok := runLog.(*logging.Logger); ok {
		runLogLogger.Section("gate " + name)
	} else {
		logger.Info("gate "+name, "banner", "section", "name", "gate "+name)
	}
	// CR-74: the expected-red debt is printed on every gate run, never silent —
	// a run of one stage is still a gate run.
	if err := logExpectedRed(logger, dir); err != nil {
		return gateOperationalError("expected-red", "", 0, err)
	}
	cfg, err := loadKeelDevConfig(dir)
	if err != nil {
		return gateOperationalError("config", "", 0, err)
	}
	selected, ok := selectGateStage(ciStepsWithConfig(ctx, logger, dir, cfg), name)
	if !ok {
		return cli.NewUsageError("unknown gate stage %q\nstages: %s", name, strings.Join(gateStageNames(), ", "))
	}
	if err := runGateSteps(ctx, logger, runLog, dir, []step{selected}); err != nil {
		return err
	}
	logger.Info("gate stage green", "gate", name)
	return nil
}

// classifyGateArgvError names the offending token when a caller asks for a gate
// stage that does not exist. Shared dispatch reports a namespace miss as a bare
// usage line; keel/user_need-7 asks a refusal to say what it refused.
//
// DHF-REQ: keel/requirement-136
func classifyGateArgvError(words []string, err error) error {
	if err == nil || len(words) < 2 {
		return err
	}
	var usage cli.UsageError
	if !errors.As(err, &usage) {
		return err
	}
	stages := gateStageNames()
	for _, name := range stages {
		if words[1] == name {
			return err
		}
	}
	return cli.NewUsageError("unknown gate stage %q\nusage: keel-dev gate <stage>\nstages: %s", words[1], strings.Join(stages, ", "))
}

func gateOperationalError(stepName, logFile string, startLine int, err error) error {
	return &logging.OperationalError{
		Op:        "keel-dev ci",
		Message:   fmt.Sprintf("ci gate %q failed", stepName),
		Err:       err,
		Task:      "ci:" + stepName,
		LogFile:   logFile,
		StartLine: startLine,
		ExitCode:  gateExitCode(err),
		Hint:      gateFailureHint(logFile, startLine),
	}
}

func gateExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func gateFailureHint(logFile string, startLine int) string {
	if logFile == "" || startLine <= 0 {
		return "rerun keel-dev ci with file logging enabled and inspect the failing gate records"
	}
	return fmt.Sprintf("open %s at line %d", logFile, startLine)
}

// runStep executes one gate step. Subprocess steps go through keel/exec; child
// stdout is mirrored verbatim to the terminal (keel/exec still records it via
// keel/log at debug), except where the step inspects stdout itself.
//
// DHF-REQ: keel/requirement-11
func runStep(ctx context.Context, logger *slog.Logger, dir string, s step) error {
	started := time.Now()

	if s.fn != nil {
		if err := s.fn(ctx, logger, dir); err != nil {
			return err
		}
		logger.Debug("step complete", "step", s.name, "elapsed_ms", time.Since(started).Milliseconds())
		return nil
	}

	// Resolve and verify the pinned external tool before shelling out to it: the
	// gate runs the binary its own branch pins, and a missing, un-installable, or
	// drifted gate tool fails loud, never a silent skip (keel/ac-42, keel/ac-465).
	program := s.program
	if s.tool != "" {
		resolver := s.resolver
		if resolver == nil {
			cfg, err := loadKeelDevConfig(dir)
			if err != nil {
				return err
			}
			resolver = newToolResolver(cfg.toolPins())
		}
		resolved, err := resolver.resolve(ctx, logger, s.tool)
		if err != nil {
			return err
		}
		program = resolved
	}

	req := procexec.Request{
		Program:        program,
		Args:           s.args,
		Dir:            dir,
		Logger:         logger,
		MaxOutputBytes: s.maxOutputBytes,
	}
	if s.quietStderr {
		req.Logger = quietStderrLogger{Logger: logger, step: s.name}
	}
	// Child output travels through keel/log, never as a raw terminal stream
	// (keel/ac-35, keel/issue-2): line-wise records for live progress, except
	// where the step inspects stdout itself.
	var capture *strings.Builder
	var lines *lineLogWriter
	if s.stdoutFails != nil {
		capture = &strings.Builder{}
		req.Stdout = capture
	} else {
		lines = newLineLogWriter(logger, s.name, "stdout")
		req.Stdout = lines
	}

	proc, err := procexec.ProcessStart(ctx, req)
	if err != nil {
		return err
	}
	res, waitErr := proc.Wait()
	if lines != nil {
		lines.Flush()
	}

	// Advisory steps surface their output (above) but never fail the gate: a
	// non-zero exit or spawn error is logged and swallowed (keel/ac-41).
	if s.advisory {
		if waitErr != nil {
			logger.Warn("advisory step error (ignored)", "step", s.name, "error", waitErr.Error())
		} else if res.ExitCode != 0 {
			logger.Warn("advisory step reported findings (non-blocking)", "step", s.name, "exit_code", res.ExitCode)
		}
		logger.Debug("step complete", "step", s.name, "elapsed_ms", time.Since(started).Milliseconds())
		return nil
	}

	if waitErr != nil {
		return withRemedy(fmt.Errorf("%s %s: %w", s.program, strings.Join(s.args, " "), waitErr), s.remedy)
	}
	if res.ExitCode != 0 {
		return withRemedy(fmt.Errorf("%s %s exited %d", s.program, strings.Join(s.args, " "), res.ExitCode), s.remedy)
	}
	if s.stdoutFails != nil {
		if msg := s.stdoutFails(capture.String()); msg != "" {
			return withRemedy(fmt.Errorf("%s", msg), s.remedy)
		}
	}
	logger.Debug("step complete", "step", s.name, "elapsed_ms", time.Since(started).Milliseconds())
	return nil
}

type quietStderrLogger struct {
	*slog.Logger
	step string
}

// DHF-REQ: keel/requirement-17, keel/requirement-24, keel/requirement-25
func (l quietStderrLogger) Error(msg string, args ...any) {
	fields := stderrProcessOutputFields(args)
	if fields.step == "" {
		fields.step = l.step
	}
	if fields.processOutput && fields.stderr && isKnownBenignStderr(fields.step, fields.data) {
		l.Debug(msg, args...)
		return
	}
	l.Logger.Error(msg, args...)
}

type processOutputFields struct {
	processOutput bool
	stderr        bool
	step          string
	data          string
}

func stderrProcessOutputFields(args []any) processOutputFields {
	var fields processOutputFields
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		switch key {
		case "event_type":
			fields.processOutput = args[i+1] == "process_output"
		case "stream":
			fields.stderr = args[i+1] == "stderr"
		case "step":
			fields.step, _ = args[i+1].(string)
		case "data":
			fields.data, _ = args[i+1].(string)
		}
	}
	return fields
}

// isKnownBenignStderr is deliberately caller-level and narrow: keel/exec keeps
// stderr at Error, while keel-dev can reinterpret tool progress it understands.
func isKnownBenignStderr(step, line string) bool {
	line = strings.TrimSpace(stripANSI(line))
	switch step {
	case "cspell":
		return strings.HasPrefix(line, "CSpell: Files checked:") &&
			strings.Contains(line, "Issues found: 0 in 0 files.")
	case "gitleaks":
		return strings.HasPrefix(line, "INF ") ||
			strings.Contains(line, " INF ")
	case "govulncheck":
		return strings.HasPrefix(line, "Scanning ") ||
			strings.HasPrefix(line, "Fetching ") ||
			strings.HasPrefix(line, "No vulnerabilities found")
	default:
		return false
	}
}

func stripANSI(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] != 0x1b || i+1 >= len(line) || line[i+1] != '[' {
			b.WriteByte(line[i])
			continue
		}
		i += 2
		for i < len(line) {
			c := line[i]
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				break
			}
			i++
		}
	}
	return b.String()
}

// runLogCoreDependencyQuarantine proves that consumers building only keel/log
// or keel/exec do not reach optional exporter dependencies such as the
// OpenTelemetry SDK, or the keel-dev YAML parser.
//
// DHF-REQ: keel/requirement-22, keel/requirement-118 (keel/ac-452)
func runLogCoreDependencyQuarantine(ctx context.Context, logger *slog.Logger, dir string) error {
	for _, pkg := range []string{"./log", "./exec"} {
		if err := runCoreDependencyQuarantine(ctx, logger, dir, pkg); err != nil {
			return err
		}
	}
	return nil
}

func runCoreDependencyQuarantine(ctx context.Context, logger *slog.Logger, dir, pkg string) error {
	if _, err := os.Stat(filepath.Join(dir, strings.TrimPrefix(pkg, "./"))); os.IsNotExist(err) {
		return nil
	}
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: "go",
		Args:    []string{"list", "-deps", pkg},
		Dir:     dir,
		Logger:  logger,
	})
	if err != nil {
		return err
	}
	res, waitErr := proc.Wait()
	if waitErr != nil {
		return waitErr
	}
	for _, dep := range strings.Split(res.Stdout, "\n") {
		dep = strings.TrimSpace(dep)
		if dep == "" || dep == modulePath || strings.HasPrefix(dep, modulePath+"/") {
			continue
		}
		if strings.Contains(dep, "yaml") {
			return fmt.Errorf("keel-dev: core dependency quarantine failed: %s depends on YAML package %q", pkg, dep)
		}
		first, _, _ := strings.Cut(dep, "/")
		if strings.Contains(first, ".") {
			return fmt.Errorf("keel-dev: core dependency quarantine failed: %s depends on external package %q", pkg, dep)
		}
	}
	return nil
}

// DHF-REQ: keel/requirement-120 (keel/ac-460, keel/ac-461)
func runModuleHygiene(ctx context.Context, logger *slog.Logger, dir string) error {
	originals, err := readManifestFiles(dir)
	if err != nil {
		return err
	}
	scratchParent, err := os.MkdirTemp("", "keel-dev-module-hygiene-*")
	if err != nil {
		return fmt.Errorf("keel-dev: create module-hygiene scratch dir: %w", err)
	}
	defer os.RemoveAll(scratchParent)

	scratch := filepath.Join(scratchParent, "repo")
	if err := copyModuleHygieneTree(dir, scratch); err != nil {
		return err
	}
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: "go",
		Args:    []string{"mod", "tidy"},
		Dir:     scratch,
		Logger:  logger,
	})
	if err != nil {
		return fmt.Errorf("keel-dev: module-hygiene run go mod tidy: %w", err)
	}
	res, waitErr := proc.Wait()
	if waitErr != nil {
		return fmt.Errorf("keel-dev: module-hygiene go mod tidy: %w", waitErr)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("keel-dev: module-hygiene go mod tidy exited %d", res.ExitCode)
	}

	scratchManifests, err := readManifestFiles(scratch)
	if err != nil {
		return err
	}
	var offenders []string
	for _, name := range []string{"go.mod", "go.sum"} {
		if !manifestFileEqual(originals[name], scratchManifests[name]) {
			offenders = append(offenders, name)
		}
	}
	if len(offenders) > 0 {
		return fmt.Errorf("keel-dev: module-hygiene failed: go mod tidy would change %s", strings.Join(offenders, ", "))
	}
	return nil
}

// DHF-REQ: keel/requirement-8 (keel/ac-464)
func runModuleZipCheck(ctx context.Context, logger *slog.Logger, dir string) error {
	files, err := listTrackedModuleZipFiles(ctx, logger, dir)
	if err != nil {
		return err
	}
	checked, err := zip.CheckFiles(files)
	if err == nil {
		return nil
	}
	if len(checked.Invalid) == 0 {
		return fmt.Errorf("keel-dev: module-zip failed: %w", err)
	}
	var offenders []string
	for _, invalid := range checked.Invalid {
		offenders = append(offenders, invalid.Error())
	}
	return fmt.Errorf("keel-dev: module-zip failed: tracked tree cannot be packaged as a Go module zip:\n%s", strings.Join(offenders, "\n"))
}

func listTrackedModuleZipFiles(ctx context.Context, logger *slog.Logger, dir string) ([]zip.File, error) {
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: "git",
		Args:    []string{"ls-files", "-z"},
		Dir:     dir,
		Logger:  logger,
	})
	if err != nil {
		return nil, fmt.Errorf("keel-dev: module-zip list tracked files: %w", err)
	}
	res, waitErr := proc.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("keel-dev: module-zip list tracked files: %w", waitErr)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("keel-dev: module-zip list tracked files: git ls-files exited %d", res.ExitCode)
	}
	var files []zip.File
	for _, rel := range strings.Split(res.Stdout, "\x00") {
		if rel == "" {
			continue
		}
		files = append(files, trackedZipFile{root: dir, rel: filepath.ToSlash(rel)})
	}
	return files, nil
}

type trackedZipFile struct {
	root string
	rel  string
}

func (f trackedZipFile) Path() string {
	return f.rel
}

func (f trackedZipFile) Lstat() (os.FileInfo, error) {
	return os.Lstat(filepath.Join(f.root, filepath.FromSlash(f.rel)))
}

func (f trackedZipFile) Open() (io.ReadCloser, error) {
	return os.Open(filepath.Join(f.root, filepath.FromSlash(f.rel)))
}

type manifestFile struct {
	data   []byte
	exists bool
}

func readManifestFiles(dir string) (map[string]manifestFile, error) {
	files := make(map[string]manifestFile, 2)
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && name == "go.sum" {
				files[name] = manifestFile{}
				continue
			}
			return nil, fmt.Errorf("keel-dev: module-hygiene read %s: %w", name, err)
		}
		files[name] = manifestFile{data: data, exists: true}
	}
	return files, nil
}

func manifestFileEqual(a, b manifestFile) bool {
	return a.exists == b.exists && string(a.data) == string(b.data)
}

func copyModuleHygieneTree(src, dst string) error {
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if d.IsDir() && moduleHygieneSkipDir(filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyModuleHygieneFile(path, target, info.Mode().Perm())
	})
}

func moduleHygieneSkipDir(rel string) bool {
	switch rel {
	case ".git", ".logs", ".devtools", "bin", "worktrees", "node_modules",
		"out", "dist", ".vscode-test", "vsix/node_modules", "vsix/out",
		"vsix/dist", "vsix/.vscode-test":
		return true
	default:
		return false
	}
}

func copyModuleHygieneFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
