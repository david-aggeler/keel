// Package log is keel's structured-logging foundation: a thin layer over the
// standard library's [log/slog] that every keel consumer shares. It is imported
// under the alias "logging" by convention (import logging
// "github.com/david-aggeler/keel/log") to avoid colliding with the stdlib "log"
// package.
//
// # Three sinks
//
// A production logger fans one log record out to as many as three sinks at once:
//
//   - the console — either machine JSON ([New]) or human-readable text
//     ([NewConsole]) on stdout;
//   - a daily human-readable rolling file ([NewHumanFileHandler]); and
//   - a daily JSON Lines rolling file ([NewJSONFileHandler]).
//
// The file handlers are opened once and owned by the caller for the invocation
// lifetime (they satisfy [io.Closer] and must be Closed once); pass them to
// [New] or [NewConsole] via [Config]. All three sinks share one field schema —
// ts (RFC3339Nano), level (lowercase), msg, service — so the JSON and human
// renderings of a record always agree.
//
// # Redaction at the boundary
//
// Every rendered string — messages, attr values, and error text — passes through
// the same secret-scrubbing path ([RedactString], [RedactErr]) before it reaches
// any sink: DSN passwords, bearer tokens, and PATs in URLs or query params are
// masked, and attrs whose key looks sensitive (token/password/secret/pat) are
// dropped wholesale. Redaction is applied once, at the log boundary, so callers
// never have to pre-scrub values they log.
//
// # Beyond the sinks
//
// The package also carries the diagnostic surfaces built on top of the sinks:
// [OperationalError] (a multi-field error carrier that redacts itself for slog),
// [RecentBuffer]/[TeeRecent] (an in-process ring buffer of recent warn/error
// records for a /diag surface), the [Metric]/[Emit] metrics convention, the
// [Header]/[Section]/[Field] human banner helpers, and [LogBuildIdentity] for
// startup build-identity logging. [NewForTesting] and [RecordCapture] let tests
// assert on emitted records without touching the global default logger.
package log
