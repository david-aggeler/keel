# Step 01 — Merge Half

**Transition:** `ready_to_merge → merged`

**Goal:** Record the code change ref and move the unit to `merged`. This is the normal path for code changes.

## Actions

**1. Confirm merge readiness**

Confirm the change request is currently `ready_to_merge`. If it is still
`implementation_review`, return to the `review` verb and complete review before
running close.

If this unit is a non-code deliverable (doc change, superseded work, etc.), skip
to step 3 and select the appropriate `close_reason` from the reference table in
the verb router.

**2. Normal path (`close_reason: merged`) — merge with the client verb**

Do not hand-collect a SHA and do not type a merge incantation. Run the mechanical
merge verb, which executes the product's **committed** `merge_command` and reports
the commit:

```bash
openbrain-client merge <branch>
```

Read `code_change_ref=<sha>` from its two-line output. `already_merged=true` means
the branch was already in — the reported ref is still the correct
`code_change_ref`, so an interrupted close resumes by re-running the same command.
On a non-zero exit, halt and report the exit code (`64` usage, `1` otherwise —
including *no `merge_command` declared*); never substitute a raw `git merge`.
See SKILL.md § Merging for the full contract.

Then call `update_change_request`:
- `status: merged`
- `code_change_ref`: the reported SHA
- `close_reason: merged`

**3. Non-code carve-out**

If `close_reason` is one of `canceled`, `abandoned`, `rejected`, or `superseded`, the schema gate does not require `code_change_ref`. Call `update_change_request`:
- `status: merged`
- `close_reason`: the selected exempt reason

Proceed to `step-02-gate.md`.
