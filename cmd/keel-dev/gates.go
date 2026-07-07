package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
)

// step is one gate in the ci pipeline: a labelled subprocess plus, optionally,
// a treatment of its stdout (gofmt reports unformatted files on stdout while
// still exiting 0, so it needs a non-exit-code verdict).
type step struct {
	name    string
	program string
	args    []string
	// stdoutFails, when set and returning a non-empty message for the captured
	// stdout, turns a zero-exit run into a failure carrying that message.
	stdoutFails func(stdout string) string
}

// ciSteps is the canonical gate definition. CI (.github/workflows) and the
// release preflight both run this exact sequence via `keel-dev ci`, so the gate
// lives in one place and never drifts between local, CI, and release paths.
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
		{name: "test", program: "go", args: []string{"test", "./..."}},
	}
}

// runCI runs the verification gate in dir, fail-fast: the first failing step
// aborts and its error is returned. Every step is launched through keel/exec
// (START/END lifecycle logging) and every line of output flows through keel/log.
//
// DHF-REQ: keel/requirement-11
func runCI(ctx context.Context, logger *slog.Logger, dir string) error {
	logSection(logger, "keel-dev ci")
	for _, s := range ciSteps() {
		if err := runStep(ctx, logger, dir, s); err != nil {
			return fmt.Errorf("ci gate %q failed: %w", s.name, err)
		}
		logger.Info("gate passed", "gate", s.name)
	}
	logger.Info("ci gate green")
	return nil
}

// runStep launches one gate subprocess via keel/exec. Child stdout is mirrored
// verbatim to the terminal; child stderr is surfaced through keel/log (keel/exec
// logs it as a process_output record). gofmt's stdout is additionally captured
// so its zero-exit "unformatted files" list can fail the gate.
//
// DHF-REQ: keel/requirement-11
func runStep(ctx context.Context, logger *slog.Logger, dir string, s step) error {
	started := time.Now()

	req := procexec.Request{
		Program: s.program,
		Args:    s.args,
		Dir:     dir,
		Logger:  logger,
	}
	// Verbatim stdout to the terminal for a normal `go test` experience; keel/exec
	// still records it through keel/log at debug. gofmt needs its stdout inspected,
	// so capture it instead of streaming.
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
	elapsed := time.Since(started)

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
	logger.Debug("step complete", "step", s.name, "elapsed_ms", elapsed.Milliseconds())
	return nil
}

// logSection renders a ruled human-mode section header through keel/log.
func logSection(logger *slog.Logger, name string) {
	logger.Info(strings.Repeat("-", 60) + " " + name)
}
