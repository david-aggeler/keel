---
name: epic/plan-checklist
description: 'Post-decomposition verification for epic units. Run after /epic plan, /epic add, or /epic create step 5.'
x-openbrain-content-hash: sha256:525221de6579758147c80653cd3acb2c3e7a168d9d447eb7c95799d638a5540e
---

# Epic Decomposition Checklist

Verify against the record graph, not against what you intended to write. Every line is a live query.

## Epic

- [ ] `get_epic` resolves and `plan` points at a real `dd_plan` (not prose)
- [ ] `related_requirements` lists every requirement the units reference
- [ ] `exclusions` names what is deliberately out — an empty exclusions list on a non-trivial epic is a finding, not a pass
- [ ] `status` matches reality: `planned` before work starts, `active` while units are open

## Units — `list_change_request filter={"parent":"<epic_ref>"}`

For each unit:

- [ ] `kind` is `feature`
- [ ] `requirements` contains at least one requirement ref — the structural invariant, checked at every status
- [ ] `parent` resolves to the intended epic and is **not** an issue
- [ ] `iteration` is set — without it the unit is invisible to a run-queue drain
- [ ] `executor` is set — without `agent` the unit never qualifies for one
- [ ] `status` is `draft` (or a later rung reached deliberately)
- [ ] `title` is action-oriented and distinct from every sibling
- [ ] `depends_on` reflects the real order; no cycles

## Requirements behind the units

- [ ] Each referenced requirement resolves via `get_requirement`
- [ ] Each has at least one `ac` record with `parent` set to it (`list_ac`)
- [ ] Any unit intended for near-term approval has at least one requirement at `approved` or later — otherwise the approval write will be rejected

## Coverage

- [ ] Every requirement in the epic's `related_requirements` is referenced by at least one unit
- [ ] No requirement is claimed by units under two different epics
- [ ] No unit duplicates a sibling's requirement set without a stated reason

## Reporting

Report as a markdown table with one row per check and an explicit pass/fail. A failing row is fixed before completion is reported — never carried forward as a note.
