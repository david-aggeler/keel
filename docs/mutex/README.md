# Mutex (mutually-exclusive desired-state) handling

This folder is the consolidated map of keel's mutually-exclusive
desired-state ("mutex") machinery: what it is, how state flows through it,
why it has absorbed **2 RCAs and ~14 issue fixes** without becoming
reliable, and what has to be decided to sort it out for good.

> "Mutex" here means *mutually-exclusive desired-state groups* in the
> test bridge (`DesiredStateGroup.MutuallyExclusive`), **not** Go
> `sync.Mutex`.

## The problem in one paragraph

An exclusive group ("app-db is postgres | sqlite | Unknown State") must
show **exactly one active member** in the VS Code Test Explorer. But the
active/reset truth is simultaneously represented in **four places** —
desired-state rows, discovery item metadata, the run-event stream, and
sticky VS Code result icons — and no single layer owns the rendering.
The icons (the highest-salience surface) are only reconciled when a run
fires; at rest, after a plain refresh, they can show every member green.
Fixes kept landing at layers the owner cannot see, verified by tests that
never observed the rendered icons, so gates went green while the editor
stayed wrong (`keel/rca-3`, reopened 2026-07-17).

## Documents

| Doc | Contents |
|---|---|
| [architecture.md](architecture.md) | Component diagram, the four state representations, per-row state machine, invariants and where each is (not) enforced, design tensions |
| [sequences.md](sequences.md) | Sequence diagrams: at-rest refresh (with the issue-86 gap), activation run end-to-end, post-run reconcile, probe-execution inventory |
| [history.md](history.md) | The full defect/fix trail: rca-3, rca-4, the ~14 issue fixes, and why the fixes didn't stick |
| [vscode-object-model.md](vscode-object-model.md) | What the VS Code testing API actually guarantees (evidence-classed facts), the target rendering model, and the mechanism-mapping that selects the fix design |

## Current open records (2026-07-17)

| Record | Status | What it holds |
|---|---|---|
| `keel/rca-3` | **open** (reopened) | Root: no authoritative rendering layer; icons contradict metadata. Roots #2–#4 fixed by CR-116 (data layer); root #1 unremediated. |
| `keel/issue-86` | open | The isolated mechanism: nothing reconciles exclusive result icons from desired-state **at rest / after refresh** — only an activation-run `cleared` event does. |
| `keel/rca-4` | open | Sibling disease: lane→framework package rows stuck on spinner; no owned Test Explorer state contract. |
| `keel/issue-84` / `keel/issue-47` | open | rca-4's concrete defect + the missing real-controller reproduction harness. |

## What "sorted out" requires (decision, then work)

Owner direction recorded in `rca-3` (2026-07-17):

1. **Pick the single authoritative rendering layer.** Direction: the
   **bridge run-event stream** drives the icon layer — not VSIX branching
   on `mutually_exclusive` (`design_decision-5`: VSIX stays
   business-logic-free). On refresh, the bridge must drive icons to agree
   with desired-state (active member marked, non-active concrete members
   genuinely result-dropped, Unknown State reset) — **or** exclusive
   members stop carrying persistent pass/fail results at all.
2. **Resolve the dd-5 contradiction already in the code.** CR-119's
   `refreshMutexStates` (`vsix/src/extension.ts:359`) branches on
   `mutually_exclusive` and `active` inside the VSIX — exactly what
   design_decision-5 forbids and what rca-3's corrective direction rules
   out. Either dd-5 is formally amended by `requirement-93`, or that
   logic migrates behind the bridge boundary. See
   [architecture.md § Tensions](architecture.md#design-tensions).
3. **Prove it on a real controller, post-refresh, no activation.**
   cr-114's real-`vscode.TestController` expected-red harness covers only
   the activation switch. The new spec must assert the **at-rest,
   post-refresh** icon state (the exact state the owner keeps catching by
   eye). Red first (redlist), then the paired fix CR.
4. **Process gate:** no exclusive-group CR closes `corrected`/`tested`
   without a real-controller assertion of rendered icon state — internal
   maps and event lists are not evidence (repeat offenders: rca-1, rca-2,
   rca-3's reopen).

All new corrective records parent under `keel/epic-2` and route through
the normal issue → approved CR front door.
