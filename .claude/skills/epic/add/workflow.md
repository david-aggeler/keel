---
name: epic/add
description: 'Add one new thin unit to a running epic. Use when the operator says "add a unit to epic N" or "one more task for this epic".'
---

# /epic add — Add a Unit to a Running Epic

**Goal:** Create one thin change-request husk (`status=draft`) linked to an existing epic. This is decomposition-for-one — the same inline creation pattern used by `/epic create` step 3, with zero extra ceremony.

## Execution

<workflow>

<step n="1" goal="Identify the target epic">
  <check if="epic ref or sequence provided">
    <action>Call `get_epic` for the provided ref or sequence to load the epic record.</action>
  </check>
  <check if="no epic ref provided">
    <action>Call `list_epic` with `filter={"status":"active"}` to find the active epic.</action>
    <check if="exactly one active epic found">
      <action>Use it. Confirm with the operator: "Adding a unit to {epic_title} — is that right?"</action>
      <action>Wait for confirmation.</action>
    </check>
    <check if="multiple active epics or none">
      <action>Present the list and ask the operator which epic to target.</action>
    </check>
  </check>
  <action>Store {epic_ref} and {epic_title}.</action>
</step>

<step n="2" goal="Elicit the unit">
  <ask>What is the title of the new unit? (clear, action-oriented)</ask>
  <action>Wait for the operator to provide the title.</action>
  <ask>One-sentence summary — what does it do and which requirement does it address?</ask>
  <action>Wait for the operator to provide the summary.</action>
</step>

<step n="3" goal="Create the thin husk">
  <action>Call `create_change_request` with:
    - `title` — the unit title from step 2
    - `summary` — the one-sentence summary from step 2
    - `parent` — {epic_ref}
    - `status` — `draft`
  </action>
  <action>Record the returned ref as {unit_ref}.</action>
  <output>Unit created: {unit_ref} — "{unit_title}" under {epic_title}.

To detail this unit at pickup, run `/change-request create` and reference {unit_ref}.</output>
</step>

</workflow>
