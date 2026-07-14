# Keel Test Lanes — Interface Specification

**Status:** rev 3, implemented by `keel/change_request-76`.
Rev 3 incorporates the owner decision from 2026-07-14: every discovery-served
lane is defined in `.vscode/test-lanes.json`; `detect-lanes` seeds the gate
lanes from keel-dev's gate knowledge plus workspace-derived category lanes.
Lane-duration attribution follows the reviewers'/assistant's recommendation —
the owner delegated that mechanism (see §6).
**Deciding records:** `keel/exploration-2` (dialogue), `keel/prototype-1`
(visual target).
**Scope:** the contract between the Keel Test Bridge VSIX and a consumer
devtool (keel-dev first), covering the lanes definition file, lane
composition, the new CLI verbs, discovery projection, run semantics, and cost
measurement.
**Non-goals:** VS Code tasks integration (rejected), tree-driven editing of
any config file other than the devtool-owned lanes file written by
`detect-lanes` maintenance, CR-class → merge-gate mapping (a later change-control policy
that *consumes* this interface).

---

## 1. Terms

| Term | Meaning |
|---|---|
| **Lane** | A runnable aggregation of test work, shown under `C - Lanes`. |
| **Gate lane** | A lane with a conventional id (`lint`, `test-fast`, `test-coverage`, `vsix-ci`, `ci`) seeded into the lanes file by `detect-lanes`. The file supplies its tree shape and covers; the compiled gate verb remains authoritative for execution. |
| **File lane** | Declared in the lanes file. Defined by a **member set**, never by an opaque command (the vela lesson: opaque commands forced a hardcoded covers-switch that drifts). |
| **Member** | One selection a file lane aggregates: a Go package glob, a framework root, or **another lane**. |
| **Effective member set** | The transitive, deduplicated expansion of a lane's members. |
| **Covers subtree** | Alias children under a lane item showing its direct members (decided: YES). |

## 2. The lanes file

### 2.1 Ownership, location, lifecycle

- Path: **`.vscode/test-lanes.json`**, checked into the consumer repo.
- **Ownership: 100% the consumer devtool's** (owner decision, 2026-07-12) —
  the go.mod model: keel-dev owns the file and writes it (`detect-lanes` maintenance);
  the human edits it by hand; both are first-class writers. The VSIX never
  reads the file — it only watches the path to trigger re-discovery.
- The VSIX adds the path to its existing file-watch glob; any file write
  triggers automatic re-discovery — new lanes appear with no
  VS Code restart. *Latency note: with today's `go list`-based discovery a
  refresh takes seconds; the ~1 s fast loop arrives with the go/parser
  discovery upgrade (CR-B). This spec depends on CR-B for the speed promise,
  not for correctness.*
- Absent file = valid bootstrap state: `C - Lanes` renders empty until
  `detect-lanes` writes `.vscode/test-lanes.json`. A malformed file must NOT
  break discovery: `C - Lanes` renders one non-runnable diagnostic item (kind
  `group`, limitation = parse/validation error text) and no fallback lanes.

### 2.2 Schema (version 1)

```json
{
  "version": 1,
  "lanes": [
    {
      "id": "go-log",
      "label": "log subsystem",
      "order": "c.40",
      "description": "keel/log package family",
      "members": [ { "go": "./log/..." } ],
      "prerequisites": ["go-toolchain", "keel-module-root"]
    },
    {
      "id": "go-exec",
      "label": "exec + adapters",
      "order": "c.41",
      "members": [
        { "go": "./exec/..." }
      ]
    },
    {
      "id": "core",
      "label": "core rollup",
      "order": "c.50",
      "description": "everything a log/exec-touching CR must pass",
      "members": [
        { "lane": "go-log" },
        { "lane": "go-exec" },
        { "lane": "lint" }
      ]
    }
  ]
}
```

Field rules:

