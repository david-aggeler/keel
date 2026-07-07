---
name: change-request/close
description: 'Close the unit through the two-half gate: merge half then gate half. Use when the unit is ready_to_merge and ready to land.'
---

# Close Change Request

**Transitions:** `ready_to_merge → merged → closed`

**Goal:** Two-half gate. The merge half records the code change and moves to `merged`. The gate half runs the declared merge gate commands, creates `issue_fix` rows when the parent is an issue, and moves to `closed`.

## Execution

Read and follow in order:

1. `steps/step-01-merge.md` — merge half: `ready_to_merge → merged`
2. `steps/step-02-gate.md` — gate half: run merge gate commands
3. `steps/step-03-issue-fix.md` — gate half continued: `merged → closed`, issue_fix rows
4. `steps/step-04-leftover-capture.md` — capture any unresolved follow-ups as `action_item`s linked back to this unit (so closing never loses the thread)

## Close reason quick reference

| close_reason | code_change_ref required? | When to use |
|---|---|---|
| `merged` | Yes | Normal path: code was written, tested, merged |
| `canceled` | No | Work stopped before implementation |
| `abandoned` | No | Work deprioritized; not expected to resume |
| `rejected` | No | Spec was rejected after review |
| `superseded` | No | Work absorbed by another unit or rendered moot |

The `code_change_ref` requirement is enforced by the schema's `x-status-requires` gate. The carve-out suppresses it only for the four exempt reasons above.
