package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

// step is one gate in the ci pipeline: either a labelled subprocess or an
// in-process check (fn). Subprocess steps may additionally judge their stdout
// (gofmt reports unformatted files on stdout while still exiting 0).
type step struct {
	name    string
	program string
	args    []string
	// stdoutFails, when set and returning a non-empty message for the captured
	// stdout, turns a zero-exit run into a failure carrying that message.
	stdoutFails func(stdout string) string
	// fn, when set, runs in-process instead of spawning a subprocess (used for
	// the compiled-in lint policies; keeps CI hermetic — no external lint binary).
	fn func(ctx context.Context, logger *slog.Logger, dir string) error
	// tool, when set, names an entry in pinnedTools whose presence and exact
	// version are verified before the subprocess runs (keel/ac-42) — a missing
	// or drifted external tool fails the gate loud, never a silent skip.
	tool string
	// advisory marks a step whose output is surfaced through keel/log but whose
	// failure (non-zero exit) never fails the gate (keel/ac-41: deadcode).
	advisory bool
	// quietStderr reclassifies only known-benign child stderr progress records
	// for noisy tools whose progress stream is not itself a failure signal.
	quietStderr bool
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
// shfmt, advisory deadcode) is version-pinned via pinnedTools and runs after
// the in-process checks. Each external tool is presence/version-verified before
// it runs (keel/ac-42) so a missing or drifted tool fails loud.
//
// DHF-REQ: keel/requirement-10, keel/requirement-11, keel/requirement-12
func ciSteps(dir string) []step {
	// Shell scripts are enumerated up front so shellcheck/shfmt receive explicit
	// paths (no shell is involved to expand a glob). Sorted for stable output.
	scripts, _ := filepath.Glob(filepath.Join(dir, "scripts", "*.sh"))

	steps := []step{
		{
			name:    "gofmt",
			program: "gofmt",
			args:    []string{"-l", "."},
			stdoutFails: func(out string) string {
				files := strings.TrimSpace(out)
				if files == "" {
					return ""
				}
				return "unformatted files:\n" + files
			},
		},
		{name: "build", program: "go", args: []string{"build", "./..."}},
		{name: "vet", program: "go", args: []string{"vet", "./..."}},
		{name: "lint", fn: func(_ context.Context, _ *slog.Logger, dir string) error {
			return runLint(dir)
		}},
		// DHF-REQ: keel/requirement-22
		{name: "log-core-deps", fn: runLogCoreDependencyQuarantine},
		// --- static-tool battery (keel/requirement-12) ---
		// DHF-REQ: keel/requirement-12 (keel/ac-38)
		{name: "golangci-lint", tool: "golangci-lint", program: "golangci-lint", args: []string{"run", "./..."}, quietStderr: true},
		// DHF-REQ: keel/requirement-12 (keel/ac-39)
		{name: "govulncheck", tool: "govulncheck", program: "govulncheck", args: []string{"./..."}, quietStderr: true},
		// DHF-REQ: keel/requirement-12 (keel/ac-40)
		{name: "cspell", tool: "cspell", program: "cspell", args: []string{"--no-progress", "**/*.md", "**/*.go"}, quietStderr: true},
		// gitleaks scans the git history + working tree for committed secrets and
		// exits non-zero on any finding (default --exit-code 1), so a leak fails
		// the gate. --no-banner keeps the log quiet; --redact prevents any matched
		// secret from being echoed through keel/log. The .gitleaks.toml at the
		// repo root (auto-loaded from the source path) supplies the ruleset +
		// keel's test-fixture allowlist. Version pin is enforced at install time
		// (presence-only here — see pinnedTools), so this only fails loud if the
		// tool is missing (keel/ac-45).
		// DHF-REQ: keel/requirement-13 (keel/ac-45), keel/requirement-8
		{name: "gitleaks", tool: "gitleaks", program: "gitleaks", args: []string{"detect", "--no-banner", "--redact"}, quietStderr: true},
	}

	if len(scripts) > 0 {
		// DHF-REQ: keel/requirement-12 (keel/ac-43)
		steps = append(steps, step{name: "shellcheck", tool: "shellcheck", program: "shellcheck", args: scripts, quietStderr: true})
		// DHF-REQ: keel/requirement-12 (keel/ac-44)
		steps = append(steps, step{
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
		})
	}

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
	return steps
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
// DHF-REQ: keel/requirement-18, keel/requirement-25
func runCIWithRunLog(ctx context.Context, logger *slog.Logger, runLog runLogLocator, dir string) error {
	if runLogLogger, ok := runLog.(*logging.Logger); ok {
		runLogLogger.Section("ci")
	} else {
		logger.Info("ci", "banner", "section", "name", "ci")
	}
	for _, s := range ciSteps(dir) {
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
	logger.Info("ci gate green")
	return nil
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

	// Verify the pinned external tool before shelling out to it: a missing or
	// drifted gate tool fails loud, never a silent skip (keel/ac-42).
	if s.tool != "" {
		pin, ok := pinnedTools[s.tool]
		if !ok {
			return fmt.Errorf("keel-dev: no version pin registered for gate tool %q", s.tool)
		}
		if err := verifyToolPin(ctx, logger, pin); err != nil {
			return err
		}
	}

	req := procexec.Request{
		Program: s.program,
		Args:    s.args,
		Dir:     dir,
		Logger:  logger,
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
		return fmt.Errorf("%s %s: %w", s.program, strings.Join(s.args, " "), waitErr)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s %s exited %d", s.program, strings.Join(s.args, " "), res.ExitCode)
	}
	if s.stdoutFails != nil {
		if msg := s.stdoutFails(capture.String()); msg != "" {
			return fmt.Errorf("%s", msg)
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

// runLogCoreDependencyQuarantine proves that consumers building only keel/log do
// not reach optional exporter dependencies such as the OpenTelemetry SDK.
//
// DHF-REQ: keel/requirement-22
func runLogCoreDependencyQuarantine(ctx context.Context, logger *slog.Logger, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "log")); os.IsNotExist(err) {
		return nil
	}
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: "go",
		Args:    []string{"list", "-deps", "./log"},
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
		first, _, _ := strings.Cut(dep, "/")
		if strings.Contains(first, ".") {
			return fmt.Errorf("keel-dev: log core dependency quarantine failed: ./log depends on external package %q", dep)
		}
	}
	return nil
}
