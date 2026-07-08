package main

import (
	"context"
	"fmt"
	"log/slog"
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
		// --- static-tool battery (keel/requirement-12) ---
		// DHF-REQ: keel/requirement-12 (keel/ac-38)
		{name: "golangci-lint", tool: "golangci-lint", program: "golangci-lint", args: []string{"run", "./..."}},
		// DHF-REQ: keel/requirement-12 (keel/ac-39)
		{name: "govulncheck", tool: "govulncheck", program: "govulncheck", args: []string{"./..."}},
		// DHF-REQ: keel/requirement-12 (keel/ac-40)
		{name: "cspell", tool: "cspell", program: "cspell", args: []string{"--no-progress", "**/*.md", "**/*.go"}},
		// gitleaks scans the git history + working tree for committed secrets and
		// exits non-zero on any finding (default --exit-code 1), so a leak fails
		// the gate. --no-banner keeps the log quiet; --redact prevents any matched
		// secret from being echoed through keel/log. The .gitleaks.toml at the
		// repo root (auto-loaded from the source path) supplies the ruleset +
		// keel's test-fixture allowlist. Version pin is enforced at install time
		// (presence-only here — see pinnedTools), so this only fails loud if the
		// tool is missing (keel/ac-45).
		// DHF-REQ: keel/requirement-13 (keel/ac-45), keel/requirement-8
		{name: "gitleaks", tool: "gitleaks", program: "gitleaks", args: []string{"detect", "--no-banner", "--redact"}},
	}

	if len(scripts) > 0 {
		// DHF-REQ: keel/requirement-12 (keel/ac-43)
		steps = append(steps, step{name: "shellcheck", tool: "shellcheck", program: "shellcheck", args: scripts})
		// DHF-REQ: keel/requirement-12 (keel/ac-44)
		steps = append(steps, step{
			name: "shfmt", tool: "shfmt", program: "shfmt",
			args: append([]string{"-d"}, scripts...),
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
	steps = append(steps, step{name: "deadcode", tool: "deadcode", program: "deadcode", args: []string{"-test", "./..."}, advisory: true})

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
	logging.Section(logger, "keel-dev ci")
	for _, s := range ciSteps(dir) {
		if err := runStep(ctx, logger, dir, s); err != nil {
			return fmt.Errorf("ci gate %q failed: %w", s.name, err)
		}
		logger.Info("gate passed", "gate", s.name)
	}
	logger.Info("ci gate green")
	return nil
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
