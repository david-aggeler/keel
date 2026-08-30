package main

import (
	"errors"
	"testing"

	"github.com/david-aggeler/keel/cli"
)

// TestHelpErrorExitCode covers every branch of helpErrorExitCode: no error, a
// usage error carrying the tree's exit code, and any other error. The
// unresolvable help topic path returns a cli.UsageError, so this is the
// mapping that makes that path exit 2 instead of 0.
//
// DHF-TEST: keel/ac-640
func TestHelpErrorExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "no error exits zero", err: nil, want: 0},
		{name: "usage error exits two", err: cli.UsageError{Err: errors.New("unknown help topic \"bogus\"")}, want: 2},
		{name: "wrapped usage error exits two", err: errors.Join(errors.New("context"), cli.UsageError{Err: errors.New("unknown help topic \"bogus\"")}), want: 2},
		{name: "other error exits one", err: errors.New("render failed"), want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := helpErrorExitCode(tc.err); got != tc.want {
				t.Fatalf("helpErrorExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}
