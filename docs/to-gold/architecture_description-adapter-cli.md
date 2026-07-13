---
dto_type: architecture_description
product: keel
title: Adapter invocation contract (VSIX ↔ devtool CLI)
summary: >-
  Chapter: the exact command-line wire the Keel Test Bridge VSIX emits against
  a consumer devtool, organized by interaction — discovery, planning,
  execution, demo block, config migration — each with its goal, trigger,
  literal argv, and answer contract; the unenforced cross-binary argv gap
  that let the run --format skew ship; and the agreed target design (owner,
  2026-07-13): a hardcoded canonical `test-bridge` token with launcher-only
  args (config v3), records pending; draft.
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

### Target design — hardcoded canonical token (agreed 2026-07-13, records pending)

The args-as-protocol-prefix scheme above conflates two jobs in one config
field: *how to launch the devtool* and *where the protocol subtree lives*.
That conflation is what forces the demo args-surgery (interaction 4) and the
hardcoded config-upgrade exception (interaction 5), and it makes the argv
contract user data instead of a constant — the root enabler of the
Enforcement gap below. Owner-decided target design (2026-07-13 dialogue; no
issue/CR filed yet — **file records before implementing**):

- The VSIX hardcodes the canonical protocol token **`test-bridge`**
  (consistent vocabulary with the extension name "Keel Test Bridge" and the
  config file `test-bridge.json`). Every invocation becomes:

  ```
  command <launcher-args> test-bridge tests discover --format json
  command <launcher-args> test-bridge tests desired-state --format json --id <id>…
  command <launcher-args> test-bridge tests run --id <id>…
  command <launcher-args> test-bridge demo status|block <lane-id>|unblock
  command <launcher-args> test-bridge config upgrade
  ```

- Config `args` becomes **launcher-only** — config schema **v3**, migrated by
  the existing `config upgrade` machinery (v2→v3 strips a trailing
  `vscode tests` from `args`).

  `<launcher-args>` = the config's `args` field: the extra tokens some
  devtools need just to **start the process**, before any protocol word. The
  invocation is assembled as
  `command + args (launcher) + test-bridge <family> <verb> <flags>`.
  For most consumers it is empty:

  | Devtool situation | `command` | `args` | Actual invocation |
  |---|---|---|---|
  | Compiled binary (keel today) | `bin/keel-dev` | `[]` | `bin/keel-dev test-bridge tests discover --format json` |
  | Run from Go source, no build step | `go` | `["run", "./cmd/keel-dev"]` | `go run ./cmd/keel-dev test-bridge tests discover --format json` |
  | Node-based devtool | `node` | `["dist/devtool.js"]` | `node dist/devtool.js test-bridge tests run --id x` |
  | Version-manager wrapper | `mise` | `["exec", "--", "devtool"]` | `mise exec -- devtool test-bridge demo status` |

  The point of the split: today `args = ["vscode", "tests"]` mixes two
  concerns — *how to launch* the tool and *where the protocol subtree lives*.
  Launching stays configurable (it genuinely varies per consumer); the
  protocol path becomes a hardcoded constant the VSIX owns. `args` keeps only
  the launch half — hence "launcher-args". It exists so a devtool that cannot
  be invoked as a single executable is not locked out.
- **The `plan` verb renames to `desired-state`** (owner, 2026-07-13) —
  aligning the wire with content family (c): the verb *is* the desired-state
  report for a selection; "plan" was the vaguest name on the wire. Rides the
  same breaking change since the token/config break already forces lockstep.
- **Aliases are the devtool's business.** The contract pins only the canonical
  spelling the VSIX emits; keel-dev keeps `vscode` (or any shorter name) as a
  human-facing alias for the same subtree, and may alias `plan` →
  `desired-state` likewise. An alias never appears on the wire, so it cannot
  drift it.
- Consequences: the demo args-surgery and the upgrade exception are **removed
  wholesale** — every verb follows the same one rule; and the full argv tail
  becomes a constant, making the missing cross-binary contract test trivial
  (a literal table both suites assert against).

