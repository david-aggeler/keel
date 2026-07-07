---
name: code-review
description: "Review code changes adversarially using parallel review layers (Blind Hunter, Edge Case Hunter, Acceptance Auditor) with structured triage into actionable categories. Use when the user says: '/code-review', 'review this code', 'run code review', 'review the diff'"
allowed-tools: mcp__gold__create_formal_review, mcp__gold__update_formal_review, mcp__gold__list_formal_review, mcp__gold__get_template_for
targets_templates:
  - formal_review-template
x-openbrain-source: code-review/v2
x-openbrain-content-source-hash: sha256:3076e9f571780396aa4c1aa3fcef12dc8d5ec3db8dc2c4d96fc99ec4ac93c549
x-openbrain-content-hash: sha256:aeaccc9772fb0a9e3bba3ac79ac257f07a8db11b81b4a3f747550276911397d9
---

# Code Review Workflow

**Goal:** Review code changes adversarially using parallel review layers and structured triage.

**Your Role:** You are an elite code reviewer. You gather context, launch parallel adversarial reviews, triage findings with precision, and present actionable results. No noise, no filler.

## Review Layers

Three parallel passes run over the diff or file(s) under review:

1. **Blind Hunter** — Read the code with no prior context. Flag anything that looks wrong, incomplete, or surprising on its face — missing error handling, off-by-one conditions, shadowed variables, silent discards.
2. **Edge Case Hunter** — Invoke the `edge-case-hunter` skill. Scope: the diff or provided content. Outputs a JSON array of unhandled paths. Merge into triage.
3. **Acceptance Auditor** — Load the story or acceptance criteria for the work under review (if available). For each AC item, determine pass/fail against the diff. Surface any gap between what was specified and what was shipped.

## Triage Categories

After the three passes complete, triage all findings into:

| Category | Definition |
|---|---|
| **Blocking** | Must fix before merge — correctness bug, security gap, missing AC |
| **Should Fix** | Strong recommendation; deferral needs explicit justification |
| **Nitpick** | Style, naming, optional — call out once, don't repeat |
| **Deferred** | Acknowledged, out of scope for this story — log it |

## MCP

Tools and target templates are declared in the frontmatter (`allowed-tools`, `targets_templates`); invoke a tool as `mcp__gold__<tool>`. Before authoring any record, fetch its template with `get_template_for dto_type=<type>` — it is authoritative for fields and enums.

## FIRST STEP

Read fully and follow: `.claude/skills/code-review/steps/step-01-gather-context.md`
