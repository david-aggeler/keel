package log_test

import (
	"log/slog"
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

// DHF-TEST: keel/requirement-167
func TestServiceProfileSplitsSeverityAcrossStdoutAndStderr(t *testing.T) {
	// keel/ac-709: service profile, no caller handler → stdout carries exactly
	// Debug and Info, stderr carries exactly Warn and Error.
	streams := swapStdStreams(t)
	cfg := logging.ServiceProfile("svc")
	if len(cfg.Handlers) != 0 {
		t.Fatalf("service profile carries %d handlers, want none", len(cfg.Handlers))
	}
	logger, err := logging.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Debug("rec-debug")
	logger.Info("rec-info")
	logger.Warn("rec-warn")
	logger.Error("rec-error")

	assertExactly := func(stream, got string, want, forbid []string) {
		t.Helper()
		for _, w := range want {
			if strings.Count(got, w) != 1 {
				t.Fatalf("%s = %q, want exactly one %s record", stream, got, w)
			}
		}
		for _, f := range forbid {
			if strings.Contains(got, f) {
				t.Fatalf("%s = %q, must not carry %s", stream, got, f)
			}
		}
	}
	all := []string{"rec-debug", "rec-info", "rec-warn", "rec-error"}
	assertExactly("stdout", streams.Stdout(), all[:2], all[2:])
	assertExactly("stderr", streams.Stderr(), all[2:], all[:2])
}

// DHF-TEST: keel/requirement-167
func TestProfilesResolveToAmendableConfigForNew(t *testing.T) {
	// keel/ac-710: each profile is a Config the consumer reads, amends, and
	// passes to the single New constructor.
	streams := swapStdStreams(t)
	profiles := map[string]func(string) logging.Config{
		"cli":     logging.CLIProfile,
		"service": logging.ServiceProfile,
	}
	for name, profile := range profiles {
		t.Run(name, func(t *testing.T) {
			var cfg logging.Config = profile("svc-" + name)
			if cfg.Service != "svc-"+name {
				t.Fatalf("Service = %q, want svc-%s", cfg.Service, name)
			}
			if cfg.Console == "" || cfg.Console == logging.ConsoleNone {
				t.Fatalf("Console = %q, want a visible console", cfg.Console)
			}
			var buf strings.Builder
			cfg.Writer = &buf
			cfg.WarnWriter = nil
			logger, err := logging.New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			logger.Info("amended-info")
			logger.Error("amended-error")
			if got := buf.String(); !strings.Contains(got, "amended-info") || !strings.Contains(got, "amended-error") {
				t.Fatalf("amended writer = %q, want both records", got)
			}
		})
	}
	if got := streams.Stdout() + streams.Stderr(); got != "" {
		t.Fatalf("amended profiles still wrote to std streams: %q", got)
	}

	cli := logging.CLIProfile("tool")
	if cli.Writer != os.Stderr || cli.WarnWriter != nil {
		t.Fatalf("CLI profile Writer=%v WarnWriter=%v, want stderr and no split", cli.Writer, cli.WarnWriter)
	}
	svc := logging.ServiceProfile("daemon")
	if svc.Writer != os.Stdout || svc.WarnWriter != os.Stderr {
		t.Fatalf("service profile Writer=%v WarnWriter=%v, want stdout and stderr", svc.Writer, svc.WarnWriter)
	}
}

// DHF-TEST: keel/requirement-167
func TestChildOutputLevelIsReadableFromLogger(t *testing.T) {
	var buf strings.Builder
	logger, err := logging.New(logging.Config{Writer: &buf, ChildOutputLevel: slog.LevelWarn})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for name, l := range map[string]*logging.Logger{"base": logger, "with": logger.With("k", "v"), "group": logger.WithGroup("g")} {
		if lv := l.ChildOutputLevel(); lv == nil || lv.Level() != slog.LevelWarn {
			t.Fatalf("%s ChildOutputLevel = %v, want WARN", name, lv)
		}
	}
	var unset *logging.Logger
	if lv := unset.ChildOutputLevel(); lv != nil {
		t.Fatalf("nil logger ChildOutputLevel = %v, want nil", lv)
	}
}

// DHF-TEST: keel/requirement-167
func TestSeveritySplitRedactsOnBothBranches(t *testing.T) {
	var low, high strings.Builder
	cfg := logging.ServiceProfile("svc")
	cfg.Writer, cfg.WarnWriter = &low, &high
	logger, err := logging.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.With("api_token", "s3cr3t-with").Info("info-rec", "password", "s3cr3t-info")
	logger.Warn("warn-rec", "url", "https://ghp_abcdefghijklmnopqrstuvwxyz0123456789@github.com/o/r.git")
	for name, got := range map[string]string{"low": low.String(), "high": high.String()} {
		if strings.Contains(got, "s3cr3t") || strings.Contains(got, "ghp_abcdefghijklmnopqrstuvwxyz0123456789") {
			t.Fatalf("%s branch leaked a secret: %q", name, got)
		}
	}
	if !strings.Contains(low.String(), "info-rec") || strings.Contains(low.String(), "warn-rec") {
		t.Fatalf("low branch = %q, want only info-rec", low.String())
	}
	if !strings.Contains(high.String(), "warn-rec") || strings.Contains(high.String(), "info-rec") {
		t.Fatalf("high branch = %q, want only warn-rec", high.String())
	}
}