**Candidate (idea stage, 2026-07-13 dialogue — no decision yet):** a
**Desired state** tree group surfacing family (c) in the Explorer — one item
per resource showing which is currently active/ready (the plan's `current`
column at discovery time). Placement: **above Lanes** (owner) — the group
takes letter `b.`, Lanes shifts into the previously-unassigned `c.`; the
renumber is free by design, since ordinals live only in labels + `sort_text`
and never in item ids, so no results are invalidated:

```
a. Maintenance
b. Desired state     ← new group
c. Lanes             ← consumes the reserved gap letter
d. Frameworks
```

Needs **zero VSIX changes** (a new discovery group + status in
labels/descriptions is pure data), and each resource item could be made
runnable so clicking it executes its reconciling action through ordinary
interaction 3. Open question before it graduates to a record: probe cost at
discovery time (discovery runs on activation/refresh/watch, so probes must be
fast or cached with a staleness marker).

Until those records land, the sections below describe the **current** wire;
target-design deltas are flagged inline.

### What the devtool asserts — the four content families

Everything the bridge shows or prepares is asserted by the devtool; the VSIX
renders. Four content families, each arriving on a specific interaction
(exact shapes: `keel/vscode/schemas/`; tree semantics: sibling chapter `-2`).
*Note: these family letters are this chapter's outline, **not** the tree's
group letters (`a. Maintenance`, `b. Lanes`, `d. Frameworks` — display
ordinals decided in keel/exploration-2).*

- **(a) The test trees — discovery proper** *(arrives: discovery,
  interaction 1)*. The reason a test explorer exists: the real test
  hierarchies of **whatever frameworks the consumer's workspace actually
  contains**. The protocol mandates no framework — `framework` is a plain
  string label on items. keel's workspace has two testable codebases, so its
  tree has exactly two subtrees under `d. Frameworks`: Go as
  package → file → test (go/parser; `uri` + `range` for click-to-source), and
  Mocha for the VSIX's own headless suite under `vsix/` (per-file members). A
  pure-Go consumer ships only a Go subtree; a Python consumer ships pytest.
- **(b) Consumer-specific maintenance** *(arrives: discovery, interaction 1;
  invoked: run, interaction 3)*. Runnable operational actions the devtool
  chooses to advertise — for keel: detect lanes, unlock test bridge, clear
  results, clear local test state. Discovered so that even recovery actions
  are data, not VSIX code: another consumer ships a different action set with
  zero extension changes. The capabilities id-lists (below) mark which of
  these opaque actions carry bridge-visible side effects.
- **(c) Desired state** *(arrives: plan, interaction 2; reconciled: inside
  run, interaction 3)*. The test-infrastructure contract for a concrete
  selection: whatever must exist before its tests can be honest — a Docker
  environment up, a database present *and seeded with fixture data*,
  background services a, b, c, toolchains — expressed declaratively as
  resource rows (desired vs live-probed current → the reconciling action),
  with ownership flags deciding teardown: an *owned temporary* resource the
  run spun up is torn down afterwards; a *shared reusable* one (a dev DB kept
  warm across runs) is left standing. Per-selection by nature, so it
  cannot live in the discovery document; the devtool computes it fresh for
  every plan call and establishes it itself while executing the run.
  **Visibility:** every Run click shows it — the plan for the clicked
  selection is printed at the top of that run's output, before the first
  event streams. There is no tree-side preview: desired state is never a
  node, and plan is not user-invokable on its own. Detail: interaction 2.
- **(d) The lanes** *(arrive: discovery, interaction 1; defined (planned) in
  `.vscode/test-lanes.json`)*. The aggregation targets: system lanes compiled
  into the devtool (lint, test-fast, test-coverage, vsix-ci, ci) plus
  (planned) file lanes from the lanes file. Each lane brings its covers
  subtree (alias items via `canonical_id`) and its measured last-run
  duration — the gate-sizing dataset.

### 1. Discovery — populate the tree

