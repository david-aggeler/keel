---
name: automated-change-request/merge
description: 'Land a reviewed unit on main through the dependency guard and configured merge command, runnable by a non-resident executor (codex). Use when status=ready_to_merge and the unit is cleared to merge.'
x-openbrain-content-hash: sha256:2c35adfc51f18dcdebe8bf2fe826bb8d4e08362fe2cb37bbabe50e1c6df01710
---

# Automated Merge

**Transition:** `ready_to_merge → merged` (landed)

**Goal:** Land `cr-<seq>` on `main`. Run the dependency guard, apply the dirty-checkout
procedure, then invoke `openbrain-client merge <ref>` and record the reported
`code_change_ref`. The merge session does not run runner-owned gate stages; the
runner owns those after this verb exits. On a green landing the verb **ends at
`merged`** — the post-mortem close and gold wrap-up belong to `verify`.

## Executor contract (condensed — full text in `../SKILL.md`)

- **Self-sufficient:** this file is your complete instruction set for `merge`.
- **No fabrication:** never record a `code_change_ref` you did not obtain from a real
  merge command result. On any failure, STOP and report the exact command + verbatim
  output.
- **Linear:** run every step yourself, in order; no subagents required.
- **Sparse writes:** every `update_change_request` uses only the changed keys via
  `fields:`; re-read after each status write.
- **Gate, don't ask:** owner-only steps default to an explicit hand-back — never block
  waiting for an answer.

## 1. Precondition check

`get_change_request product=openbrain id=<id>`. Confirm `status == ready_to_merge`; if it
differs, **halt** and report. Read `close_reason`, `parent`, `depends_on`,
`deferred_pending`, and `transition_gate` for use below.

## 2. Dependency guard

Read `depends_on` and `deferred_pending` (from step 1). For each ref, `get_<type>` and
check its status. If **any** referenced unit is not `closed`:

- If `auto_merge` is currently `true`: `update_change_request`
  `fields: { auto_merge: false }` and **halt** —
  > auto_merge forced off at merge: depends on `<ref>` (status `<status>`), not
  > yet closed. Resolve the dependency and rerun `merge`.
- If `auto_merge` is already `false`: continue; note the open dependency in your run
  summary.

## 3. Dirty-checkout procedure

Before invoking the merge command, inspect the primary checkout:

```bash
git status --porcelain=v1
```

- If it is clean, continue.
- If every dirty path is a tracked generated artifact or untracked build output,
  park those paths outside the checkout, remember exactly what was moved, and restore
  them after the merge command finishes or fails.
- If any dirty path is not clearly generated/build output, **halt** and report the
  dirty paths. That is someone's work; do not stash, move, overwrite, or discard it.

## 4. Merge onto main (`ready_to_merge → merged`)

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
2. Invoke the merge through the client:

   ```bash
   openbrain-client merge "$BRANCH"
   ```

   The client reads the committed `merge_command` from `openbrain-client.yaml`, appends
   the branch/ref, and prints `code_change_ref=<sha>` (or `already_merged=true` with the
   existing SHA on an idempotent retry). It does not read gold for command text and it
   does not run `transition_gates.<rung>.runner_owned`; the runner does that after this
   verb exits.

   **Do not merge with raw `git`** — it bypasses the configured product command. If
   `openbrain-client merge` exits non-zero, **STOP and HALT for a human** — report its
   verbatim output. A conflict is never routed back to dev automatically. **Never
   fabricate a SHA.**
3. `update_change_request` with
   `fields: { status: "merged", code_change_ref: "<sha>" }`. Re-read and confirm
   `status == merged`. The runner cross-checks that the merge commit is reachable on
   `main`; a claimed `merged` with no merge commit on `main` is fail-closed.

## 5. Runner-owned gate handoff

If this unit's `transition_gate` has `runner_owned` stages in committed
`openbrain-client.yaml`, this merge session must not run them. The runner runs them
after this verb exits and owns any red-result routing. Do not pre-run, derive, skip,
or mark those stages as already satisfied.

The merge verb is complete once the `merged` status and `code_change_ref` have been
written and re-read. Report the merge SHA, the transition-gate rung, and that `verify`
is the next verb after the runner-owned stages pass. **Do not run `verify` in this
session** — one verb, one session. The post-mortem close, `issue_fix`, and parent-issue
wrap-up all belong to `verify`.
