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
- `run-queue`/`run-queue-claude`: `--list` takes the full ref form
  `change_request-<n>`, not `cr-<n>`.
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

Small change? Still a CR. Docs-only? Still a CR (`transition_gate: prose`).

## Transition gates

The field is `transition_gate`. Its values are five cumulative evidence
classes — `prose` ⊂ `static` ⊂ `unit` ⊂ `integration` ⊂ `system`.

Commands are NEVER read from a record. Every invoker resolves them from the
committed `openbrain-client.yaml` at the repo root, under
`transition_gates.<rung>`, split into two execution moments:

- `in_session`   — run inside the dev/review verb session, before it writes its
                   status transition.
- `runner_owned` — run by the invoker after that session exits. A session must
                   NOT run these itself.

The invoker's stages run at three boundaries: `implementation_review` and
`ready_to_merge` over the unit's branch, `merged` over the integrated tree.
Everything is green before `ready_to_merge` is written — that is the promise.

keel binds both gates (`keel-dev ci` + `keel-dev vsix ci`) at every rung, because
`ac-288` (core-only changes skip the VSIX gate) is retired while keel is
pre-stability. `prose` and `static` carry an empty `runner_owned`. At `unit` and
above the indivisible `ci` sits in both lists and runs twice — a documented
deviation, removed once `keel-dev ci` grows rungs. See `keel/requirement-89`,
`keel/requirement-10`, and `openbrain/interface_spec-26`.

The merge is the git operation ONLY. It runs no gate and tears nothing down.

## Worktrees

`keel-dev worktree` is THE worktree lifecycle entry point for manual/operator
work, backed by the `keel/worktree` package — NOT raw `git worktree` or
`git checkout -b` on the primary checkout (that is blocked).

`openbrain-client worktree` now exists too (`up | down | resume | create-for |
enter | status | allow-merge | disallow-merge`) and is what the run-queue tail
uses for its own `cr-<seq>` worktrees. It is NOT a drop-in for `keel-dev
worktree`: it has no `branch-delete` and no `compare`, and keel's refusal
semantics live in `keel/worktree`.

Six leaves, and what each is for:

- `up`            — create the worktree from the local default branch (or a caller-supplied base), or reuse the one already there
- `resume`        — re-attach a worktree to a branch that already exists
- `down`          — remove the checkout, keeping the branch; refuses one that still holds work
- `branch-delete` — delete the branch once its checkout is gone; refuses an unmerged branch
- `status`        — read-only state report, for one checkout or for every one matching a pattern
- `compare`       — read-only branch-vs-base facts, no verdict

Usage strings and flags: see `keel-dev help worktree <leaf>` (and `keel-dev help
worktree` for the family and its exit codes). The binary is the source of truth
for argv; this list carries only intent, which generated help does not.

The `worktree-*.sh` wrappers are GONE. They lived at
`.claude/skills/change-request/scripts/` — inside a gold-provided skill, a path
the catalog owns and re-materializes — and the refreshed catalog deleted them
and now calls `openbrain-client worktree up cr <seq> <slug>` directly. Do not
restore them; do not patch gold-provided skills. Local skills (`merge/`) are
keel's to change.

This leaves `requirement-114`'s wrapper clauses (`ac-410`, `ac-412`, `ac-421`)
and two test files — `cmd/keel-dev/worktree_scripts_test.go`,
`cmd/keel-dev/worktree_wrapper_name_test.go` — pinning files that no longer
exist, so `keel-dev ci` is RED until that is retired under a CR. The
`no-shell-worktree-lifecycle` lint is unaffected: it skips absent scripts.

Never hand-create a `cr-<seq>` worktree — the run-queue tail owns those.

## The gate

- Run `go run ./cmd/keel-dev ci`. That is THE gate: gofmt, build, vet,
  in-process lint policies, tests with a total-coverage floor.
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
- `keel-dev test-bridge` verbs reserve stdout for protocol JSON/JSONL and route the
  keel/log console sink to stderr; the `.logs/` file sinks remain enabled. The
  VS Code bridge uses the in-repo fixture set only — no peer fixture-sync path.

## Tests

- Tests stay hermetic. Stub binaries for the codex/claude adapters.
  CI never needs a real codex or claude.
- Live smokes exist but are env-gated: `CODEXCLI_LIVE_SMOKE=1`,
  `CLAUDECLI_LIVE_SMOKE=1`. They always skip in CI.
- A total-coverage floor is enforced by the gate. The floor lives in
  `cmd/keel-dev/coverage.go` and is the only place it is stated; raise it
  only under a record.

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
