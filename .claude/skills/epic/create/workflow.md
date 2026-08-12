---
name: epic/create
description: 'Turn an approved requirement set into epics under a dd_plan, then decompose each epic into feature units. Use when the user says "create the epics" or "decompose the plan into epics".'
x-openbrain-content-hash: sha256:fe91eb2efe739c6b7086300f3531d68d1c3d122c853cbd6b16f4500893e9059a
---

# /epic create — Requirement Set to Epics and Units

**Goal:** Partition a product's approved requirement set into epics, each anchored on a `dd_plan`, and decompose each epic into `kind=feature` change-request units that are immediately drainable.

**Your role:** You are decomposing with the operator, not for them. You bring partitioning discipline; they bring product priority. Disagree out loud when the partition looks wrong.

**Read `SKILL.md` "Record model contract" first.** In particular: a unit cannot exist before its requirement does.

## Inputs — all from the record graph

There are no input files. Load, in this order:

| What | How |
|---|---|
| Glossary | `list_glossary_term` (`include_summary=true`, `limit=100`) |
| Product intent | `list_vision`, `list_user_need`, `list_intended_use` |
| The requirement set | `list_requirement` with `include_summary=true`, then `list_ac` per requirement of interest |
| Existing epics | `list_epic` — you are extending a tree, not starting one |
| Backlog candidates | `list_epic_backlog` (`status=proposed` / `ready`) |
| Design context | `list_architecture_description`, `list_interface_spec`, `list_design_decision` |

If `list_requirement` returns nothing approved for the product, **stop**. Epics partition requirements; with no requirements there is nothing to partition. Say so and point the operator at requirement authoring.

## Execution

<workflow>

<step n="1" goal="Establish the dd_plan">
  <action>Call `list_dd_plan` for the product.</action>
  <check if="an active dd_plan exists">
    <action>Use it. Store {plan_ref}. Report which plan the epics will hang from.</action>
  </check>
  <check if="no dd_plan exists">
    <action>Explain that `epic.plan` is a required dd_plan ref and cannot be prose.</action>
    <action>Fetch `get_template_for dto_type=dd_plan`, elicit title, scope_summary, and phases from the operator, and call `create_dd_plan` with `status=active`.</action>
    <action>Store the returned ref as {plan_ref}.</action>
  </check>
</step>

<step n="2" goal="Build the coverage map">
  <action>Assemble every approved-or-later requirement for the product into a working coverage map: requirement ref → title → the user_need it serves.</action>
  <action>Present the map to the operator as a markdown table and ask which requirements are in scope for this round of epics. Explicitly ask what is deliberately OUT.</action>
  <action>Wait for the answer. Record the out-of-scope set — it becomes each epic's `exclusions`.</action>
  <halt-condition>Do not invent requirements to fill a gap you perceive. If a needed capability has no requirement, name the gap and ask whether to author the requirement now (step 4 does this) or defer the epic.</halt-condition>
</step>

<step n="3" goal="Design the epic partition">
  <action>Propose a partition of the in-scope requirements into epics. Each epic must:
    - deliver a coherent slice of user-facing value, stated in `user_value`
    - own a disjoint subset of requirements — no requirement in two epics
    - name what it excludes
    - be sequenced: state which epics depend on which, and why
  </action>
  <action>Present the proposed partition as a table (epic title, user_value, requirement refs, exclusions, depends_on) and ask for approval.</action>
  <action>Iterate until the operator approves. Do not create records before approval.</action>
  <check if="the partition leaves an in-scope requirement uncovered">
    <action>Say so explicitly and resolve it before proceeding. Silent drop is the failure mode this step exists to prevent.</action>
  </check>
</step>

<step n="4" goal="Create the epic records">
  <action>Fetch `get_template_for dto_type=epic` and follow its structure for `details`.</action>
  <action>For each approved epic call `create_epic` with:
    - `product`, `title`, `summary`, `details`
    - `plan` — {plan_ref} (required; a dd_plan ref, never prose)
    - `status` — `planned`
    - `user_value`
    - `related_requirements` — the epic's requirement refs
    - `covers_user_needs` — only where the epic genuinely satisfies the need
    - `inclusions` / `exclusions`
    - `acceptance_criteria` — epic-level, bullet form
    - `depends_on` — earlier epic refs where sequencing demands it
  </action>
  <action>Record each returned ref. Report the created epics as a table.</action>
  <check if="an epic_backlog entry motivated this epic">
    <action>Call `update_epic_backlog` setting `status=promoted` and `promoted_to` to the new epic ref.</action>
  </check>
</step>

<step n="5" goal="Decompose each epic into units — requirement-first">
  <action>For each epic, read `plan/workflow.md` and follow it from its step 3 onward, with {epic_ref} already bound. That workflow owns the requirement-first unit creation contract; do not restate it here.</action>
  <action>Work one epic at a time and report after each.</action>
</step>

<step n="6" goal="Validate the result against the record graph">
  <action>For each created epic call `list_change_request` with `filter={"parent":"<epic_ref>"}`.</action>
  <action>Verify, and report as a table, that every unit:
    - has `kind=feature`
    - lists at least one requirement in `requirements`
    - has `parent` resolving to the intended epic
    - carries an `iteration`
    - carries an `executor`
  </action>
  <action>Verify every in-scope requirement from step 2 is referenced by at least one unit. Name any that is not — an uncovered requirement here means the epic cannot deliver what it claims.</action>
  <check if="any check fails">
    <action>Fix it now with `update_change_request` / `update_epic`. Do not report completion over a failing check.</action>
  </check>
  <action>Set each epic that is ready to begin to `status=active` via `update_epic`; leave the rest `planned`.</action>
</step>

</workflow>

## Output

Epic records under a dd_plan, feature units under each epic, and a table naming what was created. No files.