**Goal.** Give VS Code's Test Explorer its entire content. The devtool asserts
*everything* the user sees — groups, ordering (`sort_text`), lanes,
maintenance actions, click-to-source locations — and the VSIX only renders.
Discovery is how "per-consumer variation is data, not code" actually happens:
a different devtool answering this one verb produces a completely different
tree with zero extension changes. Discovery delivers content families (a),
(b), and (d); family (c) is plan-time.

Around that content, the document carries its **envelope and mechanics**:

- **Identity** — workspace name, module path, generation timestamp, document
  `version`.
- **Capabilities handshake** — the devtool declares which optional semantics
  it supports, so the VSIX enables behavior from data instead of assuming it.
  The two id-list keys are the working core: they name *which maintenance
  items* (from content family b) carry bridge-visible side effects, letting
  the VSIX trigger consumer-defined actions it has no knowledge of:

  | Key | The devtool declares | What the VSIX does with it |
  |---|---|---|
  | `clear_results_test_ids` | "when one of these maintenance items passes, everything you are displaying is stale" | on a `passed` event for one of these ids, invalidates **all** shown results (`shouldInvalidateResultsForEvent`) — so keel's *clear test results* action wipes the Explorer state the moment the adapter confirms it |
  | `clear_state_test_ids` | "these maintenance items clear my local test state" | the *Clear Local State* command runs exactly these ids via interaction 3; a devtool that advertises none makes the command fail with a visible "does not advertise a clear-state maintenance item" error — never a silent no-op |
  | `clear_results` (bool) | a clear-results action exists | **declared, not read** — the id list above is what drives behavior |
  | `refresh_invalidates_results` (bool) | results should be treated as stale on refresh | **declared, not read** — the VSIX invalidates on every refresh unconditionally (`refresh()` calls `invalidateTestResults()` first) |
  | `neutral_parent_rollups` (bool) | parent/group items carry no verdict of their own — results live on leaves and neutral parents must not inherit failure | **declared, not read** |

  The three booleans are honest-signal declarations reserved for gating: they
  let a future VSIX condition these behaviors per devtool without a protocol
  version bump. Until then the VSIX behaves unconditionally — a devtool
  should still declare them truthfully (keel-dev declares all three `true`).
- **Per-item render/run metadata** — stable `id` (ordinals live in labels +
  `sort_text` only, never in ids, so renumbering never invalidates results),
  `parent_id`, `label`, `sort_text` (VS Code has no sort concept — order is
  data), `kind`, `runnable` + `profiles` (run/debug/coverage), `lane_id`,
  `canonical_id` (alias → canonical result mirroring), `required_resources`
  (rendered as tags), `limitations` (rendered as description).

The devtool discovers all of this **fresh on every invocation** — the VSIX
caches nothing across refreshes (a generation counter discards stale
responses), so the tree is always a pure function of the workspace state.

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

**Responsibility split.** The entire matrix — which selection requires which
resources, what state each must be in, what its *current* state is, which
action would reconcile the two, and who owns/tears down what
(`owned`/`reusable`) — is computed **devtool-side**. This is the
zero-toolchain-knowledge rule applied to environments: the VSIX cannot know
that keel's `vsix-ci` lane needs `go-toolchain` + `keel-module-root` +
`stub-binaries`, and the `current` column comes from live devtool probes at
plan time (keel-dev answers `"go is on PATH"` by checking, not from static
data). The VSIX renders the plan verbatim into the run output — it performs
no checks, takes no actions, and owns no resources. Consequence: a wrong or
stale plan is always a devtool bug, and the VSIX's only planning decision is
abort-on-failure (below).

**What the VSIX needs from the plan: one bit.** Did the plan call succeed?
That is the entire semantic consumption — everything else is passed through
to the human. Desired-state *reconciliation* is not a separate interaction:
the devtool establishes whatever the selection needs **inside interaction 3**
as part of executing it. The plan exists because that reconciliation is
invisible from the outside — over Remote-SSH, a lane spending 40 seconds
preparing its environment is indistinguishable from a hang unless the user
was first shown what preparation was coming. Plan = preview for the human;
run = reconcile + execute, devtool-owned end to end.

