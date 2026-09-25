// Package log is keel's structured-logging foundation: a thin layer over the
// standard library's [log/slog] that every keel consumer shares. It is imported
// under the alias "logging" by convention (import logging
// "github.com/david-aggeler/keel/log") to avoid colliding with the stdlib "log"
// package.
//
// # Start from a profile
//
// Two profiles configure the whole console shape in one call:
//
//   - [CLIProfile] — human-readable diagnostics on stderr. stdout stays free for
//     the payload a command's verbs declare.
//   - [ServiceProfile] — JSON records split by severity: Info and below on
//     stdout, Warn and above on stderr, the split log aggregators tag by.
//
// A profile returns an ordinary [Config]. Read or amend any field, then pass it
// to [New]; there is no second construction path.
//
//	cfg := log.CLIProfile("gateway")
//	cfg.TextDir = ".logs"
//	logger, err := log.New(cfg)
//	if err != nil {
//		return err
//	}
//	defer logger.Close()
//
//	logger.BuildIdentity("gateway", version, gitCommit) // ruled startup banner + build identity
//	logger.Debug("config loaded", "path", cfgPath)
//	logger.Info("listening", "addr", addr)
//	logger.Warn("retrying", "attempt", n)
//	logger.Error("request failed", "err", err)
//	logger.Section("shutdown")
//
// # The fields are the escape hatch
//
// Every [Config] field stays public for the consumer a profile does not fit,
// and the zero value is usable. A nil Config.Writer resolves to os.Stderr —
// diagnostics never default onto stdout. Config.WarnWriter splits the console
// by severity; Config.ForceColor and Config.DisableColor are the explicit color
// policy, which keel/term applies, and beat NO_COLOR; Config.ChildOutputLevel
// sets the severity keel/exec gives a child process's output lines.
// Config.FileRetention is the consumer-owned retention age for both daily file
// sinks. Its zero value keeps every file; positive values prune only this
// service's exact <service>-YYYY-MM-DD.log and .jsonl names. Per-run JSONL files
// are never pruned by this setting, and the active file for today is always
// preserved.
//
// # Four sinks
//
// A production logger fans one log record out to the sinks selected by [Config]:
//
//   - the console — sparse-AI, human-readable text, machine JSON, or none;
//   - a daily human-readable rolling file under Config.TextDir; and
//   - a daily JSON Lines rolling file under Config.JSONLDir.
//
// [New] is the single public logger constructor. File sinks opened by [New] are
// owned by the returned [Logger] and released by [Logger.Close]. All sinks share
// one field schema — ts (RFC3339Nano), level (uppercase), msg, service — so the
// JSON and human renderings of a record always agree.
//
// The leveled methods — [Logger.Debug], [Logger.Info], [Logger.Warn],
// [Logger.Error] and their *Context variants — take a message and alternating
// key/value args, exactly like [log/slog]. The minimum level emitted is set by
// Config.ConsoleVerbosity (nil defaults to Info), so Debug is dropped unless
// ConsoleVerbosity is lowered to slog.LevelDebug. File sinks use
// Config.FileVerbosity (nil defaults to Debug).
//
// # Request-scoped loggers
//
// [WithLogger] carries a request-scoped [log/slog.Logger] through a
// [context.Context], and [FromContext] reads it back at downstream call sites,
// falling back to slog.Default when the context has no logger. Callers enrich
// the logger with slog's own With method before storing or emitting; keel does
// not interpret context values or inject attributes itself.
//
// # Redaction at the boundary
//
// Every rendered string — messages, attr values, and error text — passes through
// the same secret-scrubbing path before it reaches any sink: DSN passwords,
// bearer tokens, and PATs in URLs or query params are masked, and attrs whose
// key looks sensitive (token/password/secret/pat) are dropped wholesale.
// [RedactErr] exposes the same treatment for errors. Redaction is applied once,
// at the log boundary, so callers never have to pre-scrub values they log.
//
// # Beyond the sinks
//
// [OperationalError] is an error type that bundles an operation name, a
// human-facing message, the underlying cause, task/log-file/line/exit-code/hint
// diagnostics, and arbitrary structured metadata into one value. It implements
// [log/slog.LogValuer], so its string content is redacted at the log boundary
// like any other logged value; reach for it only where the same multi-field
// failure context is logged repeatedly.
//
// The remaining surface hangs off [Logger]. [Logger.Header] emits a banner-only
// rule, [Logger.BuildIdentity] emits a startup banner plus the structured build
// identity event, and [Logger.Section] emits section banners. [Logger.Field] and
// [Logger.Fields] emit aligned label/value rows ([FieldRow]) — these render in
// every console mode, not just plain text. [Logger.Emit] logs a metrics event,
// and [Logger.LogBuildIdentity] logs only the one-line build-identity record.
package log
