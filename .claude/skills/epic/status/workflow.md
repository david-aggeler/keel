---
name: epic/status
description: 'Read-only rollup of epics and their units, with risk callouts. Use when the user says "epic status", "where is epic N", or "how is the epic going".'
x-openbrain-content-hash: sha256:ef81fec41a3405cc7927d37f6a3ee5ea3512a5cf3b00ad0b4074993346c02e20
---

# /epic status — Epic and Unit Rollup

**Goal:** Answer "where do things stand" from live records. **Read-only — this workflow writes nothing.**

## Execution

<workflow>

<step n="1" goal="Scope the report">
  <check if="an epic was named">
    <action>Call `get_epic` for it.</action>
  </check>
  <check if="no epic was named">
    <action>Call `list_epic` for the product and report every epic at `planned` or `active`. Include `done` epics only if the operator asks.</action>
  </check>
</step>

<step n="2" goal="Load the units">
  <action>For each epic in scope call `list_change_request` with `filter={"parent":"<epic_ref>"}` and `include_summary=true`.</action>
  <action>Roll up counts by status across the full ladder: `draft`, `approved`, `in_progress`, `implementation_review`, `ready_to_merge`, `merged`, `closed`, `on_hold`.</action>
</step>

<step n="3" goal="Render the rollup">
  <action>Report as a markdown table — one row per epic:</action>

| Epic | Status | Units | draft | approved | in_progress | impl_review | ready_to_merge | merged | closed | on_hold |
|---|---|---|---|---|---|---|---|---|---|---|

  <action>Then, per epic in scope, a unit table: ref, title, status, iteration, executor, requirement refs.</action>
</step>

<step n="4" goal="Surface risks">
  <action>Call out each of the following that is present. These are the conditions that make a healthy-looking board fail to progress:</action>

  - **Undrainable units** — `status=approved` with no `iteration`, or with `executor` unset or `human` where an agent drain was expected. These will never be picked up by a run-queue drain. Name them individually.
  - **Approval-blocked units** — units at `draft` whose every listed requirement is still below `approved`. Their approval write will be rejected. Name the blocking requirement.
  - **Structurally invalid units** — `kind` missing, or `kind=feature` with an empty `requirements` array. These fail on the next write of any kind.
  - **Stalled units** — `on_hold` with a `block_reason`, or long-lived `in_progress`.
  - **Dependency knots** — `depends_on` pointing at units that are not closed, and any cycle.
  - **Uncovered scope** — requirements in the epic's `related_requirements` that no unit references.
  - **Closure readiness** — for an `active` epic whose units are all `closed`, say that `/epic end` is now available.
</step>

<step n="5" goal="Point at the next move">
  <action>Recommend one next action, grounded in what the tables show — for example: `/change-request dev <ref>` on the next open unit, `/epic add` for uncovered scope, `/epic end` when every unit is closed, or a specific fix for an undrainable unit.</action>
</step>

</workflow>

## Boundary

This workflow does not change record state. If the rollup reveals something that must be fixed, say what needs fixing and which verb owns it — `/epic correct` for scope, `/epic add` for a missing unit, `/change-request correct` for a unit's own fields.
