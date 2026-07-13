---
dto_type: architecture_description
product: keel
title: Adapter invocation contract (VSIX ↔ devtool CLI)
summary: >-
  Chapter: the exact command-line wire the Keel Test Bridge VSIX emits against
  a consumer devtool, organized by interaction — discovery, planning,
  execution, demo block, config migration — each with its goal, trigger,
  literal argv, and answer contract; plus the unenforced cross-binary argv
  gap that let the run --format skew ship; draft.
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
consumer **devtool** it drives, organized by interaction: what each
interaction is *for*, when the VSIX fires it, the literal argv, and what a
conforming devtool must answer. This chapter owns the **agreement** (the
intersection both binaries must honor), not the shape of the JSON payloads
(that is the wire schemas, `keel/vscode/schemas/*.json`, drift-gated —
interface_spec §2) and not the lanes file (interface_spec §4). It deliberately
leaves protocol-document structure, the target tree, and lane semantics to
sibling chapter `-2`.

## Affected components

`Keel Test Bridge VSIX` (emitter; `vsix/src/bridgeAdapter.ts`,
`vsix/src/extension.ts`), `cmd/keel-dev` (the reference devtool / adapter;
`cmd/keel-dev/vscode.go`), `keel/vscode` (protocol library). Names per root §5.

## Quality goals served

- **Goal 4 (SSH-first interactive loop).** The adapter is a subprocess on the
  workspace host; the exact argv is the entire integration surface, so it must
  be pinned or the two independently-built binaries drift. This chapter exists
  because that drift shipped (see *Enforcement*).
- **Goal 5 (one tag, module + VSIX).** The VSIX and the reference devtool
  release under one tag precisely so the argv on both sides moves together;
  the contract below is what "moves together" means concretely.

## Topic narrative (concept chapter: the CLI wire, one interaction at a time)

### Common ground (holds for every interaction)

The VSIX carries **zero toolchain knowledge**. It reads
`.vscode/test-bridge.json` → `{ version, command, args, displayName, env }`
and drives the devtool by executing `command` as a subprocess. Ground rules:

- **cwd** is the workspace root on every invocation; **env** is
  `{...process.env, ...config.env}` (`bridgeAdapter.ts:178-180`).
- The configured `args` are a **prefix**, not the whole command — the VSIX
  appends a verb and flags per interaction. Default config: `command =
  bin/keel-dev` (workspace-relative; `keel-dev.exe` on Windows), `args =
  ["vscode","tests"]`, so invocations land as `bin/keel-dev vscode tests
  <verb> …` (`bridgeAdapter.ts:38-46`).
- **stdout is protocol, stderr is logging.** The VSIX parses stdout only and
  never derives semantics from stderr. On the keel-dev side this is enforced
  by routing the keel/log console sink to stderr plus the
  `no-raw-stdout-stream` lint.
- **`--format json` exists on read verbs only** (discover, plan) and is the
  only supported format. Run takes no `--format` — see interaction 3 and the
  Enforcement section for the incident this caused.
- Buffered calls (`execFile`) carry a Node `maxBuffer` ceiling; exceeding it
  kills the call and the user sees an opaque `maxBuffer exceeded` error.

### 1. Discovery — populate the tree

**Goal.** Give VS Code's Test Explorer its entire content. The devtool asserts
*everything* the user sees — groups, ordering (`sort_text`), lanes,
maintenance actions, click-to-source locations — and the VSIX only renders.
Discovery is how "per-consumer variation is data, not code" actually happens:
a different devtool answering this one verb produces a completely different
tree with zero extension changes.

**When it fires.** At activation, on the Test Explorer Refresh button, and on
watcher events (config file changes; planned: `test-lanes.json` writes
re-render the tree without a manual refresh). Stale responses are discarded
via a generation counter (`extension.ts`), so overlapping refreshes are safe.

**Invocation.**

```
command <args> discover --format json          # execFile, cwd=workspace root
```

**Devtool answer.** Exactly one discovery JSON document on stdout
(`version: 1`, `items[]` — schema `keel/vscode/schemas/`), exit 0, within a
**16 MiB** stdout ceiling. Read-only — discovery must never mutate workspace
state. keel-dev verb: `vscode tests discover [--format json]`.

**On failure.** Non-zero exit or unparseable stdout ⇒ the VSIX clears/keeps
the tree and reports to the output channel; the tree never renders a
half-parsed document.

