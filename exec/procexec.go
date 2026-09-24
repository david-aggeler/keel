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
	"unicode/utf8"

	logging "github.com/david-aggeler/keel/log"
)

type processLogger interface {
	Debug(msg string, args ...any)
	Error(msg string, args ...any)
	Info(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
}

// DefaultMaxOutputBytes is the fail-closed child-output capture ceiling used
// when a request supplies no positive MaxOutputBytes value.
//
// DHF-REQ: keel/requirement-81
const DefaultMaxOutputBytes = 64 * 1024 * 1024

// ErrOutputLimitExceeded identifies runs killed because their combined captured
// stdout and stderr reached the configured output ceiling.
//
// DHF-REQ: keel/requirement-81
var ErrOutputLimitExceeded = errors.New("keel/exec: output limit exceeded")

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
	// Stdout, when non-nil, is the tee that receives a verbatim copy of the
	// child's stdout, and it then becomes the caller's only copy: [Result.Stdout]
	// is left empty so the same bytes cannot be read twice. Set CaptureWithTee to
	// have both carry the stream. Nil selects the capture instead, and the
	// line-wise child-output log is emitted either way.
	Stdout io.Writer
	// Stderr, when non-nil, is the tee that receives a verbatim copy of the
	// child's stderr, and it then becomes the caller's only copy: [Result.Stderr]
	// is left empty so the same bytes cannot be read twice. Set CaptureWithTee to
	// have both carry the stream. Nil selects the capture instead, and the
	// line-wise child-output log is emitted either way.
	Stderr io.Writer
	// CaptureWithTee delivers a stream through both paths at once — the tee
	// writer and the Result capture — for every stream whose tee writer is
	// non-nil. It is the only way to get both, and it exists for the caller who
	// wants one path live and the other quotable (streaming progress plus a tail
	// in an error message). The zero value keeps delivery to exactly one path.
	//
	// DHF-REQ: keel/requirement-150
	CaptureWithTee bool
	// MaxOutputBytes is the hard ceiling on combined captured stdout and
	// stderr bytes for this run. A non-positive value uses
	// [DefaultMaxOutputBytes]; no value disables the ceiling.
	MaxOutputBytes int
	// Logger receives the START/END lifecycle and per-line output records. Nil
	// produces no output at all — keel/exec never reaches for a sink the caller
	// did not supply. Inject a logger to get records, or [keel/log.Discard] to
	// state the silence deliberately.
	Logger processLogger
	// SensitiveArgs marks argv indices whose values must be masked as [REDACTED]
	// in the logged command line (e.g. a token passed positionally).
	SensitiveArgs map[int]bool
	// Configure, when non-nil, is called with the prepared [os/exec.Cmd] just
	// before it starts — the escape hatch for setting a process group, custom
	// Cancel, WaitDelay, or other fields keel/exec does not model directly.
	Configure func(*exec.Cmd)
	// Classify, when non-nil, is the per-process-type adapter (keel/requirement-2)
	// for a child whose output format the caller knows. keel/exec calls it once
	// per clean output line with the stream name ("stdout" or "stderr") and the
	// line, and applies the returned [LineClass]. Core keel/exec never reads a
	// level out of the text itself; nil leaves every line unclassified.
	Classify func(stream, line string) LineClass
	// FailureLevel is the severity of the END record and the replayed output
	// tail when the child exits non-zero. Nil means Error. Set it for a child
	// whose non-zero exit is an answer rather than a failure — a probe such as
	// "git rev-parse --verify" — or whose findings are advisory, so the exit
	// status still decides and no false Error is recorded.
	//
	// DHF-REQ: keel/requirement-24
	FailureLevel slog.Leveler
}

// LineClass is what a [Request.Classify] adapter reports about one child
// output line. The zero value says nothing and changes nothing.
//
// DHF-REQ: keel/requirement-24
type LineClass struct {
	// Level overrides the child-output level for this line's record. Nil keeps
	// the level configured on the logger (Config.ChildOutputLevel), else Debug.
	Level slog.Leveler
	// Declared is the level the child itself declared on this line, carried in
	// the record's "declared_level" field. It never changes the record's own
	// severity. Nil means the child declared none, or the adapter could not
	// read one, and the field is then absent: an absent level is never
	// reported as an inferred one.
	Declared slog.Leveler
}

