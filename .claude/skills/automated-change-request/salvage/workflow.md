---
name: automated-change-request/salvage
description: "Analyze an interrupted autonomous-tail run after run-tail detects gold status ahead of branch/worktree evidence; classify recovery as salvage, hand-back, reset, or manual, with suggest-only default and opt-in auto-apply for the green salvage class."
---

# Automated Salvage

**Transition:** `in_progress -> in_progress` or `approved`

**Goal:** When `run-tail` detects a state divergence before resuming a unit, gather
objective evidence and produce a durable recovery recommendation. This verb is a
detector-triggered recovery stage, not part of the normal tail.

## Executor Contract

- Run only when invoked as `/automated-change-request salvage <product/change_request-N>`.
- Accept optional `--auto-salvage`. Without `--auto-salvage`, this verb is **suggest-only**.
- Never guess. A recommendation of `salvage` requires both a successful build and
  successful package tests for the dirty work.
- Record the recommendation durably in a `formal_review` linked to the CR. The
  runner also surfaces the halt outcome, but logs alone are not enough.

## 1. Read State

1. `get_change_request product=openbrain id=<id>`.
2. Confirm `status == in_progress` and `executor == agent`. If not, record a
   `manual` recommendation with the observed status/executor and stop.
3. From the current worktree, gather:
   - branch name: `git rev-parse --abbrev-ref HEAD`
   - commits ahead of main: `git log --oneline main..HEAD`
   - dirty file list: `git status --short`
   - diffstat: `git diff --stat`
   - prior build-temp residue if visible under the run's build-temp directory
   - latest formal review for the CR, if available

## 2. Classify

Use this taxonomy exactly:

- `reset`: zero commits ahead of main and no dirty work. Recommendation: return
  the CR to `approved` for a fresh dev run.
- `salvage`: dirty work exists, and both the build and package tests pass.
  Recommendation: commit the work on the unit branch, then resume the tail at
  review.
- `hand-back`: dirty work exists, but the build or package tests fail.
  Recommendation: owner inspects/fixes the failing work; never recommend salvage.
- `manual`: evidence is ambiguous, branch is unexpected, commits already exist
  with dirty work, commands cannot run, or the CR precondition is not met.

When dirty work exists, run the mechanical gates before deciding:

```bash
just build-local
go run ./cmd/openbrain-dev test unit
```

If either command fails, classify `hand-back` and include the failing command and
verbatim output in the formal review details.

## 3. Record Recommendation

Create a `formal_review`:

- `subject_refs`: the CR ref
- `status`: `completed`
- `outcome`: `follow_up_required`
- `type`: `other`
- `type_other`: `automated tail salvage`
- `title`: `Salvage recommendation for <change_request-N>`
- `details`: classification, evidence gathered above, commands run, exit status,
  and any failing output.

## 4. Optional Auto-Apply

If `--auto-salvage` is absent, stop after recording the recommendation.

If `--auto-salvage` is present:

- Apply only classification `salvage`.
- Commit the dirty work on the current unit branch with a conventional subject
  such as `fix(cr-<N>): salvage interrupted dev work.`.
- Do not apply `hand-back`, `reset`, or `manual`; stop after the review record.
- After committing, leave the CR at `in_progress` so the runner can re-check the
  branch state and resume at `review`.
- Be idempotent: if the work is already committed and the tree is clean on a
  re-run, do not create a duplicate commit.

## 5. Exit

Exit zero after the formal review is recorded and any allowed auto-apply step is
complete. Exit non-zero only when the verb itself cannot record the durable
recommendation.
