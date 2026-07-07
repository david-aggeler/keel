---
name: change-request/plan
description: 'Ratify the unit spec and stamp it approved. Use when the owner says "plan this CR" or "approve the unit".'
---

# Plan Change Request

**Transition:** `draft → approved`

**Goal:** Architect brief (exception-only KDs), requirement validation against codebase reality, owner one-batch confirmation, stamp `executor`/`merge_gate`/`auto_merge`.

## Execution

**1. Architect brief**

Review the unit's Decisions table (from `get_change_request`). The brief covers exceptions only:

- Decisions that contradict a front-loaded interview answer
- Decisions that contradict a `dev_defaults` catalog row (cite the key)
- Genuinely novel decisions with no precedent in the defaults catalog

Do not re-ask decisions the owner already confirmed in `create`. An answer already marked `(owner)` or carrying an override marker is owner-confirmed precedence — it is **never** re-surfaced as an exception. Shrink the brief to what is truly new or contradictory. **Surface a plan-phase question ONLY when a `dev_defaults` value is genuinely absent or conflicting — never merely to re-confirm a settled default.** A value is genuinely absent when no catalog key exists for the answer in question. A value is genuinely conflicting when two catalog rows disagree for the same key. Any other deviation is already settled and must not be re-raised. The plan owner-confirmation batch is the lifecycle's last owner gate before fully-AFK `dev`; keep it minimal.

**Owner unavailable.** If genuine exceptions exist (absent or conflicting `dev_defaults` values, per above) and the owner is unavailable to validate them, **block the unit** — leave it in `draft` and do not stamp `approved`. Do not proceed past unvalidated exceptions on a guessed answer. This block also covers create-phase answers that were not fully resolved before reaching plan: an unconfirmed cluster split or an unconfirmed `merge_gate` tier from the create batch are equally unresolved owner gates and must be settled here before `approved` is stamped. This is the *only* sanctioned block point: `dev` and `close` never wait on the owner, so any unresolved exception must be settled here or the unit waits in `draft`.

**2. Requirement validation**

For each ref in `acceptance_criteria`, call `get_requirement` and review the statement against codebase reality:

- If the requirement is accurate: no change.
- If the requirement conflicts with what the codebase already does: call `update_requirement` to correct the statement and GWT atoms. Add a row to the Decisions table noting the change.
- If a new requirement emerges from the architectural review: apply the search-first rule (`search_requirement`), then `create_requirement` and add the ref to `acceptance_criteria`.

**3. Owner confirmation batch**

Present the owner with a single batch:

- The revised Decisions table (changed rows marked)
- The requirement list (statement + GWT atoms for each ref)
- Proposed `executor` (agent or human), `merge_gate` tier, `auto_merge` flag

Ask for one-pass confirmation or override.

**4. Dependency check — auto_merge forced off if open dependencies exist**

Before stamping, read the unit's `depends_on` and `deferred_pending` fields from `get_change_request`.

For each ref listed in `depends_on` and `deferred_pending`:

1. Call `get_change_request` (or the appropriate `get_<type>`) on each ref.
2. If any referenced unit has a status that is not `closed`, **set `auto_merge: false`** regardless of the batch answer, and report to the operator:

> `auto_merge` forced off: unit depends on `<ref>` (status: `<status>`), which is not yet closed. Rerun `plan` once all dependencies are closed to re-evaluate auto_merge.

If all dependencies are closed (or there are none), use the owner-confirmed `auto_merge` flag.

**5. Pre-approval gates — both must pass before `approved` is stamped**

These are hard gates on the `draft → approved` transition. If either fails, **do not**
stamp `approved`: fix the record in place, or — when the gap needs an owner decision —
leave the unit in `draft` and report what is missing.

- **Template conformance.** Call `get_template_for type=change_request` and compare the
  unit's `details` body against the template's prescribed structure. Every section the
  server-side template requires (the 4-section body: motivation/context, proposed
  change, Decisions table, acceptance criteria) must be **present and filled** — no
  missing section and no leftover template stub/placeholder text. A CR whose body does
  not match the template stored on the MCP server is not approvable; reshape the body to
  conform before stamping.
- **Comprehensive acceptance criteria.** The unit must carry acceptance criteria that
  are comprehensive, not token. Confirm that:
  - `acceptance_criteria` is non-empty and every ref resolves (validated in step 2);
  - the criteria collectively cover the proposed change — every behavioral claim in the
    body maps to a requirement/GWT atom a reviewer could objectively check;
  - material paths are not missing (error/edge behavior, observability, and the unit's
    own discipline/golden test where the plan calls for one).

  If the criteria are thin or miss a material behavior, add the missing requirement
  (`search_requirement` → `create_requirement`, append the ref to `acceptance_criteria`)
  before approving. A CR with absent or superficial acceptance criteria must not be
  approved.
- **New DTO definition of done.** If the unit adds or registers a DTO type, the
  plan must explicitly cover requirement-723: released authoring template in the
  catalog, HELIX01 IncludedTypes entry at the schema version, data-movement
  coverage through the schema-driven fixture generator, and coverage-chain
  exercise where the type participates in verification coverage. If any item is
  deferred or absent, do not approve the unit until scope or acceptance criteria
  are corrected.

**6. Stamp and transition**

On owner confirmation (with the dependency-corrected `auto_merge`), call `update_change_request`:
- `status: approved`
- `executor`: confirmed value
- `merge_gate`: confirmed tier
- `auto_merge`: dependency-corrected flag (forced false if any open dependency exists; owner-confirmed flag otherwise)
- `acceptance_criteria`: updated list (if any new refs were added)
- `details`: updated body with revised Decisions table

The unit is now queue-eligible for agent pickup if `executor=agent`.
