<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 -->
# Step 2: Architectural Walkthrough — Identify Dependency Clusters

**Goal:** Walk the PRD as Winston (the architect) and group the FR/NFR surface into **dependency clusters** — sets of capabilities that share infrastructure or naturally ship together. The plan you draft later is a sequence over these clusters, not over individual FRs.

## Why this matters

A 184-FR PRD is too granular to sequence directly. A 5–20 step plan has to cluster FRs into chunks the user can reason about. The clusters should respect three principles:

1. **Technical cohesion.** FRs that depend on the same infrastructure go together (e.g., all FRs needing the Proxmox emulator cluster behind it).
2. **User-value coherence.** A cluster should ship something a user can *use*, not just a half-feature (e.g., "vApp CRUD" needs the API + the emulator + the basic UI to be a real ship).
3. **Cuttability.** A cluster is a candidate for one execution step. Too big = it can't ship in a sprint; too small = the plan has 50 steps.

## Actions

1. **Walk the PRD's Functional Requirements section.** As Winston, group FRs into clusters by capability domain. Most PRDs already use capability-area subheadings (e.g., "vApp Lifecycle", "Catalog & Templates", "IPAM & DNS", "Identity & Audit") — use those as the starting cluster boundaries, then split or merge as the dependency picture demands.

2. **For each cluster, note:**
   - **Cluster name** (one of: a capability area, a cross-cutting concern, or an infrastructure layer)
   - **FRs and NFRs covered** (FR-N references; rough count)
   - **What it depends on** (other clusters; specific infrastructure; specific contracts)
   - **What depends on it** (downstream clusters)
   - **What user value it delivers when shipped standalone** — if "none, it's pure infrastructure", that's a flag it belongs in the base infra step

3. **Identify cross-cutting concerns** that don't fit a single capability cluster. Common ones:
   - **API contract + spec drift gate** — touches every cluster
   - **Test infrastructure** (mock server, emulator, in-memory DB) — touches every cluster
   - **Audit / observability / structured logging** — touches every cluster
   - **Auth + RBAC middleware** — touches every cluster
   - **CI / build / deployment pipeline** — touches every cluster
   - **Database schema + migrations** — touches every cluster
   - These typically belong in the base infrastructure step (Step 1 of the plan), not as their own user-value steps.

4. **Surface the dependency graph briefly.** You don't need to draw a diagram (though a Mermaid block can help if the user asks). You need to be able to answer "if I shipped only cluster X, which other clusters does it require?" for any X.

5. **Identify the smallest end-to-end cluster.** This is the candidate for "first feature" (Step 3 of the plan). It should:
   - Exercise the API + storage + at least one external integration (or mocked equivalent)
   - Map to the PRD's primary user journey (journey 1, almost always)
   - Not require any other capability cluster to be valuable
   - Typically: a CRUD-shaped operation on the system's central entity

## Output of this step

A working list (in your context, not yet on disk) that looks roughly like:

```
Cluster: Base Infrastructure (cross-cutting)
  - API contract + spec drift gate
  - Mock server with seed data
  - Hypervisor / external-substrate emulator (if applicable)
  - CI pipeline + test pyramid (unit / component / integration)
  - PostgreSQL schema baseline + migrations tool
  - Auth middleware + RBAC primitives
  - Structured logging + audit log baseline
  - Single-binary build + deployment plumbing

Cluster: Feedback Loop (cross-cutting)
  - Prometheus /metrics
  - /health, /ready, /version, /build endpoints
  - Smoke test against deployed instance
  - Drift gate in CI (spec vs implementation)

Cluster: <Capability-1> — e.g., vApp Lifecycle
  - FR-N..FR-M (~K FRs)
  - Depends on: Base Infrastructure, <X>, <Y>
  - Delivers: <user value sentence>

Cluster: <Capability-2>
  ...
```

## Hand-off

Proceed to `steps/step-03-base-infra.md`. Steps 3–6 turn these clusters into a sequenced plan.
