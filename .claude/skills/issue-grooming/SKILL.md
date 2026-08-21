---
name: issue-grooming
description: "Groom the open issue backlog in gold for the keel product: classify each issue by the version it was found in (backfilling affects_versions), bucket by the work class it needs (catalog, SoR records, code), assign each bucket to its own dedicated open iteration, then conclude the code bucket only — catalog and SoR issues are done once assigned to a lane. Use when the user says: '/issue-grooming', 'groom the issues', 'issue grooming', 'triage the new issues', 'what came in since the last release', 'sort the backlog into iterations', 'assign issues to iterations', 'bucket the open issues'"
allowed-tools: mcp__gold__list_issue, mcp__gold__get_issue, mcp__gold__update_issue, mcp__gold__search_issue, mcp__gold__list_change_request, mcp__gold__update_change_request, mcp__gold__list_iteration, mcp__gold__get_iteration, mcp__gold__create_iteration, mcp__gold__update_iteration, mcp__gold__admin_list_product_versions, mcp__gold__list_inbound_refs, mcp__gold__get_template_for, mcp__gold__search_requirement, mcp__gold__get_requirement
targets_templates:
  - iteration-template
x-openbrain-source: issue-grooming/v3
x-openbrain-content-source-hash: sha256:19947a16b75840d1c2825607a6e11e3082607688e0ef2f9ad95b16d20998e224
x-openbrain-content-hash: sha256:fabf8a070f5dfde15beb5f4e264a211f47b4c9e597eefbf36cdd56132589374e
---

# Issue Grooming

Turns an unsorted open-issue backlog into assigned, concluded work. Runs in five
steps; do them in order and show the user the table at step 4 before writing
anything.

Everything is gold, product `keel`. Every write is a **sparse**
`update_issue ... fields={...}` — `update_*` is a full-payload replace
otherwise.

## Step 0 — Build the version timeline

`admin_list_product_versions product=keel`. Two facts come out of it:

- **The released head** — the highest-numbered version with `Status: released`.
- **The version windows.** A version record is minted `in_development` at the
  moment its predecessor is released, so version *V* was the line under
  development over `[V.CreatedAt, next(V).CreatedAt)`. That interval is what
  maps an issue's `created_at` onto a version.

## Step 1 — Scope the sweep

`list_issue product=keel status=["new","analyzed","reviewed","reopened","blocked","on_hold"] include_summary=true limit=100` (page with `offset`).

Default scope is **the current line** — issues whose `created_at` falls at or
after the released head's window opened. On a 1.5.0 released head that means
everything filed against 1.5.x. Anything older is pre-existing backlog: leave it
out unless the user widens the scope, and say how many you skipped.

## Step 2 — Classify version-found

`affects_versions` (array of **qualified root refs**, `keel/1.5.0` — a bare
`1.5.0` is rejected) is the version-found property. It exists but is not
maintained, so classify before you sort.

Derive a candidate from `created_at` × the step-0 windows, then correct it with
`source`:

| `source` | Found on | Version-found |
|---|---|---|
| `session_observation`, `ci_failure`, `code_review` | the development line, mid-development | the **in-development** version of that window |
| `user_report`, `external_audit` | a deployed instance | the **released** version live at that time — usually the window's predecessor |
| `other`, absent | — | ambiguous; ask |

Write the derived value with `fields={affects_versions:["keel/<v>"]}` only
where the rule lands unambiguously. Where source and timing disagree, or the
issue predates the current line, list it for the user rather than guessing —
a wrong version-found is worse than an empty one. Keep an already-populated
value as it stands; that one was asserted.

## Step 3 — Bucket by work class

Three buckets, decided by **what has to change to close the issue**, not by
where the symptom showed up:

| Bucket | The change lands in | Examples |
|---|---|---|
| **catalog** | skills, templates, methods, runbooks, executors — authored in gold, moved by catalog export/import | skill prose contradicts the shipped binary; a template lags a renamed field; agent projection stale |
| **SoR records** | record content in gold — requirements, ACs, docs-as-records, wiring, statuses | a doc-tier sweep, a requirement amendment, re-parenting, a stale iteration body |
| **code** | the repo — application code, schemas, gates, generated artifacts | everything else |

