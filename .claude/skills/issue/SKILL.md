---
name: issue
description: "Issue management in gold for the keel product. Use when the user says: '/issue', 'create an issue', 'file an issue', 'log this bug', 'close issue', 'triage issues', 'list issues'"
allowed-tools: mcp__gold__create_issue, mcp__gold__update_issue, mcp__gold__list_issue, mcp__gold__get_issue, mcp__gold__search_issue, mcp__gold__get_template_for, mcp__gold__search_requirement, mcp__gold__get_requirement, mcp__gold__create_requirement, mcp__gold__update_requirement
targets_templates:
  - issue-template
  - issue_fix-template
x-openbrain-source: issue/v6
x-openbrain-content-source-hash: sha256:fa85a36cc4de16dcf403448fd5572fa3991e3591a88846fc12ac970c54b1076d
x-openbrain-content-hash: sha256:2cacd1fd8d5cf9f65654a8781ab4ee7b6d1d378670ae6dbe2c29c7896e494607
---

# Issue Skill

Manages MCP hosted issues. Create, update, and triage. The unique ID is assigned by the server when an issue is created.

## MCP

Tools and target templates are declared in the frontmatter (`allowed-tools`, `targets_templates`); invoke a tool as `mcp__gold__<tool>`. Before authoring any record, fetch its template with `get_template_for dto_type=<type>` — it is authoritative for fields and enums. ID minting and field updates are server-side.

| Intent | Action |
|---|---|
| File a new bug / improvement / doc gap | `get_template_for dto_type=issue` (authoritative for fields and enums) → `create_issue` |
| Analyze, review, close, reopen | `update_issue` with the sparse `fields` overlay — e.g. `fields={status:"analyzed"}` or `fields={status:"closed", close_reason:"obsolete"}`. Pass ONLY the fields you are changing. |
| List, browse, triage | `list_issue product=keel` (`include_deferred=true` for deferred). No server-side status filter — filter client-side or use `search_issue`. Render results as a markdown table. |

When in doubt between create and update: no existing issue ID in the
message → create.

## Objective evidence on create

When creating an issue, collect **comprehensive objective evidence** as available
from logs and code, and embed it in the record (`details`) so the issue is
actionable without a follow-up round-trip:

- **From code** — cite specific `file:line` references and quote the relevant
  lines verbatim. Name the commit you read at (e.g. current `HEAD`) so the
  evidence is anchored in time.
- **From logs** — paste the verbatim log line(s), error message(s), stack
  trace, or command output. We have centralized logging — no claim without the
  log that backs it.
- Prefer **questionless, objective** statements (file:line / verbatim quote)
  over speculation. Verify every claim against the current repo and gold before
  writing it down — do not file a bare assertion.

## Analyzed stage

<!-- DHF-REQ: openbrain/requirement-778 -->
Use `status: analyzed` as the explicit review-preparation state, ordered between `new` and `reviewed`. This is where the issue is shaped into an implementable contract before any change request is created.

Before advancing an issue to `status: analyzed`
(`update_issue ... fields={status:"analyzed"}`), confirm **both**:

1. **Template conformance** — the record matches its template as stored on the
   MCP server. Fetch the authoritative template with
   `get_template_for dto_type=issue` and verify every field and enum it declares
   is present and well-formed in the record. Do not rely on memory of the
   template — re-fetch it.
2. **Comprehensive objective evidence** — the issue carries the objective
   evidence described in *Objective evidence on create* (code `file:line`,
   verbatim logs, reproduction) in enough depth that an executor can act without
   asking a question. If the evidence is thin, gather it **before** marking
   analyzed — never advance on an unverified or speculative claim.

Then locate or create the linked requirement:

1. Inspect `related_requirements` and any requirement refs in `related`. If none
   clearly states the issue's required behavior, call `search_requirement`; reuse
   an equivalent requirement or call `create_requirement` for the missing
   behavior.
2. Ensure that requirement carries acceptance criteria. The requirement
   `acceptance_criteria` field must contain at least one objective GWT-style atom
   or equivalent checkable criterion for the behavior the issue needs fixed. If
   the acceptance criteria are missing or wrong, call `update_requirement` to
   correct them.
3. If a linked requirement was already `approved` but its acceptance criteria
   were missing or wrong, drop the requirement from approved while correcting it
   (for example to `draft` or the product's current pre-approval state), then
   re-approve the requirement only after the acceptance criteria are in place.
4. Link the issue back to the requirement with a sparse `update_issue` to
   `related_requirements` if needed. Do not remove unrelated links.

## Reviewed gate

Before advancing an issue from `analyzed` to `reviewed`
(`update_issue ... fields={status:"reviewed"}`), confirm the linked requirement is `approved`. This is the workflow-enforced gate that proves the requirement + acceptance criteria are in place before the issue becomes promotable. Do not use or add a server-side transition reject for this gate; violations are caught by workflow discipline and consistency checking.

Exception: a trivial/non-code exempt issue may move to `reviewed` without a
linked approved requirement when the issue records why no implementable software
contract is needed. Keep the exemption explicit in `details`.

## Updating without dropping fields

`update_issue` has two write modes. Always use the **`fields`** overlay for a
partial change such as closing or reopening:

- **`fields={...}`** — a sparse overlay. Supply only the keys you are changing;
  every other existing field (`summary`, `details`, `type`, `source`, …) is
  preserved. This is the correct way to change status or close an issue.
- **Top-level field args** (`status=...`, `title=...`) — these are merged onto
  the existing record by default, but passing `replace=true` makes them REPLACE
  the whole payload, dropping any field you did not resupply. Do not use
  `replace=true` for a status change.

Rule of thumb: to close an issue, call
`update_issue product=keel id=<issue-id> fields={status:"closed", close_reason:"<reason>"}`.
Never re-send the whole payload just to change one field, and never pass
`replace=true` for a partial update.

## Fix work routing

Fix work always lands in a unit (change request), not in an issue directly:

- Use `/change-request create` and set `parent: <issue-ref>` to link the fix unit to this issue.
- The unit's `close` verb creates the `issue_fix` record(s) — one per version fixed, with `fixed_in_version` and a `change_request` ref back to the unit. `issue_fix` is a per-version fix-documentation record, not a competing work lane.
- When all versions have an `issue_fix` record, `close` offers to close the parent issue.
- Do not create `issue_fix` records directly from the issue skill.