| Field | Req | Rules |
|---|---|---|
| `version` | yes | Integer. Unknown major version → whole-file diagnostic (see 2.1). |
| `id` | yes | `[a-z][a-z0-9-]*`, unique across lanes in the file. Wire item id becomes `keel::lane::<id>`. IDs never carry ordinals. |
| `label` | yes | Human label, no ordinal prefix (the devtool prepends `order`). |
| `order` | yes | Dotted ordinal, e.g. `c.40`. Shape-checked only (`letter.digits`) — the devtool owns group placement, so renumbering top-level groups never invalidates lanes files. `detect-lanes` seeds gate lanes at `c.1` lint, `c.2` test-fast, `c.3` test-coverage, `c.10` vsix-ci, `c.30` ci; category detection starts at `c.40`. Duplicate orders still render (V7 warning; `sort_text` breaks ties by id). |
| `description` | no | Free text; the devtool appends the measured duration hint (see §6). |
| `members` | yes | Non-empty array. Member forms in 2.3. |
| `prerequisites` | no | Resource ids surfaced as `required_resources`; checked by `PrepareLane`. Prerequisites of referenced lanes are inherited (union). |

Unknown top-level or lane-level fields are a validation **warning** (forward
compatibility), unknown member forms are an **error** (silently running less
than declared is the one unforgivable failure of a merge-gate candidate).

### 2.3 Member forms

```json
{ "go": "./log/..." }                          // Go package glob — native go-test semantics
{ "root": "go" }                               // a whole framework root: "go" | "vsix"
{ "vsix": "src/test/suite/tree.test.ts" }      // ONE extension test file (path under vsix/)
{ "lane": "go-exec" }                          // another lane by id
```

- `go` globs use `go list` pattern syntax verbatim. A glob matching zero
  packages with test files is a validation warning (tree still renders).
- `root: "vsix"` means the whole Mocha suite; `vsix: "<file>"` selects a
  single extension test file (owner decision 2026-07-12, Option 2: per-file
  granularity ships in v1; the root form remains as whole-suite shorthand).
  Both imply the `pnpm` prerequisite; per-file selection requires Mocha
  file-filtering in the suite runner and static vsix file discovery (no node
  at discovery time) so covers aliases resolve to real `vsix::file::…` items.
  The headless VS Code boot cost is paid once per run regardless of how many
  files are selected.
- `lane` refs point at other lanes in the same file or at the conventional
  gate lane ids seeded by `detect-lanes`.
- **Normalization for dedup:** go members normalize to their matched package
  set at expansion time — `{ "root": "go" }` and `{ "go": "./..." }` denote
  the same effective set and dedup against each other; overlapping globs
  dedup at the package level, not the glob-string level.

## 3. Lane composition semantics

1. **Graph shape:** members form a directed graph over lanes; it MUST be a
   DAG. Cycles are a whole-file validation error naming the cycle
   (`core → go-all → core`). Max composition depth: 8; exceeding it is a
   **depth-exceeded** error naming the deepest path (not misreported as a
   cycle).
2. **Expansion:** the effective member set is the transitive closure of
   non-lane members, **deduplicated** per the 2.3 normalization rule.
3. **Union only.** No exclusions, no intersections in v1. If a composition
   needs "all except X", define a narrower glob. (Exclusion syntax is the
   first candidate for v2 — deliberately out.)
4. **Prerequisites** and implied resources union across the closure.
5. **Ordering is per-lane, not inherited:** a lane referenced by another lane
   still renders once, at its own `order` slot. Composition never duplicates
   tree items outside covers aliases.

## 4. CLI verbs (the VSIX ↔ devtool contract)

All verbs: protocol JSON on **stdout** only, logs to stderr + `.logs/` sinks
(existing keel-dev test-bridge output rule). Exit 0 = document produced (even with
validation warnings); non-zero = verb itself failed.

