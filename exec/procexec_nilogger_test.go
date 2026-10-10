package exec_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	osexec "os/exec"
	"strconv"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

// DHF-TEST: keel/requirement-122
func TestProcessStartWithNilLoggerWritesNothingToTheDefaultSink(t *testing.T) {
	var captured bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
		Program: "sh",
		Args:    []string{"-c", "printf 'child stdout\n'; printf 'child stderr\n' >&2"},
	})
	if err != nil {
		t.Fatalf("ProcessStart returned error: %v", err)
	}
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	if got := captured.String(); got != "" {
		t.Fatalf("a Request with no Logger wrote %d bytes to the process-wide default sink; want none:\n%s", len(got), got)
	}
}

// DHF-TEST: keel/requirement-122 (keel/ac-795)
func TestProcessStartWithTypedNilLoggerIsSilentAndDoesNotPanic(t *testing.T) {
	var nilSlog *slog.Logger
	var nilKeel *logging.Logger
	for _, tc := range []struct {
		name string
		set  func(*procexec.Request)
	}{
		{"slog", func(r *procexec.Request) { r.Logger = nilSlog }},
		{"keel-log", func(r *procexec.Request) { r.Logger = nilKeel }},
	} {
		for _, exit := range []int{0, 3} {
			t.Run(tc.name+"/exit-"+strconv.Itoa(exit), func(t *testing.T) {
				var captured bytes.Buffer
				previous := slog.Default()
				slog.SetDefault(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug})))
				t.Cleanup(func() { slog.SetDefault(previous) })

				req := procexec.Request{
					Program: "sh",
					Args:    []string{"-c", "printf 'child stdout\n'; printf 'child stderr\n' >&2; exit " + strconv.Itoa(exit)},
				}
				tc.set(&req)
				proc, err := procexec.ProcessStart(context.Background(), req)
				if err != nil {
					t.Fatalf("ProcessStart returned error: %v", err)
				}
				res, err := proc.Wait()
				var exitErr *osexec.ExitError
				if err != nil && (exit == 0 || !errors.As(err, &exitErr)) {
					t.Fatalf("Wait returned error: %v", err)
				}
				if res.ExitCode != exit {
					t.Fatalf("ExitCode = %d, want %d", res.ExitCode, exit)
				}
				if res.Stdout != "child stdout\n" || res.Stderr != "child stderr\n" {
					t.Fatalf("Result output = %q / %q, want the child's captured lines", res.Stdout, res.Stderr)
				}
				if got := captured.String(); got != "" {
					t.Fatalf("a typed-nil Logger wrote %d bytes to the process-wide default sink; want none:\n%s", len(got), got)
				}
			})
		}
	}
}
