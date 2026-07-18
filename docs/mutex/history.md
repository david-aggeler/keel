# Mutex defect/fix history — and why the fixes didn't stick

Source of truth: gold records (product `keel`). This is a rendered
timeline for orientation; follow the record ids for evidence.

## The two RCAs

| RCA | Status | Scope | Root causes (categories) |
|---|---|---|---|
| `keel/rca-3` | **open** — reopened 2026-07-17 (new_evidence) | Mutex visualization split across desired-state rows, discovery metadata, and sticky result icons | #1 no authoritative rendering layer (design, **unremediated** → issue-86) · #2 demo consumer starved selected desired-state (design, fixed CR-116) · #3 tests verify internals not rendered UI (tooling, partially fixed) · #4 exactly-one-active not enforced on ambiguity (design, fixed at data layer CR-116) |
| `keel/rca-4` | open | Lane→Frameworks/Go package rows stuck on spinner after a green lane run | separate projections with no state-ownership contract (design) · conditional reducer instead of completion invariant (design) · no real-controller verification of the boundary (tooling) · fix loop attacked candidates before reproduction (process) |

Related earlier pattern: `rca-1` (epic-2 quality escape — UI defects
shipped green) and `rca-2` (desired-state "satisfied" was a compile-time
constant; tautological ACs). Four RCAs, one theme: **green gates that
never observe the user-visible surface.**

## Issue-fix trail (chronological by sequence)

| Fix | CR (where known) | What it changed | Layer touched |
|---|---|---|---|
| `issue_fix-49` | — | Desired-state rows serve `run_id`, activate as pass/fail re-probes | bridge/data |
| `issue_fix-52` | — | Bridge-owned Group-B derivation moved into keel/testbridge | bridge |
| `issue_fix-54` | — | Probe-derived statuses (kills rca-2's compile-time constants) | bridge |
| `issue_fix-59` | — | Multi-id desired-state runs emit per-row events | bridge/events |
| `issue_fix-64` | CR-95 | Discovery derives `current`+`action` from the row probe (issue-64 row-level action-derivation bug) | bridge/discovery |
| `issue_fix-68` | — | **Exclusive groups introduced**: synthesized `Unknown State` peer + sibling deactivation via new `cleared` event (requirement-88) | bridge/events |
| `issue_fix-70` | CR-105 | `refreshChain` serializes refreshes — fixed the ~40%-flaky race a 2-run check had called green (see memory: flaky gates need ≥10 repeats) | VSIX |
| `issue_fix-73` | — | Sibling `cleared` also fires on the consumer-reconcile run path | bridge/events |
| `issue_fix-75` | — | External-run import path mirrors cleared-result invalidation | VSIX |
| `issue_fix-76` | — | `cleared` messages name deactivated row + winner | bridge/events |
| `issue_fix-77` | CR-114 | Real-`vscode.TestController` **expected-red** spec for the exclusive activation switch (broke the false-green test cycle) | test harness |
| `issue_fix-78` | CR-115 | Genuine result removal on a live controller: `invalidateTestResults` only marks outdated → rebuild/replace the TestItem | VSIX |
| `issue_fix-82` | CR-116 | Mutex selected-row desired-state reports without stealing run routing (rca-3 roots #2–#4, data layer) | bridge/data |
| `issue_fix-84` | CR-119 | `refreshMutexStates`: post-run icon reconcile from the desired-state doc | VSIX ⚠ (dd-5 tension) |

Fourteen fixes; the owner's screen still showed an all-green exclusive
group on 2026-07-17 → rca-3 reopened, gap isolated as `keel/issue-86`.

## Why the fixes didn't stick — the pattern

1. **Symptom-scoped CRs.** Each fix corrected the layer where the defect
   was *diagnosed*, never claiming ownership of the rendering model. The
   split-truth architecture (four surfaces, see
   [architecture.md](architecture.md)) survived every one of them.
2. **Verification below the waterline.** Gates asserted maps,
   limitations arrays, and event JSONL. The only surface the owner ever
   looks at — result icons on a real controller — got its first test in
   CR-114, and that spec covers only the activation switch, not the
   at-rest state that keeps failing.
3. **Event-driven reconcile for a state problem.** Icon cleanup rides
   run events (`cleared`) and post-run hooks. Any state change that
   doesn't pass through a run in *this* window (external reconcile,
   window reload, backend drift) leaves icons orphaned. State problems
   need an at-rest reconcile, which nothing owns (issue-86).
4. **Doctrine drift.** dd-5 says the VSIX must not branch on
   `mutually_exclusive`; CR-119 shipped exactly that branch because the
   bridge event stream had no way to express "reconcile at rest". The
   missing design decision — *which layer owns rendered mutex state* —
   is precisely rca-3 root #1.

## Live-repro discipline (what finally worked, keep doing it)

From the iteration-26 breakthrough and rca-4's process root:

- Real `vscode.TestController` expected-red spec **first**, tracked in
  the redlist, then the paired fix CR (object-identity proxy, since
  VS Code has no result-readback API).
- ≥10 serialized repeat runs for any VSIX-touching lane before calling
  it stable (the acr REVIEW verb already enforces this for vsix CRs).
- No exclusive-group CR closes `corrected`/`tested` on internal-surface
  evidence alone.
