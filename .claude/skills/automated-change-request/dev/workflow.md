---
name: automated-change-request/dev
description: 'Implement an approved unit via a linear vertical-slice TDD loop, runnable by a non-resident executor (codex). Use when executor=agent and status is approved (fresh claim), in_progress with a prior blocking formal_review (rework resume), or in_progress with commits ahead of base and no such review (interrupted run), and the operator says "dev this CR headless" or "codex dev".'
x-openbrain-content-hash: sha256:dc85e4467da5a248f1c118ae41ee357e742a515709aff2772376b69adf680ae2
---

# Automated Dev

**Transition:** `approved → in_progress` (fresh entry) **or** `in_progress → in_progress`
(rework resume, interrupted run) → `implementation_review` (on a complete, green,
non-empty-diff unit)

**Goal:** Implement the unit's requirements **and their acceptance criteria** with a
vertical-slice TDD loop, **run linearly by one executor** — no subagent fan-out.
Resolve the requirement list kind-aware (see `../SKILL.md` § acceptance contract),
then each requirement's ACs (its GWT atoms) — the ACs are what each slice's test proves.

## Principles

- Every silent error is a bug the author hasn't met yet
- Testability and observability gaps before stylistic ones

## Executor contract (condensed — full text in `../SKILL.md`)

- **Self-sufficient:** this file is your complete instruction set for `dev`.
- **No fabrication:** never claim a test passed you did not run; on any unavailable
  tool/command or failed step, STOP and report the exact command + verbatim output.
- **Linear:** run every step yourself, in order; no subagents required.
- **Sparse writes:** `update_change_request` with only the changed keys via `fields:`
  (top-level args = full REPLACE → silent field drop); re-read after a status write.
- **Gate, don't ask:** take the determinate path or halt; never wait for an answer.
- **Record what blocks you, before you stop.** If something blocks this verb and is not
  part of this unit's acceptance contract, record it before you reach the landing state or
  report the halt: `create_action_item` when resolving it needs an owner decision;
  `create_issue`, carrying the command run and the output observed, when it is a defect in
  the product or the pipeline. If it does not block you, or it is this unit's own scope, do
  not file — implement it or report it in this verb's own output.

## 1. Precondition check

1. `get_change_request product=keel id=<id>`.
2. Confirm `executor == agent`. If it differs, **halt** and report the actual executor —
   this unit is not for an autonomous executor.
3. Confirm the status is one of the **three** admissible entry states. Read the table
   top to bottom and take the **first** row that matches; decide mechanically, do not
   interpret.
   <!-- DHF-REQ: openbrain/requirement-867 -->

   | Observed `status` | Entry state | Action |
   |---|---|---|
   | `approved` | **fresh claim** | proceed; step 2 writes `in_progress`. |
   | `in_progress` **and** a `formal_review` with `outcome: follow_up_required` naming this change request in `subject_refs` exists | **rework resume** | proceed; step 2 skips the status write (the unit is already `in_progress`). Treat that review's findings as this round's work. |
   | `in_progress`, **no** such `formal_review`, **and** the unit branch carries at least one commit ahead of its base | **interrupted run** | proceed; step 2 skips the status write (the unit is already `in_progress`). There are no findings to act on — step 2's resume check reads the branch for what is already done. |
   | `in_progress`, **no** such `formal_review`, **and no** commit ahead of base | not resumable | **halt** and report — the unit claimed `in_progress` and produced nothing, so there is neither committed work to resume nor recorded findings to act on. |
   | anything else | not eligible | **halt** and report the actual status. |

   **Two different signals distinguish the two resumes, and the order above is load-bearing.**

   A **rework resume** is signalled by the **prior blocking `formal_review`** — a round that
   a reviewer, a post-merge revert (either the `merge` verb's own in-session gate or the
   runner-owned gate that runs after `merge` has exited), or a `verify` no-op reopen routed
   back to `dev`, leaving that record behind. **Which actor wrote it does not change what you
   do**: every one of those routes leaves the same pair — `in_progress` plus a blocking
   review naming this unit — and the findings in the review are this round's work either way.
   Read it with `list_formal_review product=keel` (or
   `search_formal_review`) and match `subject_refs` on
   `keel/change_request-<seq>`; the newest matching row is the one that routed
   this unit back. It is legitimate for the corrective work of that round to have been
   applied outside a `dev` child (by the run-queue supervisor or by hand).

   An **interrupted run** is signalled by **commits ahead of the base**, and only by that. A
   prior `dev` session claimed the unit and was cut off mid-flight — turn-budget truncation,
   crash, out-of-memory, operator kill — so it left work on the branch but no record of why
   it stopped. Establish the count before you decide:

   ```bash
   git rev-list --count main..HEAD
   ```

   A count greater than zero is the whole discriminator, and it is a required conjunct, not
   supporting rationale: without it the **interrupted run** row would swallow the
   **not resumable** row and admit a unit that has nothing to resume. Do not substitute a
   judgement about whether the unit "looks" resumable, and do not treat an uncommitted
   working tree as evidence — only commits count.

   Where a resumed unit picks up its remaining work is the same for both resumes and is not
   inferred here: **step 2's resume check reads the branch**, so already-committed slices are
   recognized as done rather than redone, and the slice loop restarts at the first resolved
   requirement ref with no committed slice.
