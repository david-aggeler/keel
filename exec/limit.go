package exec

import (
	"errors"
	"time"
)

// ErrQuotaExhausted attributes a failed CLI-adapter run to the wrapped
// executor's own usage limit. An adapter wraps it into the failure error when,
// and only when, the last limit state the executor reported says the limit was
// reached, so a caller identifies the cause with errors.Is instead of matching
// stderr text — the trap that sent a 2026-09-03 drain chasing a phantom cache
// fault (keel/issue-230).
//
// It attributes a failure; it does not create one. The verdict itself stays
// with [DecideCLIOutcome]: a quota-exhausted run failed before the sentinel was
// wrapped, and a successful run never carries it.
//
// DHF-REQ: keel/requirement-161
var ErrQuotaExhausted = errors.New("keel/exec: executor usage limit reached")

// LimitReachedFraction is the used fraction at or above which a reported limit
// state counts as reached even when the executor did not say so itself. The
// fraction is a share of one window, so 1 is a full window.
//
// DHF-REQ: keel/requirement-161
const LimitReachedFraction = 1.0

// LimitState is the usage-limit state a wrapped CLI reported about itself on
// its own event stream. It is the one shape both keel/exec CLI adapters report,
// so a caller reads a single struct whichever executor it dispatched.
//
// The zero value means the executor reported nothing. That is deliberately not
// the same value as a reported empty window: an absent measurement rendered as
// zero usage reads as "plenty of quota left" and would invite a caller to
// dispatch into an exhausted executor. Read [LimitState.Reported] before
// reading any other field.
//
// DHF-REQ: keel/requirement-161
type LimitState struct {
	// Reported is true when the executor reported a limit state at all. It is
	// what separates "said nothing" from "said zero".
	Reported bool
	// UsedFraction is the share of the window already consumed: 0 for an empty
	// window, 1 for a full one. Meaningful only when Reported is true.
	UsedFraction float64
	// Window is the length of the limit window the fraction measures. It is 0
	// when the executor named a usage share without naming its window.
	Window time.Duration
	// ResetsAt is when the window refills. It is the zero Time when the
	// executor did not report a reset instant.
	ResetsAt time.Time
	// ReachedReported is the executor's own statement that its limit is
	// reached, carried separately from UsedFraction because an executor may
	// declare the limit hit without publishing a fraction that says so.
	ReachedReported bool
}

// Reached reports whether the executor is at its usage limit — either because
// it said so itself or because the fraction it published is at
// [LimitReachedFraction]. A state that was never reported is never reached: an
// adapter must not attribute a failure to a quota it has no reading for.
//
// DHF-REQ: keel/requirement-161
func (s LimitState) Reached() bool {
	if !s.Reported {
		return false
	}
	return s.ReachedReported || s.UsedFraction >= LimitReachedFraction
}
