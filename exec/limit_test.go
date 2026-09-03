package exec_test

import (
	"testing"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
)

// TestLimitStateAbsentIsNotZeroUsage pins the distinction the whole field
// exists for: a state nobody reported must not read as a window with room left.
//
// DHF-TEST: keel/requirement-161
func TestLimitStateAbsentIsNotZeroUsage(t *testing.T) {
	var absent procexec.LimitState
	reportedEmpty := procexec.LimitState{Reported: true, UsedFraction: 0}

	if absent.Reported {
		t.Errorf("zero LimitState.Reported = true, want false")
	}
	if !reportedEmpty.Reported {
		t.Errorf("reported empty window .Reported = false, want true")
	}
	if absent == reportedEmpty {
		t.Errorf("absent state compares equal to a reported 0-usage state; a caller cannot tell them apart")
	}
}

// TestLimitStateReached covers the threshold rule both adapters key their quota
// attribution on, including the never-reported case that must never attribute.
//
// DHF-TEST: keel/requirement-161
func TestLimitStateReached(t *testing.T) {
	cases := []struct {
		name  string
		state procexec.LimitState
		want  bool
	}{
		{"never reported", procexec.LimitState{}, false},
		{"never reported, stale fraction", procexec.LimitState{UsedFraction: 1}, false},
		{"reported below the threshold", procexec.LimitState{Reported: true, UsedFraction: 0.99}, false},
		{"reported at the threshold", procexec.LimitState{Reported: true, UsedFraction: procexec.LimitReachedFraction}, true},
		{"reported above the threshold", procexec.LimitState{Reported: true, UsedFraction: 1.5}, true},
		{"executor said so below the threshold", procexec.LimitState{Reported: true, UsedFraction: 0.4, ReachedReported: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.state.Reached(); got != tc.want {
				t.Errorf("Reached() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLimitStateCarriesWindowAndReset checks the fields the shared type
// promises a caller, so a policy decision (wait until the reset, fail over) has
// what it needs without re-reading a transcript.
//
// DHF-TEST: keel/requirement-161
func TestLimitStateCarriesWindowAndReset(t *testing.T) {
	reset := time.Unix(1788859323, 0).UTC()
	s := procexec.LimitState{Reported: true, UsedFraction: 0.8, Window: 7 * 24 * time.Hour, ResetsAt: reset}
	if s.Window != 7*24*time.Hour {
		t.Errorf("Window = %v, want 168h", s.Window)
	}
	if !s.ResetsAt.Equal(reset) {
		t.Errorf("ResetsAt = %v, want %v", s.ResetsAt, reset)
	}
}
