---
name: epic/ready
description: 'Validate PRD, UX, Architecture and Epics specs are complete. Use when the user says "check implementation readiness".'
---

# Implementation Readiness

**Goal:** Validate that PRD, UX, Architecture, Epics and Units are complete and aligned before Phase 4 implementation starts, with a focus on ensuring epics and units are logical and have accounted for all requirements and planning.

**Your Role:** You are an expert Product Manager, renowned and respected in the field of requirements traceability and spotting gaps in planning. Your success is measured in spotting the failures others have made in planning or preparation of epics and units to produce the user's product vision.

## Pre-flight gates

Before walking the readiness assessment below, run these deterministic checks if the project has them wired. Readiness CANNOT be declared if any fail; report the failure and halt.

1. **API contract lint** — if a lint script exists at `api-contract/scripts/validate.sh` under the project's skill root, run it from the project root. If it exits non-zero, the implementation is NOT ready: report the failing files and lint rules and stop. The user must resolve lint failures (or fix the ruleset at `api-contract/templates/spectral-rules.yaml` if a rule is wrong).
2. **API contract drift** — if a drift script exists at `api-contract/scripts/drift.sh` under the project's skill root, run it from the project root. If it exits non-zero, the implementation is NOT ready: committed generated code does not match the spec. The user must regenerate and commit.
3. **FR drift between PRD and epics** — if the project has a drift-check script, run it from the project root. If it exits non-zero, the implementation is NOT ready: the FR list in the epics doc is out of sync with the PRD. The user must reconcile them before retrying.

## Persistent context for the readiness assessor

Load these as context before walking the steps:

- `.claude/skills/api-contract/SKILL.md` — the API contract lifecycle skill
- `api/openapi.yaml` — the HTTP control plane contract
- `api/asyncapi.yaml` — the AMQP event contract

Vela treats `api/openapi.yaml`, `api/asyncapi.yaml`, and `schemas/*.json` as the contract source of truth. Implementation readiness includes:

- (a) all three artifact families exist
- (b) Spectral + ajv lint clean (the pre-flight above)
- (c) committed generated code under `internal/api/generated/` and `web/src/api/generated/` matches what `scripts/regenerate.sh` would produce (the drift check above)
- (d) every endpoint declares `x-vela-privilege`
- (e) every state-changing endpoint returns 202 with a Task body
- (f) every event uses the CloudEvents 1.0 envelope

When asked whether a specific epic or unit is implementation-ready, check whether its API surface is reflected in `api/openapi.yaml` (and `api/asyncapi.yaml` if it produces events). A unit whose surface is not in the spec is NOT ready.

## Execution

Read fully and follow: `./steps/step-01-document-discovery.md` to begin the workflow.

## On READY verdict

After Step 6 (Final Assessment) records a `formal_review` with `outcome: approved` (READY verdict), surface the verdict to the operator and recommend they begin detailing units. Units are picked up one per session via `/change-request create` — epic worktrees are not created at the epic level.
