# Close — leftover capture

**When:** after the unit reaches `closed` (step-03), before you report done.

**Why:** a close gate often surfaces follow-ups that are neither fix-now nor a
real defect — a small refactor noticed in passing, a doc to update, a deferred
hardening. Forcing these into `issue`s pollutes the defect tracker, and dropping
them loses the thread. Capture them as `action_item`s so the unit can close
cleanly — which is what makes unattended/overnight merge-and-test safe.

## Procedure

For each unresolved follow-up identified during dev/review/close:

1. `create_action_item product=<product>` with:
   - `title` — the follow-up, one line.
   - `details` — what to do and why it surfaced.
   - `source: merge_sweep` — marks it as a close-out leftover.
   - `related: ["<product>/change_request-<n>"]` — back-link to THIS unit, so the
     origin is always recoverable (`list_inbound_refs` on the unit surfaces them).
   - optionally `parent` (the epic or issue this unit sat under), `owner`, `due`.
2. Leave each `action_item` at `status: open`. Do not promote here — promotion is
   a deliberate later act (see the `action-item` skill).

If there are no leftovers, skip this step — it is not mandatory ceremony.

This is the **capture** direction only (`create_action_item`); it needs no
promote verb and never blocks the close.
