<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 -->
# Step 3: Create Thin Unit Records

## STEP GOAL

For each approved epic, inline-create thin change-request husks via `create_change_request` — one record per unit, `status=draft`, title and summary only. FR coverage is asserted at title+summary granularity here; full detailing happens at pickup via `/change-request create`.

**This step does NOT drive units through their lifecycle.** One unit = one fresh session. The decomposition creates N husks in one sitting; that is record creation, not orchestration.

## EXECUTION RULES

- Process epics in sequence.
- For each unit, call `create_change_request` directly — do NOT dispatch `/change-request` here.
- Set `status=draft`, `parent=<epic_ref>`, `title=<title>`, `summary=<one-sentence scope>`.
- Do NOT author acceptance criteria or detailed body at this stage (G5). Detail-at-pickup: see `/change-request create`.
- FR coverage is asserted at title+summary granularity: every FR from Step 2 must be traceable to at least one unit title or summary.

## DECOMPOSITION PROCESS

### 1. Load Approved Epic Structure

Review context from Steps 1 and 2:

- Approved epics list and their epic refs (from `create_epic`)
- FR coverage map from Step 2
- Any NFRs or additional requirements relevant to each epic

### 2. Explain Decomposition Approach

For each epic, decompose into the minimal set of units that covers all FRs. Each unit should:

- Be scoped to a single coherent behavior or deliverable
- Be completable in one implementation session without depending on future units within the same epic
- Have a title and summary that make the FR coverage traceable (no FR should be invisible from this list)

### 3. Process Epics Sequentially

For each epic:

#### A. Unit Breakdown

Work with the operator to identify the units. For each unit establish:

- **Title**: Clear, action-oriented (e.g., "Add user authentication endpoint")
- **Summary**: One sentence describing what it does and which FRs it covers

Present the proposed unit list to the operator.

Ask: "Does this unit list cover all FRs for this epic at title+summary granularity? Approve, edit, or note missing coverage."

Wait for confirmation before creating records.

#### B. Create Thin Husks Inline

For each approved unit, call `create_change_request` with:

- `title` — the unit title
- `summary` — one-sentence scope
- `parent` — the epic ref
- `status` — `draft`

Record the returned ref for the FR coverage check.

#### C. FR Coverage Assertion

After creating all units for this epic, verify:

- Every FR assigned to this epic maps to at least one unit by title or summary.
- No FR is invisible from the unit list.

If a gap is found, create an additional thin husk to cover it before proceeding.

#### D. Epic Completion

After all units for an epic are created:

- Display: epic title, count of units created, list of refs.
- Confirm FR coverage is complete.
- Ask the operator to confirm before moving to the next epic.

### 4. Final Coverage Check

After all epics are processed:

- Call `list_change_request` filtered by parent for each epic ref to confirm all expected unit records exist.
- Verify all FRs across all epics are covered at title+summary granularity.

### 5. Continue

When coverage is confirmed, read fully and follow: `./step-04-final-validation.md`
