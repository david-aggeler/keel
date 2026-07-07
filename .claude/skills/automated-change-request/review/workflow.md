---
name: automated-change-request/review
description: 'Produce an advisory DHF annotation coverage report and a formal_review record, runnable by a non-resident executor (codex). Use when implementation is complete (status=implementation_review) and the unit is ready for merge-gate review.'
---

# Automated Review

**Transition:** `implementation_review → ready_to_merge` (sound) or `→ in_progress` (blocking findings / red gate)

**Goal:** Advisory DHF-REQ/DHF-TEST annotation coverage report plus a blocking
declared-merge-gate check for the reviewed HEAD; produce a `formal_review`
record. Run linearly, no fan-out.

## Executor contract (condensed — full text in `../SKILL.md`)

- **Self-sufficient:** this file is your complete instruction set for `review`.
- **No fabrication:** the coverage table must report what `rg` actually returned,
  and the gate result must be from the command you actually ran; never invent hits
  or gate passes. On any unavailable tool/command, STOP and report it.
- **Linear:** run every step yourself, in order; no subagents required.
- **Sparse writes:** `update_change_request` with only the changed keys via `fields:`;
  re-read after the status write.
- **Gate, don't ask:** you are the sole reviewer by default; do not block for one.

## 1. Precondition check

1. `get_change_request product=openbrain id=<id>`.
2. Confirm `status == implementation_review`. If it differs, **halt** and report the
   actual status — the unit is not ready for `review`.
3. Collect the `acceptance_criteria` requirement refs.

## 2. Advisory coverage report

For each requirement ref in `acceptance_criteria`, run the two searches **and record
their actual results** (do not assume coverage you did not see):

- `rg "DHF-REQ: openbrain/requirement-<id>"` — implementing-code markers.
- `rg "DHF-TEST: openbrain/requirement-<id>"` — test markers.

Emit a coverage table:

| Requirement | DHF-REQ hits | DHF-TEST hits | Status |
|---|---|---|---|
| openbrain/requirement-<id> | <n> | <n> | covered / missing |

**This report is advisory only.** Missing annotations are a finding to surface in the
`formal_review` notes — they are **not** a blocker for `merge`. Enforcement
(close-blocking, deterministic lint) is deliberately deferred. Report the gaps; do not
halt on them.

## 3. Blocking merge_gate check

Review cannot pass a HEAD whose declared gate is red.

1. Re-read this change request with `get_change_request product=openbrain id=<id>`
   and read its `merge_gate` tier. If it is absent, **halt loudly** — review cannot
   pass without a declared gate.
2. `get_dev_defaults product=openbrain`. If not found, **halt loudly** — the gate
   cannot run without it.
3. Read the `details` row matching the unit's `merge_gate` field:

   | Unit's merge_gate | Row key |
   |---|---|
   | `docs` | `merge_gate.docs` |
   | `standard` | `merge_gate.standard` |
   | `full` | `merge_gate.full` |

   If the matching row is absent, **halt loudly** and name the missing row.
4. Extract the backticked command string from that row's rule text and **run it
   exactly as stored**, from this worktree root. Treat it as an opaque shell
   invocation. Do **not** interpolate record fields into it.
5. Record the command and result honestly in the review notes.
6. **On gate failure:** do not create an approving review and do not advance to
   `ready_to_merge`.
   - If the gate command is a read-only validator, re-run up to **3 times total**.
     Do not run a 4th time.
   - If the command has side effects, do not retry; treat the first non-zero exit
     as the final failure.
   - Create a `formal_review` with outcome `follow_up_required` naming the failing
     tier, run count, command, and last failing output verbatim.
   - `update_change_request` with `fields: { status: "in_progress" }` and re-read to
     confirm. This routes the unit back to `dev`: the runner reads `in_progress`,
     reads your `formal_review`, and re-dispatches `dev` with it as the to-do.
     **Never leave the status unchanged** — an unchanged `implementation_review` is
     an out-of-set landing the runner halts on.

Regression discipline: the CR-443 `build-context-parity` failure is a review
blocker under this step. A branch made `static-tools` red by its dev edit must be
routed back to dev here (write `in_progress` + `formal_review`) if dev somehow
failed to stop it; it must not reach `merge` and rely on the post-merge issue-166
backstop.

## 4. Produce the formal_review record

You are the **sole reviewer** by default — do not wait for an operator to name
reviewers. Call `create_formal_review` with:

- `subject_refs`: the ref to this change request (`openbrain/change_request-<id>`).
- `outcome`: your verdict from the coverage report, the green merge_gate, and a
  read of the diff — `approved` if coverage is complete and the diff looks sound,
  `approved_with_actions` if only non-blocking annotation gaps or suggestions
  remain, otherwise `follow_up_required` with the gaps named.
- `details`: the coverage table, the merge_gate tier/command/result, plus any code
  concerns or suggestions.

A `follow_up_required` verdict is a **blocking** review — it pairs with the
`in_progress` transition in step 3.6 (routes back to dev). `approved` /
`approved_with_actions` pair with the `ready_to_merge` transition below. When the
gate is red you have already written `formal_review` + `in_progress` in step 3.6;
skip to the report line at the end of step 5.

If an interactive operator has named additional reviewers, create one
`formal_review` per reviewer; otherwise the single record above is the review.

## 5. Transition

- **Sound unit (green gate, no blocking findings):** `update_change_request` with
  `fields: { status: "ready_to_merge" }`. Re-read and confirm
  `status == ready_to_merge`. The runner then applies the `auto_merge` gate: it
  dispatches `merge` when `auto_merge` is set, or parks for a human otherwise.
- **Blocking findings / red gate:** you already wrote `in_progress` +
  `follow_up_required` `formal_review` (step 3.6). The runner re-dispatches `dev`.

**Never leave the status unchanged** — every review run writes either
`ready_to_merge` or `in_progress`. Report the outcome and that `merge` (or `dev`
on a blocking review) is the next verb. **Do not run it in this session** — one
verb, one session.
