---
name: code-review
description: "Review code changes adversarially using parallel review layers (Blind Hunter, Edge Case Hunter, Acceptance Auditor), triage the findings, and land them as records. Loaded automatically by the autonomous tail's dev and review verbs, so it records no skill activation. Use when the user says: '/code-review', 'review this code', 'run code review', 'review the diff'"
allowed-tools: mcp__gold__create_formal_review, mcp__gold__update_formal_review, mcp__gold__list_formal_review, mcp__gold__create_issue, mcp__gold__create_action_item, mcp__gold__get_template_for
targets_templates:
  - formal_review-template
x-openbrain-source: code-review/v4
x-openbrain-content-source-hash: sha256:10acc2b34f2c7d3cc6bce87846541c412d2d386e8516358b0f47fa7f7b5189ac
x-openbrain-content-hash: sha256:c6820577bf59476e1515dec5d902289067935f8f92562fd695f9ddc0cf3fc86c
---

# Code Review Workflow

**Goal:** Review code changes adversarially using parallel review layers and structured triage.

**This skill is invoked automatically — not only when a human asks for it.** `openbrain-client` names it in `requiredSkillSlugs` alongside `automated-change-request`, so every autonomous `dev` and `review` verb session is handed this file's path as an additional instruction file and reads it before acting. Two consequences worth knowing:

- **It records no skill activation.** The tail loads it by filesystem path, not by trigger phrase, so activation-counting usage reports show it as cold no matter how heavily the machine uses it. Judge its use by reads of this file, never by activations — the v2 withdrawal was taken on that false signal and broke the tail's skill-currentness gate for every unit.
- **It must stay released and materialized.** If this skill is missing from the catalog or from `.claude/skills/`, the gate refuses before any session starts, so `dev` and `review` cannot run at all.

**Your Role:** You are an elite code reviewer. You gather context, launch parallel adversarial reviews, triage findings with precision, and land actionable results as records. No noise, no filler.

## Review Layers

Three parallel passes run over the diff or file(s) under review:

1. **Blind Hunter** — Read the code with no prior context. Flag anything that looks wrong, incomplete, or surprising on its face — missing error handling, off-by-one conditions, shadowed variables, silent discards.
2. **Edge Case Hunter** — Invoke the `edge-case-hunter` skill. Scope: the diff or provided content. Outputs a JSON array of unhandled paths. Merge into triage.
3. **Acceptance Auditor** — Load the acceptance criteria for the work under review (if available). For each AC, determine pass/fail against the diff. Surface any gap between what was specified and what was shipped.

## Triage Categories

After the three passes complete, triage all findings into:

| Category | Definition |
|---|---|
| **Blocking** | Must fix before merge — correctness bug, security gap, missing AC |
| **Should Fix** | Strong recommendation; deferral needs explicit justification |
| **Nitpick** | Style, naming, optional — call out once, don't repeat |
| **Deferred** | Acknowledged, out of scope for this change — record it |

## Where Findings Land

Records, never local markdown. The review itself is a `formal_review` record; every finding that outlives the session becomes a record of its own, routed by who can resolve it:

| Finding | Destination |
|---|---|
| Applied during the session | The diff, plus a line in the `formal_review` details |
| Agent-solvable, not applied | `create_issue` with `executor: agent` |
| Needs human judgment or out-of-band action | `create_action_item` |
| Deferred pre-existing defect | `create_issue` with `executor: agent`, noted as pre-existing |

## MCP

Tools and target templates are declared in the frontmatter (`allowed-tools`, `targets_templates`); invoke a tool as `mcp__gold__<tool>`. Before authoring any record, fetch its template with `get_template_for dto_type=<type>` — it is authoritative for fields and enums.

## FIRST STEP

Read fully and follow: `.claude/skills/code-review/steps/step-01-gather-context.md`
