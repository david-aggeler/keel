# AGENTS.md

Read `CLAUDE.md`. It is the rulebook. This file is the short version.

- One public module. Anonymous `go get` must work. NO credentials on any
  build path. Ever.
- Dev records live in gold (product `keel`), not in markdown files.
- EVERY code change goes through a CR. No exceptions, no quick-change
  path. CR approved BEFORE code; closed with the merge SHA only after
  checking the diff against every AC with written evidence. Defects get
  an issue first; the fix still runs under a CR. Never fix silently.
- Manual worktrees go through `keel-dev worktree
  up|resume|down|branch-delete|status|compare` (backed by `keel/worktree`) —
  not raw `git worktree`/`git checkout -b`. The run-queue tail owns its own
  `cr-<seq>` worktrees and uses `openbrain-client worktree` for them. The
  old `worktree-*.sh` wrappers are gone; don't restore them.
- The gate is `go run ./cmd/keel-dev ci`, plus `keel-dev vsix ci` for the
  VSIX. Same commands for a developer, the release preflight, and a CR
  transition. Don't invent other gates.
- CR gates: the field is `transition_gate`, five cumulative rungs
  `prose|static|unit|integration|system`. Commands come
  ONLY from the committed `openbrain-client.yaml`, split `in_session`
  (run by the verb session) / `runner_owned` (run by the invoker after it
  exits). Never from a record, never from `openbrain-client.local.yaml`.
- The merge is the git operation only — it runs no gate and tears nothing
  down.
- All keel-dev output through keel/log, three sinks (console + `.logs/`
  human `.log` + `.jsonl`). Child output only via lineLogWriter. Handing
  os.Stdout to a subprocess fails lint.
- Tests hermetic. Adapters test against stubs. Live smokes are env-gated
  (`CODEXCLI_LIVE_SMOKE=1`, `CLAUDECLI_LIVE_SMOKE=1`) and skip in CI.
- Total-coverage floor is gate-enforced; the number lives in
  `cmd/keel-dev/coverage.go`.
- Error prefixes = keel package path (`keel/exec:`, `keel/exec/codex:`,
  `keel/exec/claude:`). Never bare `exec:` (stdlib collision).
