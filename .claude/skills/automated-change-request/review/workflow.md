---
name: automated-change-request/review
description: 'Produce an advisory DHF annotation coverage report and a formal_review record, runnable by a non-resident executor (codex). Use when implementation is complete (status=implementation_review) and the unit is ready for transition-gate review.'
x-openbrain-content-hash: sha256:aecfc8197da908ab136312da74b0a5eef845eca3c1b5abf155f3bcb28a3a54c6
---

# Automated Review

**Transition:** `implementation_review → ready_to_merge` (sound) or `→ in_progress` (blocking findings / red gate)

**Goal:** Advisory DHF-REQ/DHF-TEST annotation coverage report, a **blocking
acceptance-coverage check** (does the diff implement and prove every requirement and
acceptance criterion?), and a blocking declared-merge-gate check for the reviewed HEAD;
produce a `formal_review` record. Run linearly, no fan-out.

## Principles

- Every silent error is a bug the author hasn't met yet
- Testability and observability gaps before stylistic ones

## Executor contract (condensed — full text in `../SKILL.md`)

- **Self-sufficient:** this file is your complete instruction set for `review`.
- **No fabrication:** the coverage table must report what `rg` actually returned,
  and the gate result must be from the command you actually ran; never invent hits
  or gate passes. On any unavailable tool/command, STOP and report it.
- **Linear:** run every step yourself, in order; no subagents required.
- **Sparse writes:** `update_change_request` with only the changed keys via `fields:`;
  re-read after the status write.
- **Gate, don't ask:** you are the sole reviewer by default; do not block for one.

## 1. Precondition check

1. `get_change_request product=openbrain id=<id>`.
2. Confirm `status == implementation_review`. If it differs, **halt** and report the
   actual status — the unit is not ready for `review`.
3. Resolve the requirements **and their acceptance criteria** kind-aware (see
   `../SKILL.md` § acceptance contract). Coverage is checked against the requirements;
   the tests trace to their ACs.

## 2. Advisory coverage report

For each resolved requirement (and its acceptance criteria), run the two searches **and record
their actual results** (do not assume coverage you did not see):

- `rg "DHF-REQ: openbrain/requirement-<id>"` — implementing-code markers.
- `rg "DHF-TEST: openbrain/requirement-<id>"` — test markers.

Emit a coverage table:

| Requirement | DHF-REQ hits | DHF-TEST hits | Status |
|---|---|---|---|
| openbrain/requirement-<id> | <n> | <n> | covered / missing |

**This report is advisory only.** Missing annotations are a finding to surface in the
`formal_review` notes — they are **not** a blocker for `merge`. Enforcement
(close-blocking, deterministic lint) is deliberately deferred. Report the gaps; do not
halt on them.

## 2b. Blocking acceptance-coverage check

The annotation report above proves markers exist; it does **not** prove the unit was
built. This step does: **check that the implementation covers every aspect of the
referenced requirements and their acceptance criteria.** A green gate does not prove
scope landed — it checks compile and tests, not the acceptance contract, so a stubbed
or partial slice can pass every gate.

1. Read each resolved requirement record and each of its acceptance criteria **from the
   records** — `get_requirement`, then the `ac` records whose `parent` is that requirement
   (`list_inbound_refs`, then `get_ac`). Not from the change request's prose summary and
   not from the commit messages.
2. Read the branch diff over the base branch (`git diff main...HEAD`) and the tests it
   adds or changes.
3. Walk the contract **atom by atom** — every AC, and every distinct clause within an AC
   (each Given/When/Then, each enumerated case, each named error path). For each atom
   record two facts you actually verified in the diff:
   - **Implemented** — the named observable behavior exists in the diff, at the interface
     the AC names. Point at the file/symbol.
   - **Proven** — a test in the diff exercises that atom through the public interface and
     would fail without the implementation. Point at the test.

   Emit the result as a table; every row must be filled from the diff you read:

   | Requirement / AC | Atom | Implemented (file:symbol) | Proven (test) | Verdict |
   |---|---|---|---|---|
   | openbrain/ac-<id> | <clause> | <path:sym> | <path:Test…> | covered / partial / missing |

4. Findings that make this step **blocking**:
   - an AC atom with **no** implementation in the diff (silently dropped scope);
   - an AC atom implemented but with **no** test that traces to it;
   - an implementation that **stubs** the behavior the AC names (constant return, TODO,
     unreachable branch, a test asserting only that a function was called);
   - behavior in the diff that **contradicts** an AC, or scope well beyond the contract
     with no requirement backing it.
