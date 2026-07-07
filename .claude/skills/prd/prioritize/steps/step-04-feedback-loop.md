# Step 4: Plan Step 2 — Deployment-Feedback Loop

**Goal:** Define the *second* step of the execution plan: the loop that lets the project self-correct once base infrastructure is up. Without this, every subsequent step is flying blind.

## Why this is its own step (and not part of base infrastructure)

Base infrastructure (Step 1) gets the project to "the system can run". The feedback loop (Step 2) gets the project to "the system tells me when it's broken". These are different commitments. Bundling them into one step makes Step 1 too big to land in a single iteration; splitting them lets Step 1 ship sooner and Step 2 layer cleanly on top.

## What goes in here

The deployment-feedback loop has three components:

1. **Operational signal from the deployed instance** — `/health`, `/ready`, `/version`, `/build`, `/metrics` (Prometheus exposition format on the metrics endpoint). The deployed instance can answer "are you up?" and "what version are you?" without a human looking at logs.

2. **Automated verification post-deploy** — a smoke test that runs after every deploy and asserts a known-good behaviour (e.g., "POST /resources returns 201 with a task ID; GET /tasks/{id} reaches `completed` within N seconds"). Failure means the deploy didn't take.

3. **Drift gate in CI** — for spec-driven projects: regenerate stubs from spec, compare to committed stubs, fail the merge if they diverge. For projects without a spec, the equivalent is a contract test against a recorded baseline.

Optional but commonly worth including:

- **Structured-log shipping** to a queryable destination (the deployed instance's logs are fetchable and grep-able, not just on the box).
- **Alert on operational signal** — even a hand-curated alert is enough at this stage (a Slack webhook fired by `curl /health` failing is a feedback loop).
- **A "what's broken" dashboard** — even a static HTML page that pulls `/metrics` and renders five graphs is enough to start.

## What does NOT go in here

- **Threshold alerting on user-defined metrics** — that's product surface, not feedback loop. Comes later.
- **Distributed tracing** — useful but the basic feedback loop runs without it. Defer to a feature step that needs it.
- **Full observability platform** — Grafana + Prometheus + Loki + Tempo is *operational maturity*, not feedback loop. Static `/metrics` + smoke test is enough at this step.
- **Synthetic monitoring** — same logic.

## Acceptance criteria for "Step 2 done"

- A deployed instance answers `/health`, `/ready`, `/version`, `/build`, `/metrics`.
- A post-deploy smoke test runs automatically and reports pass/fail.
- The drift gate runs on every CI build and blocks merge on spec divergence.
- An operator can answer "is the deployed instance OK?" in under 10 seconds without SSH'ing to the box.

## Actions

1. **Draft the Step-2 description** as ~100–200 words plus a bulleted list. Reference the FRs and NFRs it satisfies (typically the operability/observability NFRs and any FRs about health endpoints, build provenance, drift detection).

2. **List explicit exclusions** with rationale — what's *not* in the feedback loop and why. (Threshold alerting, full tracing, etc.)

3. **Mark Step 2 as TECHNICAL NECESSITY.** Always. The rest of the plan can't self-correct without it.

## Hand-off

Proceed to `steps/step-05-first-feature.md`. Step 3 of the plan is the first executable feature — the smallest end-to-end thing that ships and delivers user value.
