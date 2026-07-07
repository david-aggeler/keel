---
name: change-request/create
description: 'Create a new change request (unit of implementation). Use when the user says "create a CR", "start a unit", or "implement this".'
---

# Create Change Request

**Goal:** Establish a new unit-of-implementation record with a 4-section body, requirements extracted, and fields stamped for the agent queue.

**Modes:**
- **New unit** — elicit from the operator, run the batch interview, emit the 4-section body.
- **Convert-on-pickup** — a backlog story is picked up; carry key/epic/title/summary, reshape into 4 sections, extract requirements, mark story superseded. See `steps/step-04-convert-on-pickup.md`.

## Execution

Read and follow in order:

1. `steps/step-01-dev-defaults.md` — load or bootstrap the product's dev defaults record.
2. `steps/step-02-batch-interview.md` — run the front-loaded 7+3+tier question batch.
3. `steps/step-03-body-and-requirements.md` — emit the 4-section body and extract requirements.

**Convert-on-pickup trigger:** if the operator references a backlog story (by key or description), skip steps 1–3 and follow `steps/step-04-convert-on-pickup.md` instead.

## Parent mode

Before step 1, ask:

> Is this unit for an epic, a defect fix, or a standalone chore?
> - **epic** — provide the epic ref; the unit's `parent` links to it.
> - **issue** — provide the issue ref; `close` will create `issue_fix` rows at the end.
> - **none** — standalone chore; no parent required.

Record the parent mode; it shapes the `create_change_request` call and the `close` verb's behavior.

### Issue parent — reviewed gate (hard precondition)

When the parent mode is **issue**, before running the interview call
`get_issue` on the ref and confirm `status == reviewed`. A change request may
only be created from a **reviewed** issue — this enforces that the issue's quality
has been vetted (scope, evidence, acceptance bar) and that the issue-review
workflow has established the requirement + acceptance criteria contract before
implementation work is scoped against it.

- If `status == reviewed`: proceed.
- If the issue is at any other status (`new`, `in_progress`, `closed`, …):
  **halt** and report the actual status. Triage the issue to `reviewed` first (the
  `issue` skill's review/triage path), then re-run `create`. Do not create the CR
  off an unreviewed issue.
