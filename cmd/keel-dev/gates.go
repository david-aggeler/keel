package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
}

// ciSteps is the canonical gate definition: gofmt, build, vet, lint, test.
// CI (.github/workflows) and the release preflight both run this exact sequence
// via `keel-dev ci`, so the gate lives in one place and never drifts between
// local, CI, and release paths.
//
// DHF-REQ: keel/requirement-10, keel/requirement-11
func ciSteps() []step {
	return []step{
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
		{name: "test", program: "go", args: []string{"test", "./..."}},
	}
}

// runCI runs the verification gate in dir, fail-fast: the first failing step
// aborts and its error is returned. Every subprocess step is launched through
// keel/exec (START/END lifecycle logging) and every line of output flows
// through keel/log.
//
// DHF-REQ: keel/requirement-11
func runCI(ctx context.Context, logger *slog.Logger, dir string) error {
	logging.Section(logger, "keel-dev ci")
	for _, s := range ciSteps() {
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

	req := procexec.Request{
		Program: s.program,
		Args:    s.args,
		Dir:     dir,
		Logger:  logger,
	}
	var capture *strings.Builder
	if s.stdoutFails != nil {
		capture = &strings.Builder{}
		req.Stdout = capture
	} else {
		req.Stdout = os.Stdout
	}

	proc, err := procexec.ProcessStart(ctx, req)
	if err != nil {
		return err
	}
	res, waitErr := proc.Wait()

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
