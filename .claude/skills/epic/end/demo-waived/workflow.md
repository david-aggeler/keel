---
name: epic-close
description: 'End-of-epic ritual — demo-waived variant. Child-unit closure check, then reviewer-triage loop (cassandra + edge-case-hunter + winston), followed by epic-level end reviews by winston and cassandra. Findings land in a final unit via create_change_request(parent=<epic>). Use for backend-only and infrastructure epics where a demo is not warranted.'
---

# Epic Close — Demo-Waived Variant

## Pre-conditions

The parent workflow (`end/workflow.md`) has already confirmed all child units are closed. Do not re-run the closure check here.

## The sequence

```text
Step 0  Pre-flight summary                           (no skip)
Step 1  Impact assessment                            (no skip)
Step 2  Reviewer round (parallel)                    (always full roster)
Step 3  Winston triage + autonomous fixes            (no operator input by default)
Step 4  Loop check                                   (return to step 2 if SEV-1 still open)
Step 5  Epic-level end reviews: winston + cassandra  (always, G15)
Step 6  Final unit for findings                      (always, G15)
Step 7  Closeout                                     (operator sign-off)
```

### Step 0 — Pre-flight summary

Call `get_epic` for the closing epic. Call `list_change_request` with `filter={"parent":"<epic_ref>"}` to get the full unit list.

Output: a one-paragraph summary — what was the epic, what units shipped, what was deferred (if any units were closed as canceled/abandoned).

### Step 1 — Impact assessment

Decide which optional reviewers apply. Two checks:

**Sera check (security review readiness):**

Sera is **ready** when all of:

- `./architecture.md` is up to date for the epic's surface.
- The production binary serves at least one auth-bearing user-facing handler.
- Multi-tenancy boundary surfaces are real.

If any check fails, Sera is **NOT ready** — skip her.

Output of step 1: a small table showing reviewer status and reasoning.

Pause briefly for the operator to override (one-line "skip Sera" / "force Sera" / "go"). Default to the heuristic if no override arrives in the same turn.

### Step 2 — Reviewer round (parallel)

Spawn the always-on reviewers concurrently. Spawn Sera too if step 1 marked her ready.

- **Cassandra:** spawn the `cassandra` agent (adversarial reviewer) via the Agent tool. Brief: review the epic's deliverables (units closed in this epic). Find at least 8 findings — fewer if the surface really is clean; severity-tag each (SEV-1 / SEV-2 / SEV-3).
- **edge-case-hunter:** invoke `edge-case-hunter` against the epic's diff. Returns findings tagged `ech:` when passed to winston for triage.
- **Winston-as-reviewer:** invoke the `winston` agent to review the epic's delta with four questions only: (1) does the surface area introduced match the epic's stated scope? (2) are package boundaries respected? (3) is there abstraction debt? (4) is anything structurally unsound? File findings prefixed `winston-self:`.
- **Sera (if ready):** invoke `cse`. Brief: attack-surface mapping, control review, STRIDE enumeration, prioritised mitigations.

Each reviewer's findings get archived in `docs/findings/` — one file per pass, stable IDs. After the reviewer round, tag the work-in-progress.

### Step 3 — Winston triage + autonomous fixes

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

### Step 4 — Loop check

**Loop when:**

- Any SEV-1 finding is still open after triage.
- Any reviewer's "anything before close?" answer is non-trivial.

**Stop when:**

- All reviewers report "nothing material before close."
- All SEV-1 findings are resolved or have a deferred home.
- The operator calls "close it."

**Cap at 3 rounds by default.** Each round bumps a tag. Operator override on the cap if the surface is genuinely large.

If looping, return to step 2.

### Step 5 — Epic-level end reviews by winston and cassandra

Once the triage loop converges, run the epic-level end reviews. These are distinct from the per-round reviewer passes — they look at the epic as a whole:

**Winston epic review:** invoke the `winston` agent with the brief: "Perform an epic-level architectural end review. Review the full set of units closed in this epic and assess: (1) does the combined surface area form a coherent whole? (2) is the architecture direction correct? (3) what technical follow-up should land in the next epic?"

**Cassandra epic review:** invoke the `cassandra` agent with the brief: "Perform an epic-level adversarial end review. Review the complete set of deliverables and surface any systemic risks, missing coverage, or cross-unit inconsistencies that were not caught in per-round reviews."

Collect findings from both reviews.

### Step 6 — Final unit for findings

If either winston or cassandra surfaced findings that warrant follow-up work (improvements, debt items, systemic fixes), capture them in a final unit:

Call `create_change_request` with:
- `title` — "Epic {N} follow-up: {brief description of findings}"
- `summary` — one sentence summarizing the follow-up scope
- `parent` — the epic ref (same parent as the other units)
- `status` — `draft`

This final unit follows the normal unit lifecycle at pickup via `/change-request create`. It is not resolved before the epic closes — it is the handoff vehicle for findings that survived the triage loop.

If neither reviewer surfaced anything worth a follow-up unit, skip this step.

### Step 7 — Closeout

Once end reviews and the final unit (if any) are in place:

- Call `update_epic` with `status=done`, a `done_reason` of one sentence, and a `details` closeout addendum covering: units shipped, full tag arc, findings outcome (counts by reviewer, with status), final-unit ref (if created).
- Commit any remaining code changes. Tag the commit as the last in the series — that tag IS the close marker per the cadence directive.
- Push behind operator sign-off. Do NOT auto-push at this stage.
- Surface to the operator: "Epic N closed. Push when you're ready."
- Open Epic (N+1) when the operator signals readiness — that is a separate operation.
