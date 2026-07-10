package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	logging "github.com/david-aggeler/keel/log"
)

type processLogger interface {
	Debug(msg string, args ...any)
	Error(msg string, args ...any)
	Info(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
}

// Request describes a plain external command launch. Only Program is required;
// the zero value of every other field is a usable default.
type Request struct {
	// Program is the binary to run, resolved via PATH when not an absolute path.
	Program string
	// Args are the command arguments (argv without the program name).
	Args []string
	// Dir is the child's working directory. Empty means the current process
	// working directory.
	Dir string
	// Env is the child's full environment as "KEY=VALUE" entries. Nil inherits
	// the parent process environment unchanged.
	Env []string
	// Stdin, when non-nil, is connected to the child's standard input.
	Stdin io.Reader
	// Stdout, when non-nil, receives a verbatim copy of the child's stdout in
	// addition to the captured [Result.Stdout] and the line-wise debug log.
	Stdout io.Writer
	// Stderr, when non-nil, receives a verbatim copy of the child's stderr in
	// addition to the captured [Result.Stderr] and the line-wise error log.
	Stderr io.Writer
	// Logger receives the START/END lifecycle and per-line output records. Nil
	// uses slog.Default.
	Logger processLogger
	// SensitiveArgs marks argv indices whose values must be masked as [REDACTED]
	// in the logged command line (e.g. a token passed positionally).
	SensitiveArgs map[int]bool
	// Configure, when non-nil, is called with the prepared [os/exec.Cmd] just
	// before it starts — the escape hatch for setting a process group, custom
	// Cancel, WaitDelay, or other fields keel/exec does not model directly.
	Configure func(*exec.Cmd)
}

// Result reports the observed outcome of a launched process, filled in by
// [Process.Wait] once the child has exited.
type Result struct {
	// ExitCode is the child's exit status (-1 if it never produced one).
	ExitCode int
	// Duration is the wall-clock time from start to reap.
	Duration time.Duration
	// Stdout is the full captured standard output.
	Stdout string
	// Stderr is the full captured standard error.
	Stderr string
}

// Process is a started subprocess supervised by ProcessStart.
type Process struct {
	cmd     *exec.Cmd
	started time.Time
	stdout  *captureWriter
	stderr  *captureWriter
	logger  processLogger
	waitErr error
	waitCh  chan error
	once    sync.Once
}

// ProcessStart launches the command described by req and returns a running
// [Process] without waiting for it to finish. It emits the "process start"
// lifecycle record, wires stdout/stderr through the capturing, mirroring,
// redacting writers, and starts the child; cancelling ctx kills the process.
// Call [Process.Wait] to block for completion and obtain the [Result].
//
// It returns an error ("keel/exec: …") when Program is empty or the child fails
// to start; a non-zero exit is not an error here — it is reported by Wait.
//
// DHF-REQ: openbrain/requirement-565, keel/requirement-1
func ProcessStart(ctx context.Context, req Request) (*Process, error) {
	if req.Program == "" {
		return nil, errors.New("keel/exec: program is required")
	}

	cmd := exec.CommandContext(ctx, req.Program, req.Args...)
	cmd.Dir = req.Dir
	if req.Env != nil {
		cmd.Env = req.Env
	}
	if req.Stdin != nil {
		cmd.Stdin = req.Stdin
	}
	if req.Configure != nil {
		req.Configure(cmd)
	}

	logger := req.Logger
	if logger == nil {
		logger = slog.Default()
	}
	workingDir := req.Dir
	if workingDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workingDir = cwd
		}
	}
	logger.InfoContext(ctx, "process start",
		"event_type", "process_start",
		"program", req.Program,
		"command_line", renderCommandLine(req.Program, req.Args, req.SensitiveArgs),
		"working_dir", workingDir,
	)

	stdout := &captureWriter{stream: req.Stdout, logger: logger, streamName: "stdout"}
	stderr := &captureWriter{stream: req.Stderr, logger: logger, streamName: "stderr"}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("keel/exec: start %s: %w", req.Program, err)
	}

	p := &Process{
		cmd:     cmd,
		started: started,
		stdout:  stdout,
		stderr:  stderr,
		logger:  logger,
		waitCh:  make(chan error, 1),
	}
	go func() {
		p.waitCh <- cmd.Wait()
		close(p.waitCh)
	}()
	return p, nil
}

// Wait blocks until the process exits and returns its captured result.
func (p *Process) Wait() (Result, error) {
	if p == nil {
		return Result{ExitCode: -1}, errors.New("keel/exec: nil process")
	}

	p.once.Do(func() {
		p.waitErr = <-p.waitCh
		// cmd.Wait has returned, so all output has been copied to the capture
		// writers; emit any trailing unterminated line before building Result.
		p.stdout.flush()
		p.stderr.flush()
	})

	result := Result{
		ExitCode: p.cmd.ProcessState.ExitCode(),
		Duration: time.Since(p.started),
		Stdout:   p.stdout.String(),
		Stderr:   p.stderr.String(),
	}
	p.logger.Info("process end",
		"event_type", "process_end",
		"exit_code", result.ExitCode,
		"elapsed_ms", result.Duration.Milliseconds(),
	)
	return result, p.waitErr
}

type captureWriter struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	pending    bytes.Buffer
	stream     io.Writer
	logger     processLogger
	streamName string
}

// DHF-REQ: openbrain/requirement-602, keel/requirement-24
func (w *captureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(p)
	if w.logger != nil {
		// os/exec does not guarantee a logical child line arrives in one Write,
		// so carry any unterminated fragment across calls and only emit complete
		// newline-delimited lines; the trailing partial is flushed at completion.
		w.pending.Write(p)
		for {
			data := w.pending.Bytes()
			idx := bytes.IndexByte(data, '\n')
			if idx < 0 {
				break
			}
			line := string(data[:idx])
			w.pending.Next(idx + 1)
			w.logLine(line)
		}
	}
	if w.stream != nil {
		if _, err := w.stream.Write(p); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// flush emits any final unterminated line still buffered after the process has
// stopped writing. Callers must invoke it once the child's output is complete.
// DHF-REQ: keel/requirement-24
func (w *captureWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.logger == nil || w.pending.Len() == 0 {
		return
	}
	line := w.pending.String()
	w.pending.Reset()
	w.logLine(line)
}

// logLine records one child-output line: trailing CR trimmed, blank lines
// dropped, stdout at Debug and stderr at Error. The caller holds w.mu.
func (w *captureWriter) logLine(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	log := w.logger.Debug
	if w.streamName == "stderr" {
		log = w.logger.Error
	}
	log("process output",
		"event_type", "process_output",
		"stream", w.streamName,
		"data", logging.RedactString(line),
	)
}

func (w *captureWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func renderCommandLine(program string, args []string, sensitiveArgs map[int]bool) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(program))
	for i, arg := range args {
		if sensitiveArgs[i] {
			arg = "[REDACTED]"
		}
		parts = append(parts, shellQuote(arg))
	}
	return logging.RedactString(strings.Join(parts, " "))
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' ||
			r == '=' || r == '@' || r == '+' ||
			(r >= '0' && r <= '9') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z'))
	}) == -1 {
		return s
	}
	return strconv.Quote(s)
}
