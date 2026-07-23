# Step 04 — Convert-on-Pickup Mode

**Goal:** Resume a backlog story by creating a unit that carries the story's intent forward. This is a create mode, not a separate verb.

**Trigger:** the operator picks up a story by key or description instead of starting a fresh unit.

## Actions

1. **Load the story** — call `get_story key=<story-key>` (or the applicable lookup).
   Capture: key, epic ref, title, summary, any requirement refs in `acceptance_criteria`.

2. **Reshape into 4 sections** — draft the unit body:
   - **Context:** why this story existed; the trigger from the original story statement.
   - **Scope:** in/out boundary carried from the story's scope section (if present).
   - **Decisions:** a fresh Decisions table; the story's open architectural decisions become rows.
   - **References:** link back to the original story key.

3. **Requirement extraction** — treat each acceptance criterion from the story as a behavior statement.
   Apply the search-first rule from `step-03-body-and-requirements.md`: search, then update or create.
   Collect refs into the CR's `requirements` field.

4. **Create the unit** — a story-converted unit is `kind: feature` (its parent is an epic). Call `create_change_request` with:
   - `title` and `summary` carried from the story
   - `kind: feature`
   - `parent` set to the story's epic ref (if present)
   - `status: draft`
   - `details` body (the 4-section markdown)
   - `requirements` (requirement refs extracted above)

5. **Mark the story superseded** — call `update_story` with:
   - `status: done` and `done_reason: superseded`
   - `related`: add a ref to the newly created change request

6. Inform the operator: "Story {key} marked superseded; unit {cr-ref} created and ready for `plan`."

## Notes

- This is manual field-carry, not a copy primitive. The body is reshaped, not blindly copied.
- The type changes shape (story body → 4-section unit body), so a field-for-field copy would carry the wrong structure.
- Done stories are left untouched; only backlog/ready/in-progress stories are convert candidates.
