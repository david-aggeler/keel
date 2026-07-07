# Step 02 — Merge Gate

**Goal:** Re-check open dependencies and run the merge gate commands for the unit's declared `merge_gate` tier, sourced from `dev_defaults`.

## Actions

**1. Re-check open dependencies — auto_merge guard**

Read the unit's `depends_on` and `deferred_pending` fields from `get_change_request`.

For each ref listed in those fields, call `get_change_request` (or the appropriate `get_<type>`) and check its status.

If any referenced unit has a status that is not `closed`:

- If `auto_merge` is currently `true` on this unit, call `update_change_request auto_merge: false` and halt with:

> auto_merge forced off at close gate: unit depends on `<ref>` (status: `<status>`), which is not yet closed. Resolve the dependency and rerun `close`.

- If `auto_merge` is already `false`, continue (the plan verb already forced it off, but re-stating this to the operator is informative):

> Note: open dependency `<ref>` (status: `<status>`) — auto_merge is already false. Proceeding with manual merge gate.

**2. Load dev_defaults**

Call `get_dev_defaults product=<product>`.

If not found: **halt loudly.**

> Cannot run the merge gate: no `dev_defaults` record found for product {product}.
> Create one with `create_dev_defaults` and populate the `merge_gate.*` rows before closing.

**3. Read the gate row**

Look up the row in `dev_defaults.details` matching the unit's `merge_gate` field:

| Unit's merge_gate | Row key to read |
|---|---|
| `docs` | `merge_gate.docs` |
| `standard` | `merge_gate.standard` |
| `full` | `merge_gate.full` |

If the matching row is absent from the details table: **halt loudly.**

> Cannot run the merge gate: `dev_defaults` has no `{merge_gate.<tier>}` row.
> Edit the `dev_defaults` record and add the row before closing.

**4. Run the command**

The row value is the command string configured by the operator for this stack. Treat it as an opaque shell invocation.

**Important constraints:**
- Run the command string exactly as stored — do not interpolate change request record fields into it. No `$(get_change_request ...)` substitution. The command must only contain what the operator configured in `dev_defaults`.
- These commands are product-specific examples. The operator must have edited the `merge_gate.*` rows in `dev_defaults` to match their actual stack before first use. The template ships example values for an openbrain-based stack; a different consumer's stack will have different commands.

**5. On gate failure — retry up to 3 total, then park**

**Idempotency caveat:** Only retry if the configured gate command is a read-only
validator (e.g. `just static-tools && just test-unit`). If the operator's gate
command has side effects, park on the first failure instead of retrying — do not
re-run a side-effecting command against a partially-applied state.

For read-only validators: re-run the gate. A flaky gate may pass on retry. Run
the command up to 3 times total; park on the 3rd failure. If the gate is still
failing after the 3rd run, **park this unit** — do not run a 4th time, do not
wait for the owner:

1. Stop. Do not proceed to step 03.
2. Leave the unit at `merged` (the merge half already landed; do not change
   status — this park is stop + blocker-in-place, not the `on_hold` scheduling
   status). The blocker is at the gate, not the merge. `code_change_ref` is
   already recorded but the unit is not closed and step-03 `issue_fix` rows are
   pending; the owner picks up at the gate, not at the merge.
3. Record the blocker (both writes must succeed — if the second write fails,
   retry it before exiting; park is incomplete until both records exist):
   - `create_formal_review` naming the failing gate tier `{tier}`, the run count
     reached (3), and the last failing command output.
   - `update_change_request` appending a `details` note: "merge gate `{tier}`
     parked at the 3-run cap — see formal_review; code_change_ref recorded,
     step-03 issue_fix pending."
4. Exit cleanly so the owner can pick up the failing gate later. On owner resume,
   re-run the gate from step 4; on pass, proceed to `step-03-issue-fix.md` to
   complete the close.

This is the AFK-safe abort: bounded gate retries, then a recorded blocker and a
clean exit. Never spin past 3 runs; never wait for the owner.

**6. On gate pass**

Proceed to `step-03-issue-fix.md`.
