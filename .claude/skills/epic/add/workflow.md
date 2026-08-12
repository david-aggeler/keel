---
name: epic/add
description: 'Add one feature change-request unit to a running epic. Use when the operator says "add a unit to epic N" or "one more unit for this epic".'
x-openbrain-content-hash: sha256:149bae0c1cd8efd62d2f9f11f0a36fcee1e02ec9ba5b5a6e689f87ec8aae5720
---

# /epic add — Add One Unit to a Running Epic

**Goal:** Create a single `kind=feature` unit under an existing epic — decomposition-for-one, same contract as `/epic plan`, no extra ceremony.

**Read `SKILL.md` "Record model contract" first.** The unit needs a requirement before it can exist.

## Execution

<workflow>

<step n="1" goal="Identify the target epic">
  <check if="an epic ref or sequence was provided">
    <action>Call `get_epic` to load it.</action>
  </check>
  <check if="no epic was named">
    <action>Call `list_epic` with `filter={"status":"active"}`. With exactly one hit, confirm — "Adding a unit to {epic_title} — is that right?" — and wait. With zero or several, present the list and ask.</action>
  </check>
  <action>Store {epic_ref} and {epic_title}.</action>
</step>

<step n="2" goal="Elicit the unit">
  <ask>What is the title of the new unit? Action-oriented.</ask>
  <action>Wait for the answer.</action>
  <ask>One sentence — what does it do, and what capability is it delivering?</ask>
  <action>Wait for the answer.</action>
</step>

<step n="3" goal="Resolve the requirement">
  <action>Search for an existing requirement covering that capability: `search_requirement` on the capability wording, then `search_ac`, then `search_records`. Search what the capability IS, not the phrasing the operator just used.</action>
  <action>Report what you found before writing anything.</action>
  <check if="an existing requirement covers it">
    <action>Confirm the match with the operator and use its ref.</action>
  </check>
  <check if="nothing covers it">
    <action>Say so, fetch `get_template_for dto_type=requirement`, and author it: `create_requirement` with `product`, `title`, a shall-form `statement`, `type`, `status`.</action>
    <action>Author its acceptance criteria in the same pass — `create_ac` with `parent` set to the new requirement ref.</action>
    <action>Add the requirement ref to the epic's `related_requirements` via `update_epic`.</action>
  </check>
  <halt-condition>Do not proceed to step 5 without a requirement ref. A feature unit with an empty `requirements` array is rejected at write time, at every status.</halt-condition>
</step>

<step n="4" goal="Resolve iteration and executor">
  <action>Call `list_iteration` and determine the iteration this unit belongs to. If none fits, ask whether to create one — a unit with no `iteration` never appears in a run-queue drain.</action>
  <ask>Executor for this unit — `agent` or `human`?</ask>
  <action>Wait for the answer.</action>
</step>

<step n="5" goal="Create the unit">
  <action>Call `create_change_request` with:
    - `product`, `title`, `summary` — from step 2
    - `kind` — `feature`
    - `parent` — {epic_ref}
    - `requirements` — the ref from step 3
    - `iteration`, `executor` — from step 4
    - `status` — `draft`
  </action>
  <action>Store the returned ref as {unit_ref}.</action>
</step>

<step n="6" goal="Confirm">
  <action>Call `get_change_request` for {unit_ref} and confirm `kind`, `parent`, `requirements`, `iteration`, `executor`, and `status` all read back as intended.</action>
  <output>
Added {unit_ref} — "{title}" — to {epic_title}.
  requirement: {requirement_ref}   iteration: {iteration}   executor: {executor}   status: draft

Detail it at pickup with `/change-request create`.
  </output>
</step>

</workflow>
