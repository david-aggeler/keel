---
name: automated-change-request/review
description: 'Produce an advisory DHF annotation coverage report and a formal_review record, runnable by a non-resident executor (codex). Use when implementation is complete (status=implementation_review) and the unit is ready for transition-gate review.'
x-openbrain-content-hash: sha256:fa40285d12d233e557ca861189d446548294f4c234013215178c43a3fd4de3c2
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
- **Record what blocks you, before you stop.** If something blocks this verb and is not
  part of this unit's acceptance contract, record it before you reach the landing state or
  report the halt: `create_action_item` when resolving it needs an owner decision;
  `create_issue`, carrying the command run and the output observed, when it is a defect in
  the product or the pipeline. If it does not block you, or it is this unit's own scope, do
  not file — implement it or report it in this verb's own output.

## The `formal_review` write procedure

Three steps below write a `formal_review` — the blocking acceptance finding (step 2b),
the red-gate finding (step 3), and the verdict (step 4). All three write it **this** way.
This section defines the procedure; do not run it on its own — run it at the point the
step that names it tells you to.
<!-- DHF-REQ: openbrain/requirement-1178, openbrain/ac-3837, openbrain/ac-3838, openbrain/ac-3839 -->

1. **Finish the analysis first, and compose the whole record before you reach for a
   receipt.** Everything whose duration you do not control — reading the diff, running
   gate stages, dispatching sub-agents, walking the acceptance atoms, composing the
   findings — belongs before this procedure begins. Have the finished `subject_refs`,
   `outcome` and `details` text in hand.
2. **Take the search receipt immediately before the write.** Call
   `search_formal_review product=keel` with a query describing the review you
   are about to write, and carry the `search_receipt` it returns straight into the next
   call. Nothing goes between these two calls — no gate stage, no diff read, no sub-agent
   dispatch, no further analysis. The receipt is a freshness proof with a **5-minute**
   life, so a receipt taken before a stretch of work you do not bound has expired by the
   time you write. (The `template_receipt` from `get_template_for` is the long-lived one —
   24 hours — so it may be taken at any earlier point in the session.)
3. `create_formal_review`, presenting both receipts.
4. **On a `search_receipt_expired` rejection, re-acquire and retry.** Re-run
   `search_formal_review` and retry the **identical** create with the fresh receipt. This
   is safe by construction: receipt validation is non-destructive and touches no
   persistent storage, so a rejected create left nothing behind that the retry would
   duplicate. Halt only if the retry fails for a reason **other** than receipt expiry.
5. **A create you could not complete is an unwritten record — report it as one.** State
   that the record was not written and name the rejection code you received, verbatim. A
   refusal citing duplicate risk is **not** evidence that the earlier write committed:
   never report a `formal_review` ref you did not receive from a successful create, and
   never send the operator looking for one. Reporting an unwritten verdict honestly is
   what makes the runner's halt actionable.

## 1. Precondition check

1. `get_change_request product=keel id=<id>`.
2. Confirm `status == implementation_review`. If it differs, **halt** and report the
   actual status — the unit is not ready for `review`.
3. Resolve the requirements **and their acceptance criteria** kind-aware (see
   `../SKILL.md` § acceptance contract). Coverage is checked against the requirements;
   the tests trace to their ACs.

## 2. Advisory coverage report

For each resolved requirement (and its acceptance criteria), run the two searches **and record
their actual results** (do not assume coverage you did not see):

- `rg "DHF-REQ: keel/requirement-<id>"` — implementing-code markers.
- `rg "DHF-TEST: keel/requirement-<id>"` — test markers.

Emit a coverage table:

| Requirement | DHF-REQ hits | DHF-TEST hits | Status |
|---|---|---|---|
| keel/requirement-<id> | <n> | <n> | covered / missing |

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
   | keel/ac-<id> | <clause> | <path:sym> | <path:Test…> | covered / partial / missing |

4. Findings that make this step **blocking**:
   - an AC atom with **no** implementation in the diff (silently dropped scope);
   - an AC atom implemented but with **no** test that traces to it;
   - an implementation that **stubs** the behavior the AC names (constant return, TODO,
     unreachable branch, a test asserting only that a function was called);
   - behavior in the diff that **contradicts** an AC, or scope well beyond the contract
     with no requirement backing it.
5. **On any blocking finding:** do not create an approving review and do not advance to
   `ready_to_merge`. Write a `formal_review` **via the write procedure above** with outcome
   `follow_up_required`, naming this change request in `subject_refs` and quoting each
   uncovered atom with its ref — the atom-by-atom walk is finished at this point, so the
   receipt is taken now and not before it. Then `update_change_request` with
   `fields: { status: "in_progress" }` and re-read to confirm — same routing as a red gate
   (step 3.6). The runner re-dispatches `dev` with your findings as the to-do.
6. Non-blocking observations belong in the review notes, not in this gate — ordered by the
   Principles above.

## 3. Blocking in-session transition-gate check

Review cannot pass a HEAD whose declared in-session gate is red.

1. Re-read this change request with `get_change_request product=keel id=<id>`
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
   - Write a `formal_review` **via the write procedure above** with outcome
     `follow_up_required` naming the failing rung, run count, stage, and last failing
     output verbatim. The gate runs are finished at this point — take the receipt after
     the last one, never before the first.
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
reviewers. Compose the record in full, then write it **via the write procedure above** —
by this point every gate stage, diff read and coverage walk is behind you, which is
exactly the ordering the procedure requires. The create carries:

- `subject_refs`: the ref to this change request (`keel/change_request-<id>`).
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
`formal_review` per reviewer; otherwise the single record above is the review. Each
create is a separate write and takes its **own** freshly-issued search receipt
immediately before it — one receipt does not cover a run of creates.

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

**If the `formal_review` could not be written at all** (the write procedure's step 5
case), report the verdict you reached, that it was **not** persisted, and the rejection
code you received, verbatim. Do not name a `formal_review` ref — there is none — and do
not describe the outcome in words that imply a record exists.