| Verb | Status | Contract |
|---|---|---|
| `test-bridge tests discover` | exists | Full tree document. Lanes come exclusively from `.vscode/test-lanes.json`; each has ordinal label, `sort_text`, covers aliases, duration hint, `required_resources`. Lanes-file diagnostics appear as the 2.1 diagnostic item. |
| `test-bridge tests desired-state [--id]` | exists; extension planned | The **desired-state detection** verb. `desired_state` rows gain real per-resource checks (go toolchain, module root, pnpm for vsix members, lanes-file validity as a resource). |
| `test-bridge tests run --id` | exists | Accepts lane ids. Gate lane ids execute the compiled gate behavior; category/composed lanes execute their member sets. Run semantics in §7. |
| `file-backed lane discovery` | implemented as lane inventory | Effective lane definitions as JSON: per lane — id, label, order, direct members, **expanded** member set, inherited prerequisites, validation findings, last measured duration. This is also the artifact a future CR-class→gate mapping consumes. |
| `detect-lanes maintenance` | exists | Scans the module (`WalkDir`, no compilation) and **rewrites** `.vscode/test-lanes.json` as the canonical detect-owned file: gate lanes plus one category lane per top-level test package family (creating or replacing the file even when absent, stale, hand-edited, or invalid). A first-class write under the devtool-ownership model -- not a config-policy exception, and not silent (runs only as explicit user action: verb or tree item a.1). Idempotent full regeneration; manual edits are transient and do not survive a detect run. Reports the delta versus the previously persisted content as JSON `{added, removed, changed, unchanged}`; run through the tree, the delta is emitted as `output` events so the run shows what changed, and the file write itself triggers watcher-driven re-discovery -- new lanes appear without a restart. |

`detect-lanes` maintenance is exposed in-tree as maintenance item **a.1 detect lanes**
(kind `maintenance`), alongside a.2 unlock, a.3 clear results, a.4 clear state.

### 4.1 `detect-lanes` maintenance — full contract

**Invocation:** `keel-dev test-bridge detect-lanes maintenance [--format json] [--dry-run]`

- `--format json` — only supported format (consistency with sibling verbs).
- `--dry-run` — compute and report the delta WITHOUT writing the file.
- No other parameters in v1: detection is convention-driven, not configured.

**Detection algorithm (deterministic, normative):**

1. **Enumerate** test-bearing packages: `WalkDir` from the module root (no
   compilation); skip hidden dirs, `vendor`, `node_modules`, `testdata`,
   `bin`, `worktrees`; a package counts iff its directory contains ≥ 1
   `*_test.go` file.
2. **Seed gate lanes**: generate `lint`, `test-fast`,
   `test-coverage`, `vsix-ci`, and `ci` with the conventional `c.*` orders
   and member sets that drive covers (`go` root, `vsix` root, or lane refs).
3. **Group into families**: family key = first path segment under the module
   root (`log`, `exec`, `cli`, `vscode`, `cmd`, …); a family's glob is
   `./<segment>/...` (root-level test-bearing packages, if any, form family
   `root` with glob `./`).
4. **Candidate lane per family**: id `go-<segment>`, label `<segment>`,
   members `[{"go": "./<segment>/..."}]`, description
   `"detected category — N packages"`, order = next free slot in the
   category range starting at `c.40` (ascending, rebuilt from the sorted
   detected family list).
5. **Delta check**: compare the freshly generated lane set against the
   previously persisted content by lane id and definition. Prior lanes absent
   from the generated set are `removed`; same-id lanes with different
   definitions are `changed`; byte-equivalent definitions are `unchanged`; new
   ids are `added`.
6. **Write** (unless `--dry-run`): write the freshly generated document to
   `.vscode/test-lanes.json`, creating or replacing it with `{"version": 1}`.
   Existing entries are not preserved unless they exactly match the generated
   lane definition.

**Result document (stdout, exactly one JSON object):**

```json
{
  "version": 1,
  "file": ".vscode/test-lanes.json",
  "written": true,
  "added": [
    { "id": "go-log", "label": "log", "order": "c.40",
      "members": [ { "go": "./log/..." } ], "packages": 3 }
  ],
  "removed": [ { "id": "legacy-log", "reason": "not detected" } ],
  "changed": [ { "id": "go-exec", "reason": "definition changed" } ],
  "unchanged": [ { "id": "ci", "reason": "unchanged" } ]
}
```

