# Step 02 — Symmetric Two-Actor Per Slice

**Goal:** Each slice uses two generic subagents: a tester (writes the failing test) and a coder (implements to green). The main session orchestrates; neither subagent is a named seeded skill.

## Tester subagent

Spawn a generic subagent with this framing:

> You are the tester for this slice. You will write a failing test that verifies the following behavior.
>
> **GWT atom:** {gwt_atom}
>
> **Public interface:** {public_interface}
>
> Write the test against the public interface only. Do not look at or infer implementation internals. The test must fail (red) before any implementation is written. Return the failing test file path(s) and the exact error output.

Wait for the tester subagent to return red test output. Do not proceed until the test is confirmed red.

## Coder subagent

Spawn a generic subagent with this framing:

> You are the coder for this slice. A failing test exists at {test_file_path}. Implement only what is needed to make that test green.
>
> **Public interface:** {public_interface}
>
> Rules:
> - Do not modify the test file.
> - Do not refactor code that is not touched by this slice.
> - Return the green test output.

Wait for the coder subagent to return green test output. Confirm the test passes.

**Green-attempt cap (max 3 rounds).** The initial coder spawn counts as round 1.
If the coder returns non-green, re-spawn the coder with the same framing plus the
failing output — this is round 2. Allow **up to 3 rounds** total (1 initial spawn
+ at most 2 re-spawns). If the test is still not green after the 3rd round,
**park this slice** — do not re-spawn a 4th time, do not wait for the owner:

1. Stop the slice loop immediately. Do not start any remaining slices. Discard
   any in-flight tester output for the parked slice.
2. Leave the unit at `in_progress` (do not change status).
3. Record the blocker (both writes must succeed — if the second write fails,
   retry it before exiting; park is incomplete until both records exist):
   - `create_formal_review` naming the parked slice (its requirement ref), the
     round count reached (3), and the last failing test output.
   - `update_change_request` appending a `details` note: "slice `<req-ref>`
     parked at the 3-round green cap — see formal_review."
4. Exit cleanly so the owner can pick up the parked slice later.

This is the AFK-safe abort: bounded retries, then a recorded blocker and a clean
exit. Never spin past 3 rounds; never wait for the owner.

## Sequential guarantee

Tester runs first and must be red before coder starts. Coder runs second and must be green before annotation. No slice-parallelism in this version; that is an explicit future measurement.

## No named skills

Both subagents are generic — spawned via the Task mechanism, not by invoking a seeded skill slug. This keeps the workflow portable to any consumer install regardless of their seeded skill roster.
