---
name: automated-change-request
description: "Executor-portable autonomous tail of the change-request lifecycle for keel — dev, review, merge, verify, salvage — written so a non-resident linear executor (e.g. codex) can run each verb in a fresh session with no project memory. Use when the user says: '/automated-change-request', 'run the automated tail', 'dev this CR headless', 'codex dev', 'codex review', 'codex merge', 'verify this tail', 'autonomous dev/review/merge/verify/salvage', 'drive this approved CR to closed', 'salvage this tail'"
allowed-tools: mcp__gold__get_change_request, mcp__gold__update_change_request, mcp__gold__search_change_request, mcp__gold__get_requirement, mcp__gold__get_ac, mcp__gold__list_ac, mcp__gold__list_inbound_refs, mcp__gold__create_formal_review, mcp__gold__create_issue, mcp__gold__create_action_item, mcp__gold__create_issue_fix, mcp__gold__list_issue_fix, mcp__gold__update_issue, mcp__gold__get_issue, mcp__gold__admin_list_product_versions
x-openbrain-source: automated-change-request/v17
x-openbrain-content-source-hash: sha256:9b98f3fe8e8587712add57612a9d488267cfcf3dd184a4169f22def658cac0d3
x-openbrain-content-hash: sha256:bf3f685b59cafd9e4ee1f16fbeef6dd2828c6e682bba841b270c67cc3ed51d99
---

# Automated Change Request

The **autonomous tail** of the unit-of-implementation lifecycle, reformulated for a
**non-resident, linear executor** (e.g. codex) as well as Claude Code. This is the
codex-runnable twin of `change-request`'s `dev` / `review` / `merge` verbs, with
an independent `verify` verb run by headless Claude after merge — same state
transitions, same transition gate, but **linear** (no subagent fan-out) and
**self-contained** (each verb file is complete on its own).

The **front half** of the lifecycle is not here. Promoting a reviewed issue into a
`change_request`, the interactive `create` / `plan` steps, and the `status` / `correct`
verbs all stay in the human-driven `change-request` skill. This skill covers only the
tail that runs unattended, starting from a unit that is already `approved`.

## Principles

- Every silent error is a bug the author hasn't met yet
- Testability and observability gaps before stylistic ones

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
6. **Record what blocks you, before you stop.** If something blocks the verb you are
   running and is not part of this unit's acceptance contract, record it before you reach
   the landing state or report the halt: `create_action_item` when resolving it needs an
   owner decision; `create_issue`, carrying the command run and the output observed, when
   it is a defect in the product or the pipeline. If it does not block you, or it is this
   unit's own scope, do not file — implement it or report it in the verb's own output.
   Each verb file states this rule itself; the trigger is obstruction, not inspection.

## Verbs

| Verb | Status transition | Summary |
|---|---|---|
| `dev` | `approved → in_progress → implementation_review`, or `in_progress → implementation_review` (rework resume) | Read the acceptance contract first — every referenced requirement **and its acceptance criteria, from the records** — and halt on a contract that contradicts itself, its parent requirement, or the unit's Scope (`formal_review`, no code). Then a vertical-slice TDD loop, run linearly: write `in_progress` on a fresh entry (skip the write on a rework resume, which is already `in_progress` — admissible only when a blocking `formal_review` names the unit), then per slice write a failing test from the public interface + GWT atom only (red), implement to green, annotate DHF-REQ/DHF-TEST. 3-round green cap → park (leave at `in_progress`). Before declaring dev complete, read the unit's `transition_gate` and run the committed `openbrain-client.yaml` `transition_gates.<rung>.in_session` stages only; runner-owned stages stay with `openbrain-client` after the session exits. Gate red means dev-not-done and must be fixed inside this change request or parked. On a complete, green, non-empty-diff unit write `implementation_review`. |
| `review` | `implementation_review → ready_to_merge \| in_progress` | Three checks on the reviewed HEAD: an **advisory** DHF-REQ/DHF-TEST annotation report via inline `rg`; a **blocking** acceptance-coverage check that walks the contract atom by atom against the branch diff and records, per atom, where it is implemented and which test proves it (a dropped, untested, stubbed, or contradicted atom is a blocker — a green gate checks compile and tests, not the contract); and a **blocking** re-run of the committed `transition_gates.<rung>.in_session` stages for the unit's `transition_gate`. Executor is the sole reviewer by default. Sound unit + full coverage + green in-session gate → write `ready_to_merge`. Any blocking finding or a red gate → record a `formal_review` (outcome `follow_up_required`) and write `in_progress` (routes back to dev). Never leave the status unchanged. |
| `merge` | `ready_to_merge → merged` | Run the dependency guard, apply the dirty-checkout procedure, then invoke `openbrain-client merge <ref>` and record the reported `code_change_ref`; write `merged`. The merge session does not run runner-owned gate stages; `openbrain-client` runs them after this verb exits and owns any red-result routing. A merge conflict or unsafe dirty checkout halts for a human (leave `ready_to_merge`). Merge ENDS at `merged`; it does NOT close or wrap up. |
| `verify` | `merged → closed \| in_progress` | Independent post-merge scope-fidelity audit via `claude -p`: confirm all SoR records are correct, then perform the gold wrap-up (derive `fixed_in_version`, mint/satisfy the `issue_fix`, drive the parent issue to closed, multi-change-request-guarded) and write `closed`. A no-op / scope shortfall routes back to dev by writing `in_progress`. |
| `salvage` | `in_progress → in_progress` or `approved` | Interrupted-run recovery analysis invoked only by run-queue's divergence detector: gather gold/branch evidence, run the mechanical build + package-test checks when dirty work exists, classify salvage/hand-back/reset/manual, and record the recommendation. Suggest-only by default; `--auto-salvage` may apply only the green salvage class. |