// FailureTailLines is the number of most recent child output lines, across
// both streams, that [Process.Wait] replays when the child exits non-zero, at
// Error unless [Request.FailureLevel] says otherwise.
//
// DHF-REQ: keel/requirement-24
const FailureTailLines = 20

// failureTailLineBytes caps one replayed tail line; the full line stays in the
// per-line record.
const failureTailLineBytes = 4096

// defaultChildOutputLevel is the level of a child output line when neither the
// adapter nor the logger configures one. It is the same on both streams: the
// stream a child writes to is its transport choice, not a severity.
const defaultChildOutputLevel = slog.LevelDebug

// Result reports the observed outcome of a launched process, filled in by
// [Process.Wait] once the child has exited.
type Result struct {
	// ExitCode is the child's exit status (-1 if it never produced one).
	ExitCode int
	// Duration is the wall-clock time from start to reap.
	Duration time.Duration
	// Stdout is the full captured standard output, and it is empty when the run's
	// [Request.Stdout] tee writer was non-nil — that tee then holds the bytes
	// instead. Set [Request.CaptureWithTee] to have both carry the stream.
	Stdout string
	// Stderr is the full captured standard error, and it is empty when the run's
	// [Request.Stderr] tee writer was non-nil — that tee then holds the bytes
	// instead. Set [Request.CaptureWithTee] to have both carry the stream.
	Stderr string
}

// Process is a started subprocess supervised by ProcessStart.
type Process struct {
	cmd     *exec.Cmd
	program string
	started time.Time
	stdout  *captureWriter
	stderr  *captureWriter
	logger  processLogger
	tail    *outputTail
	failure slog.Level
	waitErr error
	result  Result
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
// DHF-REQ: openbrain/requirement-565, keel/requirement-1, keel/requirement-81
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
		// A library handed no sink stays silent: reaching for slog.Default()
		// would emit outside the caller's formatter, file sinks, and redaction.
		// DHF-REQ: keel/requirement-122
		logger = logging.Discard()
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

	outputLimit := newOutputLimit(req.MaxOutputBytes, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	// A stream with a tee writer delivers through that tee alone unless the caller
	// asked for both paths, so the capture cannot silently hand back a second copy
	// of the same bytes. The buffer still fills either way: MaxOutputBytes
	// accounting and the line-wise logging read it and must stay unaffected.
	// DHF-REQ: keel/requirement-150
	tail := &outputTail{}
	stdout := &captureWriter{stream: req.Stdout, logger: logger, program: req.Program, streamName: "stdout", limit: outputLimit,
		suppressCapture: req.Stdout != nil && !req.CaptureWithTee, classify: req.Classify, tail: tail}
	stderr := &captureWriter{stream: req.Stderr, logger: logger, program: req.Program, streamName: "stderr", limit: outputLimit,
		suppressCapture: req.Stderr != nil && !req.CaptureWithTee, classify: req.Classify, tail: tail}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("keel/exec: start %s: %w", req.Program, err)
	}

	p := &Process{
		cmd:     cmd,
		program: req.Program,
		started: started,
		stdout:  stdout,
		stderr:  stderr,
		logger:  logger,
		tail:    tail,
		failure: slog.LevelError,
		waitCh:  make(chan error, 1),
	}
	if req.FailureLevel != nil {
		p.failure = req.FailureLevel.Level()
	}
	go func() {
		p.waitCh <- cmd.Wait()
		close(p.waitCh)
	}()
	return p, nil
}

