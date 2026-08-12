---
name: epic/retro
description: 'Post-epic retrospective anchored on a formal_review record. Use when the user says "run a retrospective" or "let us retro the epic".'
x-openbrain-content-hash: sha256:d5bf4fd438114b905e162a2f87ed4f40b458a33db343cf44c306e399a3b3913c
---

# /epic retro — Epic Retrospective

**Goal:** Extract what should change, from evidence, and land it as records. The output is a `formal_review` of type `epic_retrospective` plus whatever requirements, design decisions, and issues the discussion earns.

**Facilitation:** Systems and process, never people. Ask about evidence, not impressions. No time estimates — no hours, days, weeks, or velocity figures; they measure nothing useful here.

## Execution

<workflow>

<step n="1" goal="Identify the epic">
  <check if="an epic was named">
    <action>Call `get_epic` for it.</action>
  </check>
  <check if="no epic was named">
    <action>Call `list_epic` and offer epics at `done`, most recent first. If exactly one is `done` and unretroed — no `formal_review` of type `epic_retrospective` relates to it — propose that one and wait for confirmation.</action>
  </check>
  <action>Store {epic_ref} and {epic_title}.</action>
</step>

<step n="2" goal="Gather the evidence">
  <action>Load, and report as a table before any discussion:</action>

  | Evidence | Source |
  |---|---|
  | Units and their outcomes | `list_change_request filter={"parent":"<epic_ref>"}`, then `get_change_request` for close reasons |
  | Acceptance verdict | The epic's requirements and their `ac` records — which criteria were met |
  | Findings raised | `list_issue` filtered to those created during the epic; `list_inbound_refs` on the epic |
  | Corrections applied | The epic's `details` closeout, and any `design_decision` authored mid-epic |
  | Prior retro | The previous epic's `formal_review` of type `epic_retrospective`, via `list_formal_review` |

  <action>If a prior retrospective exists, list its action items and check each one: was it done, and did it help? A retro that never revisits its own last output produces the same findings forever.</action>
</step>

<step n="3" goal="Open the review record">
  <action>Fetch `get_template_for dto_type=formal_review`.</action>
  <action>Call `create_formal_review` with `type=epic_retrospective`, `status=in_progress`, a title naming the epic, and `related` pointing at {epic_ref}.</action>
  <action>Store {review_ref}. Write findings into it as the discussion goes, not in one pass at the end.</action>
</step>

<step n="4" goal="Work the four questions">
  <action>Take these in order with the operator, one at a time, each grounded in the step 2 evidence rather than recollection. Wait for an answer before moving on.</action>

  1. **What worked that we should keep doing?** Ask for the specific unit or decision that demonstrates it.
  2. **What cost us, and where did it first become visible?** Look for the earliest point the problem was detectable — that is where the process failed, not where it hurt.
  3. **What did we learn that is not yet written down anywhere?** Anything durable here is a requirement or a design_decision, not a retro note.
  4. **What would have made this epic go differently if we had known it at the start?** This is the input to the next epic's readiness gate.

  <action>Push back on vague answers. "Communication was poor" is not a finding; "the interface contract was agreed in conversation and never recorded, so two units implemented it differently" is.</action>
</step>

<step n="5" goal="Route every conclusion to a record">
  <action>Each conclusion gets exactly one destination. State it explicitly:</action>

  | Conclusion type | Destination |
  |---|---|
  | A durable rule we should follow | `create_requirement` + its `ac` records — normative statements live in requirements, nowhere else |
  | A choice with a rationale worth preserving | `create_design_decision` |
  | A concrete defect or gap | `create_issue` with the objective evidence |
  | A change to how the next epic is scoped | Carried into `/epic ready` and the next epic's `exclusions` |
  | An observation with no action | Stays in the review body; do not manufacture an action item for it |

  <action>Record the resulting refs in the review's `action_items`.</action>
  <halt-condition>Do not close this workflow with an action item that has no owning record. An unrouted action item is a note that will not be read again.</halt-condition>
</step>

<step n="6" goal="Close the review">
  <action>Write the review body per the template: what was delivered, what worked, what cost us, what was learned, and the routed action items with their refs.</action>
  <action>Call `update_formal_review` with `status=completed`.</action>
  <output>
Retrospective {review_ref} completed for {epic_title}.
  requirements authored: {refs}   decisions: {refs}   issues filed: {refs}

Next: `/epic ready` before the next epic opens.
  </output>
</step>

</workflow>
