---
name: change-request/status
description: 'Show change request status legend and progress. Use when the operator says "cr list", "show CR status", or "what is the status of this unit".'
x-openbrain-content-hash: sha256:90e1b97084d9fb595c9778f80bc5ed5c02dcb6ded62645ec8589e55bde9b4d48
---

# Change Request Status

**Goal:** Read-only legend and progress summary. No status transitions.

## Status legend

| Status | Meaning |
|---|---|
| `draft` | Idea detailed. Body in the 4-section shape; requirements extracted. Freely editable. |
| `approved` | Spec ratified. `executor`, `transition_gate`, `auto_merge` stamped. Queue-eligible if `executor=agent`. |
| `in_progress` | Implementation underway. Worktree active; slice loop running. |
| `implementation_review` | Review underway. Advisory DHF coverage is being checked and `formal_review` records are being produced. |
| `ready_to_merge` | Review complete. Findings are resolved or accepted for this unit, and the branch is ready for the close merge half. |
| `merged` | Merged. `code_change_ref` recorded; the declared `transition_gate` rung passed. |
| `closed` | Learned/verified. `close_reason` **and** `code_change_ref` set; `issue_fix` rows created if parent was an issue. **Immutable.** |
| `on_hold` | Parked. Orthogonal to the quality axis — entered from any non-closed state, returns to any non-closed state. **`block_reason` is required**; deferral fields (`deferred_reason`, `deferred_until`) add the scheduling detail. |

These eight are the whole enum. `planned` is not a member — it left with the target lifecycle (CR-643), so no unit can be read or written carrying it, and a `planned` filter matches nothing.

**Status axis:** quality/maturity of the record.
**Scheduling axis:** refs + deferral fields + `on_hold`. Use `on_hold` when scheduling changes, not status.

## Park / Resume

To park a unit on any non-closed status: call `update_change_request status=on_hold` with **`block_reason`** — the server requires it and rejects the write without it — plus `deferred_reason` / `deferred_until` for the scheduling detail.

A park driven by this verb is a **human-side backlog hold**, so the value is one of `user_choice`, `low_importance`, `no_capacity`, or one of the universal reasons `needs_owner_input` / `dependency_open` / `precondition_unmet`. The agent-execution reasons (`findings_remediable`, `capability_mismatch`, `no_evidence_noop`, `executor_error`) belong to the orthogonal blocked flag and must not be used to park a unit here.

To resume: call `update_change_request` restoring the previous status (or `draft` to return to the beginning).

## Progress

Call `list_change_request product=<product>` to enumerate units by status. Filter by `parent` to see units for a specific epic or issue.
