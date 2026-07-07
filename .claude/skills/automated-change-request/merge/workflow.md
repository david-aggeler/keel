---
name: automated-change-request/merge
description: 'Land a reviewed unit on main through the dependency guard + merge gate, runnable by a non-resident executor (codex). Use when status=ready_to_merge and the unit is cleared to merge.'
---

# Automated Merge

**Transition:** `ready_to_merge → merged` (landed) or `→ in_progress` (self-caused post-merge failure, reverted)

**Goal:** Land `cr-<seq>` on `main`. Run the dependency guard, merge via the project
merge script, then run the declared merge-gate tier command. On a green landing the
verb **ends at `merged`** — the post-mortem close and gold wrap-up belong to `verify`.
A post-merge gate failure attributable to THIS change reverts `main` and routes the
unit back to `dev`; a foreign/flaky failure or a merge conflict halts for a human.

## Executor contract (condensed — full text in `../SKILL.md`)

- **Self-sufficient:** this file is your complete instruction set for `merge`.
- **No fabrication:** never record a `code_change_ref` you did not obtain from a real
  merge; never report the gate as passed without running it and seeing it pass. On any
  failure, STOP and report the exact command + verbatim output.
- **Linear:** run every step yourself, in order; no subagents required.
- **Sparse writes:** every `update_change_request` uses only the changed keys via
  `fields:`; re-read after each status write.
- **Gate, don't ask:** owner-only steps default to an explicit hand-back — never block
  waiting for an answer.

## 1. Precondition check

`get_change_request product=openbrain id=<id>`. Confirm `status == ready_to_merge`; if it
differs, **halt** and report. Read `close_reason`, `parent`, `depends_on`,
`deferred_pending`, and `merge_gate` for use below.

## 2. Dependency guard

Read `depends_on` and `deferred_pending` (from step 1). For each ref, `get_<type>` and
check its status. If **any** referenced unit is not `closed`:

- If `auto_merge` is currently `true`: `update_change_request`
  `fields: { auto_merge: false }` and **halt** —
  > auto_merge forced off at merge gate: depends on `<ref>` (status `<status>`), not
  > yet closed. Resolve the dependency and rerun `merge`.
- If `auto_merge` is already `false`: continue; note the open dependency in your run
  summary.

## 3. Merge onto main (`ready_to_merge → merged`)

> **Where this verb runs.** The runner roots the **merge** session in the
> **primary checkout** (where `main` is checked out), *not* in the unit's
> `cr-<seq>` worktree — the merge below needs a checkout of `main`, which a linked
> worktree cannot give. So your current directory is the repo root and
> `git rev-parse --abbrev-ref HEAD` here is `main`. The unit's branch is
> `cr-<seq>`, derived from the record id (`change_request-333` → `cr-333`).

1. Derive the unit's branch from the record id — `change_request-<seq>` →
   `cr-<seq>` (e.g. `change_request-333` → `cr-333`). Confirm it exists before
   merging:

   ```bash
   BRANCH="cr-<seq>"
   git rev-parse --verify "refs/heads/$BRANCH" >/dev/null \
     || { echo "no branch $BRANCH to merge" >&2; exit 1; }
   ```

   If the branch does not exist, **halt and report it** — `dev` never committed,
   so there is nothing to land.
2. Merge the unit's branch to `main` **via the project merge script**, run from
   the repo root (your current directory):

   ```bash
   bash .claude/skills/merge/scripts/merge-branch.sh "$BRANCH"
   ```

   Pass **no** worktree argument — the branch must survive in case the gate below
   reverts and dev re-works it. Capture the resulting merge commit SHA from its
   output.

   **Do not merge with raw `git`.** The script carries the issue-166 post-merge
   `openbrain-dev ci static-tools` re-verify that reverts the merge in place if the merged `main`
   tree is red, and that guard must run on **every** path that advances `main`. A raw
   `git merge` bypasses it — that bypass is exactly what left `main` red after cr-286
   (see openbrain/issue-175). If the script exits non-zero because of a **merge conflict**
   or a **dirty tree**, **STOP and HALT for a human** — report its verbatim output.
   `main` was not advanced; nothing was recorded. A conflict is never routed back to
   dev automatically. **Never fabricate a SHA.**
3. `update_change_request` with
   `fields: { status: "merged", code_change_ref: "<sha>" }`. Re-read and confirm
   `status == merged`. The runner cross-checks that the merge commit is reachable on
   `main`; a claimed `merged` with no merge commit on `main` is fail-closed.

## 4. Post-merge gate

The merge landed; now confirm the merged `main` tree is green on the declared tier.

1. `get_dev_defaults product=openbrain`. If not found, **halt loudly** — the gate
   cannot run without it.
2. Read the row matching the unit's `merge_gate` field:

   | Unit's merge_gate | Row key |
   |---|---|
   | `docs` | `merge_gate.docs` |
   | `standard` | `merge_gate.standard` |
   | `full` | `merge_gate.full` |

   If the matching row is absent, **halt loudly** and name the missing row.
3. **Run the command string exactly as stored.** Treat it as an opaque shell
   invocation. Do **not** interpolate record fields into it.
4. **On gate failure:**
   - **Idempotency caveat:** retry only if the gate command is a **read-only validator**
     (e.g. `go run ./cmd/openbrain-dev --repo "$PWD" ci static-tools && go run ./cmd/openbrain-dev --repo "$PWD" test unit`). If it has side effects, go straight
     to the classification below on the first failure (skip the retries) — never re-run
     a side-effecting command against partial state.
   - For read-only validators: re-run up to **3 times total**; a flaky gate may pass on
     retry. If still failing after the 3rd run, classify the failure:
     - **Attributable to THIS change** (the merged diff caused the red): **revert and
       route back to dev.**
       1. Restore `main` to the merge's first parent: `git reset --hard <code_change_ref>^1`
          (or `git revert -m 1 <code_change_ref>` if the merge was already pushed).
          Confirm `git rev-parse HEAD` is the pre-merge SHA. The unit's branch is
          intact (you passed no worktree arg), so dev re-works on the branch.
       2. `create_formal_review` (outcome `follow_up_required`) naming the failing tier,
          run count (3), the last failing command output verbatim, and that the merge
          was reverted.
       3. `update_change_request` `fields: { status: "in_progress" }`; re-read to confirm.
          This routes the unit back to `dev`: the runner reads `in_progress`, reads your
          `formal_review`, and re-dispatches `dev` with it as the to-do. Also append to
          `details`: "post-merge gate `<tier>` red — merge reverted, unit returned to
          in_progress; see formal_review."
       4. Exit cleanly.
     - **Foreign / flaky** (a pre-existing red on `main` unrelated to this diff, or an
       environmental flake that does not clear on retry): **do NOT route back to dev.**
       Revert the merge as above so `main` is not left red, record a `formal_review`
       describing the foreign failure, and **HALT for a human** — leave the status at
       `ready_to_merge` (do not write `in_progress`). A human decides whether the
       failure is real before the unit re-enters the tail.
5. **On gate pass:** the merge verb is **complete**. The unit stays `merged`. Report
   the merge SHA, the gate tier/command/result, and that `verify` is the next verb.
   **Do not run `verify` in this session** — one verb, one session. The post-mortem
   close, `issue_fix`, and parent-issue wrap-up all belong to `verify`.
