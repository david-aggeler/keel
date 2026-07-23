---
name: change-request/review
description: 'Check DHF annotation coverage and produce formal review records. Use when implementation is complete and the unit should be reviewed before merge.'
---

# Review Change Request

**Transition:** `in_progress → implementation_review → ready_to_merge`

**Goal:** Advisory DHF-REQ/DHF-TEST annotation coverage report; produce `formal_review` records.

## Execution

**1. Load the unit**

Resolve the unit's requirements **and their acceptance criteria** kind-aware (see SKILL.md § acceptance contract): `feature` — the CR's `requirements`; `fix` — `get_issue` on `parent`, its `related_requirements`. Coverage is checked against the requirements; the tests trace to their ACs.

Call `update_change_request status=implementation_review` before starting review work.

**2. Advisory coverage report**

For each resolved requirement (and its acceptance criteria):

- Run inline: `rg "DHF-REQ: {product}/requirement-{id}"` to find implementing code markers.
- Run inline: `rg "DHF-TEST: {product}/requirement-{id}"` to find test markers.
- Record: ref, implementing files found, test files found.

Emit a coverage table:

| Requirement | DHF-REQ hits | DHF-TEST hits | Status |
|---|---|---|---|
| {product}/requirement-{id} | {n} | {n} | covered / missing |

**This report is advisory only.** Missing annotations are a finding to surface, not a blocker for the `close` verb. Enforcement (close-blocking, deterministic lint) is explicitly deferred per G7.

**3. Produce formal_review records**

For each reviewer (operator-specified, or the main session as the sole reviewer):

Call `create_formal_review` with:
- `subject`: ref to this change request
- `verdict`: in_progress (or the reviewer's verdict if already given)
- `notes`: review observations (coverage gaps, code concerns, suggestions)

**4. Transition**

When review is complete and no blocking findings remain, call `update_change_request status=ready_to_merge`.

Inform the operator: "Unit is now `ready_to_merge`. Run `close` to land it."