### 2. Planning — show what the run needs before running

**Goal.** Before executing anything, tell the user what the selected run
requires and what the devtool would do about it: a devtool identity block, the
resolved run items, `required_resources`, **desired-state rows** (resource ×
current vs desired × action), preflight `checks`, `actions`, and the teardown
policy (`protocol.ts:47-108`). The plan is printed into the run output so an
SSH-remote user sees environment preparation instead of a silent hang. It is
the desired-state half of the contract: run = plan + execute.

**When it fires.** Immediately before **every** run (interaction 3), with the
same selection. Not user-invokable on its own.

**Invocation.** One `--id` per selected item (repeatable):

```
command <args> plan --format json --id <id> [--id <id> …]    # execFile
```

**Devtool answer.** Exactly one setup-plan JSON document (`version: 1`,
`items[]`), exit 0, ≤ **16 MiB**. Read-only — the plan *describes* actions; it
must not perform them. keel-dev verb:
`vscode tests plan [--format json] [--id test-id]…`.

**On failure.** A failed plan **aborts the run** — the VSIX reports the error
into the run output and never spawns interaction 3 (`extension.ts`).

### 3. Execution — run the selection, stream live results

**Goal.** Actually run what the user selected — lane, package, file, single
test, or a maintenance action (maintenance items are ordinary runnable items;
e.g. "clear local test state" and "detect lanes" run through this same verb) —
and stream progress back **live**, so a long lane shows per-test
pass/fail as it happens instead of one verdict at the end. This is the
interaction the whole bridge exists for.

**When it fires.** On every Run click in the Test Explorer, and internally for
maintenance items the discovery document advertises (e.g.
`capabilities.clear_state_test_ids`).

**Invocation.** One `--id` per selected item; **no `--format` flag** (the
output format is fixed: JSONL):

```
command <args> run --id <id> [--id <id> …]     # spawn, streaming
```

Spawned with `detached: true` on POSIX so cancellation can signal the whole
process **group** (`kill(-pid)`) — the adapter typically shells out (pnpm →
vitest workers → playwright browsers), and killing only the immediate child
would leave grandchildren holding ports and CPU (`bridgeAdapter.ts:118-131`).
Windows falls back to `child.kill(signal)`.

**Devtool answer.** A stream of run-event JSONL on stdout — `run_started`,
`test_started`, `output`, `passed`/`failed`/`errored`/`skipped`/`cancelled`,
`artifact` — until the terminal `run_finished` carrying the `exit_code`
(`protocol.ts:110-133`). Event ordering is scoped to leaf tests: a leaf's
events arrive after its `test_started`; lane/rollup events are exempt. Runs
serialize on the devtool's `run.lock`. No stdout ceiling (streaming), but
there is currently no cap on captured output either — `keel/issue-40`.
keel-dev verb: `vscode tests run --id test-id…`.

**On failure.** Adapter exit without `run_finished` ⇒ the VSIX closes the run
as errored; protocol failures surface as `errored` events plus a non-zero
`run_finished.exit_code` — never via stderr parsing.

### 4. Demo block — a showcase switch, not a real feature

**Goal.** Purely presentational (`keel/requirement-41`): let a presenter fake
a "blocked lane" so the Test Explorer's blocked-state rendering can be
demonstrated (screenshots, walkthroughs) without actually breaking anything.
The devtool persists a tiny bit of local state ("lane X is blocked"), and
discovery/run honor it until unblocked. No production behavior depends on it —
a devtool with no demo need may answer with an empty status object.

**When it fires.** Only from an explicit VS Code command (the demo-toggle
command). The VSIX first polls status, then toggles: if a lane is blocked it
unblocks; otherwise it blocks the hardcoded showcase lane
`keel::lane::test-fast` (`extension.ts`).

**Invocation.** These verbs live in a different command family (`demo`, not
`tests`), so the VSIX performs **args surgery**: it takes the configured
`args`, replaces the *last* `tests` token with `demo` (appends `demo` if no
`tests` token exists), then appends the verb (`bridgeAdapter.ts:182-195`):

```
command <demo-args> status              # execFile, ≤ 1 MiB — poll current state
command <demo-args> block <lane-id>     # execFile, ≤ 1 MiB — persist a fake block
command <demo-args> unblock             # execFile, ≤ 1 MiB — clear it
```

