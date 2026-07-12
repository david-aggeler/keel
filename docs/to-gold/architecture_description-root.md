---
dto_type: architecture_description
product: keel
title: Keel Architecture
summary: >-
  Shared Go foundation (log/exec/cli/vscode) plus the Keel Test Bridge VSIX;
  #1 quality goal: anonymous go get always works — zero credentials in any
  build path; draft.
status: draft
chapters:
  - keel/architecture_description-2   # VS Code Test Bridge & lanes (to-gold sibling)
related:
  - keel/exploration-2
  - keel/prototype-1
---

# Architecture Description: Keel Architecture

## 1. Introduction & quality goals

**Purpose.** Keel is the shared foundation extracted from openbrain: one public
Go module (`github.com/david-aggeler/keel`) providing structured logging
(`log`), subprocess lifecycle management (`exec`) with CLI-agent adapters
(`exec/claude`, `exec/codex`), a small command framework (`cli`), and the
VS Code test-bridge protocol library (`vscode`) — plus the Keel Test Bridge
VSIX under `vsix/` that turns any consumer repo's devtool into a first-class
VS Code Test Explorer citizen. Downstream consumers today: vela and openbrain.

**Top quality goals** (prioritized):

| # | Quality goal (concrete) | Priority | Carried by |
|---|---|---|---|
| 1 | Anonymous `go get` must always work: no GOPRIVATE, tokens, netrc, or Docker secrets may ever enter any build path. A consumer on a clean machine fetches and builds keel with zero credentials. | highest | keel/requirement-8 |
| 2 | Zero external dependencies in the `keel/log` + `keel/exec` core compile graph; `log/otel` is a quarantined sibling that may split into its own module. Enforced by the `log-core-deps` gate step. | high | keel/requirement-22 |
| 3 | Tests stay hermetic: CI never needs a real `codex` or `claude` binary (stub binaries; live smokes env-gated and always skipped in CI). A total-coverage floor is gate-enforced (the number is a gate knob, not architecture — constant in `cmd/keel-dev/coverage.go`). | high | keel/requirement-4 (adapters); broader record pending |
| 4 | SSH-first interactive test loop: the bridge adapter is a subprocess on the workspace host, so the Test Explorer works identically over Remote-SSH; discovery and single-test runs complete in seconds, making the tree usable as a merge-gate-sizing instrument. | high | keel/requirement-49 (fast discovery), keel/requirement-51 + -53 (lanes as the sizing instrument); keel/exploration-2 (concluded) |
| 5 | One version across the Go module and the VSIX, one tag; `keel-dev release` refuses on dirty tree, existing tag, red core gate, or red VSIX gate, and verifies anonymous fetch post-release. | medium | keel/requirement-9 |

**Stakeholders.** Owner/maintainer (David) — sole approver of CRs and releases.
Coding agents — implement under change control; need unambiguous conventions.
Consumer products (vela, openbrain) — need additive-only API evolution.
VS Code users of consumer repos — need the Test Bridge to work over Remote-SSH
without local toolchains.

## 2. Constraints

