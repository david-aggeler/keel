---
name: merge
description: Merge a completed unit's branch into main for the keel project, then hand the System-of-Records close off to the gold-backed change-request skill. Use when the user wants to merge a CR branch, land a feature branch, or finish a worktree — "merge this", "merge CR-XX", "land the branch", "merge it", "/merge", "ship it" (when the context is merging, not committing). Runs keel's gate once pre-merge, merges with a guarded --no-ff (post-merge re-verify + auto-revert on red), then delegates the gold close. keel has no deploy/health step. Commands live in the Justfile — this skill only names `just` recipes and the change-request skill.
---

# Merge

Land a unit's branch on main, then delegate the SoR transitions to gold.

This skill owns the **git side** (diff → gate → merge → push). The **SoR side** —
recording `code_change_ref`, moving the unit through `merged → closed`, creating
`issue_fix` rows, and closing the parent issue — is owned by the gold-backed
`change-request` skill's `close` verb. This skill runs the keel gate **once, pre-merge**,
then invokes `/change-request close` and tells it the gate already passed, so it is never
double-run.

Every command lives in the Justfile; this skill only names `just` recipes. It never
embeds raw `git` / `go` / `keel-dev` invocations.

> **keel is one Go module + a VSIX — there is no running stack.** Unlike a service repo,
> there is no deploy/health-check step between merge and push: the guarded merge itself
> re-runs the full gate against the merged tree (`keel-dev ci`) and auto-reverts on red,
> so main is never advanced red.

**Records live in gold, product `keel`, not the filesystem.** Unit and issue state are
read from and written to the `gold` MCP instance (`mcp__gold__*`). There is no
`docs/change-requests/` or `docs/issues/` step.

## Prerequisites (gold)

- The unit exists on gold (product `keel`) in status `implementation_review` (the review
  step has run), or the user is landing an operator branch directly.
- The `gold` data-plane tools are reachable (`get_change_request`, `list_change_request`,
  `get_dev_defaults`). If they return `Invalid session ID` or similar, stop and
  re-establish the gold MCP session before merging — the close hand-off cannot complete
  otherwise.

## Step 0: Identify the unit, branch, and worktree

Determine the unit, its branch, and its worktree path.

- **User says "merge CR-XX"** — look the unit up on gold:
  `mcp__gold__get_change_request product=keel` (or `list_change_request product=keel
  include_summary=true`). Confirm `status=implementation_review` and read its `merge_gate`
  tier and `parent`.
- **Branch name** follows keel's worktree convention: `cr-<seq>` (the run-queue tail and
  the change-request worktree scripts own these). It is not a stored field.
- **Worktree path** — resolve with the change-request worktree helper (read-only):

      bash .claude/skills/change-request/scripts/worktree-status.sh cr <seq> <slug>

  (This is the change-request skill's own helper, not a keel command — invoke it as
  documented by that skill.)
- **User says "merge this"** on a worktree branch — use the current branch/worktree and
  map back to the unit by branch name.

If the unit, branch, or worktree can't be determined, ask the user.

## Step 1: Pre-merge diff review

Show the user what will be merged:

    just merge-diff <branch>

One last look before the gate runs. If the diff is surprisingly large or includes
unexpected files, flag it.

## Step 2: Pre-merge gate

Run keel's gate once, and record that it passed so `close` does not re-run it:

    just ci

`just ci` is `keel-dev ci` — keel's single canonical gate (gofmt, build, vet, lint, test,
85% coverage floor). If the unit declares `merge_gate: full` and touches `vsix/`, also run:

    just vsix

**No prose opt-out.** "just skip it", "I already ran tests", or "it's tiny" is not license
to skip. To change the gate expectation, change the unit (`/change-request correct`), not
this run.

**If the gate fails:** stop. Show the failures and help fix them on the branch. Do not
merge. **If green:** note that the gate passed pre-merge — you pass this fact to `close` in
Step 5.

## Step 3: Commit outstanding changes

If the worktree has uncommitted changes, the unit's work hasn't been committed yet. Commit
in the worktree (via `/commit` or a plain commit) before merging. This is the typical flow —
the dev loop writes code but does not commit; merge lands it.

## Step 4: Merge (guarded) and clean up

    just merge-branch <branch>

This runs the keel-native guarded merge: `--no-ff` into main, then a **post-merge
re-verify** of the full gate (`keel-dev ci`) against the merged tree. If the merged tree is
red, it reverts the merge in place (`git reset --hard` to the pre-merge SHA) so main never
lands red, and exits non-zero — fix on the branch and re-run. On success it prints
`MERGE_SHA=<sha>`. **Capture that SHA** — it is the `code_change_ref` the close hand-off
needs.

If the merge has conflicts, the script aborts and leaves main unchanged. Resolve on the
branch with the user; never force-resolve.

## Step 5: Push, then hand SoR close to the change-request skill

Push main, then delegate every gold write to the `close` verb:

    just push        # only after Step 4 succeeded; never force-push main

Then invoke `/change-request close` for this unit and provide:

- **the merge commit SHA** from Step 4 → recorded as `code_change_ref`
- **`close_reason: merged`**
- **the gate already passed pre-merge in Step 2** — so close's gate-half is a confirmation,
  not a re-run.

`close` then performs, via `mcp__gold__*` (product `keel`): the merge half
(`update_change_request status=merged code_change_ref=<SHA> close_reason=merged`), the
auto-merge dependency guard, and — if the unit's `parent` is an `issue` — an `issue_fix`
plus the final `status=closed`. Do not hand-roll these writes here.

## Step 6: Sweep dangling threads → gold issues or handoff

A merge is where the session's loose ends get captured — the owner clears context between
units, so any thread that lives only in this conversation evaporates on the next `/clear`.
From the **current conversation only**, enumerate: parked ideas, "we'll do X later"
deferrals, code/doc TODOs this unit left, and any follow-up review flagged but the unit did
not action.

For each: a discrete trackable task → file a gold issue via `/issue` (product `keel`,
referencing the merged unit); session-continuation state → fold into one `/handoff`. Check
for an existing merge-sweep issue first to avoid duplicates.

Then report: merge commit SHA, files-changed count, worktree/branch cleanup, the unit's new
gold status, any issue closed, and the sweep outcome. Only when every thread is a filed gold
issue or a handoff, say so plainly.

## What this skill never does

- Merges without keel's gate passing pre-merge
- Advances or pushes a main that is red on `keel-dev ci` (the guarded merge auto-reverts)
- Runs the gate twice (pre-merge here, then again in close)
- Force-pushes to main
- Resolves merge conflicts without user input
- Writes unit/issue SoR state directly — that is delegated to `/change-request close`
- Embeds raw `git` / `go` / `keel-dev` commands — merge/diff/gate steps are `just` recipes
- Reports done while dangling threads remain untracked
