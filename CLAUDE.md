# CLAUDE.md

Rules for coding agents in this repo. Short and blunt on purpose.

## What keel is

- ONE public Go module: `github.com/david-aggeler/keel` plus the Keel Test
  Bridge VSIX under `vsix/`. Apache-2.0.
- Subpackages: `log`, `exec`, `exec/claude`, `exec/codex`, `cmd/keel-dev`,
  plus approved dev/example binaries such as `cmd/keel-demo`.
- One tag. One version across the Go module and VSIX. Zero external deps in the keel/log + keel/exec
  core compile graph; log/otel is a quarantined sibling and may split to a
  separate module later. No internal replaces.
- Anonymous `go get` must always work. NEVER add GOPRIVATE, tokens,
  netrc, or Docker secrets to any build path. No exceptions.
- keel is a shared foundation for downstream consumers.

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

## Worktrees

Manage per-CR worktrees with the change-request skill's scripts in
`.claude/skills/change-request/scripts/` — NOT raw `git worktree` or
`git checkout -b` on the primary checkout (that is blocked). `openbrain-client`
has no worktree verb.

- `worktree-up.sh <kind> <seq> <slug>`     — new worktree on a fresh branch off main
- `worktree-down.sh <kind> <seq> <slug>`   — pre-merge teardown; refuses dirty, keeps the branch
- `worktree-resume.sh <kind> <seq> <slug>` — re-attach a worktree to an existing branch
- `worktree-status.sh <kind> <seq> <slug>` — read-only existence check

Manual/operator work only. The run-queue tail creates and owns its own
`cr-<seq>` worktrees — never hand-create those.

## The gate

- Run `go run ./cmd/keel-dev ci`. That is THE gate: gofmt, build, vet,
  in-process lint policies, tests with a total-coverage floor (85%).
- The local gate and the release preflight run the same command. Do not
  re-list checks anywhere else. keel runs no GitHub Actions CI.
- VSIX: `keel-dev vsix ci` is the Node-backed sibling gate for `vsix/`
  (pnpm build/lint/headless suite). Core `keel-dev ci` stays node-free.
- Release: `keel-dev release vX.Y.Z`. It refuses on dirty tree, existing
  tag, red core gate, or red VSIX gate. Then it stamps/builds the VSIX release
  asset, tags, creates the GitHub release with the VSIX attached, and runs the
  anonymous-fetch check.
- Doc: `docs/release.md`.

## keel-dev output rules

- ALL run output goes through keel/log. Three sinks always: console,
  daily human `.log`, daily `.jsonl` — both files under `<root>/.logs/`.
- Child process output only via `lineLogWriter`. NEVER hand `os.Stdout`
  or `os.Stderr` to a subprocess. The lint (no-raw-stdout-stream) will
  fail you.
- Every subprocess goes through keel/exec (START/END lifecycle logging).
- Verbs anchor at keel's module root. Refuse foreign modules.
- `keel-dev vscode` verbs reserve stdout for protocol JSON/JSONL and route the
  keel/log console sink to stderr; the `.logs/` file sinks remain enabled. The
  VS Code bridge uses the in-repo fixture set only — no peer fixture-sync path.

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
- keel-dev is the development/release CLI. Approved dev/example binaries may
  exist outside keel-dev when backed by SoR requirements. No SoR client code in
  keel — record ops use `openbrain-client` from PATH.
- The Keel Test Bridge VSIX activates only on `.vscode/test-bridge.json`.
  `testBridge.*` VS Code settings are intentionally not supported.
