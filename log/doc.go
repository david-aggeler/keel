// Package log is keel's structured-logging foundation: a thin layer over the
// standard library's [log/slog] that every keel consumer shares. It is imported
// under the alias "logging" by convention (import logging
// "github.com/david-aggeler/keel/log") to avoid colliding with the stdlib "log"
// package.
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
// # Typical use
//
// Construct one logger from a [Config], defer its [Logger.Close], then log
// through the leveled methods and the banner/field helpers:
//
//	logger, err := log.New(log.Config{Service: "gateway", Console: log.ConsolePlain, TextDir: ".logs"})
//	if err != nil {
//		return err
//	}
//	defer logger.Close()
//
//	logger.Header("gateway", version) // ruled startup banner
//	logger.Debug("config loaded", "path", cfgPath)
//	logger.Info("listening", "addr", addr)
//	logger.Warn("retrying", "attempt", n)
//	logger.Error("request failed", "err", err)
//	logger.Section("shutdown")
//
// The leveled methods — [Logger.Debug], [Logger.Info], [Logger.Warn],
// [Logger.Error] and their *Context variants — take a message and alternating
// key/value args, exactly like [log/slog]. The minimum level emitted is set by
// Config.Level (nil defaults to Info), so Debug is dropped unless Level is
// lowered to slog.LevelDebug.
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
// The remaining surface hangs off [Logger]. [Logger.Header] and [Logger.Section]
// emit ruled banners, and [Logger.Field]/[Logger.Fields] emit aligned
// label/value rows ([FieldRow]) — these render in every console mode, not just
// plain text. [Logger.Emit] logs a metrics event, and [Logger.LogBuildIdentity]
// logs a one-line build-identity record at startup.
package log
