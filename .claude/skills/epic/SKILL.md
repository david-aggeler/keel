---
name: epic
description: "Single entry point for every epic-level workflow. Subcommands cover the whole arc — create, plan, add, status, correct, end, retro, and the readiness gate before each epic opens. Use when the user says: '/epic', 'create the epics', 'plan the epic', 'epic status', 'correct course', 'wrap up epic', 'epic done', 'ready for next epic', 'add a unit to the epic'"
allowed-tools: mcp__gold__create_epic, mcp__gold__update_epic, mcp__gold__get_epic, mcp__gold__list_epic, mcp__gold__search_epic, mcp__gold__create_epic_backlog, mcp__gold__update_epic_backlog, mcp__gold__get_epic_backlog, mcp__gold__list_epic_backlog, mcp__gold__create_change_request, mcp__gold__update_change_request, mcp__gold__get_change_request, mcp__gold__list_change_request, mcp__gold__search_change_request, mcp__gold__create_requirement, mcp__gold__update_requirement, mcp__gold__get_requirement, mcp__gold__list_requirement, mcp__gold__search_requirement, mcp__gold__create_ac, mcp__gold__list_ac, mcp__gold__search_ac, mcp__gold__create_dd_plan, mcp__gold__get_dd_plan, mcp__gold__list_dd_plan, mcp__gold__create_iteration, mcp__gold__get_iteration, mcp__gold__list_iteration, mcp__gold__create_issue, mcp__gold__list_issue, mcp__gold__search_issue, mcp__gold__create_design_decision, mcp__gold__list_design_decision, mcp__gold__create_formal_review, mcp__gold__update_formal_review, mcp__gold__get_formal_review, mcp__gold__list_formal_review, mcp__gold__list_user_need, mcp__gold__search_user_need, mcp__gold__list_vision, mcp__gold__list_intended_use, mcp__gold__list_architecture_description, mcp__gold__list_interface_spec, mcp__gold__list_glossary_term, mcp__gold__create_glossary_term, mcp__gold__update_glossary_term, mcp__gold__get_template_for, mcp__gold__describe_dto_type, mcp__gold__list_inbound_refs, mcp__gold__list_relations_for, mcp__gold__search_records
targets_templates:
  - epic-template
  - epic_backlog-template
  - change_request-template
  - requirement-template
  - ac-template
  - formal_review-template
x-openbrain-source: epic/v10
x-openbrain-content-source-hash: sha256:90cb487a3902694c54142540cbee5f7693640b5ceafd19d07d10345be06cdd23
x-openbrain-content-hash: sha256:2e9b8a9b4cad3920238ec1549cba6f9da90bca4769cb9304b05b37f8eaa313dc
---

# /epic — Epic Lifecycle Management

Routes epic-level invocations to the sub-workflow at `epic/<verb>/workflow.md`. Every verb reads from and writes to the System of Record. There is no file-based document plane: no requirements file, no architecture file, no readiness report on disk, no change-proposal markdown. If a verb produces a finding, it produces a record.

Epics decompose into `change_request` units. The `story` DTO is not used.

## Subcommands

| Slash form | Stage | Workflow file | Purpose |
|---|---|---|---|
| `/epic create` | start | `create/workflow.md` | Turn an approved requirement set into epics under a dd_plan, then decompose each into feature units. |
| `/epic plan` | mid | `plan/workflow.md` | Decompose one existing epic into feature units, requirement-first. |
| `/epic add` | mid | `add/workflow.md` | Add one feature unit to a running epic. |
| `/epic status` | mid | `status/workflow.md` | Read-only rollup of epics and their units, with risk callouts. |
| `/epic correct` | mid | `correct/workflow.md` | Mid-epic course correction, expressed as record edits. |
| `/epic end` | end | `end/workflow.md` | Closeout — unit-closure check, end reviews, findings into issues, epic to `done`. |
| `/epic retro` | end | `retro/workflow.md` | Retrospective anchored on a `formal_review` of type `epic_retrospective`. |
| `/epic ready` | gate | `ready/workflow.md` | Readiness gate over the record graph before the next epic opens. |

## Routing

1. **Parse args.** If the invocation names a verb, use it. With empty args, render the table above and stop.
2. **Load the workflow.** Read the verb's `workflow.md` fully and follow it.
3. **Fuzzy match.** If the user names an intent without the verb ("wrap up epic 27" → `end`, "are we ready for the next epic" → `ready`, "add a unit to epic 3" → `add`), dispatch on the inferred verb and say which inference you made.

Additional trigger phrases: "close epic N", "epic N done", "ship epic N" → `end`; "check epic status", "where is epic N" → `status`; "run a retrospective" → `retro`; "the epic scope changed" → `correct`; "one more unit" → `add`.

## Record model contract — read this before any write

Every verb depends on these rules. Getting them wrong produces a rejected write, not a degraded record.

### epic

- `plan` is **required** and must resolve to a `dd_plan` ref. A prose plan note is rejected. If the product has no dd_plan, create one with `create_dd_plan` before the first epic.
- `status` is `planned` → `active` → `done`. `done` is terminal; set `done_reason` when you get there.
- Express coverage with `covers_user_needs` (a coverage assertion) and `related_requirements` (a soft pointer). Express negative space with `exclusions` — an epic that names nothing out of scope is under-specified.

### change_request — the unit

- **`kind` is required on every write.** This skill only ever creates `kind=feature`.
- A `kind=feature` unit must list **at least one requirement** in `requirements`, and its `parent` must not resolve to an issue. This is an always-on structural invariant at **every** status, not a gate that starts at approval.
- **There is therefore no such thing as a thin husk.** A unit cannot exist before its requirement does. Decomposition is **requirement-first**: author the requirement and its acceptance criteria, then create the unit against them.
- Once a unit reaches `approved` or later, at least one listed requirement must itself be `approved` or later.
- `kind=fix` units (parent is an issue, `requirements` empty) are never created here. Those come from the issue and conclude workflows.
- Set **`iteration`** and **`executor`** at creation. A unit with no `iteration` is invisible to a run-queue drain; a unit without `executor=agent` never qualifies for one. Both omissions produce units that look fine and never get worked.
- `kind` is mutable while drafting and frozen once the unit reaches approved-or-later.
- Status ladder: `draft` → `approved` → `in_progress` → `implementation_review` → `ready_to_merge` → `merged` → `closed`, with `on_hold` parking. Transitions are enforced — you cannot skip rungs.
- `parent` is the epic ref.

### requirement and ac

- A requirement needs `product`, `title`, `statement`, `type`, `status`. Write the statement in shall-form.
- Acceptance criteria are first-class `ac` records with `parent` set to the requirement. Author them in the same pass as the requirement, not later.
- **Search before you create.** Search the capability wording, not the wording of the moment that prompted it — `search_requirement`, `search_ac`, and `search_records` across types. A near-duplicate requirement is worse than none.

## Conventions

Every call takes `product`. IDs are allocated server-side — never pick a sequence number.

Before authoring any record, fetch its template with `get_template_for dto_type=<type>` and write into its structure. The schema says which fields exist; the template says what form the content must take. Use `describe_dto_type` when you need the current field set and enums.

Before exploring or planning, load the product glossary once with `list_glossary_term` (`include_summary=true`, `limit=100`, paginate beyond that) and use its vocabulary. When you coin or sharpen a term, record it with `create_glossary_term` / `update_glossary_term`.

## Ownership boundary

This skill creates and sequences **epics and their units**, and authors the requirements those units need. It does not detail a unit's implementation. The unit body, the branch plan, and the implementation interview belong to the change-request create verb, one unit per session, at pickup.
