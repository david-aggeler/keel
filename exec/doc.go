// Package exec is keel's subprocess facility: one place to launch external
// commands so that every child process is logged uniformly and its output is
// scrubbed of secrets. It is imported under the alias "procexec" by convention
// (import procexec "github.com/david-aggeler/keel/exec") to avoid colliding with
// the standard library's os/exec.
//
// # START/END lifecycle
//
// Every launch through [ProcessStart] emits a "process start" record (program,
// redacted command line, working directory) before the child runs and a
// "process end" record (exit code, elapsed ms) when it is reaped by
// [Process.Wait]. Child stdout and stderr are captured, mirrored to the caller's
// optional writers, and additionally logged line-wise through keel/log, with
// every line passed through the same redaction path as the rest of keel's
// logging. This is what lets a consumer reconstruct exactly what ran and what it
// printed from the logs alone.
//
// # Child output severity
//
// Both streams log at one level: keel/log's Config.ChildOutputLevel, else
// Debug. The stream is a "stream" attribute, never a severity — which stream a
// child writes to is its transport choice. The exit status is the only source
// of a child's severity: a non-zero exit replays the last [FailureTailLines]
// lines at Error and ends at Error, so a failure reads in full at the default
// console floor. [Request.Classify] is the per-process-type adapter for a child
// whose format the caller knows: it can override a line's level and report the
// level the child declared itself, which travels in a "declared_level" field
// that is absent when nothing was declared. Core keel/exec never infers one.
//
// # Usage
//
// [ProcessStart] starts the child and returns immediately; [Process.Wait] blocks
// until it exits and returns a [Result] with the exit code, duration, and the
// full captured stdout/stderr. Wait is idempotent: subsequent calls return the
// same result/error and do not emit another "process end" record. Cancelling
// the context passed to ProcessStart kills the child. [Request] carries the
// launch parameters; its SensitiveArgs map marks argv positions to mask in the
// logged command line, and Configure is an escape hatch for adjusting the
// underlying [os/exec.Cmd] (process group, cancel behavior) before it starts.
//
// # CLI-adapter outcome contract
//
// [DecideCLIOutcome] is the one place keel decides whether a wrapped CLI run
// succeeded. An adapter supplies the two per-CLI facts it can observe — the
// child's exit code and its wrapped CLI's terminal-event verdict — and gets
// back a [CLIOutcome]. Either fact alone fails the run and neither masks the
// other. The output ceiling outranks the decision entirely, so an adapter
// checks [ErrOutputLimitExceeded] and returns before calling. Keeping the rule
// here rather than in each adapter is what lets a consumer ask "did this run
// fail?" once, whichever CLI it is talking to.
//
// The keel/exec/claude and keel/exec/codex adapters are both built on this
// facility, so headless claude and codex invocations inherit the same lifecycle
// logging and redaction for free.
package exec