| Constraint | Consequence for the design |
|---|---|
| ONE public module, one tag, version shared with the VSIX | No internal replaces; no sub-module versioning; release tooling stamps both artifacts from one tag |
| Anonymous-fetch policy (goal #1) | No private deps ever; supply-chain posture is "public by construction" |
| Solo owner + coding agents; every change via gold CR (owner-decided 2026-07-07) | Conventions must be machine-checkable (lint policies in the gate), not tribal |
| No GitHub Actions CI — the local gate (`keel-dev ci`) IS the gate | Gate must be fast, deterministic, and identical to release preflight |
| `keel-dev ci` stays node-free | VSIX gate (`keel-dev vsix ci`) is a separate Node-backed verb; core gate never spawns pnpm |
| Apache-2.0; code MOVED from openbrain `pkg/` | Keep move-diff readable; don't reformat moved code without cause |
| Records live in gold, not in-repo markdown | This document is staged under `docs/to-gold/` only until the DTO type exists |

## 3. Context & scope

Keel as a black box:

| Neighbor | In/out | What crosses the boundary | Interface detail |
|---|---|---|---|
| vela (consumer product) | ← | imports `keel/log`, `keel/exec` APIs; reuses the Test Bridge VSIX with its own devtool | interface_spec §2 rows 1–4 |
| openbrain (consumer product) | ←/→ | imports keel APIs; transitional `go.work use /projects/keel` bridge until iteration 5; keel dev records live in openbrain's gold instance (via `openbrain-client`, dev-time only — no SoR client code in keel) | interface_spec §2; CLAUDE.md bridge section |
| VS Code (+ Remote-SSH) | ←/→ | hosts the Keel Test Bridge VSIX; the VSIX spawns the workspace-configured adapter subprocess and consumes protocol JSON | interface_spec §2 row "bridge protocol"; chapter: Test Bridge |
| GitHub | → | module fetch (anonymous), releases with attached VSIX asset, tags | docs/release.md |
| Go toolchain | → | build/test/vet; discovery and runs shell out to `go` | interface_spec §2 consumed rows |
| Node + pnpm | → | VSIX build/lint/headless suite only (`keel-dev vsix ci`) | interface_spec §2 consumed rows |
| claude / codex CLIs | → | runtime subprocesses driven by `exec/claude`, `exec/codex`; stubbed in all tests, live smokes env-gated | interface_spec §4 |

Outside the boundary: the gold SoR itself (keel stores *records* there but ships
no client code), vela/openbrain internals, and any CI service.

## 4. Solution strategy

| Strategy | Serves goal/constraint (§1/§2) | Settled by |
|---|---|---|
| Library-first: consumers import packages; the only shipped binaries are keel-dev (dev/release CLI) and approved dev/example binaries | goals 1–2 | CLAUDE.md — design_decision pending |
| Zero-dep core with quarantined otel sibling, enforced by a gate step | goal 2 | keel/requirement-22 |
| All subprocess work through `keel/exec` (START/END lifecycle logging), all output through `keel/log` three-sink model (console + daily .log + daily .jsonl); lint `no-raw-stdout-stream` makes violations impossible | goals 3–4, constraint "machine-checkable conventions" | keel-dev output rules — design_decision pending |
| Protocol-over-subprocess bridge: the VSIX knows no toolchain; it execs the workspace devtool and speaks versioned JSON documents (discover/plan/run) validated by embedded JSON Schemas | goal 4 | chapter: Test Bridge; keel/exploration-2 |
| One local gate = release preflight; release verb refuses on any red and checks anonymous fetch | goals 1, 5 | docs/release.md |
| Hermetic adapters: stub binaries in tests, env-gated live smokes | goal 3 | CLAUDE.md tests section |

## 5. Component inventory

| Component | Kind | Technology | Responsibility | Talks to |
|---|---|---|---|---|
| keel/log | module pkg | Go, zero-dep | Structured logging: three-sink model, operational errors with redaction, build identity | consumers; keel/log/otel |
| keel/log/otel | module pkg (quarantined) | Go + OpenTelemetry deps | OTLP bridge for keel/log; only place otel deps are allowed | keel/log; OTLP endpoint (consumer-configured) |
| keel/exec | module pkg | Go, zero-dep | Subprocess lifecycle (START/END logging, output capture, line writers) | os/exec; keel/log |
| keel/exec/claude, keel/exec/codex | module pkg | Go | CLI-agent adapters over keel/exec; hermetic via stub binaries | claude/codex CLIs (runtime); keel/exec |
| keel/cli | module pkg | Go | Command-spec framework (verbs, flags, usage errors) used by keel-dev | cmd/keel-dev |
| keel/vscode | module pkg | Go | Bridge protocol library: wire types, embedded JSON Schemas, run-event projectors (go/vitest/playwright), stamper, engine, config init/upgrade | cmd/keel-dev; consumer devtools |
| cmd/keel-dev | binary | Go | Dev/release CLI: THE gate (`ci`), vsix gate, release, and the bridge adapter verbs (`vscode tests discover/plan/run`, `vscode config`, `vscode demo`) | keel/{cli,log,exec,vscode}; go toolchain; pnpm (vsix verbs only) |
| Keel Test Bridge VSIX | extension | TypeScript, VS Code API | Renders discovery as Test Explorer tree; runs selections through the adapter; mirrors external runs; Testing-menu commands | VS Code; adapter subprocess (`bin/keel-dev` per `.vscode/test-bridge.json`) |
| Go toolchain | external | — | build/test/vet; discovery + test execution | cmd/keel-dev |
| Node + pnpm | external | — | VSIX build/lint/headless tests | keel-dev vsix verbs |
| GitHub | external | — | module distribution, releases, tags | release verb; consumers |

## 6. Runtime view — key scenarios

**Happy path — run a test from the Test Explorer (over Remote-SSH).** User
clicks run → VSIX resolves `.vscode/test-bridge.json` → spawns
`bin/keel-dev vscode tests run --id go::test::log::TestX` on the workspace
host → adapter acquires `run.lock`, checks lane readiness (`PrepareLane`),
executes `go test -json`, projects events to canonical test ids, streams
run-event JSONL on stdout and mirrors it to `.devtools/vscode-runs/<id>.jsonl`
→ VSIX maps events onto tree items → `run_finished` releases the lock. Logs go
to stderr + `.logs/` sinks; stdout carries protocol only.

**Error path — stranded run lock.** Adapter crashes mid-run → `run.lock`
remains → next run fails fast with `errored` + `run_finished(1)` naming the
lock path → recovery today is manual file deletion; becomes maintenance item
"unlock test bridge" per the Test Bridge chapter. The tree shows the failure;
no state corruption (results derive from event streams, not the lock).

**Operations — release.** `keel-dev release vX.Y.Z` → refuses on dirty tree /
existing tag → runs core gate + VSIX gate (same commands as daily use) →
stamps and builds the VSIX asset → tags, creates the GitHub release with the
VSIX attached → verifies anonymous `go get` of the new tag. Any red aborts
before the tag exists.

## 7. Deployment view

No services. Keel runs in two places: (1) the developer workspace host (often
reached via Remote-SSH) — `bin/keel-dev` built from source, state under
`<root>/.logs/` (log sinks) and `<root>/.devtools/` (bridge run streams, run
lock, demo-block state); (2) the user's VS Code instance — the VSIX (UI side
only; all execution happens on the workspace host via the adapter subprocess).
Distribution: Go module proxy / GitHub for the module; GitHub release asset
for the VSIX. Per-environment differences: none by design — the SSH case IS
the primary case.

