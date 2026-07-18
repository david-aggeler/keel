# VS Code testing object model — what the API actually guarantees

Research doc for the mutex-icon fix (rca-3 root #1). Sources: the
`@types/vscode` API contract (`vsix/node_modules/@types/vscode/index.d.ts`,
testing section ~18079–18650), the empirical results already encoded in
the real-controller specs (`vsix/src/test/suite/red-spec.test.ts`,
cr-114/cr-115), production behavior of `externalRunMirror.ts`, and owner
validation on a live editor (iteration-26, issue-86). Every claim below
is tagged with its evidence class:

- **API** — documented contract in `vscode.d.ts`
- **PROVEN** — asserted on a real `vscode.TestController` in our suite, or
  validated by the owner's eyes on a live editor
- **UNPROVEN** — documented or plausible, never observed rendered
- **RULED OUT** — tried and observed not to do what we need

## 1. The objects

```mermaid
classDiagram
    class TestController {
        +items TestItemCollection
        +createTestItem(id, label, uri) TestItem
        +createTestRun(request, name?, persist?) TestRun
        +invalidateTestResults(items?) void
        +refreshHandler
        +resolveHandler
    }
    class TestItemCollection {
        +add(item)  «same id ⇒ replaced»
        +delete(itemId)
        +get(itemId)
        +replace(items)
    }
    class TestItem {
        +id  «immutable, unique among siblings»
        +children TestItemCollection
        +parent TestItem
        +label / description / sortText / tags / range / uri
        +canResolveChildren  «true ⇒ expandable + busy while resolving»
    }
    class TestRunRequest {
        +include TestItem[]?
        +exclude TestItem[]?
        +profile TestRunProfile?
        +preserveFocus bool
    }
    class TestRun {
        +isPersisted bool
        +enqueued(test) / started(test)
        +passed(test) / failed(test) / errored(test) / skipped(test)
        +appendOutput() / addCoverage()
        +end()  «unstamped included tests: state reset»
    }
    TestController --> TestItemCollection : items
    TestItemCollection --> TestItem
    TestItem --> TestItemCollection : children
    TestController --> TestRun : createTestRun
    TestRunRequest --> TestRun : precursor
```

## 2. Facts that constrain any mutex-icon design

| # | Fact | Class | Evidence |
|---|---|---|---|
| F1 | **There is no result-readback API.** Stable `vscode.tests` exposes only `createTestController`. An extension can never ask "what icon does item X show?" | API | d.ts:18079–18088; red-spec req-88 comment |
| F2 | **There is no per-item "clear result" API.** | API | d.ts (absence); red-spec req-88 comment |
| F3 | `invalidateTestResults(items)` marks results **outdated** (faded styling) — the green check **remains** | API + PROVEN | d.ts:18360; issue-80; red-spec req-88 |
| F4 | Result icons are **sticky**: they survive discovery refresh when the same `TestItem` object survives (our `publishDiscovery` reconciles in place, preserving objects) | PROVEN | red-spec req-70 (object identity across refresh); CR-78 |
| F5 | **Replacing the `TestItem` object** (new object, same id, re-added via `TestItemCollection.add` which replaces) **drops its rendered result to no-result** | PROVEN — including on the owner's live editor | cr-115 `replacePublishedTestItem` (tree.ts:70); iteration-26 owner validation; red-spec req-88 asserts the object-identity proxy |
| F6 | Replacing an item does **not** need a `TestRun` — it is silent tree surgery: no Test Results panel entry, no focus change | PROVEN | cr-115 mechanism in production |
| F7 | Programmatic `TestRun`s (created outside any `runHandler`) are legal and drive icons | API + PROVEN | d.ts:18391 ("you can also create test requests and runs outside of the runHandler"); `externalRunMirror.ts:252–256` in production |
| F8 | `TestRun.end()` **resets the state of included-but-unstamped tests** | API, **UNPROVEN rendered** | d.ts:18533–18537. Never exercised by our specs; whether the *rendered icon* falls to no-result (vs. reverting to the previous run's result) is unobserved |
| F9 | `createTestRun(request, name, persist=false)` → results not restored after window reload (`isPersisted=false`) | API, UNPROVEN rendered | d.ts:18340–18346, 18465–18468 |
| F10 | Persisted results **are restored after window reload** — stale greens come back even though no run happened in this session | PROVEN (owner evidence in issue-86: greens present "at rest") | issue-86; VS Code behavior |
| F11 | `run.skipped(item)` renders a **skip icon**, not no-result | PROVEN | issue_fix-68 design note ("cleared … rather than a 'skipped' terminal state that merely swaps the icon") |
| F12 | Parent/group icons are **rolled up by VS Code from children**; an extension does not stamp group items directly (ours are non-runnable) | API behavior | Test Explorer rollup; requirement-71 territory |
| F13 | Verification proxy: since F1, specs can only assert **object identity** (F5's replacement observable) and behavioral contracts (which methods were called) — never the icon itself. The owner's eyes are the only true readback | PROVEN methodology | cr-114 harness design; memory: real-controller expected-red |

## 3. The rendering the owner wants (target model)

For every mutually-exclusive group, at **any** observation moment — after
an activation run, after a plain refresh, after a window reload, after
out-of-band environment drift + refresh:

| Row | Desired rendered state |
|---|---|
| The single `active=true` member | May carry its genuine last activation result (green). Never contradicted by siblings. |
| Every non-active concrete member | **No result icon at all.** Not green, not skipped, not outdated-green. |
| `Unknown State` (reset peer) | Active ⇒ may carry its reset-run result; non-active ⇒ no icon. |
| The group parent | Rolls up from the above ⇒ at most one member's state. |
| Invariant | **At most one member of the group renders a result, and it is the active one.** |

## 4. Mapping: desired state → mechanism → verdict

| Mechanism | What it does | Verdict for the at-rest reconcile |
|---|---|---|
| A. `cleared` run-event → `invalidateClearedResults` → item rebuild | Proven drop (F5), but **trigger** only exists during an activation run | Mechanism right, **trigger insufficient** — this is exactly issue-86 |
| B. `refreshMutexStates` (CR-119) | Same rebuild, triggered after run finish, VSIX branches on `mutually_exclusive` | Fixes post-run only; still no at-rest trigger; violates dd-5 |
| C. **Bridge-computed no-result list, applied by item rebuild on every refresh** | Bridge (sole authority) lists the ids that must render no result; VSIX applies the *proven* F5/F6 rebuild verbatim on every `refreshNow` | **Chosen.** Every fact it relies on is PROVEN (F5, F6, F4); no TestRun noise (F6); covers at-rest, post-run, and post-reload (F10) because refresh always runs on activation and on the refresh button; no VSIX branching on `mutually_exclusive` (verbatim id list — same pattern as `clear_state_test_ids`) |
| D. Programmatic reconcile `TestRun`: include non-active members, stamp nothing, `end()` (F8), optionally stamp active `passed` | API-documented reset; could also paint the active member green without a real run | **Rejected for now**: rendered effect UNPROVEN (F8), creates a Test Results entry per reconcile (panel noise; F6 alternative is silent), and stamping a synthetic green forges an activation result the owner never ran. Documented as fallback if C's rebuild ever proves insufficient |
| E. `invalidateTestResults` alone | Outdated-green | RULED OUT (F3, issue-80) |
| F. `skipped` stamping | Skip icon | RULED OUT (F11) |
| G. Stop persisting activation results (`persist=false` on all runs) | Reload comes back clean | RULED OUT by owner (issue-86: "option (b) did not work") — and loses genuine run history |

### Chosen design C, concretely

1. **Wire (additive, discovery.json v1):** `capabilities.reconcile_no_result_test_ids: string[]` —
   computed by `keel/testbridge` during discovery derivation: for every
   `mutually_exclusive` group, every row with a `run_id` whose derived
   `active` is false (concrete members and the synthetic Unknown peer
   alike). Bridge is the single authority (dd-5 intact).
2. **VSIX (verbatim):** after `publishDiscovery` inside `refreshNow`,
   apply the list through the existing `invalidateClearedResults`
   machinery (invalidate + `replacePublishedTestItem`). No
   `mutually_exclusive` inspection anywhere in the VSIX; CR-119's
   `refreshMutexStates` and its desired-state re-read become redundant
   and are removed (requirement-93 superseded).
3. **Reconcile coverage for free:** `refreshNow` runs on activation
   (`activate()` → `refresh`), the refresh button, watcher events, and
   after every run (`refreshDesiredStateAfterRun` → `refreshNow`) — so
   at-rest, post-run, and post-reload are all the same code path.
4. **Verification (F13):** extend the cr-114 real-controller harness with
   an **at-rest spec**: stamp a green on a non-active member via a real
   run, publish a fresh discovery carrying
   `reconcile_no_result_test_ids=[member]`, run the refresh apply, assert
   the member's `TestItem` object was replaced (result dropped) while the
   active member's object is retained. Red first (redlist), then fix.
   Final readback is the owner's live editor (F13).

### Known residual limitations (accepted, documented)

- An active member that has never been run in this session shows no
  green (only its `active=true` description) — painting one would forge
  a result (mechanism D rejected). The invariant "at most one result,
  and it is the active one" still holds.
- Rebuild churn: listed items are rebuilt on every refresh even if they
  already show no result (F1 — we cannot know). A handful of rows per
  group; watch for flicker during owner validation.
