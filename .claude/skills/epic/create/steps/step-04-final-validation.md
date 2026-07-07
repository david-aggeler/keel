# Step 4: Final Validation

## STEP GOAL

To validate complete coverage of all requirements and ensure unit records are ready for development.

## EXECUTION RULES

- Validate every FR has unit record coverage at title+summary granularity.
- Query records via `list_epic` and `list_change_request` to confirm what was created.
- Do not skip any validation checks.

## VALIDATION PROCESS

### 1. FR Coverage Validation

Call `list_epic` for the active product to retrieve all created epics. For each epic, call `list_change_request` with `filter={"parent":"<epic_ref>"}` to retrieve all units. Cross-reference against the FR coverage map from Step 2 to ensure EVERY FR is covered:

**CRITICAL CHECK:**

- Go through each FR from the requirements inventory.
- Verify it is traceable to at least one unit by title or summary.
- No FR should be left without a unit.

### 2. Architecture Implementation Validation

**Check for Starter Template Setup:**

- Does the Architecture document specify a starter template?
- If YES: the first unit should be "Set up initial project from starter template".

**Database/Entity Creation:**

- Tables/entities should be created only when needed by the unit that first requires them.

### 3. Unit Quality Validation

For each unit (inspect via `get_change_request`):

- `status` is `draft` — units are thin husks at this stage.
- `title` is clear and action-oriented.
- `summary` is one sentence scoping the unit.
- `parent` ref resolves to the correct parent epic.
- The unit does not forward-depend on units later in the sequence.

Detail-at-pickup: operators run `/change-request create` when picking up a unit to author the 4-section body and extract requirements.

### 4. Epic Structure Validation

- Epics deliver user value, not technical milestones.
- Dependencies flow naturally across epics.
- No epic requires a future epic's deliverables to function.

### 5. Complete Validation

If all validations pass:

- Confirm all epic records have status `planned` and all unit records have status `draft`.
- If any record needs correction, call `update_epic` or `update_change_request` as needed.

Confirm with the operator that the unit backlog is complete.

When confirmed, the workflow is complete. Use `/epic status` to see the full picture, or run `/change-request create` to begin detailing the first unit.

Upon Completion of task output: offer to answer any questions about the Epics and Units.

## On Complete

If the resolved `workflow.on_complete` is non-empty, follow it as the final terminal instruction before exiting.
