---
name: epic/correct
description: 'Mid-epic course correction, expressed as record edits. Use when the user says "correct course", "the epic scope changed", or "we need to re-plan this epic".'
x-openbrain-content-hash: sha256:472b24b9b7186ee8b0f7a03bafe63c7d928e257af020976406fd97fde35b4ff8
---

# /epic correct — Mid-Epic Course Correction

**Goal:** Absorb a significant change discovered mid-epic — a wrong assumption, a new constraint, a requirement that turned out to be false — and land it as record edits with a stated rationale.

**There is no change-proposal document.** The proposal is the set of record edits you are about to make; you present it, get approval, then apply it. Nothing is written to disk.

## Execution

<workflow>

<step n="1" goal="Name the trigger">
  <ask>What changed, and what surfaced it?</ask>
  <action>Wait for the answer.</action>
  <action>Classify the trigger, and say which class you picked:
    - **assumption falsified** — a requirement or design decision rests on something untrue
    - **new constraint** — external, technical, or regulatory
    - **scope discovery** — the work is materially larger or smaller than decomposed
    - **sequencing error** — units are in an order that cannot work
    - **capability gap** — needed behavior has no requirement behind it
  </action>
</step>

<step n="2" goal="Load the affected graph">
  <action>Call `get_epic` for the epic, and `list_change_request` with `filter={"parent":"<epic_ref>"}`.</action>
  <action>Load every requirement the epic references (`get_requirement`) and their acceptance criteria (`list_ac`).</action>
  <action>Call `list_inbound_refs` and `list_relations_for` on the epic and on the affected requirements — the correction's blast radius includes records that point at them, which is exactly what gets missed.</action>
  <action>Report the affected set before proposing anything.</action>
</step>

<step n="3" goal="Assess the impact">
  <action>For each affected record state, in one line each: what is now wrong, and what it should become.</action>
  <action>Distinguish sharply between:
    - work **not yet started** — the correction is cheap, edit the record
    - work **in flight** — a unit at `in_progress` or later; changing its requirements mid-flight invalidates the review it is heading into
    - work **already merged** — the correction is new work, not an edit
  </action>
  <action>State which units, if any, must be parked at `on_hold` with a `block_reason` while the correction lands.</action>
</step>

<step n="4" goal="Choose the route">
  <action>Recommend exactly one route, with the reason:</action>

  | Route | When | What it means |
  |---|---|---|
  | **Amend requirements** | The capability was mis-stated | `update_requirement` + `create_ac` / retire ACs; units keep their refs |
  | **Re-decompose** | The unit partition no longer fits | Close or park affected units, then `/epic plan` for the new set |
  | **Extend the epic** | New scope belongs here | `/epic add` per new unit, after authoring its requirement |
  | **Split the epic** | Scope grew past one coherent slice | New epic under the same dd_plan, move undecomposed scope to it |
  | **Escalate** | The change invalidates the dd_plan or the product intent | Stop; author a `design_decision` or raise it with the owner before editing anything |
  | **Record and continue** | Real but not actionable now | File an `issue` with the evidence, leave the epic as is |

  <action>Present the recommendation with the concrete record edits it implies — the exact tool calls, in order — and wait for approval.</action>
  <halt-condition>Do not apply any edit before the operator approves the route.</halt-condition>
</step>

<step n="5" goal="Apply the edits">
  <action>Apply the approved edits in dependency order: requirements and ACs first, then units, then the epic.</action>
  <action>Respect the contracts: a `kind=feature` unit must keep at least one requirement ref; `kind` is frozen once approved-or-later; status moves must follow the ladder rung by rung.</action>
  <check if="a unit must stop">
    <action>`update_change_request` to `on_hold` with a `block_reason` naming this correction. Closing a unit instead requires a `close_reason` and a `code_change_ref` unless the close reason is one of the non-code exemptions.</action>
  </check>
  <action>Record the rationale on the epic's `details` — what changed, why, and what was decided. The next reader needs the reasoning, not just the new state.</action>
</step>

<step n="6" goal="Report">
  <action>Re-query the affected records and report the before/after as a table. Then run `/epic status` for the epic so the new board state is visible.</action>
  <check if="the correction revealed a durable lesson">
    <action>That belongs in a requirement or a design_decision, not in this epic's prose. Say so and offer to author it.</action>
  </check>
</step>

</workflow>