## 8. Cross-cutting concepts

- **Output discipline** — every run's output flows through keel/log (three
  sinks); child process output only via `lineLogWriter`; `vscode` verbs
  reserve stdout for protocol JSON and move the console sink to stderr.
  Enforced by the `no-raw-stdout-stream` lint. → Test Bridge chapter.
- **Subprocess lifecycle** — every spawn goes through keel/exec with START/END
  logging; no bare `os/exec` in product code.
- **Error identity** — error prefixes are package paths (`keel/exec:`,
  `keel/exec/codex:`); never bare `exec:` (stdlib's prefix). Redaction of
  root causes and string metadata in `log.OperationalError`.
- **Traceability** — `DHF-REQ:` / `DHF-TEST:` comments bind code to gold
  records; keel-owned refs use `keel/requirement-N`.
- **Verification** — one gate, `keel-dev ci`; the step list lives in
  `cmd/keel-dev/gates.go` and is deliberately not re-listed here (CLAUDE.md:
  "do not re-list checks anywhere else"). The coverage-floor constant lives
  in `cmd/keel-dev/coverage.go`.
- **Configuration** — workspace-owned JSON files under `.vscode/`
  (`test-bridge.json` now, `test-lanes.json` planned), hand-edited, schema'd,
  watched by the VSIX. No VS Code settings surface (`testBridge.*`
  intentionally unsupported).

## 9. Risks & technical debt

| Risk / debt | Consequence | Tracked by |
|---|---|---|
| Transitional go.work bridge: openbrain compiles keel from checkout until iteration 5 | consumer breakage invisible to keel's own gate | keel/dd_plan-1 (planned exit) |
| Discovery shells `go list` + `go test -list` per package (compiles the module) | slow refresh, weakens quality goal 4 | keel/change_request-54 (approved, iteration-10) |
| `keel.tests.clearLocalState` menu command always fails (adapter never advertises clear-state capabilities) | broken user-facing command | keel/issue-38 (reviewed); fix: keel/change_request-53 |
| Emitted discovery not validated against embedded schemas; `keel::root` uses out-of-enum kind `workspace` | silent wire drift | keel/change_request-53 (approved; requirement-46 AC pins schema validation) |
| VSIX toolchain (node_modules) is a large third-party surface outside the zero-dep policy | supply-chain exposure scoped to dev/VSIX builds | threat_model §4 |
| File-run selections silently degrade to package runs (`GoSelection.TestNames` never populated) | misattributed results | keel/issue-39 (reviewed); fix: keel/change_request-54 |
| Child-process output capture is unbounded — `MaxOutputBytes` is declared on the workspace profile but has zero callers; `keel/exec` buffers without a cap | memory abuse via chatty subprocess; a documented control that does not exist | keel/issue-40 (new — not yet shaped to a requirement) |

## 10. Chapter summaries

**VS Code Test Bridge & lanes** — the protocol-over-subprocess bridge between
the VSIX and consumer devtools: wire documents and schemas, the target
Test Explorer tree (lettered groups, Maintenance actions, aggregation lanes
with covers subtrees and measured cost), the planned lanes file with
lane-in-lane composition, and the keel-dev verb contract. Read when touching
`keel/vscode`, `cmd/keel-dev` vscode verbs, or the VSIX.

*Deliberately deferred chapters (vocabulary-closure note): `keel/log` +
`keel/log/otel` internals and `keel/exec` + adapter internals have no chapter
yet — a declared decision, not an oversight; they get chapters when their
first architecture-level question arrives. Until then those §5 components are
covered by root-level sections only.*

## 11. Linked decisions & glossary

- Decisions: pending extraction into design_decision records — candidates:
  library-first module shape; zero-dep core + otel quarantine; protocol-over-
  subprocess bridge; single-tag module+VSIX versioning; change control via
  gold CRs (owner-decided 2026-07-07). Interface-policy decisions live in the
  interface_spec.
- Glossary: no glossary_term records yet; candidate terms: lane, system lane,
  file lane, covers subtree, bridge adapter, gate.
