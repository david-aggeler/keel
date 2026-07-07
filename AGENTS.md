# AGENTS.md

Read `CLAUDE.md`. It is the rulebook. This file is the short version.

- One public module. Anonymous `go get` must work. NO credentials on any
  build path. Ever.
- Dev records live in gold (product `keel`), not in markdown files.
- EVERY code change goes through a CR. No exceptions, no quick-change
  path. CR approved BEFORE code; closed with the merge SHA only after
  checking the diff against every AC with written evidence. Defects get
  an issue first; the fix still runs under a CR. Never fix silently.
- The gate is `go run ./cmd/keel-dev ci`. Same command in CI and release
  preflight. Don't invent other gates.
- All keel-dev output through keel/log, three sinks (console + `.logs/`
  human `.log` + `.jsonl`). Child output only via lineLogWriter. Handing
  os.Stdout to a subprocess fails lint.
- Tests hermetic. Adapters test against stubs. Live smokes are env-gated
  (`CODEXCLI_LIVE_SMOKE=1`, `CLAUDECLI_LIVE_SMOKE=1`) and skip in CI.
- Coverage floor 85% total, gate-enforced. Target ~90%.
- Error prefixes = keel package path (`keel/exec:`, `keel/exec/codex:`,
  `keel/exec/claude:`). Never bare `exec:` (stdlib collision).
