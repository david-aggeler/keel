---
name: automated-change-request
description: "Executor-portable autonomous tail of the change-request lifecycle for keel — prepare, dev, review, merge, verify, salvage — written so a non-resident linear executor (e.g. codex) can run each verb in a fresh session with no project memory. Use when the user says: '/automated-change-request', 'run the automated tail', 'dev this CR headless', 'codex dev', 'codex review', 'codex merge', 'verify this tail', 'autonomous dev/review/merge/verify/salvage', 'drive this approved CR to closed', 'salvage this tail', 'prepare this issue', 'promote issue to CR', 'run-prepare'"
allowed-tools: mcp__gold__get_change_request, mcp__gold__create_change_request, mcp__gold__update_change_request, mcp__gold__search_change_request, mcp__gold__get_requirement, mcp__gold__get_template_for, mcp__gold__get_dev_defaults, mcp__gold__list_dev_defaults, mcp__gold__create_formal_review, mcp__gold__create_issue, mcp__gold__create_action_item, mcp__gold__create_issue_fix, mcp__gold__list_issue_fix, mcp__gold__update_issue, mcp__gold__get_issue, mcp__gold__admin_list_product_versions, mcp__gold__list_inbound_refs
x-openbrain-source: automated-change-request/v6
x-openbrain-content-source-hash: sha256:ad03cb5627305098667e308adf1c8b6b7e3be561380a1a75b950b5001c8cbb57
x-openbrain-content-hash: sha256:4cbfe7d930de92cdde56ce7f91d69fee6fd8bfc3db8fa28b9091f5cfeaaef7fe
---

# Automated Change Request

The **autonomous tail** of the unit-of-implementation lifecycle, reformulated for a
**non-resident, linear executor** (e.g. codex) as well as Claude Code. This is the
codex-runnable twin of `change-request`'s `dev` / `review` / `merge` verbs, with
an independent `verify` verb run by headless Claude after merge — same state
transitions, same merge gate, but **linear** (no subagent fan-out) and
**self-contained** (each verb file is complete on its own).

The **front half** of the lifecycle has one automated step here too: `prepare`
promotes a **reviewed issue** into a **draft `change_request`**, deciding whether an
agent can implement it (→ `executor: agent`, advanced to `approved`) or whether a
human must answer questions first (→ `executor: human`, left at `draft`). The
remaining interactive front (`create`, `plan`) and the `status` / `correct` verbs
stay in the human-driven `change-request` skill. Apart from `prepare`, this skill
covers the tail that runs unattended.

## Executor contract (read first — non-negotiable)

You may have **no memory** of this project beyond this file and the files/tools/paths
it names. Therefore:

1. **Self-sufficiency.** The verb file you are running is your complete instruction
   set for that verb. Everything you need is in it or at a path/tool it names. Do not
   assume conventions that are not written down here.
2. **No fabrication — report and halt.** Never claim a step succeeded that you did not
   run, never invent command output, never report a gate as passed without running it.
   If a required tool, command, file, or record is unavailable, or any step fails,
   **STOP immediately** and report the exact failure (the command you ran and its
   verbatim output/error). An honest halt with a recorded blocker is success; a
   fabricated "done" is the one unrecoverable failure.
3. **Linear — no fan-out required.** Run the steps in order, yourself. Nothing in this
   skill requires spawning subagents. (A fan-out-capable executor *may* delegate
   independent sub-steps, but it is never required and none of the discipline below
   depends on it.)
4. **Sparse writes — never drop fields.** When you call `update_<type>` (e.g.
   `mcp__gold__update_change_request`), pass **only the keys you are changing** via the
   sparse `fields:` parameter. A top-level-args update is a full-payload **REPLACE**
   that silently drops every field you did not supply. After any write that changes
   `status`, **re-read the record** and confirm the new status before continuing.
5. **Gate, don't ask.** You cannot field interactive questions. Where the human path
   would ask the owner, each verb here gives you a determinate default or an explicit
   hand-back. Take the default or hand back — never block waiting for an answer.

## Verbs

