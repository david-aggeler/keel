# Step 01 — Merge Half

**Transition:** `ready_to_merge → merged`

**Goal:** Record the code change ref and move the unit to `merged`. This is the normal path for code changes.

## Actions

**1. Confirm merge readiness**

Confirm the change request is currently `ready_to_merge`. If it is still
`implementation_review`, return to the `review` verb and complete review before
running close.

Ask the operator:

> Has the branch been merged to main?
> - Provide the merge commit SHA (or PR merge ref).
> - Or, if this unit is a non-code deliverable (doc change, superseded work, etc.), select the appropriate `close_reason` from the reference table in the verb router.

**2. Normal path (`close_reason: merged`)**

Call `update_change_request`:
- `status: merged`
- `code_change_ref`: the merge commit SHA
- `close_reason: merged`

**3. Non-code carve-out**

If `close_reason` is one of `canceled`, `abandoned`, `rejected`, or `superseded`, the schema gate does not require `code_change_ref`. Call `update_change_request`:
- `status: merged`
- `close_reason`: the selected exempt reason

Proceed to `step-02-gate.md`.
