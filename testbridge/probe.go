package testbridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/david-aggeler/keel/vscode"
)

// DefaultDesiredStateProbeDeadline bounds one desired-state probe execution
// when Runtime carries no override. It is a ceiling on waiting, not a
// performance target: a probe that finishes fast is unaffected.
//
// DHF-REQ: keel/requirement-129
const DefaultDesiredStateProbeDeadline = 30 * time.Second

type probePassKey struct{}

// probeMemoKey identifies a desired-state row within one derivation pass. It is
// deliberately the row's own identity — never the probe function value: every
// closure minted from one function literal shares a code pointer while
// capturing different variables, so keying on the func pointer would make two
// distinct rows collide and render one row's verdict for the other.
type probeMemoKey struct {
	RunID    string
	Resource string
	Root     string
}

type probeOutcome struct {
	result DesiredStateProbeResult
	err    error
}

// probePass is the scope over which a probe result may be shared: one
// discover, one desired-state, or one run. It never outlives that pass, so a
// probe result can never assert something about the environment that has since
// stopped being true.
//
// DHF-REQ: keel/requirement-129
type probePass struct {
	deadline time.Duration
	mu       sync.Mutex
	outcomes map[probeMemoKey]probeOutcome
}

// withProbePass opens a derivation pass on ctx. deadline <= 0 selects the
// package default.
func withProbePass(ctx context.Context, deadline time.Duration) context.Context {
	if deadline <= 0 {
		deadline = DefaultDesiredStateProbeDeadline
	}
	return context.WithValue(ctx, probePassKey{}, &probePass{
		deadline: deadline,
		outcomes: map[probeMemoKey]probeOutcome{},
	})
}

// probePassFrom returns the pass carried by ctx, or nil. A derivation reached
// without a pass still runs: it simply memoizes nothing and bounds each probe
// with the package default.
func probePassFrom(ctx context.Context) *probePass {
	pass, _ := ctx.Value(probePassKey{}).(*probePass)
	return pass
}

func (p *probePass) load(key probeMemoKey) (probeOutcome, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	outcome, ok := p.outcomes[key]
	return outcome, ok
}

func (p *probePass) store(key probeMemoKey, outcome probeOutcome) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.outcomes[key] = outcome
}

// probeBoundError reports a probe abandoned at its bound. It names the
// responsible resource and the bound it breached, so the rendered row diagnoses
// the hang instead of restating a bare context error.
//
// DHF-REQ: keel/requirement-129
type probeBoundError struct {
	Resource string
	Deadline time.Duration
	Cause    error
}

func (e probeBoundError) Error() string {
	if errors.Is(e.Cause, context.DeadlineExceeded) {
		return fmt.Sprintf("keel/testbridge: desired-state probe for %q timed out after %s", e.Resource, e.Deadline)
	}
	return fmt.Sprintf("keel/testbridge: desired-state probe for %q was canceled after %s", e.Resource, e.Deadline)
}

func (e probeBoundError) Unwrap() error { return e.Cause }

func (e probeBoundError) currentValue() string {
	if errors.Is(e.Cause, context.DeadlineExceeded) {
		return "timeout"
	}
	return "canceled"
}

// executeDesiredStateProbe runs row's probe at most once per pass, under the
// pass deadline. A breached bound is returned as a probeBoundError, never as a
// probe result.
//
// DHF-REQ: keel/requirement-129
func executeDesiredStateProbe(ctx context.Context, root string, row DesiredStateRow) (DesiredStateProbeResult, error) {
	pass := probePassFrom(ctx)
	deadline := DefaultDesiredStateProbeDeadline
	if pass != nil {
		deadline = pass.deadline
	}
	key := probeMemoKey{RunID: row.RunID, Resource: row.Resource, Root: root}
	if pass != nil {
		if outcome, ok := pass.load(key); ok {
			return outcome.result, outcome.err
		}
	}
	result, err := runBoundedProbe(ctx, deadline, row, DesiredStateProbeRequest{RunID: row.RunID, Root: root})
	if pass != nil {
		pass.store(key, probeOutcome{result: result, err: err})
	}
	return result, err
}

// runBoundedProbe executes one probe and abandons it at the deadline. The probe
// runs on its own goroutine because a probe that ignores its context — the
// shipped probeStubBinaries waiting on a `go test` subprocess is exactly one —
// would otherwise hold the derivation regardless of any deadline the caller
// sets.
func runBoundedProbe(ctx context.Context, deadline time.Duration, row DesiredStateRow, request DesiredStateProbeRequest) (DesiredStateProbeResult, error) {
	probeCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	done := make(chan DesiredStateProbeResult, 1)
	probe := row.Probe
	go func() { done <- probe(probeCtx, request) }()
	select {
	case result := <-done:
		return result, nil
	case <-probeCtx.Done():
		// A probe that returned in the same instant the bound expired is
		// still a completed probe: prefer its verdict over the timeout.
		select {
		case result := <-done:
			return result, nil
		default:
		}
		return DesiredStateProbeResult{}, probeBoundError{Resource: row.Resource, Deadline: deadline, Cause: probeCtx.Err()}
	}
}

// logDesiredStateProbeAbandoned records the breached bound on the run log, so
// the hang is diagnosable from the log even when nobody reads the row.
func logDesiredStateProbeAbandoned(ctx context.Context, bound probeBoundError) {
	rt, ok := RuntimeFrom(ctx)
	if !ok || rt.Log == nil {
		return
	}
	rt.Log.Warn("desired-state probe abandoned at its deadline",
		"resource", bound.Resource,
		"deadline", bound.Deadline.String(),
		"cause", bound.Cause.Error(),
	)
}

// abandonedDesiredState renders a probe that breached its bound as a row rather
// than an error, so the remaining rows of the group still reach the consumer.
//
// DHF-REQ: keel/requirement-129
func abandonedDesiredState(row DesiredStateRow, bound probeBoundError) vscode.DesiredState {
	return vscode.DesiredState{
		RunID:    row.RunID,
		Resource: row.Resource,
		Kind:     row.Kind,
		Desired:  row.Desired,
		Current:  bound.currentValue(),
		Status:   "blocked",
		Action:   "manual_setup_required",
		Message:  bound.Error(),
		Detail:   row.Detail,
		Reusable: row.Reusable,
		Owned:    row.Owned,
		Active:   row.Active,
	}
}
