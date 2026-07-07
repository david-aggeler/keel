# Step 5: Plan Step 3 — First Executable Feature

**Goal:** Define the *third* step of the execution plan: the smallest end-to-end feature that delivers real user value once base infrastructure (Step 1) and the feedback loop (Step 2) are in place. This is the first time a user can use the system for its intended purpose.

## Why "smallest end-to-end" and not "biggest possible"

A first feature that ships exercises the full stack: API + storage + adapter + UI (or other client). It's the proof that base infrastructure works, that the feedback loop catches regressions, and that the team's iteration pattern (write spec → generate stubs → write handler → write test → ship) actually functions.

If the first feature is too big, Step 3 doesn't ship in a sprint and the project is stuck. If it's too small (e.g., "the system serves a static page"), it doesn't exercise enough of the stack to be a meaningful proof — base infrastructure could be subtly broken and you wouldn't know.

The right size: **the smallest CRUD-shaped operation on the project's central entity, end-to-end.** For Vela that's vApp create/get/list (and probably stop/delete). For a CMS, it's article create/get/list. For a billing system, it's invoice create/get. Pick the noun that most defines the product, then expose CRUD on it.

## What goes in here

- **The capability cluster you identified in Step 2's walkthrough as the "smallest end-to-end cluster"** — typically the FRs corresponding to the primary user journey's first ~5 minutes.
- **Whatever specific infrastructure that cluster requires that wasn't in base.** This is where the workflow's core principle — "infrastructure grows hand-in-hand with feature complexity" — first applies. If this feature needs a specific external service mocked beyond what base provides, mock it here. If it needs a specific permission primitive beyond default-deny + auth, add it here.
- **An end-to-end automated test** that exercises the feature against the mock server and the emulator (if present). The test is the user-visible proof that Step 3 shipped.
- **A user-visible client surface** — the UI (or CLI / Terraform-resource / curl-recipe) that the primary-journey user actually interacts with. Without this, the feature isn't shipped to users; it's an API endpoint waiting for a client.

## What does NOT go in here

- **Adjacent features the primary user doesn't need on day one** — e.g., for vApp CRUD, the catalog tier is not in this step (it ships in a later feature step).
- **Operational nice-to-haves** — bulk operations, advanced filters, soft-delete recovery. Single-record CRUD is enough for "first feature".
- **Multi-tenant isolation, sharing, fine-grained RBAC** beyond ownership — defer to the access-control feature step.

## Acceptance criteria for "Step 3 done"

The primary user journey's "happy path", reduced to its minimum, completes end-to-end. For Vela that's: a user logs in (with whatever auth is in base), creates a record (vApp), reads it, updates it, deletes it. All against the mock server in CI; ideally also against the emulator and a deployed instance.

The mock server stays consistent with the real implementation throughout (no divergence). The drift gate from Step 2 catches any spec drift introduced by adding the feature.

## Actions

1. **Identify the cluster from Step 2's walkthrough.** Almost always corresponds to journey 1 in the PRD's User Journeys section.

2. **Draft the Step-3 description** as ~100–200 words plus a bulleted list of FRs covered. Note the journey it corresponds to.

3. **List the incremental infrastructure this feature pulls in** — beyond what base provided. Common items: a specific table, a specific external-service mock, a specific permission primitive, a specific client-side rendering pattern.

4. **List explicit exclusions** with rationale — what was tempting to include but defers to a later step.

5. **Mark Step 3 as NEGOTIABLE.** Even though it's "first feature" by convention, the user might have a different first feature in mind based on stakeholder priorities. The negotiation step (Step 8 of this workflow) is where they reorder if needed.

## Hand-off

Proceed to `steps/step-06-growing-complexity.md`. Steps 4..N of the plan are the remaining features in growing-complexity order.