**The decisive test is the edit surface: where does the fix get typed?**

- typed into gold through `catalog_update_skill` / `catalog_update_template` /
  `catalog_update_method` / `catalog_update_runbook` / `catalog_update_executor` → **catalog**
- typed into gold through a record `update_<type>` → **SoR records**
- typed into a file in the repo and landed by a merge gate → **code**

Rules that decide the near misses:

- **A skill or template fix is catalog — always — even when the symptom is a
  repo file.** `.claude/skills/<slug>/` and `.claude/agents/*.md` in a consuming
  repo are *materialized copies*, never the fix site; the source is the
  gold `SKILL.md` and the projection follows on re-export and
  re-materialization. "The skill text is wrong / stale / contradicts the binary /
  lags a rename" is a catalog issue no matter how much repo path appears in the
  report. A repo path in the issue body never pulls one of these into the code
  bucket.
- A **DTO schema** change is *code* (the schema files live in the repo and are
  gated), even though its effect is felt on records.
- An issue that needs an owner decision before any change is legible belongs to
  its eventual bucket, but stays `on_hold`/`blocked` with the decision named —
  it is not assigned to a drainable iteration.
- **A combo issue — one whose fix genuinely spans two buckets (a skill change
  *and* a code consumer, a schema change *and* a record heal) — is not
  groomable.** Do not pick a bucket for it, do not assign it to a lane, and
  **do not conclude it**: no requirement, no AC, no CR. Mark it `blocked` and list it in
  the step-4 table under **combo — needs split**, naming the two buckets. It
  becomes groomable only after the owner splits it into one issue per bucket.
  Forcing a combo into a single lane is what produces skill work assigned to a code lane.

## Step 4 — Match or open the iteration

For each non-empty bucket, `list_iteration product=keel status=["draft","ready"]`
and find the lane that already fits. The house naming carries the line and the
round, so a match is usually literal:

- code → `v<line> line bug-fix sweep #<n>` (e.g. `v1.5 line bug-fix sweep #3`)
- catalog → a catalog/skill lane for the line
- SoR records → a record-grooming lane for the line

**One bucket per iteration — never mix.** A catalog issue and a SoR-records
issue each go to a lane of their own kind; neither belongs in a code bug-fix
sweep, and a code issue belongs in neither a catalog nor a record lane. The
lanes drain by different mechanics (gold edits + catalog export vs. a merge
gate), so a mixed lane cannot be drained by either. If the bucket is non-empty
and no lane of that kind is open for the line, **open one** — a two-issue
catalog lane is correct and expected; the code sweep is not a fallback.

**Only a `draft` or a `ready` lane is a target.** Those are the two states in which
a lane's roster is still being assembled — `draft` while it is being put together,
`ready` when it is complete and fit to drain. An `active` lane is mid-drain: a runner
is working its members now, so adding one changes the roster underneath a run that
already qualified its set, and the new member is neither picked up by the run in
flight nor visible as waiting. `completed` and `closed` lanes are history. If the
only lane of the right kind for the line is `active`, it is **not** a match — open
the next round instead.

**The fence is one-directional: it stops arrivals, not departures.** Moving a record
**out** of an active lane is allowed and is sometimes the point — a lane whose last
one or two members turn out to belong elsewhere is cleared by re-laning them, which
is what lets it complete instead of staying open on work it was never going to do.
Re-lane a departing record to a `draft` or `ready` lane of the right kind by the same
rules above. What is forbidden is adding a member to a lane a runner is already
draining, because the run qualified its set before the member existed.

If no `draft` or `ready` lane of that kind exists for the line, `create_iteration`
with the next round number in the same naming shape: `status: draft`,
`product: keel`, a `summary` that names the line, the bucket, and why the
lane was opened, and explicit `created_by` / `last_edited_by`. Leave it `draft`
while you are still assigning to it; advance it to `ready` once the roster is
complete and it is fit to drain.

## Step 4b — Sweep blocked members out of the active lanes

