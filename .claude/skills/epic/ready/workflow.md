---
name: epic/ready
description: 'Readiness gate over the record graph before an epic opens for implementation. Use when the user says "are we ready for the next epic" or "run the readiness check".'
x-openbrain-content-hash: sha256:4bdea86174319b84722fe27f9408b313f5ac1797b915b4a7146264a9470a41f9
---

# /epic ready — Readiness Gate

**Goal:** Decide whether an epic is ready to open for implementation, from the record graph. The verdict is READY or NOT READY with named blockers — never a qualified maybe.

**The gate reads records.** Intent, design, and interface contracts are `vision`, `user_need`, `requirement`, `ac`, `architecture_description`, `interface_spec`, and `design_decision` records. There is no document inventory step, no file discovery, and no readiness report written to disk — the verdict is a `formal_review` record.

## Execution

<workflow>

<step n="1" goal="Identify the epic">
  <check if="an epic was named">
    <action>Call `get_epic`.</action>
  </check>
  <check if="no epic was named">
    <action>Call `list_epic filter={"status":"planned"}` and offer the next one in sequence.</action>
  </check>
  <action>Store {epic_ref}. Report the epic's `plan`, `related_requirements`, `covers_user_needs`, and `exclusions`.</action>
</step>

<step n="2" goal="Check the intent chain">
  <action>Walk the chain the epic claims: does each `covers_user_needs` entry resolve to a real `user_need`, and does each of those trace to product intent (`vision`, `intended_use`)?</action>
  <action>Report any epic claiming to cover a need that no requirement under it addresses — a coverage assertion with nothing behind it is the failure this check exists for.</action>
</step>

<step n="3" goal="Check the requirement set">
  <action>For each ref in `related_requirements` call `get_requirement` and `list_ac`.</action>
  <action>Report per requirement: status, whether the statement is in shall-form, and how many `ac` records it has.</action>
  <check if="a requirement has no acceptance criteria">
    <action>Blocker. A requirement with no `ac` cannot be verified, so a unit implementing it cannot be reviewed against anything.</action>
  </check>
  <check if="a requirement is below approved">
    <action>Blocker for approval, not for planning. A unit may be created at `draft` against it, but the unit cannot reach `approved` until at least one of its requirements is `approved` or later. Name each one.</action>
  </check>
</step>

<step n="4" goal="Check the design context">
  <action>For the surface this epic touches, check that the relevant `architecture_description` and `interface_spec` records exist and are current. Name specifically what is missing — "the boundary this epic crosses has no interface_spec chapter" is a blocker; "the architecture tree is thin in general" is not.</action>
  <action>Check `list_design_decision` for decisions this epic depends on that are still open. An epic whose shape depends on an undecided question is not ready.</action>
</step>

<step n="5" goal="Check the units">
  <action>Call `list_change_request filter={"parent":"<epic_ref>"}`.</action>
  <action>Verify each unit: `kind=feature`, at least one requirement ref, `parent` not an issue, `iteration` set, `executor` set.</action>
  <check if="the epic has no units">
    <action>Blocker — run `/epic plan` first.</action>
  </check>
  <check if="a unit lacks iteration or executor">
    <action>Blocker. It would be approved and then never picked up.</action>
  </check>
</step>

<step n="6" goal="Check sequencing">
  <action>Check the epic's `depends_on` — is every prerequisite epic `done`?</action>
  <action>Check unit-level `depends_on` for cycles and for dependencies on units outside this epic that are still open.</action>
</step>

<step n="7" goal="Render the verdict">
  <action>Report a table: check, result, blocker detail.</action>

  | Check | Result | Detail |
  |---|---|---|
  | Intent chain | | |
  | Requirement set | | |
  | Acceptance criteria | | |
  | Design context | | |
  | Units | | |
  | Sequencing | | |

  <action>Then a single verdict line: **READY** or **NOT READY**, with the blockers named as record refs.</action>
  <halt-condition>Do not issue a conditional verdict. If a blocker exists, the epic is NOT READY and the blocker is what has to change.</halt-condition>
</step>

<step n="8" goal="Record the verdict">
  <action>Fetch `get_template_for dto_type=formal_review` and call `create_formal_review` with `type=other`, `type_other` naming it an epic readiness review, `status=completed`, `related` pointing at {epic_ref}, and the table plus verdict in the body.</action>
  <check if="NOT READY">
    <action>File each blocker that needs work as an `issue`, or author the missing `requirement` / `ac` directly if that is what is missing. Report the refs, then say which verb closes each gap.</action>
  </check>
  <check if="READY">
    <action>Set the epic to `status=active` with `update_epic` if the operator confirms it should open now.</action>
  </check>
</step>

</workflow>
