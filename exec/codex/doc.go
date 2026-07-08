// Package codex wraps headless `codex exec --json` invocations: prompt in, a
// streaming JSONL event log out. It is the codex adapter half of keel's exec
// facility and runs on top of keel/exec, so every call inherits the shared
// START/END process lifecycle logging and secret redaction.
//
// Codex streams a multi-line JSONL event sequence on stdout. The wrapper reads
// stdout line-by-line as the process runs, decoding each line into an [Event]
// and delivering it via Request.OnEvent before the process exits — true
// streaming, not buffered replay — while normalizing codex's several event
// framings so [Event.Type] is always the semantic type a consumer expects. The
// full stream, the captured thread id, and the exit code are collected in
// [Result].
//
// [Run] is the primary entry point; [Version] reports the codex binary's
// version. The zero value of [Request] is not usable — Prompt is required; Dir
// selects the working directory, Bin points at the codex binary (empty resolves
// "codex" on PATH, and tests point it at a stub so the package stays hermetic),
// and Env layers additional environment assignments onto the child.
package codex
