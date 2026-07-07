---
name: epic/end/demo-suggested
description: 'End-of-epic ritual — demo-suggested variant. Child-unit closure check, then proposes a demo at Step 1 and accepts a decline without argument. Full reviewer-triage loop runs either way. Epic-level end reviews by winston and cassandra. Findings land in a final unit via create_change_request(parent=<epic>). Use when the operator wants the option of a demo for user-visible epics.'
---

# Epic Close — Demo-Suggested Variant

## Pre-conditions

The parent workflow (`end/workflow.md`) has already confirmed all child units are closed. Do not re-run the closure check here.

## The sequence

```text
Step 0  Pre-flight summary                           (no skip)
Step 1  Impact assessment + propose demo             (propose; accept decline)
Step 2  Demo — if accepted at step 1                 (skip if declined)
Step 3  Reviewer round (parallel)                    (always full roster)
Step 4  Winston triage + autonomous fixes            (no operator input by default)
Step 5  Loop check                                   (return to step 3 if SEV-1 still open)
Step 6  Epic-level end reviews: winston + cassandra  (always, G15)
Step 7  Final unit for findings                      (always, G15)
Step 8  Closeout                                     (operator sign-off)
```

### Step 0 — Pre-flight summary

Call `get_epic` for the closing epic. Call `list_change_request` with `filter={"parent":"<epic_ref>"}` to get the full unit list.

Output: a one-paragraph summary — what was the epic, what units shipped, what was deferred (if any units were closed as canceled/abandoned).

### Step 1 — Impact assessment + propose demo

Decide which optional steps apply.

**Demo check (does this epic have user-visible deliverables that warrant a demo?):**

Review the epic's unit list. A demo is warranted if any of:

- Units produced user-visible features (forms, lists, detail pages, dashboards, settings panes).
- Units reference UX design requirements.
- New code lands under the project's frontend root.

A demo is NOT warranted if all units are backend, infrastructure, or internal protocol surfaces.

**Propose a demo.** Ask the operator whether to run a demo iteration for this epic. Accept a decline without argument. If declined, record `Demo proposal: declined` and add a single `## Demo declined` line to the closeout commit body (Step 8) so the choice surfaces in `git log`. If accepted, proceed to Step 2; if declined, skip Step 2 and continue to Step 3.

**Sera check (security review readiness):**

Sera is **ready** when all of:

- `./architecture.md` is up to date for the epic's surface.
- The production binary serves at least one auth-bearing user-facing handler.
- Multi-tenancy boundary surfaces are real.

If any check fails, Sera is **NOT ready** — skip her.

Output of step 1: a small table showing reviewer/demo status and reasoning. Pause briefly for the operator to override.

### Step 2 — Demo (if accepted at step 1)

Invoke your project's prototype or demo skill. Inputs:

- **Scope:** evolve from the latest demo version.
- **Source intent:** "Demonstrate what Epic N shipped." Map the epic's units to the surfaces the demo should expose.

After the demo lands, surface it to the operator for review. Proceed to Step 3 when the operator confirms. If the operator asks for tweaks, the demo re-runs.

If declined at Step 1, skip this step wholesale.

### Step 3 — Reviewer round (parallel)

Spawn the always-on reviewers concurrently. Spawn Sera too if step 1 marked her ready.

- **Cassandra:** spawn the `cassandra` agent (adversarial reviewer) via the Agent tool. Brief: review the epic's deliverables (units closed in this epic). Find at least 8 findings — fewer if the surface really is clean; severity-tag each (SEV-1 / SEV-2 / SEV-3).
- **edge-case-hunter:** invoke `edge-case-hunter` against the epic's diff. Returns findings tagged `ech:` when passed to winston for triage.
- **Winston-as-reviewer:** invoke the `winston` agent to review the epic's delta with four questions only: (1) does the surface area introduced match the epic's stated scope? (2) are package boundaries respected? (3) is there abstraction debt? (4) is anything structurally unsound? File findings prefixed `winston-self:`.
- **Sera (if ready):** invoke `cse`. Brief: attack-surface mapping, control review, STRIDE enumeration, prioritised mitigations.

Each reviewer's findings get archived in `docs/findings/` — one file per pass, stable IDs. After the reviewer round, tag the work-in-progress.

### Step 4 — Winston triage + autonomous fixes

Winston consolidates all findings — cassandra + edge-case-hunter (tagged `ech:`) + his own (`winston-self:`) + Sera (if applicable) — into the action-issue list.

For each finding, Winston decides:

| Decision | Criteria | Action |
|---|---|---|
| **fix-now** | Small, mechanical, low-risk. | Land the code change in this triage pass. |
| **file** | Deferred — too large or needs a decision. | Write a `docs/issues/00NN` entry. |
| **reject** | Out of scope or wrong. | Record rationale in the finding's status field. |
| **needs-operator** | Real judgment call. | Surface with the specific question and options. |

**Default to autonomous; operator is for genuine arbitration, not for rubber-stamping fix-nows.**

Land the fix-now batch as one or more commits. Tag it. Findings files get their statuses flipped.

### Step 5 — Loop check

**Loop when:**

- Any SEV-1 finding is still open after triage.
- Any reviewer's "anything before close?" answer is non-trivial.

**Stop when:**

- All reviewers report "nothing material before close."
- All SEV-1 findings are resolved or have a deferred home.
- The operator calls "close it."

**Cap at 3 rounds by default.** Each round bumps a tag. Operator override on the cap if the surface is genuinely large.

If looping, return to step 3.

### Step 6 — Epic-level end reviews by winston and cassandra

Once the triage loop converges, run the epic-level end reviews. These are distinct from the per-round reviewer passes — they look at the epic as a whole:

**Winston epic review:** invoke the `winston` agent with the brief: "Perform an epic-level architectural end review. Review the full set of units closed in this epic and assess: (1) does the combined surface area form a coherent whole? (2) is the architecture direction correct? (3) what technical follow-up should land in the next epic?"

**Cassandra epic review:** invoke the `cassandra` agent with the brief: "Perform an epic-level adversarial end review. Review the complete set of deliverables and surface any systemic risks, missing coverage, or cross-unit inconsistencies that were not caught in per-round reviews."

Collect findings from both reviews.

### Step 7 — Final unit for findings

If either winston or cassandra surfaced findings that warrant follow-up work (improvements, debt items, systemic fixes), capture them in a final unit:

Call `create_change_request` with:
- `title` — "Epic {N} follow-up: {brief description of findings}"
- `summary` — one sentence summarizing the follow-up scope
- `parent` — the epic ref (same parent as the other units)
- `status` — `draft`

This final unit follows the normal unit lifecycle at pickup via `/change-request create`. It is not resolved before the epic closes — it is the handoff vehicle for findings that survived the triage loop.

If neither reviewer surfaced anything worth a follow-up unit, skip this step.

### Step 8 — Closeout

Once end reviews and the final unit (if any) are in place:

- Call `update_epic` with `status=done`, a `done_reason` of one sentence, and a `details` closeout addendum covering: units shipped, full tag arc, findings outcome (counts by reviewer, with status), final-unit ref (if created).
- Commit any remaining code changes. Tag the commit as the last in the series — that tag IS the close marker per the cadence directive.
- If Step 1 recorded a demo decline, add a `## Demo declined` line to the commit message body.
- Push behind operator sign-off. Do NOT auto-push at this stage.
- Surface to the operator: "Epic N closed. Push when you're ready."
- Open Epic (N+1) when the operator signals readiness — that is a separate operation.
