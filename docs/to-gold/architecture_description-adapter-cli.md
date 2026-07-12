---
dto_type: architecture_description
product: keel
title: Adapter invocation contract (VSIX ↔ devtool CLI)
summary: >-
  Chapter: the exact command-line wire the Keel Test Bridge VSIX emits against
  a consumer devtool — six invocation shapes, their flags, buffer ceilings and
  stdout/exit semantics, the conformance rules a devtool must answer, and the
  unenforced cross-binary argv gap that let the run --format skew ship; draft.
status: draft
related:
  - keel/architecture_description-1   # root (to-gold sibling)
  - keel/architecture_description-2   # VS Code Test Bridge & lanes (sibling chapter)
  - keel/exploration-2
  - keel/issue-40                     # sibling gap (unwired output cap); see also the argv-contract gap below
---

# Architecture Description: Adapter invocation contract (VSIX ↔ devtool CLI)

*Chapter of the keel architecture root (see `chapters[]` there). Concept
chapter — the exact CLI wire that the sibling chapter
`keel/architecture_description-2` (VS Code Test Bridge & lanes) describes only
at concept level. Where that chapter says "the VSIX executes the configured
adapter and speaks versioned JSON," this chapter pins the argv.*

## Scope

The exact command-line contract between the **Keel Test Bridge VSIX** and any
consumer **devtool** it drives: every process invocation the VSIX emits, the
literal argv (verb + flags), the execution mode (`execFile` vs streaming
`spawn`), the per-shape buffer ceiling and stdout/exit expectation, and the
conformance rules a devtool must satisfy to answer. This chapter owns the
**agreement** (the intersection both binaries must honor), not the shape of
the JSON payloads (that is the wire schemas, `keel/vscode/schemas/*.json`,
drift-gated — interface_spec §2) and not the lanes file (interface_spec §4).
It deliberately leaves protocol-document structure, the target tree, and lane
semantics to sibling chapter `-2`.

## Affected components

`Keel Test Bridge VSIX` (emitter; `vsix/src/bridgeAdapter.ts`), `cmd/keel-dev`
(the reference devtool / adapter; `cmd/keel-dev/vscode.go`), `keel/vscode`
(protocol library). Names per root §5.

## Quality goals served

- **Goal 4 (SSH-first interactive loop).** The adapter is a subprocess on the
  workspace host; the exact argv is the entire integration surface, so it must
  be pinned or the two independently-built binaries drift. This chapter exists
  because that drift shipped (see *Enforcement*).
- **Goal 5 (one tag, module + VSIX).** The VSIX and the reference devtool
  release under one tag precisely so the argv on both sides moves together;
  the contract below is what "moves together" means concretely.

## Topic narrative (concept chapter: the CLI wire end to end)

### The rule (normative)

The VSIX carries **zero toolchain knowledge**. It reads
`.vscode/test-bridge.json` → `{ command, args, displayName, env }` and drives
the devtool by executing `command` with the workspace root as **cwd** on every
shape. The configured `args` are a **prefix**; the VSIX appends a verb and
flags. A conforming devtool MUST answer all six invocation shapes below with
exactly the stdout/exit discipline stated. stderr is free-form logging on
every shape — the VSIX never parses it for semantics.

Default config (`bridgeAdapter.ts:38-46`): `command = bin/keel-dev` (resolved
relative to the workspace root; `keel-dev.exe` on Windows), `args =
["vscode","tests"]`. So the default discover/plan/run shapes land as
`bin/keel-dev vscode tests <verb> …`.

### The contract — invocation shapes the VSIX emits

`<A>` = the configured `args` prefix (default `vscode tests`). `<D>` = the
demo-surgery prefix (see rule D). Ceilings are the Node `maxBuffer` — exceeding
one kills the call and the user sees an opaque `maxBuffer exceeded`.

| # | Trigger | Literal argv appended after `command` | Mode | Ceiling | stdout expectation | Emit site |
|---|---|---|---|---|---|---|
| 1 | Discovery / refresh | `<A> discover --format json` | `execFile` | 16 MiB | one discovery JSON doc (`version:1`, `items[]`), exit 0 | `bridgeAdapter.ts:81` |
| 2 | Setup plan (per selection) | `<A> plan --format json (--id <id>)*` | `execFile` | 16 MiB | one setup-plan JSON doc (`version:1`, `items[]`), exit 0 | `bridgeAdapter.ts:95-98` |
| 3 | Run selected items | `<A> run (--id <id>)*` | `spawn` (streaming) | — | run-event **JSONL** streamed until terminal `run_finished`; exit code carried by `run_finished` | `bridgeAdapter.ts:112-132` |
| 4 | Demo block status poll | `<D> status` | `execFile` | 1 MiB | one `DemoBlockStatus` JSON object | `bridgeAdapter.ts:137` |
| 5 | Demo block / unblock | `<D> block <lane-id>` · `<D> unblock` | `execFile` | 1 MiB | (result object; consumed for side effect) | `bridgeAdapter.ts:146-154` |
| 6 | Config auto-migration | `vscode config upgrade` **(hardcoded)** | `execFile` | 1 MiB | free-form; `{stdout,stderr}` surfaced to the user | `bridgeAdapter.ts:159` |

Shape 3 uses `spawn` with `detached: true` on POSIX so a single
`process.kill(-pid)` reaches the whole `pnpm → vitest → playwright` subtree on
cancel; Windows falls back to `child.kill(signal)` (`bridgeAdapter.ts:118-131`).

### The other side — verbs the reference devtool answers

From `keel-dev vscode --help` (generated from `cli.CommandSpec` in
`cmd/keel-dev/vscode.go` — the authoritative, drift-safe list):