`written` is false under `--dry-run`; otherwise detect writes the canonical
file even when the delta is empty. **Exit codes:** 0 = document produced and
the file regenerated or dry-run delta reported; 1 = filesystem or detection
failure that prevents regeneration; 2 = usage. A previously absent, unreadable,
unsupported, malformed, cyclic, or otherwise invalid lanes file is not a detect
failure; it is replaced by the generated file.

**Idempotence guarantee:** running detect twice in a row yields
empty `added`, `removed`, and `changed` arrays on the second run and a
byte-identical file.

**As maintenance item a.1:** same algorithm; the result document's contents
are emitted as run `output` events (one line per added/removed/changed/unchanged
entry), terminal `passed` on exit 0, `failed` with the error text on exit 1;
the file write (when any) triggers watcher-driven re-discovery.

### 4.2 file-backed lane inventory — result document

**Invocation:** `keel-dev test-bridge file-backed lane inventory [--format json]`. Read-only.

```json
{
  "version": 1,
  "workspace": "keel",
  "generated_at": "2026-07-12T19:30:00Z",
  "lanes": [
    {
      "id": "keel::lane::core",
      "source": "file",
      "label": "core rollup",
      "order": "c.50",
      "description": "everything a log/exec-touching CR must pass",
      "members": [ { "lane": "go-log" }, { "lane": "go-exec" }, { "lane": "lint" } ],
      "expanded": {
        "go_packages": ["log", "exec", "exec/claude", "exec/codex"],
        "roots": [],
        "system_lanes": ["lint"]
      },
      "prerequisites": ["go-toolchain", "keel-module-root"],
      "findings": [ { "rule": "V6", "severity": "warning", "message": "…" } ],
      "last_run": { "run_id": "…", "at": "2026-07-12T18:02:11Z",
                    "duration_ms": 9800, "exit_code": 0 }
    }
  ]
}
```

- `source`: currently `file`; gate lanes are file entries seeded by
  `detect-lanes`, not discovery fallbacks.
- `expanded` is the effective member set after DAG expansion + dedup (§3) —
  the machine-readable input for gate-sizing and the future CR-class→gate
  mapping.
- `findings` carries §11 validation results per lane (whole-file errors
  produce a top-level `findings` array instead of a `lanes` array).
- `last_run` derives from the §6 `requested` attribution; `null` when no
  attributable stream exists.
- Exit codes as in 4.1.

## 5. Discovery projection rules

1. **Ordinals:** label = `<order> <label>`; `sort_text` = order with numeric
   dotted segments zero-padded to 3 (`c.40` → `c.040`). Letters pass through.
   IDs never contain ordinals (results and expanded state survive renumbering).
2. **Groups** (owner decision 2026-07-12): top-level `a. Maintenance` (kind
   `group`), `C - Lanes` (kind `group`), and `d. Frameworks` (kind `group`) —
   the single parent for language/framework-specific enumeration trees:
   `d.1 Go` (kind `root`), `d.2 Mocha (vsix)` reserved for when the vsix tree
   lands. Adding a framework is a new `d.N` child, never a new top-level
   letter. The letter `c` is deliberately unassigned — a gap for a future
   group without renumbering. No `keel::root` workspace node (its
   `workspace` kind is out-of-schema today; removed).