With the default config this lands as `bin/keel-dev vscode demo status` etc.

**Devtool answer.** `status` returns one JSON object
(`{ blocked_lane?, source, path }`); `block`/`unblock` are consumed for side
effect. keel-dev verbs: `vscode demo status|block <lane-id>|unblock`.

**Conformance note.** A devtool whose configured args do *not* end in a
`tests`-style token still receives `demo` appended and must tolerate the
shape.

### 5. Config migration — keep `.vscode/test-bridge.json` current, hands-free

**Goal.** The config file that wires the whole bridge (`command`, `args`, …)
has its own schema version (currently 2). When the VSIX ships a new config
schema, existing workspaces must not silently break or require hand-editing:
the devtool owns the migration logic (`config upgrade` rewrites the file
in place), and the VSIX triggers it automatically so the user never has to
know a migration happened.

**When it fires.** At activation: the VSIX reads the config; if
`version < currentConfigVersion` (2), it invokes the upgrade and surfaces the
devtool's stdout/stderr in the output channel plus an information message
(`extension.ts` `migrateWorkspaceConfig`). Unreadable config ⇒ no migration
attempt. Users can also run the same verb by hand.

**Invocation.** The one sanctioned exception to the args-prefix rule — the
verb path is **hardcoded**, ignoring the configured `args` entirely (they may
be exactly what the migration needs to fix):

```
command vscode config upgrade           # execFile, ≤ 1 MiB
```

**Devtool answer.** Rewrite the config to the current schema; free-form
stdout/stderr (both shown to the user). A devtool with a different verb layout
must still answer this exact invocation — or ship a current-version config so
it never fires. keel-dev verbs: `vscode config upgrade` (and `config init`,
which is human/CLI-only and never adapter-emitted).

### What is deliberately NOT on this wire

- `vscode config init` — human/CLI bootstrap verb.
- `vscode lanes list|detect` — devtool CLI verbs; "detect lanes" reaches the
  tree as maintenance item `a.1` and executes through interaction 3
  (`run --id keel::maintenance::detect-lanes`), never as a direct `lanes`
  invocation.
- Any VS Code settings surface — `testBridge.*` settings are intentionally
  unsupported; the config file is the only knob.

### Enforcement

- **Devtool side (gated).** `cmd/keel-dev/vscode_test.go` exercises each
  handler including `--format` acceptance on discover/plan/lanes and rejection
  on `run` (`parseVSCodeIDs`) and unsupported formats
  (`rejectUnsupportedFormat`, `parseVSCodeLanesDetectArgs`). Wire payloads are
  pinned by `wire_stability_test.go` + `schema_drift_test.go`.
- **VSIX side (gated).** The headless suite drives `bridgeAdapter.ts` against
  an in-repo `fake-adapter.js`; `keel-dev vsix ci` is the sibling gate.
- **The gap (UNENFORCED — the reason this chapter exists).** No test asserts
  that the argv the VSIX *emits* equals the flag set the devtool *accepts*
  across the binary boundary. Each side's suite passes independently while the
  wire drifts: the devtool test proves `run` rejects `--format`; the VSIX test
  proves its fake adapter tolerates whatever the VSIX sends — nothing proves
  the two agree. **Incident (2026-07-12):** an installed VSIX predating the
  fix appended `--format json` to `run`; every Run click failed with
  `unknown flag "--format"` (seven failures, 22:14:38–22:15:02, in `.logs/`)
  while both gates were green — source and `out/` were already correct, but
  the packaged VSIX inside VS Code was stale. Closing the gap needs a
  cross-binary argv-contract check (the five interactions asserted against
  keel-dev's real parsers) **and** repackage-reinstall discipline keeping the
  installed VSIX in step with `out/`. *Tracking record pending — surfaced
  2026-07-12; sibling to `keel/issue-40`. File the issue + CR before relying
  on this being caught.*

## Linked decisions

Pending extraction into design_decision records: protocol-over-subprocess (no
VS Code settings surface; `testBridge.*` intentionally unsupported); one-tag
module+VSIX versioning (the reason both argv sides must move together);
config-args-as-prefix with a hardcoded migration escape hatch. Deciding
dialogue: `keel/exploration-2` (concluded 2026-07-12). Normatively carried by
`keel/requirement-40` (config), `-41` (demo), `-42` (run + upgrade).
