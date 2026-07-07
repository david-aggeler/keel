# Step 6: Plan Steps 4..N — Growing Complexity

**Goal:** Sequence the remaining capability clusters from Step 2's walkthrough into ordered execution steps after the first feature. Each step adds **only the incremental infrastructure** it requires — never bulk-loads infrastructure that won't pay off until much later.

## The growing-complexity principle

Once the first feature is shipping, the question for every subsequent feature is the same: **what does this feature need that doesn't exist yet?** That delta is the infrastructure cost of this step. If two clusters share the same delta, ship them adjacent so the infrastructure is built once and reused. If a cluster needs heavyweight infrastructure (e.g., an event broker, a search index, a TSDB) it brings that infrastructure with it — but only when the cluster's user value justifies the build.

This produces a plan with a characteristic shape:

- Each step adds **one or two capabilities** the user can directly use.
- Each step pulls in **only the infrastructure those capabilities need** — not all infrastructure.
- The plan's complexity grows monotonically — early steps are smaller and simpler, later steps are bigger and more dependency-heavy.
- The plan stops being interesting after ~10–18 steps for typical PRDs. If you've got more than 20, the clusters are too granular — merge.

## How to order the remaining clusters

Apply these rules in priority order. When two clusters tie, ask the user.

1. **Strict dependency.** If cluster B depends on cluster A's infrastructure or contract, A goes first. Non-negotiable.
2. **User-journey order.** The PRD's user journeys are an ordered list of user-value priority. Journey-2's capabilities go before journey-3's, all else equal.
3. **Risk reduction.** A cluster that mitigates a high-exposure risk (per the PRD's Risk Mitigations) earns earlier placement, since later steps benefit from the risk being closed.
4. **Infrastructure amortization.** If two clusters share the same heavyweight infrastructure (e.g., event broker), ship them adjacent so the infrastructure cost is paid once.
5. **Stakeholder pressure.** A cluster the user / stakeholder explicitly wants early earns earlier placement. Capture this in the cluster's notes.
6. **Operational maturity progression.** Reporting / monitoring / observability typically come *after* the capabilities they observe — you can't usefully report on a feature that doesn't exist yet. Defer reporting clusters until enough source capabilities ship.
7. **Federation / external integration last.** OIDC/SAML/SCIM, AMQP/event-bus, billing-feed exports — these add value but always assume the system *runs*. They go in the second half of the plan.

## What each subsequent step contains

For each cluster you place after Step 3, capture:

- **Step number** (4, 5, 6, …)
- **Step title** (one-line — what it ships)
- **Capability cluster** it materializes (from Step 2's walkthrough)
- **FRs and NFRs covered** (rough list with counts)
- **Incremental infrastructure** it brings in (this is the explicit "what's new technically")
- **Depends on** (which earlier steps' infrastructure or contracts it requires)
- **User value when shipped** (one sentence — what can the user now do that they couldn't before)
- **Acceptance criteria** (one bullet for the test that proves the step shipped)
- **Negotiability flag** (TECHNICAL NECESSITY only if a downstream step strictly depends on it; otherwise NEGOTIABLE — most steps are negotiable)

## Stop conditions

- **5–20 steps total** is the target range. If you're under 5, the clusters are too coarse — split. Over 20, they're too granular — merge.
- **All capability clusters from Step 2's walkthrough are placed.** If a cluster doesn't appear in the plan, that's an explicit deferral — note it in the "out of scope at this time" list with rationale. Vision-section items from the PRD always count as deferrals.
- **The plan ends at "Vela can be operated by a non-author team"** — for Vela specifically, that's the Transition phase exit per the project lifecycle. For other projects, the equivalent: "the project no longer requires its build team to keep running."

## Actions

1. **Sequence the clusters** using the rules above. Draft the steps inline (in your context, not yet on disk).

2. **Sanity-check the resulting count.** 5–20. If outside, merge or split.

3. **For each step, draft a one-paragraph summary** (~50–80 words). Don't go too deep — this is the priority list, not the implementation plan.

4. **Verify the dependency graph holds.** Walk the steps in order and confirm each step's "Depends on" set has already been placed earlier. If not, you have a sequencing bug — fix.

5. **Note any "almost ties"** — places where two steps could swap order without breaking anything. These are prime candidates for the user to negotiate in Step 8.

## Hand-off

Proceed to `steps/step-07-mark-negotiable.md`. The next step assigns NECESSITY / NEGOTIABLE labels to every step.
