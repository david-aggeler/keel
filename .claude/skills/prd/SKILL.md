---
name: prd
description: "Single entry point for every Product Requirements Document workflow. Subcommands cover create, edit, prioritize, validate, and the readiness gate before epic decomposition. Use when the user says: '/prd', 'create the PRD', 'edit the PRD', 'validate the PRD', 'prioritize the PRD', 'is the PRD ready', 'write a product requirements document'"
allowed-tools: mcp__gold__create_requirement, mcp__gold__update_requirement, mcp__gold__list_requirement, mcp__gold__get_requirement, mcp__gold__create_user_need, mcp__gold__update_user_need, mcp__gold__list_user_need, mcp__gold__get_user_need, mcp__gold__get_template_for, mcp__gold__list_glossary_term, mcp__gold__create_glossary_term, mcp__gold__update_glossary_term
targets_templates:
  - requirement-template
  - user_need-template
x-openbrain-source: prd/v6
x-openbrain-content-source-hash: sha256:6bc303acd8954e496335481eb86c7bd3075c36656e3fc1381f6bf8b64a298bc3
x-openbrain-content-hash: sha256:37350b8b9a3d99476e5fc5c3060dbd258568235afaec36ba9aec7552d4babd4b
---

# /prd — PRD lifecycle dispatcher

Routes PRD workflow invocations to the right sub-workflow under `.claude/skills/prd/<verb>/`. Each verb is a self-contained walk-through that produces or modifies `./prd.md`.

## Subcommands

| Slash form | Stage | Workflow file | Purpose |
|---|---|---|---|
| `/prd create` | start | `create/workflow.md` | Create a PRD from scratch. 12-step facilitated workflow — discovery → vision → success criteria → journeys → domain → innovation → project type → scoping → FRs → NFRs → polish → complete. Writes `./prd.md`. |
| `/prd edit` | mid | `edit/workflow.md` | Edit an existing PRD section by section. Loads the current PRD, walks targeted updates, preserves the rest. |
| `/prd prioritize` | mid | `prioritize/workflow.md` | Prioritize a completed PRD into a 5–20 step ordered execution plan that grows infrastructure hand-in-hand with feature complexity. Output feeds `/epic create` and `/epic plan`. |
| `/prd validate` | end | `validate/workflow.md` | Validate the PRD against standards — completeness, leanness, organization, cohesion. Run before declaring the PRD ready for downstream consumers. |
| `/prd ready` | gate | `ready/workflow.md` | Lightweight readiness check — "is this PRD ready to break into epics?". Sister gate to `/epic ready` (cross-document consistency) and `/story ready` (per-story dev-readiness). |

## Routing

1. **Parse args.** If the invocation includes a verb (`/prd create`, `/prd edit`, …), use that. If args are empty or whitespace, render the table above and stop — let the user pick.
2. **Load the workflow.** Read the workflow file at `.claude/skills/prd/<verb>/workflow.md` fully. Follow its instructions. Each workflow is self-contained — once routed, the dispatcher gets out of the way.
3. **Fuzzy match on intent.** If the user names an intent without naming the verb ("let's start a PRD" → `create`; "is my PRD complete" → `validate`; "what should we build first" → `prioritize`), dispatch on the inferred verb but call out the inference in one short line so the user can redirect.

## MCP

Tools and target templates are declared in the frontmatter (`allowed-tools`, `targets_templates`); invoke a tool as `mcp__gold__<tool>`. Before authoring any record, fetch its template with `get_template_for dto_type=<type>` — it is authoritative for fields and enums.

Before exploring or planning, load this product's glossary once with `list_glossary_term` (`include_summary=true`, `limit=100`; paginate by offset beyond 100) and use its vocabulary; when you coin or sharpen a term, record it with `create_glossary_term` / `update_glossary_term`.

## When to defer

- **Architecture and UX specs are sibling artifacts.** PRD describes *what*; architecture describes *how*; UX describes *what the user sees*. The PRD is upstream of both but doesn't author them.
- **Epics + stories are downstream.** Once the PRD is `validate`'d and `prioritize`'d, the next move is `/epic create` to decompose it. `/prd ready` is the formal gate between the two.

## Note on the workflow content

Each `<verb>/workflow.md` is a self-contained walkthrough; the substance lives in the per-step files under `steps/` and in the templates under each workflow's `templates/` directory.
