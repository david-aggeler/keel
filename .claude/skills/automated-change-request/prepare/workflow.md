---
name: automated-change-request/prepare
description: 'Promote a reviewed issue into a draft change_request, runnable by a non-resident executor (claude -p). Use when an issue has status=reviewed and the operator says "prepare this issue", "promote issue to CR", or "run-prepare".'
---

# Automated Prepare

**Transition:** `issue (reviewed) → change_request (draft → approved | draft)`

**Goal:** Turn one **reviewed** issue into a **draft** `change_request`, then decide
whether an agent can implement it with no human context. If yes, stamp
`executor: agent` and advance the CR to `approved` (ready for the autonomous tail).
If human input is needed, stamp `executor: human`, write the open questions into the
CR body, and leave the CR at `draft` — **never approve a CR that has open questions.**

Approval is also gated on two quality checks that mirror the human `plan` verb: the CR
body must match the server-side `change_request` template, and it must carry
comprehensive acceptance criteria. A CR that fails either gate is **handed back as
`executor: human`** (Section 3B), never approved.

This is the **front half** of the autonomous loop. The back half
(`dev → review → merge → verify`) is the rest of this skill and is driven separately
by `openbrain-client run-tail` once this verb has produced an `approved`,
`executor: agent` CR.

## Executor contract (condensed — full text in `../SKILL.md`)

- **Self-sufficient:** this file is your complete instruction set for `prepare`.
- **No fabrication:** never claim a record was written that you did not write; on any
  unavailable tool or failed step, STOP and report the exact tool call + verbatim error.
- **Linear:** run every step yourself, in order; no subagents required.
- **Sparse writes:** `update_change_request` with only the changed keys via `fields:`
  (top-level args = full REPLACE → silent field drop); re-read after a status write.
- **Gate, don't ask:** you cannot field interactive questions. Where a human would be
  asked, you instead record the questions on the CR and hand back — never block.

## Inputs

You are given one issue ref, e.g. `openbrain/issue-170`. The product is the part
before the `/` (e.g. `openbrain`); the id is the part after (e.g. `issue-170`).

## 1. Precondition check (halt if unmet)

1. `get_issue product=<product> id=<issue-id>`.
2. Confirm `status == reviewed`. If it is anything else (`new`, `analyzed`,
   `in_progress`, `closed`, …), **halt** and report the actual status — only a
   human-reviewed issue is promotable. A `new` issue must be triaged through
   `analyzed` to `reviewed` first, which means the issue-review workflow has
   established the requirement + acceptance criteria contract.
3. **Duplicate guard.** `list_inbound_refs ref=<product>/<issue-id> src_dto_type=change_request`.
   For each returned change_request, `get_change_request` and check its status. If any
   linked CR is **not** `closed` (i.e. an open CR already exists for this issue),
   **halt** and report that CR's ref — do not create a second CR for the same issue.

## 2. Understand the issue

Read the issue's `details`, `summary`, and `type`. From them, judge **one** question:

> Can an agent implement this with no further human input — is the scope, the desired
> behavior, and the acceptance bar clear enough that the CR body alone is a complete
> brief?

- **Yes** → this is an `executor: agent` CR (Section 3A).
- **No** (it needs a design decision, an external credential/access, a product-owner
  choice, a clarification of ambiguous scope, or manual testing) → this is an
  `executor: human` CR (Section 3B).

When in doubt, choose **human**. A false `agent` stamp sends an under-specified unit
into the autonomous tail; a false `human` stamp only costs one human glance.

## 3A. Confident → agent CR, advance to approved

1. `create_change_request` with:
   - `product`: `<product>`
   - `title`: a concise CR title derived from the issue.
   - `summary`: one or two sentences of what changes.
   - `details`: the full brief — motivation, proposed change, and an
     `## Acceptance criteria` section an agent can implement and a reviewer can check.
   - `parent`: `<product>/<issue-id>` (links the CR to its driving issue).
   - `executor`: `agent`
   - `affects_products`: `["<product>"]`
   - `created_by`: `claude`
   - `last_edited_by`: `claude`
   - `status`: `draft`
2. Note the new CR id from the create result (e.g. `change_request-NNN`).
3. **Pre-approval gates — both must pass, else hand back as `executor: human`.** You
   cannot ask, so a failed gate is not a block — it is a downgrade to Section 3B.
   - **Template conformance.** Call `get_template_for type=change_request` and compare
     the CR's `details` body against the template's prescribed structure. Every section
     the template requires (motivation/context, proposed change, and an
     `## Acceptance criteria` section) must be present and filled — no missing section
     and no leftover placeholder. If the body does not match the server template,
     **switch to Section 3B**: set `executor: human`, leave the CR at `draft`, and record
     the structural gap as an open question.
   - **Comprehensive acceptance criteria.** The `## Acceptance criteria` section must be
     comprehensive, not token: it must cover the proposed change so a reviewer can
     objectively check it (the behavior, the relevant error/edge paths, and any
     discipline/golden test the change implies). If the criteria are absent or
     superficial, **switch to Section 3B** with the gap recorded as an open question —
     do not approve a thinly-specified unit.
4. Advance to approved — **sparse** write (only after both gates in step 3 pass):
   `update_change_request product=<product> id=<new-id>`
   `fields: { status: "approved", approver: "agent:prepare", approved_at: "<ISO8601 now>" }`.
5. **Re-read** (`get_change_request`) and confirm `status == approved` **and**
   `executor == agent` before reporting success. If either differs, **halt** and report.
6. Report: `prepared <product>/<new-id> (executor=agent, approved) from <issue ref>`.
   This CR is now a valid `run-tail` input.

## 3B. Needs input → human CR, stays draft

1. `create_change_request` with the same fields as 3A **except**:
   - `executor`: `human`
   - `status`: `draft` (and leave it there — do **not** advance)
   - `details`: include everything from 3A **plus** an explicit section:

     ```markdown
     ## Open questions for the human

     1. <question that blocks a confident agent implementation>
     2. <…>

     ## Resume signal (read this before answering)

     After you answer the questions above (edit this CR's details in place):
     - if an agent should now implement it, set `executor: agent` and the CR is
       picked up by `run-tail` (it advances draft→approved itself only if you also
       approve, or re-run prepare is NOT needed — just flip executor and approve);
     - if you will implement it yourself, keep `executor: human` and drive the CR
       manually.
     The autonomous tail will NOT touch this CR while `executor == human`.
     ```
2. **Re-read** and confirm the CR exists at `status == draft`, `executor == human`.
3. Report: `prepared <product>/<new-id> (executor=human, draft — N open questions) from <issue ref>`,
   then list the questions. This is a **hand-back**: a human must answer before the
   unit can proceed.

## 4. Halt discipline

If any tool call fails, returns an error, or a re-read does not confirm the state you
just wrote, **STOP** and report the exact tool + arguments + verbatim error. Do not
retry blindly, do not create a second CR, and never report a CR as prepared that you
did not confirm by re-read.
