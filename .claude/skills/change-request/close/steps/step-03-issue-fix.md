# Step 03 — Issue Fix and Final Close

**Transition:** `merged → closed`

**Goal:** Create `issue_fix` rows when the unit's parent is an issue, then move to `closed`.

## Actions

**1. Check parent mode**

Call `get_change_request` and read the `parent` field.

- If `parent` is an `issue` ref: proceed to the issue fix block below.
- If `parent` is an `epic` ref or absent: skip to step 5.

**2. Issue fix block**

For the primary version:

Call `create_issue_fix` with:
- `issue`: the parent issue ref
- `fixed_in_version`: the product version this fix lands in (ask the operator if not known)
- `change_request`: ref to this change request ("authorised by")
- `summary` / `details`: describe the fix impact

**3. Backport**

Ask the operator:

> Is a backport needed for another product version?

If yes: call `create_issue_fix` again with the backport version. Same `issue` and `change_request` refs; set `fixed_in_version` to the backport target.

**4. Offer to close the parent issue**

Count pending `issue_fix` records for the parent issue across all product versions (via `list_issue_fix` filtered by issue ref). If no version still pends a fix:

> All versions have a fix record. Do you want to close the parent issue {issue-ref}?

If yes: call `update_issue status=closed close_reason=fixed`.

**5. Final close**

Call `update_change_request status=closed`.

The unit is now immutable. To make further changes, reopen first: call `update_change_request status=draft reopen_reason="<reason>"`.
