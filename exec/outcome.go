package exec

import "fmt"

// CLIOutcome is the shared success-or-failure verdict for one CLI-adapter run,
// as returned by [DecideCLIOutcome].
//
// DHF-REQ: keel/requirement-134
type CLIOutcome struct {
	// Failed is the verdict itself: true when the adapter must report the run
	// as a failure and return an error.
	Failed bool
	// ExitCodeFailed records that the child's exit code was the failing fact.
	ExitCodeFailed bool
	// TerminalEventFailed records that the wrapped CLI's terminal event was the
	// failing fact. Both flags can be set at once; neither masks the other.
	TerminalEventFailed bool
	// Reason states, in one clause, why the run failed — suitable for embedding
	// in the adapter's own error message. It is empty on a successful run.
	Reason string
}

// DecideCLIOutcome is the single expression of the keel/exec CLI-adapter
// outcome rule. Every adapter calls it instead of keying failure on a fact of
// its own, so a new adapter inherits the contract rather than re-deriving it
// (keel/requirement-134; the divergence it retires is keel/issue-162).
//
// It takes the two per-CLI facts an adapter supplies:
//
//   - exitCode is the child's process exit status as reported by
//     [Process.Wait]. Anything other than 0 fails the run, including the -1
//     sentinel for a child that never produced a status.
//   - terminalEventFailed is the wrapped CLI's own verdict, read from its
//     terminal event in that CLI's schema. Which event is terminal and which
//     field carries the verdict is an adapter-specific fact, specified by that
//     adapter's requirement, and is decided before this call.
//
// Either fact alone fails the run and neither can mask the other. The output
// ceiling is deliberately not an input: it is an infrastructure verdict that
// outranks this decision, so an adapter checks [ErrOutputLimitExceeded] and
// returns before reaching here.
//
// DHF-REQ: keel/requirement-134
func DecideCLIOutcome(exitCode int, terminalEventFailed bool) CLIOutcome {
	outcome := CLIOutcome{
		ExitCodeFailed:      exitCode != 0,
		TerminalEventFailed: terminalEventFailed,
	}
	outcome.Failed = outcome.ExitCodeFailed || outcome.TerminalEventFailed

	switch {
	case outcome.ExitCodeFailed && outcome.TerminalEventFailed:
		outcome.Reason = fmt.Sprintf("exited %d and its terminal event reported failure", exitCode)
	case outcome.ExitCodeFailed:
		outcome.Reason = fmt.Sprintf("exited %d", exitCode)
	case outcome.TerminalEventFailed:
		outcome.Reason = "exited 0 but its terminal event reported failure"
	}
	return outcome
}
