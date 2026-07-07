package main

import (
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
// DHF-REQ: keel/requirement-11 (keel/ac-35)
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
	w.logger.Info(line, "stream", w.stream, "step", w.step)
}