4. Resolve the requirement refs kind-aware (see `../SKILL.md` § acceptance contract) —
   this is the slice list; each slice proves that requirement's acceptance criteria.

## 1b. Read the acceptance contract

The requirements and their acceptance criteria are the basis for the implementation and
the tests. Read them all before you write code.

1. Per resolved ref: `get_requirement`, then its acceptance criteria — the `ac` records
   whose `parent` is that requirement (`list_inbound_refs`, then `get_ac`). Never search
   for them; the parent edge is a direct reference. Read the records, not the change
   request's restatement.
2. A requirement with no acceptance criterion has no test oracle — **halt**.
3. If the criteria contradict each other, their parent requirement, or the unit's Scope,
   **stop before implementing** — a conflicting contract cannot be satisfied in code.
   Record a `formal_review` (`outcome: follow_up_required`) quoting the conflicting refs,
   append the conflict to `details`, and exit. Thin-but-not-contrary is not a conflict:
   implement the narrowest reading and say so.

## 2. Start — confirm the worktree, then claim the unit

**The runner owns the worktree; do not create one.** The autonomous runner
(`openbrain-client run-queue`) has already rooted this session in the
unit's own worktree on a dedicated `cr-<seq>` branch, and your current directory
*is* that worktree. Creating a second worktree (e.g. `worktree-up.sh cr <seq>
<slug>`) is what double-rooted the unit and made dev no-op on re-run — **do not do
it.**

1. Confirm you are on the unit's branch, not the default branch:

   ```bash
   git rev-parse --abbrev-ref HEAD
   ```

   It must print `cr-<seq>` (e.g. `cr-333`). If it prints `main`/`master` or the
   `git` command fails, you are **not** in a unit worktree — **halt and report
   it** (the runner did not set the session up; do not commit to the default
   branch).

2. Claim the unit. **On either resume — rework or interrupted run (step 1's second and
   third rows) — the unit is already `in_progress`: skip this write entirely and continue
   at sub-step 3**; re-writing the same status is a no-op that only risks a spurious
   rejection. On a fresh claim,
   `update_change_request` with sparse fields only.
   <!-- DHF-REQ: openbrain/requirement-870 -->
   The claim write's sparse fields = status (plus last_edited_by) only: use
   `fields: { status: "in_progress" }` and, if you supply editor identity, include
   only `last_edited_by` with it; never include server-managed fields such as
   `created_at`, `last_edited_at`, `template`, `git_sha`, `schema_version`, or
   `source_path`.
   If the write fails with invalid_argument naming a server-managed field, strip the server-managed field and retry the claim once in-session.
   Do not halt on this first mechanical rejection; halt only if the corrected retry
   also fails. Re-read and confirm `status == in_progress` before continuing.

