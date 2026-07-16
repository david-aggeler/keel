---
name: architecture-create
description: "Create comprehensive architecture decisions through collaborative step-by-step discovery. Multi-step guided workflow authoring architecture_description records that AI agents implement consistently. Use when the user says: '/architecture', 'create architecture', 'architecture document', 'technical architecture', 'design the architecture'"
allowed-tools: mcp__gold__create_architecture_description, mcp__gold__update_architecture_description, mcp__gold__list_architecture_description, mcp__gold__get_architecture_description, mcp__gold__create_design_decision, mcp__gold__update_design_decision, mcp__gold__list_design_decision, mcp__gold__get_design_decision, mcp__gold__get_template_for, mcp__gold__list_glossary_term, mcp__gold__create_glossary_term, mcp__gold__update_glossary_term
targets_templates:
  - architecture_description-template
  - design_decision-template
  - dd_plan-template
x-openbrain-source: architecture-create/v6
x-openbrain-content-source-hash: sha256:849bb0391c43c16da66959cf5a0dcc905860443178b9924778e563bd7cd6d510
x-openbrain-content-hash: sha256:76b57e2dbd84043a503c0694679abe9b5f32168ccc2712ecefeb597a5b87cba0
---

# Architecture Workflow

**Goal:** Create comprehensive architecture decisions through collaborative step-by-step discovery that ensures AI agents implement consistently.

**Your Role:** You are an architectural facilitator collaborating with a peer. This is a partnership, not a client-vendor relationship. You bring structured thinking and architectural knowledge, while the user brings domain expertise and product vision. Work together as equals to make decisions that prevent implementation conflicts.

## Project-specific architectural facts

Load these as foundational context before beginning. They are decisions already made; do not re-open them.

- **Project:** keel
- **Primary language:** Go
- **Existing architecture decisions** are documented in gold `architecture_description` records. Load the product root with `list_architecture_description` and `get_architecture_description`; if a root exists, extend or refine it rather than restarting.

## Required output sections

The architecture document produced by this workflow MUST include two sections:

1. **Testing strategy** — test layers, coverage goals, runtime modes, mock vs real service boundaries. Architectural decisions about *what gets tested where*, not *how to write the test*.
2. **Deployment and local merge gate** — container/binary strategy, local dev stack, CI/CD gate. Architectural decisions about how the system ships.

Both sections are required. If either is absent at handoff, the architecture is not done.

## MCP

Tools and target templates are declared in the frontmatter (`allowed-tools`, `targets_templates`); invoke a tool as `mcp__gold__<tool>`. Before authoring any record, fetch its template with `get_template_for dto_type=<type>` — it is authoritative for fields and enums.

The write target for this workflow is `architecture_description`: create or update a root record plus ordered chapter records in gold. Do not create a local architecture markdown file as the canonical output.

Before exploring or planning, load this product's glossary once with `list_glossary_term` (`include_summary=true`, `limit=100`; paginate by offset beyond 100) and use its vocabulary; when you coin or sharpen a term, record it with `create_glossary_term` / `update_glossary_term`.

## Execution

Read fully and follow: `.claude/skills/architecture-create/steps/step-01-init.md` to begin the workflow.

**Note:** Input document discovery and all initialization protocols are handled in step-01-init.md.
