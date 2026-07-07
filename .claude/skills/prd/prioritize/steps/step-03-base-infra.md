# Step 3: Plan Step 1 — Base Infrastructure

**Goal:** Define the *first* step of the execution plan: the base infrastructure that has to exist before *any* feature can ship. This step is non-negotiable as a precondition for everything downstream.

## What goes in here

Only what every subsequent feature step depends on. Resist the urge to put "all infrastructure" in this step — that violates the workflow's core principle ("base infrastructure has to be there, but not all infrastructure needs to be there for all features"). Infrastructure that's only needed by one specific feature ships *with* that feature, not here.

The standard candidates for base infrastructure:

- **Build & dev environment.** Reproducible setup, language-toolchain pinning, build target ("can I build the binary at all?"), pre-commit hooks for the languages in use.
- **API contract + spec validation.** OpenAPI / AsyncAPI / JSON Schema lint and the spec-drift gate. Generated stubs committed.
- **Mock server.** Day-1 deliverable. Realistic seed data per the PRD. Same `internal/ports/` interfaces as the real server. The mock server unlocks every other parallel stream.
- **External-substrate emulator (if applicable).** For projects with a hypervisor / cloud / device backend, the CI-grade emulator that lets every integration test run with zero real infrastructure. This is non-negotiable for projects with a "tests must run without real X" requirement.
- **Database schema baseline + migrations.** Schema versioned with a migration tool. First migration creates the core tables. Without this, no feature persists state.
- **CI pipeline.** Build, lint, unit tests, component tests, integration tests against the emulator. Race detector if the language requires it. Green CI is the gate for everything downstream.
- **Auth middleware + RBAC primitives.** Default-deny middleware, the smallest auth that the project's threat model requires (often just a single shared bearer token or local accounts at this stage). Everything else hangs off this.
- **Structured logging + audit log baseline.** Append-only audit log table, structured JSON logging convention. Auditability has to be there from day one — retro-fitting it is expensive and incomplete.
- **Single-binary build + deployment plumbing.** Build artifacts produced reproducibly, packaged for the target deployment surface, the dumbest deployment that works (manual scp + systemd unit is fine if that's where the project starts).

Some projects need additional substrate (e.g., a message broker, a secret store, a key/cert PKI). Add only if the *next two or three feature steps* would be blocked without it. If only step 12 needs it, it ships with step 12.

## What does NOT go in here

- **Federation / SSO.** Useful for production but not for "the system can run". Local accounts are enough at base.
- **Event bus / AMQP.** Same logic — useful for downstream consumers but not for "the system runs".
- **Backup / DR.** Operational concern; doesn't gate any feature shipping.
- **Reporting / analytics.** Comes after data exists.
- **Distributed tracing.** Useful but step 2's basic feedback loop is enough at the start.
- **Multi-tenant isolation that doesn't apply yet.** If the project's first cohort is one tenant, multi-tenant infra ships with the second tenant.

## Acceptance criteria for "Step 1 done"

The user-facing test for whether base infrastructure is done:

- The mock server is up and serves every endpoint with seed data.
- CI is green on a hello-world PR.
- The single binary builds and deploys to the dev environment.
- A `/health` endpoint responds 200 from the deployed instance.
- The database schema is at version 1 and migrations run cleanly.

If any of those isn't true, Step 1 isn't done. Don't move past it.

## Actions

1. **Draft the Step-1 description** as ~100–200 words plus a bulleted list of inclusions. Reference the FRs and NFRs from the PRD it satisfies (typically the test-infrastructure NFRs, the API-contract NFRs, the audit-log FRs, the structured-logging NFRs).

2. **List the explicit exclusions** — items you considered for base but pushed downstream, with one-line rationale each. This list is gold for the negotiation step (Step 8) — the user can see what's *not* in base and decide if anything should be promoted.

3. **Mark Step 1 as TECHNICAL NECESSITY.** Always. This is not negotiable.

## Hand-off

Proceed to `steps/step-04-feedback-loop.md`. Step 2 of the plan is the deployment-feedback loop.