3. **Resume check (the branch may already carry work from an interrupted run).**
   Inspect what is already committed on this branch:

   ```bash
   git log --oneline main..HEAD
   ```

   For each slice whose work is **already committed here** (test + implementation
   present and green), treat that requirement ref as **done** and skip it in the
   slice loop below. Resume at the first resolved requirement ref that has no
   committed slice. Never re-write a test for an already-green slice (step 4b
   would wrongly see it pass before implementation) and never recommit work that
   is already on the branch. If every ref is already committed and green, skip to
   step 5 (definition-of-done gate) — implementation is complete, but dev is not
   done until the unit's declared transition gate is green for this worktree HEAD.

## 3. Tracer bullet

Run the **first** requirement ref in the resolved requirement list as one complete slice
(steps 4a–4d below) end-to-end before continuing. Confirm the red→green→annotate→commit
loop works on the simplest behavior first. If the tracer slice parks (step 4c), stop
per step 5 — do not start the rest.

## 4. Slice loop — one slice per requirement ref, in order

For each requirement ref (the tracer bullet is the first; never start a new slice while
any test from the current slice is red):

### 4a. Derive the slice spec

1. Use this requirement and its acceptance criteria as read in step 1b (re-read with
   `get_requirement ref=<req-ref>` if the session has since been compacted).
2. Extract:
   - **GWT atoms** — the Given/When/Then strings from this requirement's acceptance
     criteria. These are the test oracle.
   - **Public interface** — the observable surface the test exercises (function
     signature, HTTP endpoint, MCP tool name, …), derived from the requirement
     statement and the unit's Scope section.

### 4b. Red — write the failing test

Write a test that verifies the acceptance criterion (GWT atom) **against the public interface only** — the test traces to this AC.

> **Information barrier (critical — this replaces the old tester/coder subagent split).**
> When writing the test, use **only** the GWT atom and the public interface. Do **not**
> read, infer, or design against implementation internals. The test asserts observable
> behavior, not how it is achieved. This discipline is what the human path enforced by
> giving the test to a separate subagent that could not see the implementation; running
> linearly, **you enforce it on yourself.**

Run the test. Confirm it is **red** (fails) for the right reason — the behavior is
absent, not a compile/setup error unrelated to the requirement. If the test passes
before any implementation exists, it does not actually exercise the new behavior:
fix the test (still interface-only) until it is red, or **halt** and report why a red
test could not be produced.

### 4c. Green — implement to pass (3-round cap, then park)

Implement the **minimum** needed to make the failing test pass.

- Do **not** modify the test file.
- Do **not** refactor code untouched by this slice. (No refactor while any test is red.)

Run the test. **Green-attempt cap = 3 rounds total** (the first implementation attempt
is round 1; each fix-and-rerun is another round). If the test is still not green after
the **3rd** round, **park this slice** — do not attempt a 4th, do not wait for the owner:

1. Stop the slice loop immediately. Do not start any remaining slices.
2. Leave the unit at `in_progress` (do not change status).
3. Record the blocker — **both writes must succeed**; if the second fails, retry it
   before exiting (park is incomplete until both records exist):
   - `create_formal_review` naming the parked slice (its requirement ref), the round
     count reached (3), and the last failing test output verbatim.
   - `update_change_request` with `fields:` appending to `details`: "slice `<req-ref>`
     parked at the 3-round green cap — see formal_review."
4. Exit cleanly. The owner resumes the parked slice later.

This is the AFK-safe abort: bounded retries, then a recorded blocker and a clean exit.
Never spin past 3 rounds.

### 4d. Annotate and commit

Add DHF traceability markers, then commit the slice (code + test + markers) to the
worktree branch.

- **`DHF-REQ: keel/requirement-<id>`** on the smallest implementing unit
  (function/method/handler) that satisfies the requirement.
- **`DHF-TEST: keel/requirement-<id>`** on each test function that verifies it.

Use the language-appropriate comment leader. One line may carry multiple
comma-separated refs. Example (Go):

```go
// DHF-REQ: keel/requirement-42
func HandleFoo(...) { ... }

// DHF-TEST: keel/requirement-42
func TestHandleFoo_RejectsMissingBody(t *testing.T) { ... }
```