The fence's allowed direction, used on purpose. `list_iteration
product=keel status=["active"]`, read each lane's members
(`list_inbound_refs`, or `list_issue` / `list_change_request` filtered on
`iteration`), and pick out the ones that **cannot reach a terminal state as they
stand**:

- an `issue` at `blocked` or `on_hold`
- a `change_request` at `on_hold`, typically carrying a `block_reason`

**Why they cannot be left there.** A draining lane closes on an all-or-nothing
gate — `run-queue` closes the iteration only when *every* member reached
`merged`/`closed`. One blocked member never does, so it holds the lane open
indefinitely, and the lane stops meaning "this work is in flight" and starts
meaning "this work is in flight plus one thing nobody can act on". Moving it out
is what lets the lane finish.

Re-lane each to the product's **standing blocked lane** — the long-lived holding
lane for records that need an owner decision before anyone can work them, not a
numbered round.

**Resolve it at run time; never carry a lane number in this skill or in your head.**
Lane numbers are per-product and change as lanes are superseded — the standing lane
is itself replaced from time to time when its predecessor drains to zero and closes.
Find it the way step 4 finds every other lane, by house naming shape: `list_iteration
product=keel status=["draft","ready"]` and take the one whose title
begins with **`Blocked`**. Confirm from its `summary` that its exit is an owner
action rather than a drain, then use the ref you just read.

- **Exactly one match is expected.** Two open lanes of this kind is itself a finding —
  report both and ask which is current rather than picking one, because splitting
  blocked records across two holding lanes is how one of them stops being looked at.
- **No match: open one** with `create_iteration`, titled in the same shape, and say
  in its `summary` that it is a standing lane whose exit is always an owner action,
  so a later reader does not drain it like a sweep.

- **Move only. Do not re-triage.** The block stands: leave `status`, `block_reason`
  and every other field alone, and change `iteration` only. Deciding whether the
  block is still real is the owner's call, not this sweep's.
- **A blocked member in a `draft` or `ready` lane is left alone.** Those lanes are
  not draining, so nothing is held open and there is no reason to move it.
- **No-op is the common case.** No active lanes, or no blocked members in them,
  means this step writes nothing — say so and move on.

**Show the user one table before writing anything** — the groomed issues (issue,
version-found, bucket, target iteration) **and** the blocked-member departures
(record, current lane, why it cannot terminate, standing blocked lane) — and get
the go-ahead. Both sets are confirmed together; this is the one gate in the skill.

## Step 5 — Assign, then conclude the **code** issues

Assign every groomed issue to its lane with
`fields={iteration:"keel/iteration-<n>"}`, and apply the step-4b
departures the user confirmed with the same sparse write. Assignment covers all
buckets; what follows does not. A record moved to the standing blocked lane does
**not** enter the conclude loop — it is blocked, which is the whole reason it moved.

### Who enters the conclude loop

**Only the code bucket.** `conclude` matures an issue toward a change_request,
and a CR is a repo unit closed by a merge gate — so it is the wrong instrument
for anything whose fix is typed into gold.

| Bucket | Conclude? | Terminal state after this skill |
|---|---|---|
| **code** | yes, one call per issue | CR created and approved as far as evidence allows |
| **catalog** | no | assigned to its catalog iteration, status untouched |
| **SoR records** | no | assigned to its record lane, status untouched |
| **combo** | no | `blocked`, unassigned, awaiting the owner's split |

Catalog and SoR issues are **done when they are assigned to a lane**. Being in the lane *is*
the plan: they get executed by editing gold and re-exporting the catalog, and
the maturation ladder (requirement → AC → `reviewed` → CR) buys nothing there
while risking exactly the defect this rule exists to prevent. They do not enter
`conclude` — not by invocation, not by hand-rolling its steps, and not by
creating a requirement, AC, or CR for them. If one genuinely needs a repo change
too, it was a **combo** — go back to step 3.

### Per-issue level runs as a dynamic workflow

Once grooming reaches the per-issue level — the step-2 `affects_versions`
backfill writes and the step-5 conclude loop — drive it with a **dynamic
workflow**: one agent invocation per issue row, so each issue gets a fresh
context and one failed row cannot take the batch down. On an executor that
exposes a workflow orchestrator (the `Workflow` tool), author the script and
fan out one `agent()` call per row; on a linear executor, run the rows as
one fresh sub-agent session each. The ledger below stays the source of truth —
feed it the rows in order, collect per-row results, and tick a row only from its
verified result, never from the workflow having merely returned.

### The loop is mandatory

**Write the ledger first.** Before invoking `conclude` even once, emit the full
groomed set as a numbered checklist — every issue, in processing order, each row
marked with whether it enters the loop:

```
1. [ ] issue-812  (code)     → iteration-63  → conclude → CR
2. [x] issue-815  (catalog)  → iteration-64  → assigned, no conclude
3. [x] issue-819  (SoR)      → iteration-65  → assigned, no conclude
4. [x] issue-823  (combo: catalog + code)   → blocked, no lane, no conclude
```

Non-code rows are ticked the moment they are assigned (or blocked) — they are
complete, not skipped, and they never come back around. The loop below runs over
the code rows only.

Then process the code rows **top to bottom, one `conclude` invocation per row**, ticking
each row as its issue reaches its terminal state. After each row, re-read the
remaining unticked rows and continue immediately — without stopping, without a
mid-loop summary, and without asking the user whether to keep going. The user
already gated this at step 4; there is no second gate.

Rules that keep the loop honest:

- **One issue per `conclude` call.** Name the single issue explicitly in the
  invocation (`conclude issue-<n>`). A plural hand-off — "the groomed set",
  "these issues", a count — is what collapses the batch to one.
- **A failed row does not end the loop.** If an issue can't be matured (server
  reject, missing evidence, needs an owner decision), record the blocker against
  that row, leave it unticked, and move to the next row.
- **The step is complete only when no row is unticked.** The exit condition
  is every row ticked or explicitly marked blocked-with-reason — not "the first
  one worked".

### Verify before reporting

Re-read the set from gold — `list_issue` over the groomed refs, plus
`list_inbound_refs` on **every** issue. Two assertions, both required:

- each code-bucket issue **has** its CR;
- each catalog / SoR / combo issue has **no** inbound `change_request` ref.

A CR hanging off a catalog or SoR issue is a grooming defect, not a bonus:
retire it (or say so plainly in the report if you can't) before reporting the
batch done. The ledger is your intent; gold is the truth.

Read the two buckets by different tests. A **code** issue still sitting at its
pre-grooming status is an unfinished row. A **catalog / SoR** issue is *expected*
to still sit at its pre-grooming status — its only postcondition is a populated
`iteration` field, so check that, not its status.

Report at the end: the ledger with final per-issue status, counts per bucket,
iterations touched or created, and anything left unassigned or unconcluded and why.

## Gotchas

| | |
|---|---|
| `analyzed` | server-gated: rejects with `status_requires_unsatisfied` unless `related_requirements` **and** `details` are both non-empty |
| `analyzed → reviewed` | rejects with `issue_review_requires_approved_requirement` while any linked requirement is below `approved` — approve the requirement first |
| status hops | `x-status-transitions` is enforced; `new` may only go to `analyzed` or `closed`, so there is no shortcut to `reviewed` |
| ref forms | `iteration` → `keel/iteration-61`; `affects_versions` → `keel/1.5.0` (root refs must be qualified) |
| writes | always `fields={...}`; a bare `update_issue` payload replaces the whole record |
| iteration membership | the live truth is the inbound-ref set (`list_inbound_refs`), not the prose in an iteration's `details` — amend a stale body when you reassign something out of it |
| don't cache | issue and iteration status in gold is live data; re-read it, never work from a note about it |
| batch collapse | a known failure mode: handing `conclude` the whole set at once matures **one** issue and reads as done. One invocation per issue, ledger-tracked, verified against gold before reporting |
| catalog-as-code | the other known failure mode: a skill/template issue quoting a repo path gets bucketed **code**, assigned to the bug-fix sweep, and concluded into a CR no merge gate can close. The fix site is the gold `SKILL.md`, never the materialized `.claude/` copy — catalog bucket, own lane, assigned and done |
| conclude roster | `conclude` runs over the **code** bucket only. Catalog, SoR, and combo issues stay out of the loop entirely — assigning them is the whole job |
| combo | spans two buckets ⇒ ungroomable: `blocked`, unassigned, **not concluded**. The ambiguity is resolved by the owner's split, not by picking the bigger half |
