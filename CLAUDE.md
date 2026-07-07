# CLAUDE.md

Rules for coding agents in this repo. Short and blunt on purpose.

## What keel is

- ONE public Go module: `github.com/david-aggeler/keel`. Apache-2.0.
- Subpackages: `log`, `exec`, `exec/claude`, `exec/codex`, `cmd/keel-dev`.
- One tag. One version. Zero external deps. No internal replaces.
- Anonymous `go get` must always work. NEVER add GOPRIVATE, tokens,
  netrc, or Docker secrets to any build path. No exceptions.
- keel is the shared foundation for openbrain and vela.

## Where records live

- ALL dev records (requirements, ACs, CRs, issues, epics, plan) live in
  gold, product `keel`. Use `mcp__gold__*` tools or `openbrain-client`.
- NEVER write dev records as local markdown files.
- Search before creating. Every call takes `product: "keel"`.
- Plan: `keel/dd_plan-1`. Epic: `keel/epic-1`.

## Change control

EVERY code change goes through a CR. No quick-change path. No silent
fixes. Owner-decided 2026-07-07.

The loop, always:

1. File records first. Defect or gap → `issue`. Then a `change_request`
   (parent `keel/epic-1`, plan `keel/dd_plan-1` where it fits).
2. Get the CR approved BEFORE writing code.
3. Implement. Gate green (`keel-dev ci`).
4. Before closing: check the diff against EVERY acceptance criterion,
   one by one, write down the evidence. Green gate alone is NOT proof.
   (CR-2 got reopened for this. Don't repeat it.)
5. Close the CR with the merge SHA. If an issue drove it, close the
   issue via an `issue_fix` that references the CR and the SHA.

Small change? Still a CR. Docs-only? Still a CR (merge_gate: docs).

## The gate

- Run `go run ./cmd/keel-dev ci`. That is THE gate: gofmt, build, vet,
  in-process lint policies, tests with a total-coverage floor (85%).
- CI and the release preflight run the same command. Do not re-list
  checks anywhere else.
- Release: `keel-dev release vX.Y.Z`. It refuses on dirty tree, existing
  tag, or red gate. Then tag → gh release → anonymous-fetch check.
- Doc: `docs/release.md`.

## keel-dev output rules

- ALL run output goes through keel/log. Three sinks always: console,
  daily human `.log`, daily `.jsonl` — both files under `<root>/.logs/`.
- Child process output only via `lineLogWriter`. NEVER hand `os.Stdout`
  or `os.Stderr` to a subprocess. The lint (no-raw-stdout-stream) will
  fail you.
- Every subprocess goes through keel/exec (START/END lifecycle logging).
- Verbs anchor at keel's module root. Refuse foreign modules.

## Tests

- Tests stay hermetic. Stub binaries for the codex/claude adapters.
  CI never needs a real codex or claude.
- Live smokes exist but are env-gated: `CODEXCLI_LIVE_SMOKE=1`,
  `CLAUDECLI_LIVE_SMOKE=1`. They always skip in CI.
- Coverage floor is 85% total, enforced by the gate. Target ~90%.
  Raise the constant in `cmd/keel-dev/coverage.go` only under a record.

## Transitional bridge (until iteration 5)

- openbrain's `go.work` has `use /projects/keel`. openbrain's gates also
  compile and test keel. keel repo is source of truth for the moved code.
- openbrain's old `pkg/logging`, `pkg/procexec`, `pkg/claudecli`,
  `pkg/codexcli` stay until iteration 4, then die. Bridge exits in
  iteration 5. See `keel/dd_plan-1`.

## Code conventions

- Code was MOVED from openbrain `pkg/`. Keep the move diff readable in
  git history; don't reformat moved code without cause.
- Error prefixes are the keel package path: `keel/exec:`,
  `keel/exec/codex:`, `keel/exec/claude:`. (Normalized under CR-6; the
  old openbrain names `procexec:`/`codexcli:`/`claudecli:` are gone.
  Never use bare `exec:` — that is stdlib os/exec's prefix.)
- Import aliases are deliberate: `logging "…/keel/log"`,
  `procexec "…/keel/exec"`. They avoid stdlib collisions. Keep them.
- `DHF-REQ:` / `DHF-TEST:` comments point at gold records. keel-owned
  refs use `keel/requirement-N`; consumer obligations keep
  `openbrain/...` refs.
- keel ships exactly ONE CLI: keel-dev. No SoR client code in keel —
  record ops use `openbrain-client` from PATH.