| Verb | Status transition | Summary |
|---|---|---|
| `prepare` | `issue (reviewed) → change_request (draft → approved \| draft)` | Front-half promotion: read a reviewed issue, create a draft change_request linked to it (`parent`), then branch on confidence — fully-specified → `executor: agent` + advance to `approved` (ready for `dev`); needs human input → `executor: human` + open questions in the body + leave at `draft` (hand-back). Pickup requires the issue at `status=reviewed`; never approves a CR with open questions, a body that does not match the server template, or absent/superficial acceptance criteria (any such gap hands back as `executor: human`). Driven by `openbrain-client run-prepare <issue-NNN>`. |
| `dev` | `approved → in_progress → implementation_review` | Vertical-slice TDD loop, run linearly: write `in_progress` on entry, then per slice write a failing test from the public interface + GWT atom only (red), implement to green, annotate DHF-REQ/DHF-TEST. 3-round green cap → park (leave at `in_progress`). Before declaring dev complete, run the unit's declared `merge_gate` command from `dev_defaults`; gate red means dev-not-done and must be fixed in-CR or parked. On a complete, green, non-empty-diff unit write `implementation_review`. |
| `review` | `implementation_review → ready_to_merge \| in_progress` | Advisory DHF-REQ/DHF-TEST coverage report via inline `rg`; executor is the sole reviewer by default; re-run the unit's declared `merge_gate` for the reviewed HEAD. Sound unit + green gate → write `ready_to_merge`. Blocking findings or a red gate → record a `formal_review` (outcome `follow_up_required`) and write `in_progress` (routes back to dev). Never leave the status unchanged. |
| `merge` | `ready_to_merge → merged \| in_progress` | Run the dependency guard + the `merge_gate` tier command from `dev_defaults`, then merge `cr-<seq>` into `main` via `.claude/skills/merge/scripts/merge-branch.sh` and record `code_change_ref`; write `merged`. A post-merge gate failure attributable to THIS change → revert main, record a `formal_review`, write `in_progress` (routes back to dev). A foreign/flaky failure or a merge conflict → HALT for a human (leave `ready_to_merge`). Merge ENDS at `merged`; it does NOT close or wrap up. |
| `verify` | `merged → closed \| in_progress` | Independent post-merge scope-fidelity audit via `claude -p`: confirm all SoR records are correct, then perform the gold wrap-up (derive `fixed_in_version`, mint/satisfy the `issue_fix`, drive the parent issue to closed, multi-CR-guarded) and write `closed`. A no-op / scope shortfall routes back to dev by writing `in_progress`. |
| `salvage` | `in_progress → in_progress` or `approved` | Interrupted-run recovery analysis invoked only by run-tail's divergence detector: gather gold/branch evidence, run the mechanical build + package-test checks when dirty work exists, classify salvage/hand-back/reset/manual, and record the recommendation. Suggest-only by default; `--auto-salvage` may apply only the green salvage class. |

Route to: `.claude/skills/automated-change-request/<verb>/workflow.md`

## Session boundary = verb

**One verb = one fresh session.** Do not chain verbs in a single run. Each verb
advances the unit's `status` in gold and exits; the next verb is a separate
invocation that re-reads gold and picks up from the recorded status. The gold record
`status` is the only cross-session carrier — there is no in-memory state to hand off.

Pickup signal for `prepare`: an **issue** at `status=reviewed` with no open
`change_request` already linked to it. Pickup signal for `dev`: a change_request at
`status=approved` and `executor=agent` (which is exactly what a successful `prepare`
leaves behind). A verb whose precondition does not match **halts** rather than
guessing.

## Worktree model — the runner owns it (do NOT create your own)

The autonomous runner (`openbrain-client run-tail` / `run-epic`) creates **one**
worktree/branch per unit, named `cr-<seq>` (derived from the change_request id),
and roots your verb session in the right place:

- **`dev` / `review`** run **inside** the unit's `cr-<seq>` worktree, on the
  `cr-<seq>` branch. Your current directory already *is* that worktree — commit
  your slices there. Confirm with `git rev-parse --abbrev-ref HEAD` (must be
  `cr-<seq>`).
- **`merge` / `verify`** run in the **primary checkout** (where `main` lives),
  because `merge` lands `cr-<seq>` on `main` and `verify` audits the merged code
  reachable on `main`. There your current directory is the repo root and HEAD is
  `main`; the unit branch is `cr-<seq>`.

**Never run `worktree-up.sh` (or otherwise `git worktree add`) from this skill.**
Creating a second worktree (`cr-<seq>-<slug>`) is the issue-192 failure-3 defect:
it double-roots the unit, leaves the runner's `cr-<seq>` branch empty, and makes
`dev` no-op on a re-run. The `change-request/scripts/worktree-*.sh` scripts belong
to the **human** `change-request` skill only; they are not part of this autonomous
tail.
