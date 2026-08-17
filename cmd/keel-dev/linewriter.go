package main

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
)

// lineLogWriter routes child-process output through keel/log line by line:
// every complete line becomes a log record (timestamped, redacted, delivered to
// all sinks) while the child is still running. Content is verbatim — only the
// transport is the handler chain instead of a raw terminal stream.
//
// This is the only sanctioned way for keel-dev to surface child output; handing
// os.Stdout/os.Stderr to a subprocess is a lint violation (no-raw-stdout-stream).
//
// DHF-REQ: keel/requirement-11 (keel/ac-35), keel/requirement-17
type lineLogWriter struct {
	mu     sync.Mutex
	logger *slog.Logger
	stream string // "stdout" or "stderr"
	step   string
	buf    strings.Builder
}

func newLineLogWriter(logger *slog.Logger, step, stream string) *lineLogWriter {
	return &lineLogWriter{logger: logger, stream: stream, step: step}
}

// Write buffers until newline and emits one log record per complete line.
func (w *lineLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			w.emit(w.buf.String())
			w.buf.Reset()
			continue
		}
		w.buf.WriteByte(b)
	}
	return len(p), nil
}

// Flush emits any unterminated trailing line. Call once after the child exits.
func (w *lineLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() > 0 {
		w.emit(w.buf.String())
		w.buf.Reset()
	}
}

func (w *lineLogWriter) emit(line string) {
	// Trailing carriage returns from CRLF children are transport, not content.
	line = strings.TrimSuffix(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	if w.stream == "stdout" {
		w.logger.Debug(line, "stream", w.stream, "step", w.step, "event_type", "process_output")
		return
	}
	w.logger.Error(line, "stream", w.stream, "step", w.step, "event_type", "process_output")
}

// lineFuncWriter splits a child process's output stream into complete lines and
// hands each one to fn as soon as it arrives, so a consumer attached to the
// keel/exec Stdout seam observes a line at the time the producer wrote it
// rather than after the child exits. os/exec does not guarantee that a logical
// child line arrives in one Write, so an unterminated fragment is carried
// across calls; Flush emits the trailing one after the child's output is
// complete.
//
// It is the streaming counterpart of lineLogWriter: same line discipline, a
// caller-supplied sink instead of the log handler chain.
//
// DHF-REQ: keel/requirement-131
type lineFuncWriter struct {
	mu      sync.Mutex
	pending bytes.Buffer
	fn      func(line []byte)
}

func newLineFuncWriter(fn func(line []byte)) *lineFuncWriter {
	return &lineFuncWriter{fn: fn}
}

// Write buffers until newline and hands fn one complete line at a time, in the
// order the producer wrote them. The whole call is serialized on the writer's
// mutex, so fn is never entered concurrently and never out of order.
func (w *lineFuncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending.Write(p)
	for {
		data := w.pending.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := append([]byte(nil), data[:idx]...)
		w.pending.Next(idx + 1)
		w.emit(line)
	}
	return len(p), nil
}

// Flush hands fn any unterminated trailing line. Call once after the child's
// output is complete.
func (w *lineFuncWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending.Len() == 0 {
		return
	}
	line := append([]byte(nil), w.pending.Bytes()...)
	w.pending.Reset()
	w.emit(line)
}

func (w *lineFuncWriter) emit(line []byte) {
	// Trailing carriage returns from CRLF children are transport, not content.
	w.fn(bytes.TrimSuffix(line, []byte("\r")))
}
