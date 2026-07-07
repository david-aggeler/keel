---
name: change-request/dev
description: 'Implement the unit via vertical-slice TDD. Use when the unit is approved and the operator says "dev this CR" or "implement the unit".'
---

# Dev Change Request

**Transition:** `approved → in_progress`

**Goal:** Implement the unit's `acceptance_criteria` requirements using a vertical-slice TDD loop. Each slice is one behavior, driven by two generic subagents (tester then coder).

## Execution

**1. Start**

Call `update_change_request status=in_progress` to record the transition. Set up the worktree:

```bash
bash .claude/skills/change-request/scripts/worktree-up.sh cr <seq> <slug>
```

**2. Tracer bullet**

Before the full slice loop, run a tracer bullet: pick the simplest requirement ref from `acceptance_criteria` and complete one full slice (tester + coder + annotate) end-to-end. Confirm the loop works before continuing.

**3. Slice loop**

Read and follow:

- `steps/step-01-slice-loop.md` — derive the GWT atom and public interface from each requirement ref.
- `steps/step-02-two-actor.md` — symmetric two-actor per slice: generic tester subagent then generic coder subagent.
- `steps/step-03-annotate.md` — annotate touched code and tests with DHF-REQ/DHF-TEST markers.

Repeat for each requirement ref in `acceptance_criteria`. One slice = one behavior. Do not refactor while any test is red.

**4. End of loop**

If any slice was **parked** at the 3-round green cap (see
`steps/step-02-two-actor.md`), a parked slice halts the unit: do **not** announce
implementation complete and do **not** suggest `review`. Stop at the parked unit
(status stays `in_progress`) and point the owner at the recorded blocker
(`formal_review` + `details` note). The owner resumes the parked slice later.

Already-green slices stay committed; the parked slice's partial work is left
uncommitted for the owner to review. The `formal_review` blocker row is the
parked signal — it distinguishes a parked-blocked unit from one still progressing.
A unit at `in_progress` with a `formal_review` blocker row naming an unresolved
slice is parked; without that row it is still running.

Otherwise, when all slices are green and annotated, inform the operator that
implementation is complete and suggest running `review`.
