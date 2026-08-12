---
name: epic/plan
description: 'Decompose one epic into feature change-request units, requirement-first. Use when the user says "plan epic N" or "decompose this epic into units".'
x-openbrain-content-hash: sha256:4a4631beaed40f36271e1b9b2f856b5b4925df541cacbc36f0a5b2809aae2f69
---

# /epic plan — Decompose an Epic into Units

**Goal:** Turn one epic into a set of `kind=feature` change-request units that are structurally valid, requirement-backed, and drainable the moment they are approved.

**Read `SKILL.md` "Record model contract" first.**

## The contract this workflow exists to honor

A `kind=feature` unit must reference at least one requirement, at every status, or the write is rejected. So the unit of decomposition is not "a chunk of work" — it is **a requirement with its acceptance criteria, plus the unit that implements it**. Decomposition therefore runs requirement-first:

1. find or author the requirement
2. author its acceptance criteria as `ac` records
3. create the unit against them

A step that produces a unit without a requirement produces nothing at all.

## Execution

<workflow>

<step n="1" goal="Identify the epic">
  <check if="an epic ref or sequence was provided">
    <action>Call `get_epic` to load it.</action>
  </check>
  <check if="no epic was named">
    <action>Call `list_epic` with `filter={"status":"active"}`. With exactly one hit, confirm it with the operator and wait. With zero or several, present the list and ask.</action>
  </check>
  <action>Store {epic_ref}, {epic_title}, and the epic's `related_requirements`.</action>
</step>

<step n="2" goal="See what already exists">
  <action>Call `list_change_request` with `filter={"parent":"<epic_ref>"}` and report the units that already exist with their status and requirement refs.</action>
  <action>You are extending this set, not replacing it. Never create a second unit for work an existing unit already covers — check by requirement ref, not by title similarity.</action>
</step>

<step n="3" goal="Resolve requirements for the undecomposed scope">
  <action>For each piece of the epic's scope not yet covered by a unit, search for an existing requirement FIRST: `search_requirement` on the capability wording, then `search_ac`, then `search_records` across types. Search what the capability IS, not the phrasing of the request that surfaced it.</action>
  <check if="a requirement already covers it">
    <action>Use that ref. If its statement is close but imprecise, sharpen it with `update_requirement` rather than creating a near-duplicate.</action>
  </check>
  <check if="no requirement covers it">
    <action>Fetch `get_template_for dto_type=requirement`, then call `create_requirement` with `product`, `title`, a shall-form `statement`, `type`, and `status`.</action>
    <action>Immediately author its acceptance criteria as `ac` records via `create_ac` with `parent` set to the requirement ref. Do not defer this to a later pass.</action>
    <action>Link the new requirement onto the epic by adding its ref to `related_requirements` with `update_epic`.</action>
  </check>
  <action>Report the requirement set the units will be built on before creating any unit.</action>
</step>

<step n="4" goal="Propose the unit partition">
  <action>Propose the units: for each, a title, a one-sentence summary, and the requirement refs it will carry. Keep units small enough to be implemented and reviewed in one pass.</action>
  <action>State the intended order and any `depends_on` edges between units.</action>
  <action>Present the proposal and wait for approval. Do not create records before approval.</action>
</step>

<step n="5" goal="Resolve the iteration and executor">
  <action>Call `list_iteration` for the product. Determine which open iteration these units belong to.</action>
  <check if="no suitable iteration exists">
    <action>Ask the operator whether to create one with `create_iteration`, or to target an existing one. A unit with no `iteration` will never appear in a run-queue drain — say this plainly rather than proceeding silently.</action>
  </check>
  <action>Ask which executor these units are for — `agent` or `human` — and store the answer. Default to `agent` only if the operator confirms it.</action>
</step>

<step n="6" goal="Create the units">
  <action>Fetch `get_template_for dto_type=change_request`.</action>
  <action>For each approved unit call `create_change_request` with:
    - `product`, `title`, `summary`
    - `kind` — `feature` (required on every write)
    - `parent` — {epic_ref} (an epic, never an issue)
    - `requirements` — at least one requirement ref, non-negotiable
    - `iteration` — the iteration from step 5
    - `executor` — from step 5
    - `status` — `draft`
    - `depends_on` — sibling unit refs where sequencing demands it
    - `motivation` / `proposed_change` — enough for the operator to recognize the unit later; the full 4-section body is authored at pickup by `/change-request create`
  </action>
  <check if="a create is rejected with change_request_feature_requires_requirement">
    <action>The unit has no requirement ref. Go back to step 3 for that unit — do not retry with a different field.</action>
  </check>
  <action>Record each returned ref.</action>
</step>

<step n="7" goal="Verify against the record graph">
  <action>Run the checklist at `plan/checklist.md`. Report it as a table.</action>
  <action>Fix every failure before reporting completion.</action>
</step>

</workflow>

## Output

`change_request` records at `status=draft` under the epic, each carrying a requirement, an iteration, and an executor. Approval to `approved` is the operator's call — and at that point at least one listed requirement must itself be approved-or-later, so raise any requirement still in `draft` now rather than at the approval gate.

Next: `/change-request create` details a unit at pickup; `/epic status` shows the rollup.
