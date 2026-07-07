---
name: automated-change-request/dev
description: 'Implement an approved unit via a linear vertical-slice TDD loop, runnable by a non-resident executor (codex). Use when status=approved, executor=agent and the operator says "dev this CR headless" or "codex dev".'
---

# Automated Dev

**Transition:** `approved → in_progress` (on entry) → `implementation_review` (on a complete, green, non-empty-diff unit)

**Goal:** Implement the unit's `acceptance_criteria` requirements with a vertical-slice
TDD loop, **run linearly by one executor** — no subagent fan-out.

## Executor contract (condensed — full text in `../SKILL.md`)

- **Self-sufficient:** this file is your complete instruction set for `dev`.
- **No fabrication:** never claim a test passed you did not run; on any unavailable
  tool/command or failed step, STOP and report the exact command + verbatim output.
- **Linear:** run every step yourself, in order; no subagents required.
- **Sparse writes:** `update_change_request` with only the changed keys via `fields:`
  (top-level args = full REPLACE → silent field drop); re-read after a status write.
- **Gate, don't ask:** take the determinate path or halt; never wait for an answer.

## 1. Precondition check

1. `get_change_request product=openbrain id=<id>`.
2. Confirm `status == approved` **and** `executor == agent`. If either differs, **halt**
   and report the actual status/executor — this unit is not ready for autonomous `dev`.
3. Collect the `acceptance_criteria` requirement refs (this is the slice list).

## 2. Start — confirm the worktree, then claim the unit

**The runner owns the worktree; do not create one.** The autonomous runner
(`openbrain-client run-tail` / `run-epic`) has already rooted this session in the
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

2. `update_change_request` with `fields: { status: "in_progress" }`. Re-read and
   confirm `status == in_progress` before continuing.

3. **Resume check (the branch may already carry work from an interrupted run).**
   Inspect what is already committed on this branch:

   ```bash
   git log --oneline main..HEAD
   ```

   For each slice whose work is **already committed here** (test + implementation
   present and green), treat that requirement ref as **done** and skip it in the
   slice loop below. Resume at the first `acceptance_criteria` ref that has no
   committed slice. Never re-write a test for an already-green slice (step 4b
   would wrongly see it pass before implementation) and never recommit work that
   is already on the branch. If every ref is already committed and green, skip to
   step 5 (definition-of-done gate) — implementation is complete, but dev is not
   done until the unit's declared gate is green for this worktree HEAD.

## 3. Tracer bullet

Run the **first** requirement ref in `acceptance_criteria` as one complete slice
(steps 4a–4d below) end-to-end before continuing. Confirm the red→green→annotate→commit
loop works on the simplest behavior first. If the tracer slice parks (step 4c), stop
per step 5 — do not start the rest.

## 4. Slice loop — one slice per requirement ref, in order

For each requirement ref (the tracer bullet is the first; never start a new slice while
any test from the current slice is red):

### 4a. Derive the slice spec

1. `get_requirement ref=<req-ref>`.
2. Extract:
   - **GWT atoms** — the Given/When/Then strings from `requirement.acceptance_criteria`.
     These are the test oracle.
   - **Public interface** — the observable surface the test exercises (function
     signature, HTTP endpoint, MCP tool name, …), derived from the requirement
     statement and the unit's Scope section.

### 4b. Red — write the failing test

Write a test that verifies the GWT atom **against the public interface only**.

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

- **`DHF-REQ: openbrain/requirement-<id>`** on the smallest implementing unit
  (function/method/handler) that satisfies the requirement.
- **`DHF-TEST: openbrain/requirement-<id>`** on each test function that verifies it.

Use the language-appropriate comment leader. One line may carry multiple
comma-separated refs. Example (Go):

```go
// DHF-REQ: openbrain/requirement-42
func HandleFoo(...) { ... }

// DHF-TEST: openbrain/requirement-42
func TestHandleFoo_RejectsMissingBody(t *testing.T) { ... }
```

Proceed to the next slice.

## 5. Definition-of-done gate — run the unit's merge_gate

All slices green is necessary but not sufficient. Before reporting dev complete,
run the unit's declared `merge_gate` tier command against this worktree HEAD.

1. Re-read this change request with `get_change_request product=openbrain id=<id>`
   and read its `merge_gate` tier. If it is absent, **halt loudly** — dev cannot
   declare done without a declared gate.
2. `get_dev_defaults product=openbrain`. If not found, **halt loudly** — the gate
   cannot run without it.
3. Read the `details` row matching the unit's `merge_gate` field:

   | Unit's merge_gate | Row key |
   |---|---|
   | `docs` | `merge_gate.docs` |
   | `standard` | `merge_gate.standard` |
   | `full` | `merge_gate.full` |

   If the matching row is absent, **halt loudly** and name the missing row.
4. Extract the backticked command string from that row's rule text and **run it
   exactly as stored**, from this worktree root. Treat it as an opaque shell
   invocation. Do **not** interpolate record fields into it.
5. Record the command and result honestly in your run summary. Never claim a gate
   pass without running the command.
6. **On gate failure:** this is **dev-not-done**, not review-ready.
   - If the gate command is a read-only validator (for example
     `go run ./cmd/openbrain-dev ci static-tools && go run ./cmd/openbrain-dev test unit`),
     re-run up to **3 times total**; a flaky gate may pass on retry. Do not run a
     4th time.
   - If the command has side effects, do not retry; treat the first non-zero exit
     as the final failure.
   - If the final gate run is still non-zero, return to the slice loop and fix the
     failure in this CR when the failure is clearly caused by your dev edit. Write a
     failing test first when the fix is behavioral and testable; for deterministic
     static/tooling drift, make the minimal correction and re-run the same gate.
   - If you cannot identify or fix the failure within the existing 3-round green
     discipline, **park**: leave the unit at `in_progress` and record the blocker.
     Both writes must succeed; if the second fails, retry it before exiting:
     create a `formal_review` naming the failing tier, run count, command, and last
     failing output verbatim, then append to `details`: "dev merge gate `<tier>` red
     at the 3-run cap — unit remains in_progress; see formal_review."

Regression discipline: the CR-443 failure mode is the model case. If a dev edit
makes `build-context-parity` inside `static-tools` red, this step catches it here
on the branch and forces an in-CR fix before `review`; it must not be left for the
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
- **Otherwise** (all slices green + annotated + committed + declared gate green):
  the `dev` verb is complete. Advance the unit: `update_change_request` with
  `fields: { status: "implementation_review" }`, then re-read and confirm
  `status == implementation_review`. Your committed slices are the evidence — the
  runner cross-checks that this branch is a non-empty, clean diff over `main` and
  halts "dev produced no changes" if it is empty, so never write
  `implementation_review` without committed work. Report the gate tier, command,
  and passing result, then report that `review` is the next verb. **Do not run
  `review` in this session** — one verb, one session.

## Optional fan-out (not required)

A fan-out-capable executor (Claude Code) *may* delegate step 4b (red) and step 4c
(green) to two separate generic subagents to keep the information barrier mechanical
rather than self-imposed. This is an optimization only — the linear path above is the
canonical, portable contract and produces the same result.
