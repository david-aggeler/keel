// Package claude wraps headless `claude -p` invocations: prompt in, final text
// plus token/cost/turn metrics out. It is the Claude Code adapter half of keel's
// exec facility and runs on top of keel/exec, so every call inherits the shared
// START/END process lifecycle logging and secret redaction.
//
// It exists for skill-optimization harnesses (openbrain CR-0249 and successors):
// measuring how a materialized skill triggers and what it costs requires many
// structured `claude -p` calls, and shelling out ad hoc loses the usage metrics
// the JSON output format carries. The wrapper pins stream-json output, curates
// progress events into "claude progress" log records, parses the final result
// event, and surfaces tokens, cost, turns, and duration as the typed fields of
// [Result].
//
// [Run] is the single entry point. The zero value of [Request] is not usable —
// Prompt is required; Dir selects which project's .claude/ tree the headless
// session discovers, and Bin points at the claude binary (empty resolves
// "claude" on PATH, and tests point it at a stub so the package stays hermetic).
package claude
