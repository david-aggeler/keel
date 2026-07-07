# Step 7: Mark Each Step as TECHNICAL NECESSITY or NEGOTIABLE

**Goal:** Tag each step in the draft plan with one of two labels so the user knows which steps are open to reordering and which are not.

## The labels

- **TECHNICAL NECESSITY** — at least one downstream step strictly depends on this step's infrastructure or contract. The order is fixed by dependency. Reordering would break the plan.
- **NEGOTIABLE** — the step's order is driven by user-value priority, not dependency. Could be moved earlier or later without breaking other steps. The user decides.

## The rules

- **Step 1 (base infrastructure) is always TECHNICAL NECESSITY.** Without it, nothing else can ship.
- **Step 2 (feedback loop) is always TECHNICAL NECESSITY.** Without it, the rest of the plan can't self-correct.
- **Step 3 (first feature) is NEGOTIABLE.** Even though it's the conventional first feature, the user might prefer a different first cluster.
- **Steps 4..N — apply the dependency test.** A step is TECHNICAL NECESSITY if and only if at least one later step's infrastructure or contract strictly depends on it. Otherwise it's NEGOTIABLE.

## How to apply the dependency test

For each step beyond Step 2:

1. Walk forward through the remaining steps.
2. For each later step, ask: "If this current step shipped *after* the later step, would the later step still be implementable?"
3. If yes for all later steps, this step is NEGOTIABLE.
4. If no for any — there's a strict dependency — mark TECHNICAL NECESSITY and note which later step depends.

Examples:

- **Auth & RBAC primitives** (if not in base) — usually NECESSITY. Almost everything depends on the user being authenticated.
- **API contract drift gate** — NECESSITY (if not in base). Every feature depends on it.
- **A specific feature like "GPU resource management"** — usually NEGOTIABLE. No other feature depends on GPU.
- **External event bus (AMQP)** — NECESSITY *if* a later step is "external billing system consumes events". Otherwise NEGOTIABLE.
- **Federation (OIDC/SAML/SCIM)** — usually NEGOTIABLE. Local accounts work for everything else; federation is operational maturity.
- **Reporting** — usually NEGOTIABLE. The capabilities being reported on still ship without reports.

## What "negotiable" does NOT mean

- It does not mean "optional". Every step in the plan ships eventually.
- It does not mean "low priority". A NEGOTIABLE step might still be the user's #1 priority — it just means the user chooses where it lands, not the dependency graph.
- It does not mean "easy". A NEGOTIABLE feature can be very large.

## Actions

1. **Walk the draft plan from steps 3 through N**, applying the dependency test to each.

2. **Tag each step explicitly.** In your draft, add `[TECHNICAL NECESSITY — depends on by step K]` or `[NEGOTIABLE]` to each step's title.

3. **For each TECHNICAL NECESSITY step, name the downstream dependency.** "Required by Step 7 (Migration tooling, which depends on idempotent task tracking from this step)" — explicit, traceable.

4. **Count the NEGOTIABLE steps.** This is what the user negotiates in Step 8. If you have zero or one negotiable step, the plan is over-constrained — the dependency graph is too rigid, or you've labelled too many as NECESSITY without justification. Re-walk the test.

## Hand-off

Proceed to `steps/step-08-negotiate-with-user.md`. The next step presents the plan to the user and iterates with them on the negotiable items.
