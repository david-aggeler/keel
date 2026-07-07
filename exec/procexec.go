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

// Request describes a plain external command launch.
type Request struct {
	Program       string
	Args          []string
	Dir           string
	Env           []string
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	Logger        *slog.Logger
	SensitiveArgs map[int]bool
	Configure     func(*exec.Cmd)
}

// Result reports the observed outcome of a launched process.
type Result struct {
	ExitCode int
	Duration time.Duration
	Stdout   string
	Stderr   string
}

// Process is a started subprocess supervised by ProcessStart.
type Process struct {
	cmd     *exec.Cmd
	started time.Time
	stdout  *captureWriter
	stderr  *captureWriter
	logger  *slog.Logger
	waitErr error
	waitCh  chan error
	once    sync.Once
}

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
	stream     io.Writer
	logger     *slog.Logger
	streamName string
}

// DHF-REQ: openbrain/requirement-602
func (w *captureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(p)
	if w.logger != nil {
		log := w.logger.Debug
		if w.streamName == "stderr" {
			log = w.logger.Info
		}
		log("process output",
			"event_type", "process_output",
			"stream", w.streamName,
			"data", logging.RedactString(string(p)),
		)
	}
	if w.stream != nil {
		if _, err := w.stream.Write(p); err != nil {
			return 0, err
		}
	}
	return len(p), nil
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
