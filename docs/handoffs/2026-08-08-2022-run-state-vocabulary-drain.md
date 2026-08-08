---
priority: P2
status: in-progress
branch: main
last_commit: b91f657
cwd: /projects/keel
created: 2026-08-08T20:22:38+02:00
---

# Ancestor-rollup flicker fixed and merged; iteration-38's other two units still need driving

Owner reported Test Explorer ancestor nodes flickering ✓ ↔ spinner once per
test during a decomposed lane run. Root-caused, specified, and fixed; the
batch around it (`keel/iteration-38`) is one-third drained because the
automated tail is broken for keel.

## Next action

**Hand-drive `keel/change_request-164` as supervisor** — owner-decided
2026-08-08, knowing it forfeits the independent child review.

0. **First, before anything:** confirm gold is reachable on its new schema
   and re-read `keel/change_request-164` — it was approved under the old
   one. If a read or write conflicts, stop and ask the owner. The code work
   below needs no `openbrain-client`, only `keel-dev` and git, so it can
   proceed even if the client is dead; only the record updates depend on
   gold.
1. `go run ./cmd/keel-dev worktree up cr-164-errored-reachable`
2. Implement its three ACs in that worktree:
   - **ac-425** — `cmd/keel-dev/vscode.go` `emitGoTestJSONEvents` (~line 2119)
     handles `Action: "build-fail"`, emitting exactly one `errored` event
     keyed to the package id with the accumulated `build-output` as message,
     and no `failed` for the same id.
   - **ac-426** — `vscode/projectors_shared.go:67` `StatusEventName`: change
     `default: return "failed"` to `"errored"`.
   - **ac-427** — give `testbridge/testbridge.go:844`, `:862` and
     `vscode/robustness.go:156` a `test_id`, so `applyRunEvent` resolves a
     non-empty item list (`vsix/src/extension.ts:940`) and Errored finally
     reaches a tree item.
3. Producer-side tests for each, including a fixture package that fails to
   build.
4. `go run ./cmd/keel-dev ci` — Go-only surface, so `vsix ci` is not needed.
5. Check the diff against ac-425/426/427 one by one and write the evidence
   into the CR summary. A green gate is not proof.
6. Merge to main (`git merge --ff-only`), close with `close_reason: merged`
   plus the SHA.

Then the same for **`keel/change_request-165`** (docs tier: correct F8 in
`docs/mutex/vscode-object-model.md`, add the `statePriority` table, annotate
the four permitted `run.skipped` reasons in `extension.ts` — ac-428, ac-429).

Once both are merged, close `keel/iteration-38` with
`close_reason: merged` — the all-or-nothing gate is then satisfied.

## Blockers

- **Gold moved to a new version after this session's writes** (owner, right
  at handoff). Expect schema friction: every record referenced here was
  written against the *previous* schema — issue v41, change_request v49,
  requirement v39, ac v11, impact_assessment v34, iteration v35,
  issue_fix v38, action_item v12. Re-read a record before updating it, and
  **stop and ask on any conflict** rather than forcing a write. Owner
  instruction, verbatim: *"expect quite some friction. stop in case of
  conflict"*.
- **`openbrain-client is not forward compatible`** (owner, same moment).
  The installed binary is `1.5.2.262`. Do not assume any
  `openbrain-client` verb still works against the new gold — that includes
  `run-queue`, `init-skills` and the record-write fallbacks. Prefer the
  `mcp__gold__*` tools, and verify the client's health before relying on
  it. This compounds `keel/issue-127`: the automated tail was already
  unusable for keel, and is now doubly so.
