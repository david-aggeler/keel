---
name: epic/plan
description: 'Decompose an active epic into change-request unit records. Use when the user says "plan epic N" or "break down epic N into units"'
---
<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 -->

# Epic Plan Workflow

**Goal:** Decompose an active epic record into thin change-request unit records by enumerating the intended units and calling `create_change_request` for each one. Emits no tracking artifact.

**Your Role:** You are a Developer/Product Owner facilitating unit decomposition. You read the epic record, work with the operator to plan the units, and create thin husks inline.

- Communicate all responses in {communication_language}
- The output of this workflow is `change_request` records with `status=draft`, linked to the parent epic — not a file
- Unit detailing (4-section body, requirement extraction) is owned by `/change-request create` at pickup

## Execution

<workflow>

<step n="1" goal="Load and review the active epic">
  <action>Load {project_context} for project-wide patterns and conventions (if exists)</action>

  <check if="{epic_ref} is provided">
    <action>Call `get_epic` for {epic_ref} to load the full epic record</action>
  </check>

  <check if="{epic_ref} is not provided">
    <action>Call `list_epic` with `filter={"status":"active"}` ordered by sequence</action>
    <check if="no active epic found">
      <output>No active epic found. Options:
        1. Provide a specific epic ref or sequence number
        2. Run `/epic create` to create epics
        3. Run `update_epic` to set an epic to `active` status
      </output>
      <action>HALT</action>
    </check>
    <action>If exactly one active epic, use it. If multiple, present list and ask which to decompose.</action>
    <action>Call `get_epic` for the selected epic</action>
  </check>

  <action>Display the epic to the operator:
    - Title, summary, status
    - Details and plan sections (which describe the intended units)
  </action>

  <action>Call `list_change_request` with `filter={"parent":"<epic_ref>"}` to see how many units already exist for this epic</action>
  <action>Report existing unit count and status breakdown to the operator</action>
</step>

<step n="2" goal="Enumerate intended units">
  <action>From the epic `details` and `plan` fields, extract the list of intended units</action>
  <action>If the epic record describes units (title, rough scope), collect them into a planned unit list</action>

  <check if="no unit list is evident in the epic">
    <action>Work with the operator to decompose the epic: identify coherent behaviors or deliverables, logical flow, and sizing</action>
    <action>Present proposed unit list for approval before creating records</action>
  </check>

  <action>For each intended unit, establish:
    - Unit title (clear, action-oriented)
    - Summary (one sentence: what it does and which FRs it covers)
  </action>

  <action>Present the full unit list to the operator for confirmation</action>
  <ask>Do these units correctly decompose the epic? Approve [a] or edit [e]?</ask>
  <action>Iterate until operator approves the unit list</action>
</step>

<step n="3" goal="Create thin unit husks">
  <action>For each approved unit, call `create_change_request` with:
    - `title` — the unit title
    - `summary` — one-sentence scope
    - `parent` — the epic ref
    - `status` — `draft`
  </action>
  <action>Record the returned ref for each unit</action>

  <check if="all units created">
    <action>Call `list_change_request` with `filter={"parent":"<epic_ref>"}` to confirm all expected unit records exist</action>
    <action>Verify each unit has `status=draft` and `parent` ref resolves correctly</action>
  </check>
</step>

<step n="4" goal="Report and validate">
  <action>Read fully and follow `./checklist.md` to validate unit-decomposition coverage</action>

  <output>**Epic Plan Complete!**

    **Epic:** {epic_title} ({epic_ref})
    **Units Created:** {unit_count}

    **Next Steps:**
    1. Review unit records with `/epic status` or `list_change_request filter={"parent":"<epic_ref>"}`
    2. Run `/change-request create` to begin detailing the first `draft` unit
    3. Run `/epic status` at any time to check progress
  </output>
</step>

</workflow>