// Wait blocks until the process exits and returns its captured result. It may
// be called more than once; the process is reaped, captured result assembled,
// and "process end" lifecycle record emitted only on the first call.
//
// The exit status is the only source of a child's severity. A zero exit ends
// at Info. Any other status, including -1 for a child that never produced one,
// first replays the last [FailureTailLines] output lines at Error and then
// ends at Error, so the reason for a failure is visible at the default console
// floor without re-running at Debug. [Request.FailureLevel] replaces Error for
// a child whose non-zero exit is not a failure. A tail record at Warn or higher
// names the child in its program attribute, because under --quiet the Info
// "process start" record that names it is hidden.
//
// DHF-REQ: keel/requirement-1, keel/requirement-24, keel/requirement-171
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

		exitCode := -1
		if p.cmd.ProcessState != nil {
			exitCode = p.cmd.ProcessState.ExitCode()
		}
		p.result = Result{
			ExitCode: exitCode,
			Duration: time.Since(p.started),
			Stdout:   p.stdout.capture(),
			Stderr:   p.stderr.capture(),
		}
		if limitErr := p.stdout.limit.Err(); limitErr != nil {
			p.waitErr = limitErr
		}
		if p.result.ExitCode == 0 {
			p.logger.Info("process end",
				"event_type", "process_end",
				"exit_code", p.result.ExitCode,
				"elapsed_ms", p.result.Duration.Milliseconds(),
			)
			return
		}
		tail := p.tail.lines()
		for _, line := range tail {
			args := []any{
				"event_type", "process_output_tail",
				"stream", line.stream,
				"data", line.data,
			}
			if line.declared != nil {
				args = append(args, "declared_level", line.declared.Level().String())
			}
			if line.truncated > 0 {
				args = append(args, "truncated_bytes", line.truncated)
			}
			args = appendProgramAtWarn(args, p.failure, p.program)
			logAt(p.logger, p.failure, "process output tail", args...)
		}
		logAt(p.logger, p.failure, "process end",
			"event_type", "process_end",
			"exit_code", p.result.ExitCode,
			"elapsed_ms", p.result.Duration.Milliseconds(),
			"output_tail_lines", len(tail),
		)
	})

	return p.result, p.waitErr
}

type captureWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	pending bytes.Buffer
	stream  io.Writer
	logger  processLogger
	// program is the Request's Program; it tags output records at Warn or
	// higher so a line that reaches the default console names its command.
	program    string
	streamName string
	limit      *outputLimit
	// suppressCapture withholds the buffered bytes from [Result], leaving the
	// caller's tee writer as the single delivery path for this stream.
	suppressCapture bool
	// classify is the caller's per-process-type adapter; nil classifies nothing.
	classify func(stream, line string) LineClass
	// tail is shared by both streams of one process and keeps the most recent
	// lines for the failure replay. Nil keeps none.
	tail *outputTail
}

// DHF-REQ: openbrain/requirement-602, keel/requirement-24, keel/requirement-81, keel/requirement-151
func (w *captureWriter) Write(p []byte) (int, error) {
	allowed, limitErr := w.limit.Reserve(len(p))
	if allowed == 0 {
		return 0, limitErr
	}
	p = p[:allowed]

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
			// The capture buffer and the output-limit budget have both already
			// committed this chunk, and neither is reversible, so the honest
			// count is what was kept — not what the tee accepted. Reporting
			// zero here told os/exec's copy loop the bytes never landed while
			// Result and the remaining ceiling said they had.
			// DHF-REQ: keel/requirement-151
			return len(p), err
		}
	}
	return len(p), limitErr
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
// dropped. Both streams share one level — the adapter's override, else the
// logger's configured child-output level, else Debug — and the stream travels
// as an attribute. A level the adapter read from the child travels in its own
// declared_level field. A record whose resolved level is Warn or higher also
// carries the program attribute; lower records stay untagged. The caller holds
// w.mu.
//
// DHF-REQ: keel/requirement-24, keel/requirement-167, keel/requirement-171
func (w *captureWriter) logLine(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	var class LineClass
	if w.classify != nil {
		class = w.classify(w.streamName, line)
	}
	data := redactedString(line)
	w.tail.add(w.streamName, data, class.Declared)

	args := []any{
		"event_type", "process_output",
		"stream", w.streamName,
		"data", data,
	}
	if class.Declared != nil {
		args = append(args, "declared_level", class.Declared.Level().String())
	}
	// Resolve the level once: the tag decision and the emitted record must
	// agree even when the logger carries a configured child-output level.
	level := childOutputLevel(w.logger, class)
	args = appendProgramAtWarn(args, level, w.program)
	logAt(w.logger, level, "process output", args...)
}

// appendProgramAtWarn appends the program attribute to a child output record
// logged at level when that level is Warn or higher, and returns args unchanged
// below Warn.
//
// DHF-REQ: keel/requirement-171
func appendProgramAtWarn(args []any, level slog.Level, program string) []any {
	if level < slog.LevelWarn {
		return args
	}
	return append(args, "program", program)
}

