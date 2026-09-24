// Package term is keel's single source of terminal capability: whether a
// standard stream is connected to a terminal, whether output on it may carry
// color or animation, whether the process may prompt, and how large the
// terminal is.
//
// # Detection
//
// Terminal-ness is resolved per stream, by ioctl — TCGETS on Linux, TIOCGETA on
// Darwin — never by file mode. A character device is not evidence of a
// terminal: /dev/null is a character device and is reported as not a
// terminal. stdin, stdout and stderr are answered independently, so a piped
// stdout does not suppress color on a terminal stderr. On a platform without
// an ioctl binding every stream is reported as not a terminal.
//
// A failed probe means not-a-terminal, never an error: every query always
// answers, because its callers sit on rendering paths with nothing useful to
// do with an error.
//
// # Environment is policy, never evidence
//
// The environment may only forbid. NO_COLOR set to a non-empty value forbids
// color. TERM set to "dumb", or unset, or empty, forbids color and animation.
// No variable — TERM_PROGRAM, a multiplexer marker, a CI marker, FORCE_COLOR —
// ever makes a stream that is not a terminal report as one.
//
// # Precedence
//
// Explicit policy beats the environment, which beats detection. A
// [ColorAlways] policy permits color even when NO_COLOR is set; a
// [ColorNever] policy forbids it even on a terminal. [Config.NoInput] and
// [Config.NoAnimation] only forbid: no policy forces prompting or animation
// onto a stream that detection and the environment do not permit. Animation
// is permitted only where color on stderr is permitted and stderr is a
// terminal, so [ColorNever] and NO_COLOR also forbid animation, while
// [ColorAlways] never grants it into a pipe.
//
// # Read-only, live size
//
// [New] resolves a [Capability] once. Terminal-ness, color, animation and
// prompting are fixed at construction; the value has no setters. Size is the
// exception: [Capability.Size] re-reads the terminal on every call, because a
// window resize changes it within one process lifetime. No SIGWINCH handler is
// installed — a live read costs one syscall and has no process-global effect.
//
// # Injection
//
// A hermetic test cannot produce a terminal, so [Config.Probe] accepts an
// injected [Probe] and [Config.Getenv] an injected environment. The injected
// answer reaches every branch downstream of detection; it never proves the
// ioctl itself, which [IsTerminal] and [FileSize] exercise directly.
//
// The package imports no other keel package and no module outside the
// standard library, so keel/log and keel/cli can both depend on it without
// gaining an edge on each other.
package term