3. **Covers subtree:** each lane with members gets a non-runnable `covers`
   child (kind `group`) containing alias items — **fully expanded** for
   concrete members, per the vela rendering the owner confirmed on
   2026-07-12 (alias of the covered root plus its entire descendant subtree,
   every alias carrying `canonical_id`):
   - go glob → one alias per matched package AND its file/test descendants;
   - root → alias of the framework root and its descendants;
   - lane → alias of that lane's item only (`canonical_id =
     keel::lane::<id>`) — referenced lanes are NOT re-expanded (prevents
     exponential blow-up in composed lanes; the referenced lane's own covers
     are one click away).
   Cost note: full expansion multiplies discovery payload; keel's module size
   keeps this far below the 16 MiB ceiling, and the emitter warns near it.
4. **Capabilities:** discovery advertises `clear_results_test_ids` /
   `clear_state_test_ids` pointing at a.3/a.4 — this is the fix for the
   currently broken `keel.tests.clearLocalState` menu command.

## 6. Cost measurement

1. **Attribution is declared on the wire, not inferred.** *(Mechanism:
   reviewer/assistant recommendation, owner delegated; shape follows the
   owner's minimum requirement of an id/description interface.)* The
   `run_started` event gains an optional additive field:

   ```json
   { "event": "run_started",
     "requested": [ { "id": "keel::lane::test-fast", "label": "test-fast" } ] }
   ```

   This is an **additive run-event schema change** (optional field; old
   readers ignore it — permitted by the additive-only compat policy). It
   replaces rev 1's broken "lock ids" mechanism: `run.lock` is deleted at run
   end and persisted events carried no requested-selection record, so
   post-run attribution had nothing to read. The rejected alternative —
   inferring attribution from the terminal lane `passed` event — needs no
   schema change but only ever measures green runs and rests on pattern
   matching instead of a declared fact.
2. A lane's displayed duration = `run_started` → `run_finished` of the newest
   stream under `.devtools/vscode-runs/` whose `requested` set is **exactly**
   `[the lane]` — multi-selection runs never contribute (no over-attribution)
   and failed runs count (cost is not a green-runs-only statistic).
3. Discovery appends the hint to the lane description: `· last 9.8s`
   (`m s` above 90 s). The description travels on the wire `limitations`
   channel and renders as the **dimmed secondary text** next to the label
   (owner-confirmed placement, 2026-07-12) — the label itself stays clean.
   VS Code's own right-aligned run duration is session-ephemeral; this hint
   persists across restarts. Lanes with no attributable stream show no hint.
4. Per-member attribution (which glob costs what) comes free for Go members
   from `go test -json` elapsed, and is recorded in the file-backed lane inventory output —
   this, not the tree hint, is the gate-sizing dataset.
5. Staleness is acceptable by design: the hint says "last", not "typical".

## 7. Run semantics for file lanes

1. **Expansion:** resolve the effective member set (3.2) at run start; a
   validation error in the lanes file fails the run before any test executes
   (`errored` + `run_finished`, exit 1).
2. **Execution order (deterministic, by member form):** referenced **gate
   lanes** first, in declared member order; then all **Go members** merged
   into a single `go test -json <globs…>` invocation (one build, fastest);
   then **vsix members**. Cheap/fast steps fail first; no adjectives — the
   order is defined by member form, nothing else.
3. **Events:** per-test events are attributed to canonical `go::test::…` ids
   (the existing projection); the VSIX mirrors state onto covers aliases via
   `canonical_id` — no new event kinds. `run_started` carries the `requested`
   declaration (§6).
4. **Exit code:** 0 iff every member step passed. The existing run lock
   (`run.lock`) serializes lane runs; unlock is maintenance item a.2.

## 8. VSIX changes required

Deliberately minimal — the tree, aliases, sort_text, capabilities and
maintenance plumbing all exist:

1. Add `.vscode/test-lanes.json` to the file-watch glob (one path, one-line
   change). The VSIX watches the path for change events only; it never parses
   the file — lane knowledge lives entirely behind the devtool's verbs.
   Other consumers' lane-file locations are NOT pre-baked (rev 1 watched
   vela's `tools/test-lanes.json` speculatively; cut — a consumer that
   implements lanes brings its watch path in its own one-line CR).
2. Add `keel.tests.detectLanes` menu command → runs maintenance id a.1
   through the existing maintenance path (same mechanism as clear-results).
3. Nothing else.

## 9. Consumer portability

The VSIX contract is only: discovery document + plan document + run events +
maintenance capabilities. The lanes *file* — format AND location — is the
consumer devtool's private convention (100% devtool-owned); another consumer
may store lanes however it likes as long as its devtool answers the same
verbs. The only VSIX-visible trace of the file is the watch path (§8.1),
added per consumer as needed.

## 10. Versioning & compatibility

- Lanes file `version` is independent of the bridge config version and the
  discovery document version. v1 as specified; additive fields = minor
  (warnings), breaking member-form changes = major.
- `test-bridge config upgrade` does NOT touch the lanes file (it belongs to the
  lanes verbs).
- Wire schema impact: **one additive change** — the optional `requested`
  array on `run_started` (§6). Discovery/plan schemas need no changes beyond
  using existing fields; the only other schema fix is removing the
  out-of-enum `workspace` kind usage.

## 11. Validation rules (consolidated)

| # | Rule | Severity |
|---|---|---|
| V1 | unique lane ids | error |
| V2 | non-empty members, known member forms | error |
| V3 | lane refs resolve | error |
| V4 | composition is a DAG (cycle error names the cycle); depth ≤ 8 (depth error names the path) | error |
| V5 | `order` matches the `letter.digits` shape | warning |
| V6 | glob matches ≥ 1 package with tests | warning |
| V7 | duplicate order values | warning |
| V8 | unknown fields | warning |
| V9 | unknown file major version | whole-file diagnostic |
| V10 | `vsix` member path resolves to an existing test file | warning |

Errors on a single lane suppress that lane (diagnostic item notes it);
whole-file errors suppress all lanes from the file and render only the
diagnostic item under `C - Lanes`.

## 12. Decision log & remaining questions

Resolved 2026-07-12:

1. **Exclusions in v1** — NO; union only (revisit on first real need).
2. **`detect-lanes` maintenance write behavior** — WRITES the file (owner decision):
   the file is 100% devtool-owned, go.mod model. Updated 2026-07-14:
   detect-lanes full-rewrites the file; manual edits are transient and
   idempotence is byte-identical regeneration, not append-only preservation.
   Overrides the reviewer recommendation of propose-only.
3. **Cost attribution** — `requested: [{id, label}]` on `run_started`
   (reviewer/assistant recommendation; owner delegated the mechanism and set
   the id/description minimum).
4. **Covers depth** — REVISED per owner's vela screenshot: full descendant
   expansion for concrete members (packages → files → tests); lane refs stay
   single-level aliases.
5. **Watch glob** — keel path only; no speculative consumer paths.

6. **vsix member granularity** — PER-FILE in v1 (owner decision 2026-07-12,
   Option 2, after side-by-side visualization): `{ "vsix": "<file>" }` member
   form + Mocha file selection + static vsix file discovery land in v1;
   `root: "vsix"` stays as whole-suite shorthand.

7. **CR split** — REALIZED 2026-07-12 as three approved units:
   **keel/change_request-53** tree restructure + Maintenance group +
   capabilities fix + the two new gate lanes (requirements 46–48);
   **keel/change_request-54** Go discovery upgrade (requirements 49–50 —
   the fast-loop dependency named in §2.1); **keel/change_request-55**
   lanes file + composition + `detect-lanes maintenance` + cost hints + per-file
   vsix members (requirements 51–54; `depends_on` 53 + 54; the `requested`
   field lands here with its first writer). All queued in keel/iteration-10.

No questions remain open in this specification.

Resolved 2026-07-14:

8. **No discovery fallback lanes** — all lanes, including gate lanes, come
   from `.vscode/test-lanes.json`. `detect-lanes` is the bootstrap path; an
   absent file yields an empty `C - Lanes` group, and a whole-file error yields
   one diagnostic item with no compiled fallback lane set.

## 13. Traceability

Carried by keel/requirement-46…54 and amended by keel/requirement-65;
implemented by keel/change_request-53/54/55 and revised by
keel/change_request-76. Impacts existing
keel/requirement-43 (Go tree), -35..37 (runs), -40 (config), -41 (demo
block); adds one additive field to the run-event schema (§6); fixes
keel/issue-38 (`clearLocalState`, via CR-53) and keel/issue-39 (file-run
degradation, via CR-54); supersedes Winston Phase-1 decisions D2/D3 where
this spec differs (title = Maintenance, letters ordering, data-driven
lanes). Deciding record: keel/exploration-2 (concluded 2026-07-12).