// childOutputLevel resolves the level of one child output line. Nothing about
// the stream enters the decision.
func childOutputLevel(logger processLogger, class LineClass) slog.Level {
	if class.Level != nil {
		return class.Level.Level()
	}
	if cl, ok := logger.(childLevelLogger); ok {
		if level := cl.ChildOutputLevel(); level != nil {
			return level.Level()
		}
	}
	return defaultChildOutputLevel
}

// childLevelLogger is the optional logger capability that carries
// keel/log's Config.ChildOutputLevel.
type childLevelLogger interface {
	ChildOutputLevel() slog.Leveler
}

// levelLogger is the optional logger capability to emit at an arbitrary
// level. keel/log's Logger and *slog.Logger both provide it.
type levelLogger interface {
	Log(ctx context.Context, level slog.Level, msg string, args ...any)
}

// logAt emits at level. A logger without Log gets the nearest of its three
// methods at or below level.
func logAt(logger processLogger, level slog.Level, msg string, args ...any) {
	if ll, ok := logger.(levelLogger); ok {
		ll.Log(context.Background(), level, msg, args...)
		return
	}
	switch {
	case level >= slog.LevelError:
		logger.Error(msg, args...)
	case level >= slog.LevelInfo:
		logger.Info(msg, args...)
	default:
		logger.Debug(msg, args...)
	}
}

// outputTail is a bounded ring of the most recent child output lines across
// both streams, in arrival order.
type outputTail struct {
	mu   sync.Mutex
	buf  []tailLine
	next int
	full bool
}

type tailLine struct {
	stream    string
	data      string
	declared  slog.Leveler
	truncated int
}

func (t *outputTail) add(stream, data string, declared slog.Leveler) {
	if t == nil {
		return
	}
	line := tailLine{stream: stream, data: data, declared: declared}
	if len(data) > failureTailLineBytes {
		cut := failureTailLineBytes
		for cut > 0 && !utf8.RuneStart(data[cut]) {
			cut--
		}
		line.data = data[:cut]
		line.truncated = len(data) - cut
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.buf == nil {
		t.buf = make([]tailLine, FailureTailLines)
	}
	t.buf[t.next] = line
	t.next = (t.next + 1) % FailureTailLines
	if t.next == 0 {
		t.full = true
	}
}

// lines returns the retained lines, oldest first.
func (t *outputTail) lines() []tailLine {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.full {
		return append([]tailLine(nil), t.buf[:t.next]...)
	}
	return append(append([]tailLine(nil), t.buf[t.next:]...), t.buf[:t.next]...)
}

// capture returns the bytes this stream contributes to [Result], which is
// nothing when the caller's tee writer is the selected delivery path.
//
// DHF-REQ: keel/requirement-150
func (w *captureWriter) capture() string {
	if w.suppressCapture {
		return ""
	}
	return w.String()
}

func (w *captureWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

type outputLimit struct {
	mu     sync.Mutex
	max    int
	used   int
	err    error
	kill   func()
	killed sync.Once
}

func newOutputLimit(max int, kill func()) *outputLimit {
	if max <= 0 {
		max = DefaultMaxOutputBytes
	}
	return &outputLimit{max: max, kill: kill}
}

func (l *outputLimit) Reserve(n int) (int, error) {
	if l == nil {
		return n, nil
	}
	l.mu.Lock()
	if l.err != nil {
		l.mu.Unlock()
		return 0, l.err
	}
	remaining := l.max - l.used
	allowed := n
	reached := n >= remaining
	if reached {
		allowed = remaining
		l.err = fmt.Errorf("%w: captured child output reached %d bytes", ErrOutputLimitExceeded, l.max)
	}
	l.used += allowed
	err := l.err
	l.mu.Unlock()

	if reached {
		l.killed.Do(func() {
			if l.kill != nil {
				l.kill()
			}
		})
	}
	return allowed, err
}

func (l *outputLimit) Err() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
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
	return redactedString(strings.Join(parts, " "))
}

func redactedString(s string) string {
	return logging.RedactErr(errors.New(s)).Error()
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
