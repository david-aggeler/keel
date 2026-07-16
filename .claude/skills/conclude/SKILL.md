---
name: conclude
description: "Right after filing SoR records in gold, mature them in-session: collect objective evidence into the issue, then promote each issue to a change_request with requirements + acceptance criteria, and approve as far as the evidence allows. Use on '/conclude', '/consolidate', 'conclude these', 'consolidate these', 'conclude issue-xyz', 'consolidate issue-xyz'"
allowed-tools: mcp__gold__search_change_request, mcp__gold__create_change_request, mcp__gold__update_change_request, mcp__gold__get_change_request, mcp__gold__list_change_request, mcp__gold__get_issue, mcp__gold__update_issue, mcp__gold__list_issue, mcp__gold__search_requirement, mcp__gold__get_requirement, mcp__gold__create_requirement, mcp__gold__update_requirement, mcp__gold__list_requirement, mcp__gold__create_ac, mcp__gold__update_ac, mcp__gold__get_ac, mcp__gold__list_ac, mcp__gold__get_design_decision, mcp__gold__search_design_decision, mcp__gold__get_epic, mcp__gold__list_inbound_refs, mcp__gold__list_relations_for, mcp__gold__get_template_for
targets_templates:
  - change_request-template
  - requirement-template
  - ac-template
x-openbrain-source: conclude/v3
x-openbrain-content-source-hash: sha256:24c4b9582cb1312adf71690736abfc2884440bcf8357b149d0998c23a16d0ebc
x-openbrain-content-hash: sha256:862ec35014af015ee8aa765dac72c682534e267e57e11a897aec82c8515ee840
---

# Conclude

Mature freshly-filed records **in this session**, while the context is hot — not later in a fresh session that can't see the file:line you were just looking at.

For each issue created this session (or named in the call):

**1. Issue → analyzed.**
- Collect **objective evidence** into the issue: HEAD-cited (`git rev-parse --short HEAD` + `file:line` or a verbatim quote), checkable without asking the author anything.
- Verify against the current repo/records, not the record's prose. If reality differs, fix or close the record.
- Search for a **related requirement** and link it. Create a new one only if none fits.

**2. Mature the requirement.**
- Ensure it has ≥1 `acceptance_criteria` item covering this issue; fill a missing one if the session evidence supports it.
- Approve the requirement only when it has an AC and the evidence backs it. Otherwise leave it below approved with the blocking reason in its `details`.

**3. Issue → reviewed** (requires the above).

**4. Create the CR** (`parent` = the issue). `search_change_request` first to avoid duplicating an in-flight unit. Use `get_template_for change_request-template`.
- **Keep evidence DRY:** it lives in the issue. The CR `details` carries a pointer only (e.g. "Objective evidence: see `{issue}`") — don't copy or re-summarize.
- **A parentless CR** (no issue) carries its own Objective Evidence section in `details` — HEAD-cited. With no issue to point to, the CR *is* the evidence's home.

**5. Wire every link that genuinely exists** (invent none):
- **Requirement → CR `acceptance_criteria[]`** — load-bearing; never skip.
- **Governing `design_decision`** — both ways: CR `related` and the requirement's `related_decisions`.
- **Epic** — a CR has one `parent` (issue *or* epic) and no `epic` field. With a parent issue: set CR `parent: <issue>` and put the epic on the issue (`issue.epic`); the CR rolls up transitively. Without one: set CR `parent: <epic>`. Never add the CR to the epic's `related[]`. (`iteration` is a separate axis — set it on the CR/issue, not the epic.)
- **`related[]`** — real sibling issues/CRs, originating exploration, superseded records only.
- **Verify the rollup:** `list_inbound_refs(<epic>)` surfaces the issue (or parentless CR); `list_inbound_refs(<issue>)` surfaces the CR. A missing hop is a broken rollup even if each record looks assigned.

**6. Check consistency along `cr → (issue) → req → ac`** — that the records describe the *same* change, not just that links resolve. Read the text; confirm the CR addresses the issue's problem, every referenced requirement covers real CR scope (and no CR scope lacks a requirement), and every AC is a concrete test of its requirement's `statement`. Keep field names, enum values, and type slugs consistent across records. Auto-fix trivial drift (typos, stale ref, wrong slug) in-session. For substantive drift (scope mismatch, an AC that doesn't test its requirement, a contradiction): reconcile now if the evidence is present, else record the specific blocker in CR `details` and **do not approve**.

**7. Approve as far as the evidence allows.** Set `executor: agent`, `status: approved`, and `auto_merge: true` (except critical issues). A dependency (`depends_on`) is sequencing, not an approval blocker — approve anyway.

---
gold conventions: every `create_*` needs explicit `created_by`/`last_edited_by`/`status`; `update_*` is a full-payload replace — use the `fields:` sparse param for partial edits; link to an epic via the child's `parent`/`epic`, not the epic's `related[]`.