**What desired-state is for — provisioning, not just preflight.** The
document's shape gives away its ambition: `kind` per resource, `reusable` vs
`owned`, a teardown policy splitting `owned_temporary_resources` from
`shared_reusable_resources` — those fields exist so a lane can declare *spin
up the Docker environment, ensure the DB exists and carries its fixture
data, start services a, b, c*, and the devtool reconciles current reality to
that declaration inside the run, then tears down what the run owned and
leaves shared infrastructure warm. keel's own rows are deliberately boring
(`go-toolchain`, `keel-module-root`, `stub-binaries`) because keel's tests
are hermetic by policy — the protocol is sized for consumer devtools whose
integration lanes need real provisioned environments.

**When it fires.** Immediately before **every** run (interaction 3), with the
same selection. Not user-invokable on its own.

**Invocation.** One `--id` per selected item (repeatable):

```
command <args> plan --format json --id <id> [--id <id> …]    # execFile
```

**Devtool answer.** Exactly one setup-plan JSON document (`version: 1`,
`items[]`), exit 0, ≤ **16 MiB**. Read-only — the plan *describes* actions; it
must not perform them. keel-dev verb:
`vscode tests plan [--format json] [--id test-id]…`. *Target design: the verb
renames to `desired-state` (aligning the wire with family (c)); `plan` may
survive as a devtool alias.*

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
`capabilities.clear_state_test_ids`). A Run click is therefore exactly **two**
devtool processes: the plan (interaction 2, buffered) then the run
(streaming) — discovery is not re-run on click; the tree already exists.

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
shape. *Target design: the surgery is removed — the demo family is addressed
directly as `test-bridge demo <verb>`.*

### 5. Config migration — keep `.vscode/test-bridge.json` current, hands-free

**Goal.** The config file solves the **bootstrap problem**. The VSIX knows
nothing by design, so `.vscode/test-bridge.json` must answer the two
questions that precede every other interaction: *does this workspace
participate?* (the VSIX activates **only** on the file's presence — no file,
dormant extension; opt-in is a fact of the repo) and *where is the devtool?*
(`command` + launcher `args` + `env`, plus `displayName` for UI branding —
the pointer every subsequent interaction dereferences). It is deliberately a
**repo-owned file, not VS Code settings** (`testBridge.*` intentionally
unsupported): the pointer is a property of the workspace, not of a user's
editor profile — checked in, identical for every teammate and every
Remote-SSH session with zero per-machine setup, and writable by the devtool
itself. Same philosophy as the lanes file and go.mod: a repo file, both
human- and tool-writable.

The file has its own schema version (currently 2), and the wire interaction
here is the maintenance half of "the bootstrap file must never rot": when the
VSIX ships a new config schema, existing workspaces must not silently break
or require hand-editing. The devtool owns the migration logic
(`config upgrade` rewrites the file in place); the VSIX triggers it
automatically so the user never has to know a migration happened. The
bootstrap half, `config init` (scaffold the file), is human/CLI-only and
never appears on the wire.

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

*Target design: no longer an exception — `test-bridge config upgrade` follows
the same rule as every other verb (launcher args retained, since they are not
part of the protocol path and may be required to launch the devtool at all).*

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
  installed VSIX in step with `out/`. The target design (hardcoded
  `test-bridge` token, launcher-only args) is a precondition worth taking
  first: it turns the argv tail into a constant, so the contract test reduces
  to a literal table. *Tracking record pending — surfaced 2026-07-12; sibling
  to `keel/issue-40`. File the issue + CR before relying on this being
  caught.*

## Linked decisions

Pending extraction into design_decision records: protocol-over-subprocess (no
VS Code settings surface; `testBridge.*` intentionally unsupported); one-tag
module+VSIX versioning (the reason both argv sides must move together);
config-args-as-prefix with a hardcoded migration escape hatch (current state —
superseded in intent by the next item); **canonical `test-bridge` token with
launcher-only args and devtool-owned aliases** (owner-decided 2026-07-13,
this dialogue — design_decision + issue + CR all pending). Deciding dialogue:
`keel/exploration-2` (concluded 2026-07-12). Normatively carried by
`keel/requirement-40` (config), `-41` (demo), `-42` (run + upgrade).
