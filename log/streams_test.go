package log_test

import (
	"os"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// stdStreams replaces os.Stdout and os.Stderr with regular temp files for the
// duration of the test and restores them on cleanup. A regular file is never a
// terminal, so the captured bytes carry no color escapes. Tests using it must
// not run in parallel: the swap is process-wide.
type stdStreams struct {
	t      *testing.T
	stdout *os.File
	stderr *os.File
}

func swapStdStreams(t *testing.T) *stdStreams {
	t.Helper()
	dir := t.TempDir()
	stdout, err := os.CreateTemp(dir, "stdout")
	if err != nil {
		t.Fatalf("create stdout capture: %v", err)
	}
	stderr, err := os.CreateTemp(dir, "stderr")
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdout, stderr
	t.Cleanup(func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		_ = stdout.Close()
		_ = stderr.Close()
	})
	return &stdStreams{t: t, stdout: stdout, stderr: stderr}
}

func (s *stdStreams) read(f *os.File) string {
	s.t.Helper()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		s.t.Fatalf("read %s: %v", f.Name(), err)
	}
	return string(b)
}

// Stdout returns every byte written to the swapped stdout so far.
func (s *stdStreams) Stdout() string { return s.read(s.stdout) }

// Stderr returns every byte written to the swapped stderr so far.
func (s *stdStreams) Stderr() string { return s.read(s.stderr) }

// DHF-TEST: keel/requirement-164
func TestZeroValueWriterSendsConsoleRecordsToStderr(t *testing.T) {
	// keel/ac-696: Writer left nil → the Info record lands on stderr and
	// stdout receives no bytes.
	for _, console := range []logging.Console{logging.ConsoleSparseAI, logging.ConsolePlain, logging.ConsoleJSON} {
		t.Run(string(console), func(t *testing.T) {
			streams := swapStdStreams(t)
			logger, err := logging.New(logging.Config{Service: "svc", Console: console})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			logger.Info("zero-value-writer-record")
			if got := streams.Stdout(); got != "" {
				t.Fatalf("stdout received %q, want no bytes", got)
			}
			if got := streams.Stderr(); !strings.Contains(got, "zero-value-writer-record") {
				t.Fatalf("stderr = %q, want the Info record", got)
			}
		})
	}
}
