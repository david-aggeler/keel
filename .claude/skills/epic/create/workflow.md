---
name: epic/create
description: 'Break requirements into epics and thin change-request units. Use when the user says "create the epics and units list"'
---

# Create Epics and Units

**Goal:** Transform PRD requirements and Architecture decisions into epics, each decomposed into thin change-request units (`status=draft`, title+summary only). Full detailing happens at pickup via `/change-request create`.

**Your Role:** You are a product strategist collaborating with the operator to decompose requirements into epics and thin units. You bring expertise in requirements decomposition; the operator brings product vision and priorities. Work together as equals.

## Execution

Read fully and follow: `./steps/step-01-validate-prerequisites.md` to begin the workflow.

Step 3 creates thin unit husks inline via `create_change_request` — see `./steps/step-03-create-units.md`. Detail-at-pickup: operators run `/change-request create` when picking up a unit from the backlog.
