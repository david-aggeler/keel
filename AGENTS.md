# AGENTS.md

Read `CLAUDE.md` at the repo root — it is the single source of guidance for
all coding agents working in keel (module layout, gold dev records, the
transitional openbrain bridge, conventions).

Non-negotiables, restated for emphasis:

- **Change control is two-tier** (see "Change control" in CLAUDE.md):
  behavior/surface/contract changes go through a gold `change_request`
  approved *before* code; cosmetic/docs/test-only deltas go through the
  quick-change path (`issue` + `issue_fix`, no CR). Every change gets a
  record and a merge SHA. CRs close only after a literal AC-by-AC
  conformance check with evidence.
- **Gate:** `go run ./cmd/keel-dev ci` (gofmt, build, vet, in-process lint
  policies, test with an enforced total-coverage floor). CI and the release
  preflight run this same command — do not re-list checks elsewhere.
- **keel-dev output discipline:** all run output through keel/log (three
  sinks: console + daily human `.log` + `.jsonl` under `.logs/`); child
  process output only via the lineLogWriter; `os.Stdout`/`os.Stderr` outside
  the logger-construction allowlist fails lint (no-raw-stdout-stream).
- **Hermetic tests:** stub binaries for the codex/claude adapters; live
  smokes are env-gated (`CODEXCLI_LIVE_SMOKE=1`, `CLAUDECLI_LIVE_SMOKE=1`)
  and always skip in CI.
- **Public module:** never introduce GOPRIVATE/tokens/netrc/Docker secrets
  on any build path; one `go.mod`, one tag, zero external deps.