- **`keel/issue-127` (P1)** — the run-queue `dev` verb supplies
  `go run ./cmd/openbrain-dev …` as the in-session merge gate. keel has
  `cmd/keel-dev`. Every agent-driven CR in keel parks at `in_progress`
  regardless of its diff. This is why 164/165 must be hand-driven rather
  than dispatched. Fix is upstream in openbrain (runner should resolve the
  gate from the product's `dev_defaults`, which are already correct), not
  in keel.
- **`keel/issue-126`** — `openbrain-client init-skills` overwrites keel's
  thin `worktree-*.sh` delegators with the catalog's pre-CR-149 copies that
  run `git worktree add/remove/list` themselves. If anyone re-runs
  init-skills here, follow it with
  `git restore .claude/skills/change-request/scripts/` or the gate reddens.
  Upstream fix tracked as `keel/action_item-18`.

## Decisions made this session

- **Amended `requirement-71` in place** rather than minting a new
  requirement — issue-125 is the same settle-never-revert contract seen from
  the other side (never enqueued, vs. re-enqueued), and its amendment log
  already carries the issue-55 and issue-91 entries.
- **`requirement-116` covers six of seven states, deliberately.** Unset is
  not deliverable per item on this platform (no clear-result API; persistence
  re-associates by id), so `ac-429` pins that as a boundary instead of a
  goal. That ceiling is what falsified mechanisms C and D historically.
- **Kept keel's worktree wrappers over the gold catalog's** (owner call).
  keel is the source of truth for them; the catalog is behind.
- **Rejected `change_request-167` unimplemented** — it was scoped on a
  misdiagnosis (weaken the lint, adopt the catalog wrappers).
- **Supervisor executed `change_request-168` and finished `-166` by hand**,
  both owner-authorized, because no child can pass the gate that blocks them.
- **Dropped the cr-167 lint narrowing** (owner call) — worktree and branch
  removed. 3 of the 10 `no-shell-worktree-lifecycle` hits were genuine false
  positives on operator-facing `echo` lines; that cleanup is now unrecorded
  and would need re-doing from scratch if ever wanted.

## Dead ends

- **Narrowing `no-shell-worktree-lifecycle` to ignore quoted mentions does
  not unblock anything.** It removes 3 of 10 violations; the other 7 are real
  `git -C "$PRIMARY" worktree add/remove/list` invocations in the catalog's
  wrappers. Do not retry this as a way to make `init-skills` output pass.
- **Do not re-dispatch a run-queue child at 164/165 expecting a different
  outcome.** Three dispatches, three halts; two were the `openbrain-dev` gate
  (issue-127) and reproduce identically.
- **Do not launch `openbrain-client run-queue` from inside a CR worktree.**
  The shell's cwd persists between calls; one halt
  (`precondition: primary checkout on cr-166, want main`) was caused by
  exactly that. Anchor every dispatch at `/projects/keel`.
- **`-list` is stale in the skill text; the installed client 1.5.2.262 wants
  `--list change_request-<n>`.**

## Uncommitted files

(clean)

## Recent commits

```
b91f657 Enqueue VSIX run execution scope
6fa9836 cr-168: refresh the catalog skills, keep keel's worktree wrappers
4932d9b docs: stop restating the coverage floor in the rulebooks
07ea3e6 deps: bump google.golang.org/grpc to v1.82.1 for GO-2026-6061
246f4ac keel v0.7.2: stamp VSIX version
```

## Context

**The bug and its mechanism.** VS Code computes an ancestor's icon as the
maximum-priority state over its descendants. Read verbatim from the shipped
build (`workbench.desktop.main.js`, identical in 1.128–1.130):
Running 6 > Errored 5 > Failed 4 > **Queued 3 > Passed 2** > Skipped 1 >
**Unset 0**. No VSIX run path had ever called `TestRun.enqueued`, so a
descendant that had not yet started was Unset — invisible — and the settled
siblings carried the ancestor to Passed in every inter-test gap. With the
lane decomposing into one `go test` process per test, that cycled about once
a second. Fixed at `b91f657`: enqueue the selection's leaf descendants
(aliases included, so it reaches `covers::go::test::…`; desired-state ids
excluded) before results stream, in both `runSelected` and
`externalRunMirror`.

**Records written.** requirement-116 (approved) + ac-425…429;
requirement-71 amended + ac-430, ac-431; requirement-88 + ac-432;
impact_assessment-1 (the gap analysis, 8 gaps G1–G8, 7 scored dimensions);
iteration-38; issue-125 (closed via issue_fix-116); issue-126; issue-127;
change_request-164/165 (approved, undriven), -166 (merged b91f657),
-167 (rejected), -168 (merged 6fa9836); action_item-17, -18, -19.

**Verification standard to keep.** No extension can read a rendered icon
back (`docs/mutex/vscode-object-model.md` F1), so every assertion is a
behavioral proxy — ac-431's spec recomputes the rollup from recorded stamps.
The owner's live editor remains the only true readback, and the flicker fix
has **not** yet been owner-validated in a real editor.

**Worktrees left on disk:** `cr-166` (merged; run-queue owns and resets it)
and `cr-168-catalog-skill-refresh` (merged, clean, safe to remove).
See also `keel/action_item-11` on leftover worktrees generally.
