---
name: change-request/status
description: 'Show change request status legend and progress. Use when the operator says "cr list", "show CR status", or "what is the status of this unit".'
---

# Change Request Status

**Goal:** Read-only legend and progress summary. No status transitions.

## Status legend

| Status | Meaning |
|---|---|
| `draft` | Idea detailed. Body in the 4-section shape; requirements extracted. Freely editable. |
| `approved` | Spec ratified. `executor`, `merge_gate`, `auto_merge` stamped. Queue-eligible if `executor=agent`. |
| `in_progress` | Implementation underway. Worktree active; slice loop running. |
| `implementation_review` | Review underway. Advisory DHF coverage is being checked and `formal_review` records are being produced. |
| `ready_to_merge` | Review complete. Findings are resolved or accepted for this unit, and the branch is ready for the close merge half. |
| `merged` | Merged. `code_change_ref` recorded; merge gate commands passed. |
| `closed` | Learned/verified. `close_reason` set; `issue_fix` rows created if parent was an issue. **Immutable.** |
| `on_hold` | Parked. Orthogonal to the quality axis — entered from any non-closed state, returns to any non-closed state. Set deferral fields (`deferred_reason`, `deferred_until`) to explain why. |
| `planned` | **Legacy — never written in new records.** Pre-unit-model state of unknown ratification depth; cannot carry `executor`/`merge_gate`/`auto_merge` stamps, so never queue-eligible. If a unit is `planned`, re-enter at `plan` to ratify the spec and stamp it `approved`. |

**Status axis:** quality/maturity of the record.
**Scheduling axis:** refs + deferral fields + `on_hold`. Use `on_hold` when scheduling changes, not status.

## Park / Resume

To park a unit on any non-closed status: call `update_change_request status=on_hold` and set `deferred_reason` / `deferred_until`.

To resume: call `update_change_request` restoring the previous status (or `draft` to return to the beginning).

## Progress

Call `list_change_request product=<product>` to enumerate units by status. Filter by `parent` to see units for a specific epic or issue.