Proceed to the next slice.

## 5. Definition-of-done gate — run the in-session transition gate

All slices green is necessary but not sufficient. Before reporting dev complete,
run the unit's declared `transition_gate` in-session stages against this worktree HEAD.

1. Re-read this change request with `get_change_request product=keel id=<id>`
   and read its `transition_gate` rung. If it is absent, **halt loudly** — dev cannot
   declare done without a declared transition gate.
2. Read the committed `openbrain-client.yaml` from this worktree root and resolve
   `transition_gates.<rung>.in_session`. Do not read any System-of-Record command row
   or `openbrain-client.local.yaml`; execution commands live only in the
   committed product config.
3. Run each argv-array stage in order, synchronously in the foreground, from this
   worktree root. Treat each stage as argv, not as a shell string. If the runner prompt
   supplied an exact in-session gate command, run that exact command.
4. Do **not** run `transition_gates.<rung>.runner_owned` stages. Those are owned by
   `openbrain-client` after this verb exits. For `unit` and higher, this is where
   coverage-floor and unit/integration/system tests attach; the `static` rung executes
   no tests.
5. Record the rung, in-session stages, and result honestly in your run summary. Never
   claim a gate pass without running the stages.
6. **On in-session gate failure:** this is **dev-not-done**, not review-ready.
   - If the failing stage is a read-only validator — it inspects the tree and reports,
     without writing to it — re-run the in-session gate up to **3 times total**; a flaky
     gate may pass on retry. Do not run a 4th time.
   - If the failing stage has side effects, do not retry; treat the first non-zero exit
     as the final failure.
   - If the final gate run is still non-zero, return to the slice loop and fix the
     failure in this change request when the failure is clearly caused by your dev edit. Write a
     failing test first when the fix is behavioral and testable; for deterministic
     static/tooling drift, make the minimal correction and re-run the same gate.
   - If you cannot identify or fix the failure within the existing 3-round green
     discipline, **park**: leave the unit at `in_progress` and record the blocker.
     Both writes must succeed; if the second fails, retry it before exiting:
     create a `formal_review` naming the failing rung, run count, stage, and last
     failing output verbatim, then append to `details`: "dev transition gate `<rung>` red
     at the 3-run cap — unit remains in_progress; see formal_review."

Regression discipline: the change_request-443 failure mode is the model case. If a dev edit
makes `build-context-parity` inside `static-tools` red, this step catches it here
on the branch and forces a fix inside this change request before `review`; it must not be left for the
post-merge issue-166 backstop.

## 6. End of loop

- **If any slice parked** (step 4c): do **not** announce "implementation complete" and
  do **not** advance to `review`. Stop at the parked unit (`status` stays
  `in_progress`) and point at the recorded blocker (`formal_review` + `details` note).
  Already-green slices stay committed; the parked slice's partial work is left
  uncommitted for the owner.
- **If the definition-of-done gate parked** (step 5): do **not** announce
  "implementation complete" and do **not** advance to `review`. Stop at the parked
  unit (`status` stays `in_progress`) and point at the recorded blocker
  (`formal_review` + `details` note).
- **Otherwise** (all slices green + annotated + committed + in-session gate green):
  the `dev` verb is complete. Advance the unit: `update_change_request` with
  `fields: { status: "implementation_review" }`, then re-read and confirm
  `status == implementation_review`. Your committed slices are the evidence — the
  runner cross-checks that this branch is a non-empty, clean diff over `main` and
  halts "dev produced no changes" if it is empty, so never write
  `implementation_review` without committed work. Report the transition-gate rung, stages,
  and passing result, then report that `review` is the next verb. **Do not run
  `review` in this session** — one verb, one session.

## Optional fan-out (not required)

A fan-out-capable executor (Claude Code) *may* delegate step 4b (red) and step 4c
(green) to two separate generic subagents to keep the information barrier mechanical
rather than self-imposed. This is an optimization only — the linear path above is the
canonical, portable contract and produces the same result.
