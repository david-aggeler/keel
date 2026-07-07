# Step 1: Load Context

**Goal:** Bring the PRD, project context, and architecture into your working memory before drafting any plan. The plan you produce in steps 3–6 should reflect what the project actually *is*, not a generic template.

## Actions

1. **Locate the PRD.** Use the discovery rules from SKILL.md (whole-file `./*prd*.md` or sharded `./*prd*/*.md`). HALT and ask the user for the path if you can't find one.

2. **Read the full PRD.** All of it. The prioritization plan needs the whole picture — Executive Summary for the user-value frame, Success Criteria for the measurable bar, Product Scope for the in-scope list, User Journeys for the user-value sequence, FRs and NFRs for the capability surface, Risk Mitigations for the constraints.

3. **Read project context.** `**/project-context.md` if present. Project context typically encodes architectural rules, technology stack pins, and "non-obvious" implementation rules that constrain sequencing (e.g., "mock server is the Day 1 deliverable", "API-first, no internal shortcuts"). These constraints often determine what counts as base infrastructure.

4. **Read architecture if available.** `./*architecture*.md`. Architecture docs typically pre-resolve dependency questions you'd otherwise have to infer. If architecture is missing, that's fine — note it and infer from the PRD's API Backend Specifics, Non-Functional Requirements, and Domain-Specific Requirements sections.

5. **Confirm scope with the user.** Briefly: "I've loaded the PRD ({N} FRs across {M} capability areas), project context, {and architecture / no architecture document found}. Anything else I should consider before I draft the priority plan?" Wait for response. The user may flag a constraint not in the docs (e.g., "we're targeting first-org cutover by Q3", "the dev cluster only has 3 nodes"). Note any such constraints — they shape sequencing.

## What to capture

By the end of this step, you should be able to answer in one sentence each:

- **What does the system do?** (one-line product purpose)
- **Who is the primary user?** (the journey-1 persona — the happy path)
- **What's the substrate?** (Proxmox / AWS / on-prem / etc.)
- **What's the headline scale target?** (N users, N records, N requests/sec — whichever applies)
- **Is there a forced timeline or first-cutover deadline?**
- **Are there explicit out-of-scope guards?** (Vision section, deferrals, post-MVP markers)
- **What architectural rules will constrain sequencing?** (e.g., "API-first contract", "no scripting languages for core code", "single-tier topology before two-tier")

You don't have to write these answers down for the user. You do have to know them — the next step's walkthrough leans on them.

## Hand-off

Proceed to `steps/step-02-architectural-walkthrough.md`.
