---
name: epic
description: "Single entry point for every epic-level workflow. Subcommands cover the whole arc — create, plan, status, correct, end, retro, add, and the cross-document readiness gate before each epic opens. Use when the user says: '/epic', 'create the epics', 'sprint planning', 'sprint status', 'correct course', 'wrap up epic', 'epic done', 'ready for next epic', 'add a unit to the epic'"
allowed-tools: mcp__gold__create_epic, mcp__gold__update_epic, mcp__gold__list_epic, mcp__gold__get_epic, mcp__gold__create_epic_backlog, mcp__gold__update_epic_backlog, mcp__gold__list_epic_backlog, mcp__gold__get_epic_backlog, mcp__gold__get_template_for, mcp__gold__list_glossary_term, mcp__gold__create_glossary_term, mcp__gold__update_glossary_term, mcp__gold__create_change_request, mcp__gold__list_change_request, mcp__gold__create_formal_review, mcp__gold__get_formal_review, mcp__gold__list_formal_review
targets_templates:
  - epic-template
  - epic_backlog-template
x-openbrain-source: epic/v8
x-openbrain-content-source-hash: sha256:562cb7f49c9351426d12e063b62015db82ca18763a6ecb705ecb47c6974027ff
x-openbrain-content-hash: sha256:a1b0e3c5be8a38e53b9451fca9107b594d7683f0daa2852c111a0951e6962883
---

# /epic - Epic Lifecycle Management

Routes epic-level workflow invocations to the right sub-workflow under `.claude/skills/epic/<verb>/`. Each verb produces or updates epic-scoped records (epic records, change-request unit records, a retro report, an end-of-epic closeout).

openbrain decomposes epics into change_request units — the `story` DTO is not used.

## Subcommands

| Slash form | Stage | Workflow file | Purpose |
|---|---|---|---|
| `/epic create` | start | `create/workflow.md` | Break the prioritized PRD into epics and thin change-request units. Creates slim `epic` records and inline thin units via `create_change_request`. |
| `/epic plan` | mid | `plan/workflow.md` | Decompose an epic into unit records for implementation. |
| `/epic add` | mid | `add/workflow.md` | Add one new thin unit to a running epic (decomposition-for-one). |
| `/epic status` | mid | `status/workflow.md` | Summarize epic and unit status and surface risks. Read-only — uses live `list_epic`/`list_change_request` queries. |
| `/epic correct` | mid | `correct/workflow.md` | Manage significant changes mid-epic. Triage: revise PRD, file epic addendum, escalate, or document-and-move-on. |
| `/epic end` | end | `end/workflow.md` | End-of-epic ritual — child-unit closure check, epic-level end reviews, findings into a final unit. |
| `/epic retro` | end | `retro/workflow.md` | Post-epic retrospective — extract lessons, assess success, feed forward to the next epic. |
| `/epic ready` | gate | `ready/workflow.md` | Cross-document readiness check — confirms PRD, UX design, architecture, and epics list are mutually consistent. Run before opening the next epic for implementation. |

## Routing

1. **Parse args.** If the invocation includes a verb, use it. If args are empty, render the table above and stop.
2. **Load the workflow.** Read `.claude/skills/epic/<verb>/workflow.md` fully and follow its instructions.
3. **Fuzzy match.** If the user names an intent without naming the verb ("wrap up epic 27" → `end`; "are we ready to start the next epic" → `ready`; "add a unit to epic 3" → `add`), dispatch on the inferred verb and call out the inference.

## Trigger phrases beyond the verbs

The dispatcher catches common epic-close and end-of-epic phrases:

- "wrap up epic N", "close epic N", "epic N done", "ship epic N", "tag the close", "anything before we close epic N?" → `/epic end`
- "what is next?", "open epic N+1", "lets move to epic N+1" while the current epic still has unfinished closeout work → `/epic end`
- "check sprint status", "show sprint status" → `/epic status`
- "run a retrospective", "lets retro the epic" → `/epic retro`
- "correct course", "propose sprint change" → `/epic correct`
- "add a unit", "add a task to the epic", "one more unit" → `/epic add`

## MCP

Tools and target templates are declared in the frontmatter (`allowed-tools`, `targets_templates`); invoke a tool as `mcp__gold__<tool>`. Before authoring any record, fetch its template with `get_template_for dto_type=<type>` — it is authoritative for fields and enums.

Before exploring or planning, load this product's glossary once with `list_glossary_term` (`include_summary=true`, `limit=100`; paginate by offset beyond 100) and use its vocabulary; when you coin or sharpen a term, record it with `create_glossary_term` / `update_glossary_term`.

## Unit creation ownership

The epic skill creates **thin husks** (title + summary + parent + status=draft) via `create_change_request` during decomposition (`create` step 3) and the `add` verb. Full detailing of a unit — 4-section body, requirement extraction, batch interview — is owned by the `/change-request create` verb and happens at pickup, one unit per session.

## When to defer

- **Epic creation** is facilitated as part of PRD work. Invoke `/epic create` after `/prd prioritize`.
- **Tagging cadence.** Per the `tag liberally` directive, every meaningful step in `/epic end` gets a version bump — review rounds, fix rounds, the final close. Workflow files under `end/` describe the cadence in detail.