| Verb | Accepted flags | Answers shape |
|---|---|---|
| `vscode tests discover` | `[--format json]` | 1 |
| `vscode tests plan` | `[--format json] [--id test-id]…` | 2 |
| `vscode tests run` | `--id test-id`… — **no `--format`** | 3 |
| `vscode demo status` | — | 4 |
| `vscode demo block <lane-id>` / `vscode demo unblock` | — | 5 |
| `vscode config upgrade` | — | 6 |
| `vscode config init` | — | (not adapter-emitted — human/CLI verb) |
| `vscode lanes list` / `vscode lanes detect` | `list [--format json]` · `detect [--format json] [--dry-run]` | (not adapter-emitted — see rule E) |

### Conformance rules (the load-bearing exactness)

- **A. `args` is a prefix, not the whole command.** The verb and flags are
  appended. A devtool advertising a different `args` must place its
  `discover|plan|run` verbs at the tail of that prefix.
- **B. `--format json` is emitted on discover and plan ONLY, and it is the
  only supported format.** It is **NOT** emitted on `run`, and the run
  argument parser (`parseVSCodeIDs`, `cmd/keel-dev/vscode.go`) **rejects**
  `--format` as an unknown flag. Emitting `run --format json` fails with
  `unknown flag "--format"` / `usage: keel-dev vscode tests run --id test-id`.
  This is exactly the **2026-07-12 stale-VSIX skew**: an installed VSIX
  predating the fix appended `--format json` to `run`, and every Run click
  failed against the current binary (seven failures, 22:14:38–22:15:02, in
  `.logs/`). Current source (`bridgeAdapter.ts:114`) and the compiled `out/`
  build both emit shape 3 correctly — the fix shipped in source but was not
  repackaged/reinstalled into the running VS Code.
- **C. `--id` is repeatable.** Multi-select is carried as repeated `--id`
  tokens on shapes 2 and 3, one per selected leaf id.
- **D. Demo verbs reach a different command family by args-surgery.** The VSIX
  takes the configured `args`, replaces the **last** `tests` token with `demo`
  (or appends `demo` if no `tests` token exists), then appends the demo verb
  (`bridgeAdapter.ts:182-195`). `<D>` in the table is that rewritten prefix. A
  devtool whose args do not end in a `tests`-style token still gets `demo`
  appended and must answer.
- **E. Config auto-migration ignores configured args entirely.** Shape 6 is
  the **hardcoded** path `vscode config upgrade`, fired at activation when the
  config `version` is below `currentConfigVersion` (2). A devtool with a
  different verb layout must still answer this exact invocation, or ship a
  current-version config so it never fires. `vscode config init` and
  `vscode lanes list|detect` are NOT part of the VSIX invocation contract —
  `init` is a human/CLI verb; `lanes detect` surfaces as maintenance tree item
  `a.1` and runs through shape 3 (`run --id keel::maintenance::detect-lanes`),
  not a direct `lanes` call.
- **F. stdout discipline.** Exactly one JSON document on shapes 1, 2, 4;
  JSONL stream on shape 3; free-form on shape 6. All protocol output routes
  through keel/log's file sinks with the console sink on **stderr** (CLAUDE.md
  keel-dev output rules), enforced by the `no-raw-stdout-stream` lint so
  stdout stays protocol-clean.
- **G. cwd is the workspace root** on every shape; `env` is
  `{...process.env, ...config.env}` (`bridgeAdapter.ts:178-180`).

### Enforcement

- **Devtool side (gated).** `cmd/keel-dev/vscode_test.go` exercises each
  handler including `--format` acceptance on discover/plan/lanes and rejection
  on `run` (`parseVSCodeIDs`) and unsupported formats (`rejectUnsupportedFormat`,
  `parseVSCodeLanesDetectArgs`). Wire payloads are pinned by
  `wire_stability_test.go` + `schema_drift_test.go`.
- **VSIX side (gated).** The headless suite drives `bridgeAdapter.ts` against
  an in-repo `fake-adapter.js`; `keel-dev vsix ci` is the sibling gate.
- **The gap (UNENFORCED — the reason this chapter exists).** No test asserts
  that the argv the VSIX *emits* equals the flag set the devtool *accepts*
  across the binary boundary. Each side's suite passes independently while the
  wire drifts: the devtool test proves `run` rejects `--format`; the VSIX test
  proves its fake adapter tolerates whatever the VSIX sends — nothing proves
  the two agree. That blind spot is what let the `run --format json` skew reach
  a user with every Run click failing and a green gate on both sides. Closing
  it needs a cross-binary argv-contract check (e.g. a table of the six shapes
  asserted against `keel-dev`'s real parsers) **and** the repackage-reinstall
  discipline that keeps the installed VSIX in step with `out/`. *Tracking
  record pending — surfaced 2026-07-12; sibling to `keel/issue-40` (unwired
  output cap). File the issue + CR before relying on this being caught.*

### Exceptions

- **Demo verbs** (shapes 4–5) exist purely to demo blocked lanes
  (`keel/requirement-41`); a devtool with no demo need may answer with an
  empty `DemoBlockStatus`.
- **The hardcoded upgrade path** (shape 6, rule E) is the sanctioned deviation
  from the args-prefix rule (`keel/requirement-42`).

## Linked decisions

Pending extraction into design_decision records: protocol-over-subprocess (no
VS Code settings surface; `testBridge.*` intentionally unsupported); one-tag
module+VSIX versioning (the reason both argv sides must move together);
config-args-as-prefix with a hardcoded migration escape hatch. Deciding
dialogue: `keel/exploration-2` (concluded 2026-07-12). Normatively carried by
`keel/requirement-40` (config), `-41` (demo), `-42` (run + upgrade).
