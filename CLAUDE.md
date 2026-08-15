# CLAUDE.md

Rules for coding agents in this repo. Short and blunt on purpose.

## Conceptional

- **Use concise language**
- **Use software engineering language.** Name things with the vocabulary of the discipline — interface, boundary, contract, coupling, invariant, call path. Do not reach into another domain
- **Prefer long term benefits over short term win.**  
- **Observability is key. No feedback loop without logs.**
- **Prefer strict data handling.**
- **Prefer git operations over raw operations.**
- **Prefer keel-dev operations over just operations.**
- **Prefer just operations over raw operations.**
- **Testability matters.** We want to categorize code classes, so the most relevant test categories can be executed
- **Expect parallel work to be ongoing.**. It's mostly using worktrees

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

## Keel Development Documents — Use Gold

Keel's vision, change_request, issue, issue_fix, exploration, test_strategy, epic, story, formal_review, and every other HELIX01 type are owned by the **gold** instance (product `keel`) and authored only through `mcp__gold__*` — `create_<type>` / `update_<type>` / `draft_<type>`, `catalog_*` for the template and skill roots, and the product-plane tools (`admin_create_product`, `admin_create_product_version`, …) for products and versions. Never as local markdown. Every call takes `product: "keel"`, IDs are allocated server-side (never hand-pick a sequence number), and record status is live data — query it at time of use instead of caching it locally.

**Scratchpad:** put working files (drafts, one-off scripts, intermediate outputs the user may want to read) in `/projects/keel/scratchpad/` — it is gitignored and survives the session. Session-private tmp dirs are fine only for files the user never needs to open.

## Change control

EVERY code change goes through a CR. No silent fixes.

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

The merge is the git operation ONLY. It runs no gate and tears nothing down.

## The gate

- Run `go run ./cmd/keel-dev ci`. That is THE gate: gofmt, build, vet,
  in-process lint policies, tests with a total-coverage floor.
- The local gate and the release preflight run the same command. Do not
  re-list checks anywhere else. keel runs no GitHub Actions CI.
- Gate tools are resolved per pinned version, not from PATH: each
  `tools.pins` entry declaring `install.method: go` resolves from
  `${KEEL_DEV_TOOL_CACHE:-~/.cache/keel-dev/tools}/<tool>/<version>/<tool>`
  and is installed on demand, so worktrees pinning different versions are
  each gateable on one host. `install.method: path` tools (cspell,
  shellcheck) stay host-global via `scripts/setup_user.sh`.
- VSIX: `keel-dev vsix ci` is the Node-backed sibling gate for `vsix/`
  (pnpm build/lint/headless suite). Core `keel-dev ci` stays node-free.
- Release: `keel-dev release vX.Y.Z`. It refuses on dirty tree, existing
  tag, red core gate, or red VSIX gate. Then it stamps/builds the VSIX release
  asset, tags, creates the GitHub release with the VSIX attached, and runs the
  anonymous-fetch check.
- Doc: `docs/release.md`.

## Command surfaces — ask the tool, not this file

Three command surfaces, all self-describing. This file and the SoR name
commands only by intent; argv, verbs, flags, and exit codes come from
generated help at time of use. Prose copies drift — the binary is the
source of truth.

- `keel-dev` — `go run ./cmd/keel-dev --help-json` for the full command
  tree as JSON; `help <command>` for one topic.
- `just` — `just --list` for the recipe catalog. Recipes wrap canonical
  commands, they never re-define them.
- `openbrain-client` (from PATH) — `openbrain-client --help-json` for
  record ops, run-queue, and worktree lifecycle; `<command> --help` for
  one topic.

Agents run both CLIs with `--mode ai` (sparse AI-readable records);
`human` is for operators, `json` for log consumers.

If generated help is unclear, fix the help under a CR — do not
compensate here.

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
