# /prd ready — Is this PRD ready to break into epics?

**Goal:** Lightweight readiness gate at the PRD level. Confirms the PRD has the substance needed to drive `/epic create` without backtracking.

**Sibling gates:** `/epic ready` (cross-document consistency once architecture + UX also exist) and `/story ready` (per-story dev-readiness). Each gate asks a different question; don't conflate them.

## When to run

Run after `/prd create` or `/prd edit`, before `/prd prioritize`. The output of `/prd prioritize` is the input to `/epic create` — so PRD-ready means "ready to be prioritized and decomposed".

If you have already run `/prd validate` and that came back clean, this gate is lighter weight — validate is the standards check, ready is the "are we ready to move on" check.

## Procedure

1. **Load the PRD.** Read `./prd.md` fully. If the file is absent, halt — run `/prd create` first.
2. **Walk the readiness checklist below.** For each item, write a single line: `✅ <item>` or `❌ <item> — <one-sentence gap>`.
3. **Surface the gaps.** If any item is `❌`, list them as a numbered remediation plan. Each remediation maps to one of: `/prd edit`, `/prd validate`, or a separate sibling artifact (architecture, UX).
4. **Verdict.** End with one line:
   - `🟢 PRD is ready for prioritization` — every checklist item green
   - `🟡 PRD is ready with caveats` — minor gaps, prioritization can proceed but flag the caveats
   - `🔴 PRD is not ready` — major gaps, fix before prioritization

## Readiness checklist

A PRD is ready to break into epics when every section below has substance — not placeholders, not "TBD with stakeholders":

- **Vision** — names the user, the problem, and the unique angle. Not a feature list.
- **Executive summary** — six-pager discipline: someone could understand the bet in 5 minutes without reading the rest.
- **Success criteria** — measurable. "Users like it" is not measurable; "30% of trial users renew after the first vApp expires" is.
- **User journeys** — at least one journey per primary persona, end-to-end. Pain points named, current alternatives named.
- **Domain analysis** — the problem space's vocabulary is defined, not assumed. Cross-references to existing research at `./research/` where applicable.
- **Functional requirements (FRs)** — each FR has an ID, a one-sentence statement, and an acceptance criterion. No FR is "TBD". FRs are dense, not bullet-listed wishlists.
- **Non-functional requirements (NFRs)** — performance, security, reliability, scalability targets each have a concrete number or named bound.
- **Scope** — explicit "in scope" and "explicitly out of scope" lists. The out-of-scope list is the one that prevents drift; if it's empty, the PRD hasn't been seriously scoped.
- **Project type signal** — the PRD names enough about the technical shape (backend service, CLI, single-page app, etc.) that the architect can start the architecture pass without re-interviewing.
- **Internal consistency** — no FR contradicts another FR; no NFR contradicts the scope; the success criteria are achievable given the scope.
- **No internal questions left open** — every "TBD" or "we'll figure this out" is either resolved or moved to a deliberate parking-lot section labelled as such.

## What this gate does NOT check

- Cross-document consistency with architecture and UX. That's `/epic ready`.
- Spelling, formatting, link integrity. That's `bin/vela-dev lint docs` (or the markdownlint pre-commit hook).
- Whether the PRD passes Cassandra's adversarial review. That's a separate human/agent gate; recommend `talk to Cassandra` if you want the doomsayer pass.
- Whether the prioritization is right. `/prd prioritize` is the next step; this gate clears the way *to* run it.

## Output

A short report — usually 15–30 lines. End with the verdict on its own line so an agent reading the output can act programmatically.
