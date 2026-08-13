---
name: automated-change-request/verify
description: 'Independently verify post-merge scope fidelity, complete the gold wrap-up, and close a merged autonomous unit, runnable by headless Claude after merge.'
x-openbrain-content-hash: sha256:142abaf7f879d67b7e00976d9ebc79bd33b2d1c577632079aff20c8c444fde01
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

1. `get_change_request product=keel id=<id>`.
2. Confirm `status == merged`. If it differs, halt and report the actual status.
3. Confirm `code_change_ref` is present (the merge verb recorded the merge SHA). If
   absent, halt and report that there is no merge commit to verify.
4. Read `parent`, `depends_on`, and resolve the unit's requirements **and their
   acceptance criteria** kind-aware (see `../SKILL.md` § acceptance contract). Below,
   "acceptance ref" means a resolved requirement; audit the merged diff against its ACs.

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

4. Compare the diff against the change request acceptance criteria and referenced
   requirement bodies. Use the build/test gate result only as context; it is not
   the oracle.
5. Collect objective evidence as you go: relevant diff hunks, present or missing
   symbols/files/tests, and the exact requirement atom each evidence item maps to.
6. Lifecycle and environment residue audit before any close write:
   <!-- DHF-REQ: openbrain/requirement-874 -->
   - assert a formal_review exists for the closed unit before this verifier closes
     it. Call `list_inbound_refs ref=<this change_request ref>
     src_dto_type=formal_review` (or the available formal_review list/search
     surface) and confirm at least one formal_review is objectively tied to this
     change request, for example through `subject_refs`, `materials`, or `related`. If none is
     found, route a discrepancy when no formal_review is found: create an issue or
     action_item with the change request ref, merge SHA, checked command/tool, and observed
     empty result, then continue through the normal verdict routing.
   - inspect for leftover `cr-<seq>` worktree, branch, or stopped-stack residue
     using objective local evidence keyed to this unit id: `git worktree list
     --porcelain`, `git branch --list cr-<seq>`, and the local Compose/Docker stack
     listing available in the checkout. Resolve only residue that is clearly safe
     for the verifier to clean up, and report any unresolved residue as a discrepancy
     with the exact command output and why it was not resolved. This audit is
     distinct from harness teardown: do not treat teardown success as proof that
     no residue exists, and do not skip the audit because a harness or runner says
     it cleaned up.
   - The primary implement approach is mandatory here: do not narrow requirement-144
     (b)/(c) out of the verify actor's remit. Missing
     formal_review evidence and unresolved `cr-<seq>` residue are verifier
     discrepancies, not owner-only scope exclusions.

## 3. Verdict routing

Choose exactly one route:

- **No-op coding stage:** if the merged diff contains **no substantive
  implementation** of the unit's scope, first execute step 3a with verdict route
  `no_op_reopened`, then route it back to dev with `update_change_request fields:
  { status: "in_progress" }`. Re-read and confirm `status == in_progress`. The
  executor remains `agent`; do not rewrite main history. **Do NOT wrap up or
  close** — the runner reads `in_progress` and re-dispatches `dev`. Skip the rest
  of this file after the status write.
- **Technically LLM/agent-solvable gap:** call `create_issue` (well-formed:
  change request ref, requirement refs, expected behavior, observed diff gap, missing
  files/symbols/tests, relevant command output), then **continue to the wrap-up +
  close below** — the gap is tracked as its own issue; the unit still closes.

  Bring the new issue to `analyzed`:
  - Collect objective evidence into the issue: HEAD-cited (`git rev-parse --short HEAD`
    + `file:line` or a verbatim quote), checkable without asking the author anything.
  - Verify against the current repo/records, not the record's prose. If reality
    differs, fix or close the record.
  - Search for a related requirement and link it. Create a new one only if none fits.
- **Human decision or operation needed:** call `create_action_item` (change request ref,
  requirement refs, decision/operation needed, objective evidence), then **continue
  to the wrap-up + close below**.
- **Fidelity satisfied:** continue to the wrap-up + close below.

### 3a. Persist the verifier verdict before wrap-up writes

<!-- DHF-REQ: openbrain/requirement-890 -->
Before any `issue_fix`, parent-issue, or final close write, make the audit
incremental: persist the verifier verdict and objective evidence on the
change_request so a later session that exhausts its turn budget leaves a partial
artifact instead of nothing.

Use `update_change_request` with sparse `fields:` containing only `details`.
Append (or replace, if the section already exists from a prior verify attempt) a
`## Verify Verdict` section to the existing `details` with:

- verdict route selected above (`fidelity_satisfied`, `gap_tracked_issue`,
  `gap_tracked_action_item`, or `no_op_reopened`);
- `code_change_ref`;
- requirement / AC refs audited;
- concise objective evidence: commands inspected, file/symbol/test evidence, and
  any issue/action_item refs created for tracked gaps.

