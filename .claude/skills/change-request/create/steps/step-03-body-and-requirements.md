# Step 03 — Body Emission and Requirement Extraction

**Goal:** Emit the 4-section unit body, then extract or update `requirement` records from the behavior statements collected in the interview.

## Body sections

Emit a unit body with exactly these four sections (first three are required):

1. **Context** — why this unit exists now: the trigger, problem, and parent rationale. One focused paragraph.
2. **Scope** — in/out boundary: what this unit touches, not what the product does.
3. **Decisions** — the q|a GFM table from step 02, one row per question with the owner's confirmed answer.
4. **References** — non-ref links (explorations, external specs, prior art). Omit if empty.

No requirement or AC prose in the body. Behavioral statements go into `requirement` records, not the body.

## Determine `kind` first — it decides where requirements live

`kind` is required on every `change_request` write (`{fix, feature}`) and its parent/requirements shape is a server-enforced structural invariant (see "Resolving the acceptance contract (kind-aware)" in `SKILL.md`). Fix it before extracting requirements:

- **`kind: feature`** — `parent` is an `epic` or absent (**never** an issue). The unit's acceptance contract lives in the CR's own `requirements` array, authored below.
- **`kind: fix`** — `parent` **must** be an `issue`. The acceptance contract is the **parent issue's `related_requirements`**; the CR's own `requirements` array **MUST be empty** (a non-empty `requirements` on a fix is rejected). Do **not** run the extraction loop below onto the CR — the requirements are authored on the issue (via the issue skill). Skip to "Create the record" with `requirements` empty.

## Requirement extraction (`kind: feature` only)

After the body is drafted, extract behavior statements from the interview answers.

**This step is mandatory for `kind: feature` — every behavior statement must produce a `requirement` record reference in the CR's `requirements` field. It is not drafting guidance; it is an ordered sequence of MCP calls.**

For EVERY behavior statement identified:

1. **Call `search_requirement` FIRST — mandatory, every time, no exceptions.**
   Do not create a new record without searching first. A duplicate requirement record is a dual source of truth (T3).

2. Based on the search result:
   - **Found with equivalent scope:** call `update_requirement` on the existing record (add GWT atom if new; do not duplicate). Add the existing ref to `requirements` immediately.
   - **Found with different scope:** call `create_requirement` for the distinct statement. Add the new ref to `requirements` immediately.
   - **Not found:** call `create_requirement` (include: `statement`, `acceptance_criteria` as GWT atoms, `type`, `implements` ref to the parent user_need if known). Add the new ref to `requirements` immediately.

   > `acceptance_criteria` in the `create_requirement` call is the **requirement's own** GWT-atom field — not the CR field. Do not rename it.

3. **Refs into `requirements` happen in the same step as creation/update** — do not batch them at the end. Each behavior statement: search → create-or-update → ref into `requirements`.

Complete this loop for every behavior statement before calling `create_change_request`. For a `kind: feature` unit the `requirements` field must be non-empty when the record is created.

## Create the record

Call `create_change_request` with:
- `title`, `summary`, `kind` (`fix` | `feature`), `parent` (an `issue` for `fix`; an `epic` or absent for `feature`), `status: draft`
- `details` body (the 4-section markdown)
- `requirements`: for `kind: feature`, the list of requirement refs collected above (non-empty); for `kind: fix`, **empty** (the contract is the parent issue's `related_requirements`)
