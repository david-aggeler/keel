---
priority: P2
status: next-up
branch: main
last_commit: bfd0448
cwd: /projects/keel
created: 2026-08-13T22:27:45+02:00
---

# if-7 + prototype-4 landed; verify the mutex tree shape, then decide issue-135; requirements pass queued

Evening session, all SoR state in gold. This file supersedes
`2026-08-13-1936-grooming-aftermath-req114-split-question.md` (archived) and
carries its one live thread forward (requirement-114 split, below).

## Next action

**Read the code to verify the mutex tree shape** (owner: "you expect a mutex
step to have children — need to read it tomorrow"). The prototype and if-7
model the desired-state subtree as `Desired state root > mutex group > row
children` — i.e. an intermediate, non-selected group exists between root and
row, which is exactly what the contested neutral-ancestor stamp (issue-135)
would stamp. If rows hang directly under the desired-state root (no
intermediate), the ancestor-walk picture changes and prototype-4 + if-7 need a
correction. Where to look:

- `testbridge/testbridge.go`: `desiredStateGroupItem` (:1320 — group id must be
  `<parent>::group::…`, limitations carry `mutually_exclusive=…`),
  `deriveDesiredStateGroupRows` (:702), discovery derivation for
  `testbridge::desired-state`.
- `cmd/keel-demo-dev` (normative Bridge reference) and the fixture discovery
  docs for the actual item parent chains.
- The pinned spec ids `case::test::a` / `case::suite`
  (`vsix/src/test/suite/extension.test.ts:1283`) vs real desired-state ids.

## Then: the issue-135 decision

Option 1 (ratify the stamp as a fifth ac-428 reason) vs option 2 (delete it,
F12 strict). Code-derived verdict, already recorded in prototype-4 and if-7's
open-deviation bullet: **the stamp is rendering-invisible in every reachable
moment** — `enqueueExecutionScope` enqueues leaves only and excludes
desired-state items (no ancestor is ever Queued), and the stamp fires only
under `case 'passed'`, so its Skipped(1) always loses the rollup to the passed
leaf (2). Residual footprint: own-state bookkeeping + Test Results panel rows.
Option 2 is pixel-risk-free per this analysis; F13 (owner's live editor) is
the final check. issue-135 stays `blocked` on the owner call.

## Queued behind the above

1. **Owner review of `keel/interface_spec-7`** ("Consumed VS Code testing API")
   — then the **requirements pass** ("once I had a look at if-7, we'll make
   new/updated requirements"). Load-bearing choice flagged: a strict
   F12-derived requirement IS the issue-135 option-2 decision.
2. **File the adjacent finding as an issue** (owner not yet asked to file):
   `cancelActiveRun` and `rejectConcurrentRun` stamp ⊘ over the raw selection,
   which can include group items — two more extension-writes-a-group sites
   beside issue-135. Recorded in prototype-4 details + if-7 open-deviation
   bullet only.
3. **requirement-114 split question** (carried from previous handoff): split
   the compound statement into its constituent obligations, or keep as-is.
   Owner call; was gated on iteration-40's drain. NOTE: main moved during this
   session — `bfd0448` merged **cr-176** ("move gate tunables into config",
   unknown to this session) — so re-read gold (cr-163/cr-175/iteration-40
   status) AND `git log` before touching any of this.
4. Optional cleanups: banner in `docs/mutex/vscode-object-model.md` pointing at
   if-7 as normative home (repo edit, still not done); interface-spec root is
   still v1-shaped (register inline, numbered h1s) vs the v4 template — a
   future restructuring pass.

## Decisions made this session (owner rulings, all in gold)

1. **issue-132 closed** (`tested`) via `issue_fix-121`, evidence commit
   `ab2e75f`: yaml rationale comments deleted (config carries config),
   CLAUDE.md gate section slimmed, requirement-89 "Where the gate runs" amended
   to the measured once-per-tree cost.
2. **requirement-89 reframed**: new title/statement — keel-dev implements the
   transition gates, aligned with committed openbrain-client.yaml; gates match
   task complexity; rungs may group. One AC per rung: ac-440 (prose), ac-441
   (static), ac-442 (unit, folds retired ac-287/ac-290), ac-443 (integration),
   ac-444 (system — **grouped with integration** until a system-class stage is
   declared). ac-289 (node-free core) stays. Config already aligned; no CR
   needed now.
3. **CLAUDE.md halved** + new section "Command surfaces — ask the tool, not
   this file" (`--help-json`, `--mode ai` for agents). Commit `ab2e75f`.
4. **VS Code object model → SoR**: `keel/interface_spec-7` created (HTML, 5
   SVG figures, F1–F16 verbatim with evidence classes, tree-families section:
   desired-state mutex groups / lane groups / framework view). Root
   interface_spec-1 updated: register row, chapters[], 22 surfaces / 6
   chapters.
5. **prototype-4** (active): interactive issue-135 decision aid, index.html
   attached; two run-kind sections (mutex row run / regular test run),
   code-grounded after three wrong models (below).

## Dead ends (do not retry)

- **Mutex model v1 — "run the group"**: desired-state groups are served
  non-runnable; `resolveRunRequests` refuses them. You run a ROW.
- **Mutex model v2 — "sibling loop stamps inactive rows"**: the sibling loop
  needs a selected ancestor (`hasSelectedAncestorOrSelf`) — it never fires on
  a row-selected run. Exclusive siblings get `cleared` events from the bridge
  (`emitExclusiveDesiredStateSiblingClears`); their ⊘ comes from the post-run
  reconcile replay (reason d, requirement-97).
- **"Ancestor enqueued" premise**: settled NO by `enqueueExecutionScope` /
  `executionScopeLeafItems` / `isNoResultEnqueueItem` — leaves only, and
  desired-state ids excluded entirely. Never build a divergence window on it.
- Lesson the owner enforced twice: **read the code before modeling** — the
  three wrong models each shipped a wrong prototype revision.

## Uncommitted files

None. `scratchpad/prototype-issue-135/index.html` is the prototype source
(gitignored; canonical copy is the attachment on keel/prototype-4,
sha256 4f253d5f…, 19.7 KB, `node --check` green).

## Recent commits

`bfd0448` Merge cr-176 (parallel drain — NOT this session) · `3415426` /
`25e68d1` keel-dev gate-tunables (same) · `ab2e75f` this session's docs
slimming. Re-check `git log` + gold on resume; drain state in this file is
stale by definition.

## Context

- Gold writes this session: issue-132 (closed), issue_fix-121 (new),
  requirement-89 (title/statement/details), ac-440..444 (new), ac-287/ac-290
  (retired), ac-444 (grouped-with-integration amendment), interface_spec-7
  (new, edited ×3), interface_spec-1 (row/chapters/summary), prototype-4 (new,
  details edited ×4, attachment replaced ×5).
- issue-135 record itself still describes reason-(c)-fits-widest framing; the
  code-settled rendering-invisibility is recorded in prototype-4 + if-7, NOT
  yet in issue-135. Fold it in when the decision is made.
- The old handoff's blocked item issue-132/iteration pointer: issue closed;
  the stale `iteration: keel/iteration-39` field remains (needs maintenance
  token), cosmetic on a closed record.
