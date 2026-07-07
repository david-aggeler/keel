---
name: automated-change-request/verify
description: 'Independently verify post-merge scope fidelity, complete the gold wrap-up, and close a merged autonomous unit, runnable by headless Claude after merge.'
---

# Automated Verify

**Transition:** `merged → closed` (verified + wrapped up) or, for a no-op coding stage, `merged → in_progress` (routes back to dev)

**Goal:** After `merge` lands the unit on `main` (`status == merged`), independently
audit whether the merged code actually implements the change_request acceptance
criteria, then — on a sound unit — perform the gold wrap-up (derive
`fixed_in_version`, mint/satisfy the `issue_fix`, drive the parent issue to a
terminal state) and write `closed`. This verb is run by `claude -p`, distinct from
the Codex executor that produced dev/review/merge.

## Executor contract

- **Self-sufficient:** this file is your complete instruction set for `verify`.
- **No fabrication:** never claim a diff, symbol, test, issue, action_item, or
  reopen exists unless you observed or wrote it.
- **Sparse writes:** every `update_change_request` uses only the changed keys via
  `fields:`. Re-read after a status write.
- **Gate, don't ask:** owner-only sub-decisions take the determinate default below
  or are recorded and handed back — never block waiting for an answer.

## 1. Precondition check

1. `get_change_request product=openbrain id=<id>`.
2. Confirm `status == merged`. If it differs, halt and report the actual status.
3. Confirm `code_change_ref` is present (the merge verb recorded the merge SHA). If
   absent, halt and report that there is no merge commit to verify.
4. Read `parent`, `depends_on`, and collect `acceptance_criteria` requirement refs.

## 2. Evidence collection

1. For each acceptance ref, call `get_requirement` and read the statement,
   details, and Given/When/Then acceptance criteria.
2. Confirm the recorded `code_change_ref` is reachable on `main`:

   ```bash
   git merge-base --is-ancestor <code_change_ref> main
   ```

3. Inspect the merged diff:

   ```bash
   git show --stat --oneline <code_change_ref>
   git show --find-renames --find-copies --format=fuller <code_change_ref>
   ```

4. Compare the diff against the CR acceptance criteria and referenced
   requirement bodies. Use the build/test gate result only as context; it is not
   the oracle.
5. Collect objective evidence as you go: relevant diff hunks, present or missing
   symbols/files/tests, and the exact requirement atom each evidence item maps to.

## 3. Verdict routing

Choose exactly one route:

- **No-op coding stage:** if the merged diff contains **no substantive
  implementation** of the unit's scope, route it back to dev with
  `update_change_request fields: { status: "in_progress" }`. Re-read and confirm
  `status == in_progress`. The executor remains `agent`; do not rewrite main
  history. **Do NOT wrap up or close** — the runner reads `in_progress` and
  re-dispatches `dev`. Skip the rest of this file.
- **Technically LLM/agent-solvable gap:** call `create_issue` (well-formed:
  CR ref, requirement refs, expected behavior, observed diff gap, missing
  files/symbols/tests, relevant command output), then **continue to the wrap-up +
  close below** — the gap is tracked as its own issue; the unit still closes.
- **Human decision or operation needed:** call `create_action_item` (CR ref,
  requirement refs, decision/operation needed, objective evidence), then **continue
  to the wrap-up + close below**.
- **Fidelity satisfied:** continue to the wrap-up + close below.

## 4. Gold wrap-up (issue-parent, merged units)

**Parent mode (read `parent` from step 1; the merge close reason is `merged`):**

- If `parent` is an **epic** ref or absent: skip to **step 5 (Final close)**.
- If `parent` is an **issue** ref: complete the gold wrap-up — derive the version
  (4a), mint the fix (4b), drive the parent to a terminal state (4c). This is not
  deferred to the owner; the only ambiguity (`fixed_in_version`) is resolved
  deterministically below, and anything that stays ambiguous is recorded and
  handed back rather than guessed.

### 4a. Derive `fixed_in_version` (deterministic, never guess)

`admin_list_product_versions product=openbrain`. The fix ships in the version
**currently under development** — select the version(s) whose `Status` is
`in_development`:

