---
name: change-request/correct
description: 'Make a structured change to a non-closed change request. Use when the operator says "correct this CR", "change the scope", or "update a decision".'
---

# Correct Change Request

**Goal:** Classify the change, edit at the source of truth, re-confirm only what changed.

**Works in:** any non-closed status.

**On `closed`:** halt. "The unit is closed and immutable. Reopen it first: `update_change_request status=draft reopen_reason=<reason>`."

## Classification

First, classify what is changing:

| Change type | Source of truth | Action |
|---|---|---|
| **Decision change** — a Decisions-table row answer changes | Unit body (Decisions section) | Edit the row in the body; call `update_change_request details=<updated body>`. If post-`approved`, re-confirm the changed rows with the owner (micro-batch). |
| **Scope change** — in/out boundary changes | Unit body (Scope section) | Edit the Scope section; call `update_change_request`. If the scope change affects requirements, apply the requirement change flow below. |
| **Requirement change** — a requirement statement or GWT atoms change | `requirement` record | Call `update_requirement` on the affected record. The unit body does not restate requirements — the record is the source of truth (T3). |
| **Task change** — a task status or detail changes | `task` record | Call `update_task`. |
| **Cross-cutting change** — affects multiple units or the product design | `design_decision` record | Create or update a `design_decision` record. Reference it from the Decisions table of each affected unit. |

## No sibling records

Do not create amendment records, correction records, or sibling change requests to track edits. Gitea git history is the audit trail. The vault's git log records every change with its timestamp and author; that is sufficient.

## Post-approved micro-batch

If the unit is `approved` or later and a decision or scope row changes, re-confirm only the changed rows with the owner:

> These rows were updated: {changed rows}. Confirm or override.

Do not re-run the full interview. Only the delta needs owner eyes.

## Park / Resume

To park: call `update_change_request status=on_hold` and set `deferred_reason` / `deferred_until`.

To resume: call `update_change_request` restoring the previous status.
