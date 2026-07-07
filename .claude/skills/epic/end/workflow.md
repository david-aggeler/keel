# /epic end — End-of-epic ritual (menu)

**Goal:** Close out the current epic with a defensible ritual. Two paths live underneath; pick deliberately, not by accident.

## Pre-condition: child-unit closure check

Before dispatching to either path, verify all units under this epic are closed.

Call `list_change_request` with `filter={"parent":"<epic_ref>"}`. If any unit is NOT in `status=closed`, halt and surface the list:

> The following units must be closed before the epic can end:
> - {unit_ref}: {unit_title} (status: {status})
>
> Run `/change-request close` for each open unit, then return to `/epic end`.

Do not proceed until all child units are closed.

## The two paths

| Path | When to pick | Workflow file |
|---|---|---|
| **Demo-waived** | Backend-only epics, infrastructure epics, anything that does not produce user-visible surface area. Reviewer-triage loop only. | `demo-waived/workflow.md` |
| **Demo-suggested** | User-visible epics where the operator wants the option of a demo. Proposes a demo at Step 1; accepts a decline without argument. The full reviewer-triage loop runs either way. | `demo-suggested/workflow.md` |

Both paths end with:
- Epic-level end reviews by the seeded `winston` and `cassandra` agents.
- Any findings are captured in a final unit via `create_change_request(parent=<epic>)` for follow-up through the normal unit lifecycle.

## How to pick

Ask the operator which path fits this epic. Two short questions usually settle it:

1. **Does this epic produce user-visible surface area?** If clearly no — backend, infra, MCP plumbing — pick **demo-waived**. If yes — UI work, user-facing flows — pick **demo-suggested**.
2. **Has the operator asked to see a demo, explicitly?** If yes — **demo-suggested**. Default to demo-waived otherwise.

If the operator just says "close epic N" without picking, default to demo-waived and surface the choice in one short line so they can redirect.

## Dispatch

After picking the path:

- **demo-waived** → Read `demo-waived/workflow.md` fully and follow its instructions.
- **demo-suggested** → Read `demo-suggested/workflow.md` fully and follow its instructions.

## What NOT to do

- Don't run *both* paths. They overlap; running both is wasted effort.
- Don't skip the reviewer-triage loop on either path. The closing ritual includes the winston and cassandra end reviews.
- Don't skip the child-unit closure check. An epic with open units is not ready to close.