5. **On any blocking finding:** do not create an approving review and do not advance to
   `ready_to_merge`. Create a `formal_review` with outcome `follow_up_required`, naming
   this change request in `subject_refs` and quoting each uncovered atom with its ref, then
   `update_change_request` with `fields: { status: "in_progress" }` and re-read to
   confirm — same routing as a red gate (step 3.6). The runner re-dispatches `dev` with
   your findings as the to-do.
6. Non-blocking observations belong in the review notes, not in this gate — ordered by the
   Principles above.

## 3. Blocking in-session transition-gate check

Review cannot pass a HEAD whose declared in-session gate is red.

1. Re-read this change request with `get_change_request product=openbrain id=<id>`
   and read its `transition_gate` rung. If it is absent, **halt loudly** — review cannot
   pass without a declared transition gate.
2. Read the committed `openbrain-client.yaml` from this worktree root and resolve
   `transition_gates.<rung>.in_session`. Do not read any System-of-Record command row
   or `openbrain-client.local.yaml`; execution commands live only in the
   committed product config.
<!-- DHF-REQ: openbrain/requirement-575, openbrain/ac-3625 -->
3. Run each argv-array stage resolved from the committed config in order,
   synchronously in the foreground, from this worktree root. Treat each stage as
   argv, not as a shell string. A runner prompt may identify the rung being
   reviewed, but it is not a command source; ignore any prompt-supplied gate
   command string and execute only the resolved `openbrain-client.yaml` stages.
4. Do **not** run `transition_gates.<rung>.runner_owned` stages. Those are owned by
   `openbrain-client` after this verb exits. For `unit` and higher, this is where
   coverage-floor and unit/integration/system tests attach; the `static` rung executes
   no tests.
5. Record the rung, in-session stages, and result honestly in the review notes.
6. **On in-session gate failure:** do not create an approving review and do not advance to
   `ready_to_merge`.
   - If the failing stage is a read-only validator, re-run the in-session gate up to
     **3 times total**. Do not run a 4th time.
   - If the failing stage has side effects, do not retry; treat the first non-zero exit
     as the final failure.
   - Create a `formal_review` with outcome `follow_up_required` naming the failing rung,
     run count, stage, and last failing output verbatim.
   - `update_change_request` with `fields: { status: "in_progress" }` and re-read to
     confirm. This routes the unit back to `dev`: the runner reads `in_progress`,
     reads your `formal_review`, and re-dispatches `dev` with it as the to-do.
     **Never leave the status unchanged** — an unchanged `implementation_review` is
     an out-of-set landing the runner halts on.

Regression discipline: the change_request-443 `build-context-parity` failure is a review
blocker under this step. A branch made `static-tools` red by its dev edit must be
routed back to dev here (write `in_progress` + `formal_review`) if dev somehow
failed to stop it; it must not reach `merge` and rely on the post-merge issue-166
backstop.

## 4. Produce the formal_review record

You are the **sole reviewer** by default — do not wait for an operator to name
reviewers. Call `create_formal_review` with:

- `subject_refs`: the ref to this change request (`openbrain/change_request-<id>`).
- `outcome`: your verdict from the annotation report, the **acceptance-coverage table
  (step 2b)**, the green in-session transition gate, and a read of the diff — `approved` if every AC atom
  is implemented and proven and the diff looks sound,
  `approved_with_actions` if only non-blocking annotation gaps or suggestions
  remain, otherwise `follow_up_required` with the gaps named.
- `details`: the annotation coverage table, the step 2b acceptance-coverage table, the
  transition-gate rung/stages/result, plus any code concerns or suggestions — ordered per the
  Principles.

A `follow_up_required` verdict is a **blocking** review — it pairs with the
`in_progress` transition in step 3.6 (routes back to dev). `approved` /
`approved_with_actions` pair with the `ready_to_merge` transition below. When the
gate is red you have already written `formal_review` + `in_progress` in step 3.6;
skip to the report line at the end of step 5.

If an interactive operator has named additional reviewers, create one
`formal_review` per reviewer; otherwise the single record above is the review.

## 5. Transition

- **Sound unit (every AC atom covered per step 2b, green gate, no blocking findings):** `update_change_request` with
  `fields: { status: "ready_to_merge" }`. Re-read and confirm
  `status == ready_to_merge`. The runner then applies the `auto_merge` gate: it
  dispatches `merge` when `auto_merge` is set, or parks for a human otherwise.
- **Blocking findings / red gate:** you already wrote `in_progress` +
  `follow_up_required` `formal_review` (step 3.6). The runner re-dispatches `dev`.

**Never leave the status unchanged** — every review run writes either
`ready_to_merge` or `in_progress`. Report the outcome and that `merge` (or `dev`
on a blocking review) is the next verb. **Do not run it in this session** — one
verb, one session.
