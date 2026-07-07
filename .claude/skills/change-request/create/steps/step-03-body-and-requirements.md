# Step 03 — Body Emission and Requirement Extraction

**Goal:** Emit the 4-section unit body, then extract or update `requirement` records from the behavior statements collected in the interview.

## Body sections

Emit a unit body with exactly these four sections (first three are required):

1. **Context** — why this unit exists now: the trigger, problem, and parent rationale. One focused paragraph.
2. **Scope** — in/out boundary: what this unit touches, not what the product does.
3. **Decisions** — the q|a GFM table from step 02, one row per question with the owner's confirmed answer.
4. **References** — non-ref links (explorations, external specs, prior art). Omit if empty.

No requirement or AC prose in the body. Behavioral statements go into `requirement` records, not the body.

## Requirement extraction

After the body is drafted, extract behavior statements from the interview answers.

**This step is mandatory — every behavior statement must produce a `requirement` record reference in `acceptance_criteria`. It is not drafting guidance; it is an ordered sequence of MCP calls.**

For EVERY behavior statement identified:

1. **Call `search_requirement` FIRST — mandatory, every time, no exceptions.**
   Do not create a new record without searching first. A duplicate requirement record is a dual source of truth (T3).

2. Based on the search result:
   - **Found with equivalent scope:** call `update_requirement` on the existing record (add GWT atom if new; do not duplicate). Add the existing ref to `acceptance_criteria` immediately.
   - **Found with different scope:** call `create_requirement` for the distinct statement. Add the new ref to `acceptance_criteria` immediately.
   - **Not found:** call `create_requirement` (include: `statement`, `acceptance_criteria` as GWT atoms, `type`, `implements` ref to the parent user_need if known). Add the new ref to `acceptance_criteria` immediately.

3. **Refs into `acceptance_criteria` happen in the same step as creation/update** — do not batch them at the end. Each behavior statement: search → create-or-update → ref into `acceptance_criteria`.

Complete this loop for every behavior statement before calling `create_change_request`. The `acceptance_criteria` field must be non-empty when the record is created.

## Create the record

Call `create_change_request` with:
- `title`, `summary`, `parent` (if parent mode is epic or issue), `status: draft`
- `details` body (the 4-section markdown)
- `acceptance_criteria` (list of requirement refs collected above)
