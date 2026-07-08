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
// optional writers, and additionally logged line-wise through keel/log — stdout
// at Debug, stderr at Info — with every line passed through the same redaction
// path as the rest of keel's logging. This is what lets a consumer reconstruct
// exactly what ran and what it printed from the logs alone.
//
// # Usage
//
// [ProcessStart] starts the child and returns immediately; [Process.Wait] blocks
// until it exits and returns a [Result] with the exit code, duration, and the
// full captured stdout/stderr. Cancelling the context passed to ProcessStart
// kills the child. [Request] carries the launch parameters; its SensitiveArgs
// map marks argv positions to mask in the logged command line, and Configure is
// an escape hatch for adjusting the underlying [os/exec.Cmd] (process group,
// cancel behavior) before it starts.
//
// The keel/exec/claude and keel/exec/codex adapters are both built on this
// facility, so headless claude and codex invocations inherit the same lifecycle
// logging and redaction for free.
package exec
