# Step 2: Continue Existing DFMEA

## YOUR TASK

An in-progress DFMEA session was found. Resume the workflow from where it left off, or start over with the user's consent.

---

## CONTINUATION SEQUENCE

### 1. Load the Open Session

Call `list_formal_review` with `type=dfmea status=in_progress product=<slug>` (same call step-01 made).

- **Exactly one result** → proceed with that review as the session anchor.
- **More than one result** → present the list and ask the user to pick one:

  ```text
  I found multiple in-progress DFMEA sessions for this product:
    [1] <ref> — <title> (started: <conducted_at>)
    [2] <ref> — <title> (started: <conducted_at>)
    ...
  Which session would you like to resume? (enter the number)
  ```

  Wait for selection, then use that review as the session anchor.

### 2. Enumerate Failure Modes

Call `list_failure_mode` filtered by `identified_in_review=<review-ref>` to get all failure_mode records already created in this session.

### 3. Derive Current Progress

Determine which step to resume from based on record state — progress is derived from the records, not a stored array:

| Record state | Next step |
|---|---|
| No failure_mode records found | step-03-components.md (component map not yet done, or step-04 not started) |
| Failure_mode records exist, none have `severity`/`occurrence`/`detection` set | step-05-scoring.md |
| Failure_mode records have S/O/D, none have `mitigations[]` | step-06-mitigations.md |
| Failure_mode records have mitigations, none have `post_mitigation` | step-07-arch-feedback.md |
| Failure_mode records have `post_mitigation` on at least some mitigated items | step-08-complete.md |

When in doubt, default to the earlier step — it is safe to re-confirm state before advancing.

### 4. Summarize Current State

```text
Found an in-progress DFMEA session for [product].

Session anchor: <formal_review ref>
Title: <title>
Scope: <subject_text>
Started: <conducted_at>

Failure modes on record: X
  Components covered: [list distinct component values]
  Scored (S/O/D set): X of X
  With mitigations: X of X
  With post_mitigation: X of X

Options:
  [R] Resume from where I left off (Step [derived next step])
  [E] Edit scope or reload inputs before resuming
  [N] Start over (create a new DFMEA session)
```

Wait for the user's choice.

### 5. Dispatch

- **R** → load the derived next step file (from the table in §3)
- **E** → re-run the discovery and scope sections of step-01-init.md (fresh input discovery only, do not create a new formal_review), then resume
- **N** → offer to close the current session first (step-01 will be reloaded for a fresh DFMEA):

  ```text
  Before starting over, would you like me to close this session as abandoned?
  This keeps the resume list clean — declining leaves it in_progress.

  [Y] Close as abandoned, then start fresh
  [X] Leave it open, just start a new session
  ```

  Wait for the user's choice:
  - **Y** → call `update_formal_review` on the current review with `status=closed close_reason=abandoned`, then load step-01-init.md to start fresh.
  - **X** → load step-01-init.md to start fresh (old review remains in_progress).

## SUCCESS METRICS

✅ Correct session anchor identified (one found → use it; several → user picks)
✅ Failure mode inventory loaded via list_failure_mode
✅ Progress derived from record state (not a stored array)
✅ Start-over path offers to close the abandoned session before creating a new one

## FAILURE MODES

❌ Assuming "which step" from a stale frontmatter field — derive from record state
❌ Creating a new formal_review without closing or acknowledging the existing one
❌ Silently abandoning the old review (must offer the close step)
