---
name: epic/end
description: 'End-of-epic closeout — unit-closure check, optional demo, end reviews, findings into issues, epic to done. Use when the user says "wrap up epic N", "close epic N", or "epic N done".'
x-openbrain-content-hash: sha256:0ff178a3590858532bfd49915b2864efb81e5375f83b3d8a8951ec1c86ce97c0
---

# /epic end — End-of-Epic Closeout

**Goal:** Close an epic honestly: prove its units are actually closed, review what was built, route every finding into a record, and move the epic to `done` with a `done_reason` that matches reality.

**Findings become records, not files.** A finding is an `issue` record; a finding that warrants code becomes a `kind=fix` change_request parented to that issue, created through `/conclude`. Nothing is written to a findings directory or a closeout document.

## Execution

<workflow>

<step n="1" goal="Prove the units are closed">
  <action>Call `get_epic` for the epic and `list_change_request` with `filter={"parent":"<epic_ref>"}`.</action>
  <action>Report every unit with its status.</action>
  <check if="any unit is not closed">
    <action>Stop and present the open units. Ask the operator to choose per unit: finish it, close it with a `close_reason`, park it `on_hold` with a `block_reason`, or reparent it to a follow-on epic.</action>
    <halt-condition>Do not proceed to step 2 while a unit is open. An epic closed over open units is a false record — this check is the reason this workflow exists.</halt-condition>
  </check>
</step>

<step n="2" goal="Decide on a demo">
  <action>Judge from the epic's surface whether a demo is warranted: user-visible behavior warrants one, backend-only and infrastructure work usually does not.</action>
  <ask>Run a demo for this epic? (recommended: {your judgment, with the one-line reason})</ask>
  <action>Wait. Accept a decline without argument.</action>
  <check if="accepted">
    <action>Run the demo against the deployed stack, not against the diff. Capture what was shown and anything it surfaced; each surfaced problem is a finding for step 4.</action>
  </check>
  <check if="declined">
    <action>Record the decision — including that it was declined — in the epic's `details` closeout section. The choice should be visible to the next reader.</action>
  </check>
</step>

<step n="3" goal="Run the end reviews">
  <action>Run the epic-level review roster over the epic's delivered surface — the merged units, their requirements, and the acceptance criteria they claimed to satisfy:</action>

  | Reviewer | Agent | Looks for |
  |---|---|---|
  | Architect | `architect` | Structural drift: does the delivered shape match the architecture_description records, and did the epic leave the system coherent? |
  | Adversarial | `adversarial-reviewer` | What the epic's own reviews missed — unstated assumptions, absent observability, untested error paths |
  | Edge cases | `edge-case-hunter` | Boundary and branch conditions in the delivered behavior |

  <action>Give each reviewer the epic ref, the unit refs, and the requirement and ac refs. Reviews are read from the record graph and the merged code, not from a local report file.</action>
  <action>Collect the findings into one list, de-duplicated, before triaging.</action>
</step>

<step n="4" goal="Triage every finding">
  <action>For each finding decide exactly one disposition and say why:</action>

  | Disposition | When | Action |
  |---|---|---|
  | **Fix now** | Small, in-surface, clearly correct | Fix it within this epic; file the `issue` anyway so the change has a record |
  | **File** | Real, needs its own unit or a decision | `create_issue` with the objective evidence — what was observed, where, and how it was verified |
  | **Requirement gap** | The finding is really a missing normative statement | Author a `requirement` plus its `ac` records; the transient finding goes in the issue, the durable rule goes in the requirement |
  | **Reject** | Not a defect, or out of scope by design | Say so explicitly with the reason; do not silently drop it |

  <action>Do not invent a `kind=feature` unit to carry a fix. A fix is a `kind=fix` change_request parented to its issue with an empty `requirements` array — created by `/conclude`, not here.</action>
  <action>Report the triage as a table: finding, disposition, resulting record ref.</action>
</step>

<step n="5" goal="Verify the acceptance claim">
  <action>For each requirement the epic claimed to cover, check its acceptance criteria against what actually merged. Say plainly which criteria are met, which are partially met, and which are not.</action>
  <check if="a criterion is unmet">
    <action>This determines `done_reason` in step 6. An epic that delivered part of its scope is `partial`, not `delivered` — record it as what it is.</action>
  </check>
</step>

<step n="6" goal="Close the epic">
  <action>Write the closeout into the epic's `details`: what was delivered, what was demoed or why not, the review findings and their dispositions, the acceptance verdict per requirement, and what was deliberately left.</action>
  <action>Call `update_epic` with `status=done` and a `done_reason` that matches step 5 — `delivered`, `partial`, `superseded`, `merged`, `canceled`, or `abandoned`.</action>
  <check if="follow-on work was identified">
    <action>Leave no dangling thread: every follow-on is an `issue` or a unit under a named epic before this workflow reports completion. Report their refs.</action>
  </check>
</step>

<step n="7" goal="Hand off">
  <output>
Epic {epic_ref} — "{epic_title}" — closed as {done_reason}.
  units closed: {n}    findings filed: {refs}    requirements met: {n}/{total}

Next: `/epic retro` to capture what to change, or `/epic ready` before opening the next one.
  </output>
</step>

</workflow>
