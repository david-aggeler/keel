package log

import (
	"log/slog"
	"strings"
)

// OperationalError is an opt-in log carrier (not a wire-envelope type) that
// bundles an operation name, a human message, an underlying error, and
// arbitrary structured metadata into one value, and renders itself for slog
// with redaction applied at the log boundary (slog.LogValuer).
//
// It is NOT a replacement for the CR-0221 KD1 flat-field convention. Use it
// only where the same multi-field failure context is logged repeatedly.
//
// Redaction contract: LogValue() routes root_cause and every *string* value in
// Metadata through RedactString. Non-string metadata values are emitted via
// slog.Any WITHOUT redaction — callers MUST NOT place secrets (DSNs, bearer
// tokens) in non-string metadata. (KD8.)
//
// DHF-REQ: keel/requirement-18
type OperationalError struct {
	Op        string         // operation/handler name, e.g. "link_blocks"
	Message   string         // human-facing summary, e.g. "cross-product link rejected"
	Err       error          // underlying cause; may be nil
	Task      string         // failing task label, e.g. "ci:vet"
	LogFile   string         // run-log file carrying the task's records
	StartLine int            // 1-based line number of the task's first run-log record
	ExitCode  int            // process exit code to return for this failure
	Hint      string         // human instruction pointing at LogFile and StartLine
	Metadata  map[string]any // structured context; string values are redacted in LogValue
}

// Error renders Op, Message, and Err, skipping empty segments. NOT redacted —
// this is the developer-facing string; redaction happens only at LogValue. (KD10.)
// Returns "<nil>" when called on a nil receiver. Returns "operational error" when
// all fields are empty.
func (e *OperationalError) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := make([]string, 0, 3)
	if e.Op != "" {
		parts = append(parts, e.Op)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	if len(parts) == 0 {
		return "operational error"
	}
	return strings.Join(parts, ": ")
}

// Unwrap exposes the cause so errors.Is/errors.As work through the carrier.
// Unlike the wire-string concat sites, a Go error chain genuinely survives here. (KD10.)
func (e *OperationalError) Unwrap() error { return e.Err }

// reservedLogKeys are the keys that LogValue emits for the carrier's own fields.
// Metadata entries with these keys are silently dropped to avoid duplicate/ambiguous
// attrs in the emitted group.
var reservedLogKeys = map[string]struct{}{
	"op":         {},
	"message":    {},
	"root_cause": {},
	"task":       {},
	"log_file":   {},
	"start_line": {},
	"exit_code":  {},
	"hint":       {},
}

// LogValue implements slog.LogValuer. It emits a GroupValue nested under the
// caller-chosen attr key (e.g. slog.Any("err", opErr) → an "err" group). It
// never emits G1 reserved keys (ts/level/msg/service). (KD8.)
// Metadata keys op/message/root_cause are reserved and silently dropped to avoid
// colliding with the carrier's own fields.
// Returns an empty GroupValue when called on a nil receiver.
//
// DHF-REQ: keel/requirement-18
func (e *OperationalError) LogValue() slog.Value {
	if e == nil {
		return slog.GroupValue()
	}
	attrs := make([]slog.Attr, 0, 3+len(e.Metadata))
	if e.Op != "" {
		attrs = append(attrs, slog.String("op", e.Op))
	}
	if e.Message != "" {
		attrs = append(attrs, slog.String("message", e.Message))
	}
	if e.Err != nil {
		attrs = append(attrs, slog.String("root_cause", RedactString(e.Err.Error())))
	}
	if e.Task != "" {
		attrs = append(attrs, slog.String("task", e.Task))
	}
	if e.LogFile != "" {
		attrs = append(attrs, slog.String("log_file", RedactString(e.LogFile)))
	}
	if e.StartLine > 0 {
		attrs = append(attrs, slog.Int("start_line", e.StartLine))
	}
	if e.ExitCode != 0 {
		attrs = append(attrs, slog.Int("exit_code", e.ExitCode))
	}
	if e.Hint != "" {
		attrs = append(attrs, slog.String("hint", RedactString(e.Hint)))
	}
	for k, v := range e.Metadata {
		if _, reserved := reservedLogKeys[k]; reserved {
			continue // silently drop; carrier's own field wins
		}
		if s, ok := v.(string); ok {
			attrs = append(attrs, slog.String(k, RedactString(s)))
		} else {
			attrs = append(attrs, slog.Any(k, v)) // KD8: non-strings unredacted by contract
		}
	}
	return slog.GroupValue(attrs...)
}

// Compile-time assertions: *OperationalError must satisfy both interfaces.
var _ error = (*OperationalError)(nil)
var _ slog.LogValuer = (*OperationalError)(nil)
