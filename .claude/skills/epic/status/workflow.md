---
name: epic/status
description: 'Summarize epic and unit status and surface risks. Use when the user says "check sprint status" or "show sprint status"'
---
<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 -->

# Epic Status Workflow

**Goal:** Summarize epic and unit status from live records, surface risks, and recommend the next workflow action.

**Your Role:** You are a Developer providing clear, actionable project visibility. No time estimates — focus on status, risks, and next steps.

## Execution

<workflow>

<step n="1" goal="Load all epics for the active product">
  <action>Load {project_context} for project-wide patterns and conventions (if exists)</action>
  <action>Call `list_epic` for the active product to retrieve all epic records</action>
  <check if="no epics found">
    <output>No epic records found for this product. Run `/epic create` to create the first epic.</output>
    <action>Exit workflow</action>
  </check>
  <action>Record epic list with title, sequence, and status for each epic</action>
  <action>Count epic statuses: planned {epic_planned}, active {epic_active}, done {epic_done}</action>
</step>

<step n="2" goal="Load unit counts per epic">
  <action>For each epic, call `list_change_request` with `filter={"parent":"<epic_ref>"}` to retrieve all unit records</action>
  <action>For each epic, count unit statuses: draft, approved, in_progress, implementation_review, ready_to_merge, merged, closed, on_hold</action>
  <action>Accumulate product-wide totals:
    - total_draft, total_approved, total_in_progress, total_implementation_review, total_ready_to_merge, total_merged, total_closed
  </action>
  <action>Detect risks:</action>

- IF any unit has status `implementation_review`: suggest `/change-request review`
- IF any unit has status `ready_to_merge`: suggest `/change-request close` (the unit is reviewed and waiting to land)
- IF any unit has status `in_progress` AND no units have status `approved`: recommend staying focused on active unit
- IF all epics have status `planned` AND no units exist: prompt `/epic create`
- IF any epic has status `active` but has no associated units: warn "active epic has no units"
</step>

<step n="3" goal="Select next action recommendation">
  <action>Pick the next recommended workflow using priority, ordered by epic sequence then unit creation date:</action>
  1. If any unit status == `in_progress` → recommend `/change-request dev` for the first in-progress unit
  2. Else if any unit status == `ready_to_merge` → recommend `/change-request close` for the first ready-to-merge unit
  3. Else if any unit status == `implementation_review` → recommend `/change-request review` for the first in-review unit
  4. Else if any unit status == `approved` → recommend `/change-request dev`
  5. Else if any unit status == `draft` → recommend `/change-request create` to detail the first draft unit
  6. Else if any epic status == `active` and all units are `closed` → recommend `/epic retro`
  7. Else → All implementation items done; congratulate the user.
  <action>Store selected recommendation as: next_unit_ref, next_workflow</action>
</step>

<step n="4" goal="Display summary">
  <output>
## Epic and Unit Status

**Epics:** planned {epic_planned}, active {epic_active}, done {epic_done}

**Units (product-wide):** draft {total_draft}, approved {total_approved}, in_progress {total_in_progress}, implementation_review {total_implementation_review}, ready_to_merge {total_ready_to_merge}, merged {total_merged}, closed {total_closed}

**Next Recommendation:** {next_workflow} ({next_unit_ref})

  </output>
</step>

<step n="5" goal="Offer actions">
  <ask>Pick an option:
1) Run recommended workflow now
2) Show all units grouped by status
3) Show all epics with unit counts
4) Exit
Choice:</ask>

  <check if="choice == 1">
    <output>Run `{next_workflow}`.
If the command targets a unit, reference `{next_unit_ref}` when prompted.</output>
  </check>

  <check if="choice == 2">
    <action>Call `list_change_request` product-wide, group results by status</action>
    <output>
### Units by Status
- Draft: {units_draft}
- Approved: {units_approved}
- In Progress: {units_in_progress}
- Implementation Review: {units_implementation_review}
- Ready To Merge: {units_ready_to_merge}
- Merged: {units_merged}
- Closed: {units_closed}
    </output>
  </check>

  <check if="choice == 3">
    <action>For each epic (ordered by sequence), display: title, status, unit counts by status</action>
  </check>

  <check if="choice == 4">
    <action>Exit workflow</action>
  </check>
</step>

</workflow>