Re-read the change request and confirm the `## Verify Verdict` section is present before
continuing. If this write fails, halt and report it; do not continue to the
wrap-up writes without a persisted verdict artifact.

## 4. Gold wrap-up (issue-parent, merged units)

**Parent mode (read `parent` from step 1; the merge close reason is `merged`):**

- If `parent` is an **epic** ref or absent: skip to **step 5 (Final close)**.
- If `parent` is an **issue** ref: complete the gold wrap-up — derive the version
  (4a), mint the fix (4b), drive the parent to a terminal state (4c). This is not
  deferred to the owner; the only ambiguity (`fixed_in_version`) is resolved
  deterministically below, and anything that stays ambiguous is recorded and
  handed back rather than guessed.

### 4a. Derive `fixed_in_version` (deterministic, never guess)

`admin_list_product_versions product=keel`. The fix ships in the version
**currently under development** — select the version(s) whose `Status` is
`in_development`:

- **Exactly one** `in_development` version → that is `fixed_in_version`, written in
  canonical `keel/<version>` form (e.g. `keel/1.2.0`).
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
list_issue_fix product=keel
```

Filter the returned rows client-side to rows whose `issue` is the parent issue ref.
If any existing row's `fixed_in_version` matches the value derived in 4a:

- If exactly one row matches: skip `create_issue_fix`; record in your run summary:
  `issue_fix for <issue>@<version> already exists (<ref>) - fix row satisfied by this or a sibling change request`;
  treat the fix row as satisfied rather than an error; this is the checkpointed
  re-entry path when a prior verify session minted the fix and died before
  closing the parent issue and change_request.
- If two-or-more rows match: halt and report the duplicate issue_fix refs. A
  re-entrant wrap-up must create exactly one issue_fix for the parent issue at the
  derived version; multiple rows are gold residue requiring human repair, not a
  state to silently tolerate.
- Proceed to 4c (parent-close guard), then step 5 (Final close).

This is the expected re-entry / sibling-change-request idempotency path. Do not treat `duplicate_issue_fix` as the normal control path, and a single pre-existing matching fix row must not STOP at `merged`.

If no existing row matches, create_issue_fix exactly as below:

`create_issue_fix` with:

- `issue` = the parent issue ref, `change_request` = this change request ref,
- `fixed_in_version` = the value derived in 4a,
- `code_change_ref` = the merge SHA from the record (`code_change_ref`),
- `close_reason: "tested"`, `status: "closed"`,
- `title` / `fix_description` / `summary` / `details` describing the fix (root cause +
  what landed; reference this change request and the merge SHA),
- audit fields: `created_by` / `last_edited_by` / `fixed_by` / `closed_by` = `ai:claude`
  (or your executor identity), `created_at` / `last_edited_at` / `fixed_at` /
  `closed_at` = now.

Confirm it inserted. **Backport** to additional versions stays an owner decision —
emit **one** `issue_fix` row, for the `in_development` version only.

### 4c. Drive the parent issue to a terminal state (multi-change-request guard)

Before closing the parent, check it is not still owed work by a **sibling** change request:
`list_inbound_refs ref=<parent issue ref> src_dto_type=change_request`. For each
referencing change_request **other than this one**, read its status. If **any** sibling
change request is **not** `closed`, the issue spans several change requests and is not yet done:

- Do **not** close the parent issue. Record in your run summary: "parent-issue close
  deferred — open sibling change request `<ref>` (status `<status>`) still references it." Then go
  to **step 5 (Final close)** (this change request still closes; the issue legitimately outlives
  it).

Otherwise this change request is the last open child: `update_issue` with
`fields: { status: "closed", close_reason: "tested", closed_by: "ai:claude",
closed_at: "<now>" }`. Re-read and confirm `status == closed`.

## 5. Final close

Every verified path (fidelity satisfied, or a gap tracked as its own
issue/action_item) ends here:

`update_change_request` with `fields: { status: "closed", close_reason: "merged" }`.
Re-read and confirm `status == closed`. (`code_change_ref` is already present from the
merge verb; `close_reason: "merged"` satisfies the schema's `x-status-requires` gate.)

**Worked example / postcondition check:** for an epic-parented, merged change request such as
`parent: keel/epic-5`, verify audits the merged diff, skips section 4 because no
`issue_fix` or parent-issue close applies, executes this step, and confirms the record
is `closed`. The runner's `verify` postcondition must see `closed`, not `merged`.

The unit is now immutable. To make further changes it must be reopened first (that is a
human, `change-request correct`/reopen path — not part of this skill).

## 6. Exit

Exit cleanly after writing `closed` (or `in_progress` on a no-op reopen). The runner
routes on the written status: `closed` completes the tail; `in_progress` re-dispatches
`dev`. This verifier records scope-fidelity gaps as issues/action_items separate from
the close.
