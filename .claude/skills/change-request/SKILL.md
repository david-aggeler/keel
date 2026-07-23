---
name: change-request
description: "Unit-of-implementation lifecycle for keel: create, plan, dev, review, close, status, correct. Use when the user says: '/change-request', 'create a CR', 'implement this', 'open a change request', 'resume CR', 'cr list', 'list change requests', 'start a unit', 'pick up this story', 'implement the story', 'dev this story', 'implement the next story', 'convert story to unit'"
allowed-tools: mcp__gold__create_change_request, mcp__gold__update_change_request, mcp__gold__list_change_request, mcp__gold__get_change_request, mcp__gold__search_change_request, mcp__gold__get_issue, mcp__gold__get_template_for, mcp__gold__list_glossary_term, mcp__gold__create_glossary_term, mcp__gold__update_glossary_term, mcp__gold__search_requirement, mcp__gold__create_requirement, mcp__gold__update_requirement, mcp__gold__get_dev_defaults, mcp__gold__list_dev_defaults, mcp__gold__create_dev_defaults, mcp__gold__create_issue_fix, mcp__gold__create_formal_review, mcp__gold__create_action_item, mcp__gold__list_task, mcp__gold__create_task, mcp__gold__update_task, mcp__gold__get_task
targets_templates:
  - change_request-template
x-openbrain-source: change-request/v12
x-openbrain-content-source-hash: sha256:8731ffa20922e992fe118f5277b7ccdb538a19f5c8b8dbb4b1f853ccbe0adf13
x-openbrain-content-hash: sha256:faf1960b63db601c630b5c42711311ce090ce805b08385cfb8248ecae6f22b02
---

# Change Request

Dispatcher for all unit-of-implementation operations. One unit = one session.

## Verbs

| Verb | Status transition | Summary |
|---|---|---|
| `create` | → `draft` | Elicit context, run the front-loaded batch interview, emit the 4-section body, extract requirements; **issue-parent CRs gate on the issue being `reviewed`**; includes convert-on-pickup mode for backlog stories |
| `plan` | `draft → approved` | Architect brief (exception-only KDs), validate requirements against codebase, **pre-approval gates (body matches the server template + comprehensive, kind-correct requirements)**, stamp `executor`/`merge_gate`/`auto_merge` at owner confirmation |
| `dev` | `approved → in_progress` | Vertical-slice TDD loop: symmetric two-actor per slice (tester + coder subagents), DHF-REQ/DHF-TEST annotation per slice |
| `review` | `in_progress → implementation_review → ready_to_merge` | Advisory DHF-REQ/DHF-TEST coverage report via inline `rg`; produce `formal_review` records; mark reviewed units ready to merge |
| `close` | `ready_to_merge → merged → closed` | Two-half gate: merge half records `code_change_ref`; gate half runs `merge_gate` tier commands from `dev_defaults`, creates `issue_fix` rows when `parent` is an issue |
| `status` | read-only | Legend + progress; `planned` rendered as legacy; `on_hold` park/resume |
| `correct` | any non-closed | Structured change: classify → edit at source of truth; micro-reconfirm post-`approved` rows; on `closed` → halt "reopen first" |

Route to: `.claude/skills/change-request/<verb>/workflow.md`

## Resolving the acceptance contract (kind-aware)

A unit's acceptance contract is its `requirement` refs **and each requirement's acceptance criteria** — the requirement's `acceptance_criteria` GWT atoms and/or linked `ac` records. **The ACs are the implement/verify oracle, not the requirement itself.** On the CR the ref-array is `requirements` (formerly `acceptance_criteria`; that field is gone). Where the refs live depends on `kind` — server invariant at every status (requirement-955/942, dd-30):

| `kind` | `parent` | refs live in | chain |
|---|---|---|---|
| `feature` | epic, or none (never an issue) | CR `requirements` (non-empty) | cr → requirements → ac |
| `fix` | an issue | parent issue `related_requirements` (CR `requirements` empty) | cr → issue → related_requirements → ac |

Resolve the ref list by `kind`, then resolve each ref **to its acceptance criteria**, before iterating. `create`/`plan`/`dev`/`review` all consume this. `kind` is required on every write and frozen at `approved`.

## State and verb map

| # | Status | Meaning | Entered by |
|---|---|---|---|
| 1 | `draft` | Idea detailed. Born thin from `epic` decomposition or `create`; detailed at pickup into the 4-section body. Living record, freely edited. | `create` (new unit), epic decomposition husk, or convert-on-pickup from a story |
| 2 | `approved` | Spec ratified. Owner confirmed the batch in one pass; `executor`, `merge_gate`, `auto_merge` stamped. Agent-queue state: `status=approved, executor=agent` = ready for pickup. | `plan` — architect brief + owner one-batch confirmation |
| 3 | `in_progress` | Implementation underway. Worktree up, vertical-slice TDD loop running. | `dev` (start) |
| 4 | `implementation_review` | Implementation is under review: advisory DHF annotation coverage and `formal_review` records are being produced. | `review` (start) |
| 5 | `ready_to_merge` | Review complete. Findings are resolved or accepted for this unit, and the branch is ready for the close merge half. | `review` (complete) |
| 6 | `merged` | Merged. `code_change_ref` recorded; declared `merge_gate` tier commands passed. | `close` (merge half) |
| 7 | `closed` | Learned/verified. `close_reason` set, close gate passed; `issue_fix` rows created when `parent` is an issue. Immutable — reopen to change. | `close` (gate half) |
| — | `on_hold` | Parked. Orthogonal — entered from and returns to any non-closed state; deferral fields say why. Scheduling axis, not quality. | `correct` or `status` (park/resume) |
| — | `planned` | **Legacy, never written.** Pre-unit-model state of unknown ratification depth; re-enter at `plan`. | nothing — read-tolerated only |

**Status axis:** status = quality/maturity of the record.
**Scheduling axis:** refs + deferral fields + `on_hold`.

## Worktree scripts

| Script | Purpose |
|---|---|
| `scripts/worktree-up.sh <kind> <seq> <slug>` | Create a worktree on a fresh `<kind>-<seq>-<slug>` branch off main. Idempotent. |
| `scripts/worktree-down.sh <kind> <seq> <slug>` | Author-side pre-merge teardown. Refuses dirty worktrees. Does not delete the branch. |
| `scripts/worktree-resume.sh <kind> <seq> <slug>` | Re-attach a worktree to an existing branch when the directory was removed. |
| `scripts/worktree-status.sh <kind> <seq> <slug>` | Print branch/worktree existence. Read-only. |

Invoke via:

```bash
bash .claude/skills/change-request/scripts/worktree-up.sh cr <seq> <slug>
```