Route to: `.claude/skills/automated-change-request/<verb>/workflow.md`

## Transition Gates

The gate vocabulary is five cumulative rungs: `prose`, `static`, `unit`,
`integration`, `system`. `static` executes no tests. Coverage-floor measurement and
unit tests attach at `unit` and above; integration and system add their longer stages
without dropping lower-rung stages.

The unit record carries only `transition_gate`. Execution commands are never read from
records. Resolve command stages from the committed `openbrain-client.yaml` in the
worktree being gated:

- `transition_gates.<rung>.in_session` — the verb session runs these synchronously in
  the foreground before writing its status transition.
- `transition_gates.<rung>.runner_owned` — the runner runs these after the verb session
  exits; the verb session must not run them itself.

If the selected rung, either stage list required for the current verb, or the committed
config file is missing, halt and report the missing local binding. Never fall back to a
System-of-Record command string or `openbrain-client.local.yaml`.

## Resolving the acceptance contract (kind-aware)

A unit's acceptance contract is its `requirement` refs **and each requirement's acceptance criteria** (its `acceptance_criteria` GWT atoms / linked `ac` records). **The ACs are the implement/verify oracle — tests trace to the ACs, not to the requirement.** On the change request the ref-array is `requirements` (there is no `acceptance_criteria` field on `change_request`). Where the refs live depends on `kind` — server invariant at every status (requirement-955/942, dd-30):

| `kind` | `parent` | refs live in | chain |
|---|---|---|---|
| `feature` | epic, or none (never an issue) | change request `requirements` (non-empty) | cr → requirements → ac |
| `fix` | an issue | parent issue `related_requirements` (change request `requirements` empty) | cr → issue → related_requirements → ac |

Wherever a verb needs the unit's requirements (`dev` slice list, `review` coverage, `verify` fidelity): resolve the refs by `kind`, then each ref **to its acceptance criteria**, before iterating.

Read the requirement and `ac` **records** — never the change request's restatement of them. Only requirement and AC statements are normative. A requirement that resolves to no acceptance criterion has no oracle, and a contract whose atoms contradict each other cannot be satisfied: both are halts (`dev` step 1b), not judgement calls to settle in code.

`kind` is required on every write and frozen at `approved`, so a unit's contract location never moves once it is approved.

## Session boundary = verb

**One verb = one fresh session.** Do not chain verbs in a single run. Each verb
advances the unit's `status` in gold and exits; the next verb is a separate
invocation that re-reads gold and picks up from the recorded status. The gold record
`status` is the only cross-session carrier — there is no in-memory state to hand off.

Pickup signal for `dev`: a change_request at
`executor=agent` and either `status=approved` (a fresh claim) **or**
`status=in_progress` with a blocking `formal_review`
(`outcome: follow_up_required`) naming it in `subject_refs` (a **rework resume** — a
reviewer, a post-merge revert, or a verify no-op reopen routed it back to `dev`). The
blocking review is the signal, not `in_progress` on its own: an `in_progress` unit with
no such review is **not** resumable and `dev` halts on it. Who applied the round's
corrective work — a `dev` child, the run-queue supervisor, or a human — does not change
the pickup signal. A verb whose precondition does not match **halts** rather than
guessing.

## Worktree model — the runner owns it (do NOT create your own)

The autonomous runner (`openbrain-client run-queue`) creates **one**
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