- **Exactly one** `in_development` version → that is `fixed_in_version`, written in
  canonical `openbrain/<version>` form (e.g. `openbrain/1.2.0`).
- **Zero, or two-or-more** `in_development` versions → **ambiguous**. Do **not** guess:
  leave the unit at `merged`, record in your run summary that `issue_fix` +
  parent-issue close are deferred pending an unambiguous `fixed_in_version` (state the
  count you found and the version names), and **hand back**. (The runner's `verify`
  postcondition will halt on the still-`merged` status — that halt is the correct
  deferral signal.)

### 4b. Mint or satisfy the `issue_fix`

<!-- DHF-REQ: openbrain/requirement-619 -->
Before calling `create_issue_fix`, list existing issue fixes for the parent issue:

```text
list_issue_fix product=openbrain
```

Filter the returned rows client-side to rows whose `issue` is the parent issue ref.
If any existing row's `fixed_in_version` matches the value derived in 4a:

- skip `create_issue_fix`;
- record in your run summary:
  `issue_fix for <issue>@<version> already exists (<ref>) - fix row satisfied by sibling CR`;
- treat the fix row as satisfied rather than an error;
- Proceed to 4c (parent-close guard), then step 5 (Final close).

This is the expected sibling-CR idempotency path. Do not treat `duplicate_issue_fix` as the normal control path, and a pre-existing matching fix row must not STOP at `merged`.

If no existing row matches, create_issue_fix exactly as below:

`create_issue_fix` with:

- `issue` = the parent issue ref, `change_request` = this CR ref,
- `fixed_in_version` = the value derived in 4a,
- `code_change_ref` = the merge SHA from the record (`code_change_ref`),
- `close_reason: "tested"`, `status: "closed"`,
- `title` / `fix_description` / `summary` / `details` describing the fix (root cause +
  what landed; reference this CR and the merge SHA),
- audit fields: `created_by` / `last_edited_by` / `fixed_by` / `closed_by` = `ai:claude`
  (or your executor identity), `created_at` / `last_edited_at` / `fixed_at` /
  `closed_at` = now.

Confirm it inserted. **Backport** to additional versions stays an owner decision —
emit **one** `issue_fix` row, for the `in_development` version only.

### 4c. Drive the parent issue to a terminal state (multi-CR guard)

Before closing the parent, check it is not still owed work by a **sibling** CR:
`list_inbound_refs ref=<parent issue ref> src_dto_type=change_request`. For each
referencing change_request **other than this one**, read its status. If **any** sibling
CR is **not** `closed`, the issue is multi-CR and not yet done:

- Do **not** close the parent issue. Record in your run summary: "parent-issue close
  deferred — open sibling CR `<ref>` (status `<status>`) still references it." Then go
  to **step 5 (Final close)** (this CR still closes; the issue legitimately outlives
  it).

Otherwise this CR is the last open child: `update_issue` with
`fields: { status: "closed", close_reason: "tested", closed_by: "ai:claude",
closed_at: "<now>" }`. Re-read and confirm `status == closed`.

## 5. Final close

Every verified path (fidelity satisfied, or a gap tracked as its own
issue/action_item) ends here:

`update_change_request` with `fields: { status: "closed", close_reason: "merged" }`.
Re-read and confirm `status == closed`. (`code_change_ref` is already present from the
merge verb; `close_reason: "merged"` satisfies the schema's `x-status-requires` gate.)

**Worked example / postcondition check:** for an epic-parented, merged CR such as
`parent: openbrain/epic-5`, verify audits the merged diff, skips section 4 because no
`issue_fix` or parent-issue close applies, executes this step, and confirms the record
is `closed`. The runner's `verify` postcondition must see `closed`, not `merged`.

The unit is now immutable. To make further changes it must be reopened first (that is a
human, `change-request correct`/reopen path — not part of this skill).

## 6. Exit

Exit cleanly after writing `closed` (or `in_progress` on a no-op reopen). The runner
routes on the written status: `closed` completes the tail; `in_progress` re-dispatches
`dev`. This verifier records scope-fidelity gaps as issues/action_items separate from
the close.
