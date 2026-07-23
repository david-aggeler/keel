---
priority: P2
status: next-up
branch: main
last_commit: 378f87c
cwd: /projects/keel
created: 2026-07-23T10:22:00+02:00
---

# Handoff: 0.6.0 dev line + concluded all open keel issues into iteration-30

Session was entirely **gold (SoR) record work** — no code changes, git tree clean.

## Next action

Drain **keel/iteration-30** (the open 0.6.0 bugfix batch) via the run-queue, honoring per-CR auto_merge:

- `run-queue cr-129` — keel/cli owns+renders its global flags (auto_merge:true, agent, standard).
- `run-queue cr-130` — testbridge acquireRunLock steal-if-dead reclaim (**auto_merge:false — owner-supervised**; red-spec-first advisable; P1).
- `run-queue cr-127 cr-128` — pre-existing approved fix CRs for issue-92/95. **Target by CR id, NOT `run-queue iteration-30`** — see Blockers (they lack the iteration field).

Always pass `-product keel` to run-queue (openbrain/issue-414).

## Blockers

- **CR-127 / CR-128 can't take the `iteration` field.** They were approved before `kind` became mandatory in gold; `kind` is frozen on approved-or-later, so any write is rejected (`change_request_kind_required` → `change_request_kind_frozen`). Owner noted they fixed the `kind` migration only for **openbrain** CRs, not keel. To make these two iteration-resolvable, unfreeze via the advanced-ingestion maintenance token (same fix applied to openbrain) — otherwise just target them by CR id. Their parent issues (92/95) DO carry iteration-30.
- **openbrain product_version rename bug (issue-509 / CR-901, approved, fixed-in-flight).** `admin_update_product_version new_version=…` fails `vault: commit: cannot create empty commit: clean working tree` — the rename re-materializes canonical `.md` bytes that ignore body/release_notes edits, so the "perturb then rename" unwedge does NOT work. openbrain created keel 0.6.0 fresh server-side instead. When CR-901 ships, the plain rename works again.

## Decisions made this session

- **keel 0.6.0** is the in-development product_version (created fresh server-side). 0.5.10 deprecated as superseded placeholder. 0.5.9 = released. **0.1.0–0.5.8 deprecated** (owner: everything strictly `< 0.5.9`).
- **iteration-30** created: "Bugfix batch — 0.6.0 development line (post-v0.5.9)", active, parent dd_plan-1. Thin link-farm; members join via their own `iteration` field.
- Concluded all 4 open issues (see table). New records: **requirement-101** (+ac-362/363) for issue-96; **requirement-102** (+ac-364/365/366) for issue-97; **CR-129** (issue-96), **CR-130** (issue-97). Both new CRs kind:fix, requirements empty (contract on the parent issue's related_requirements).
- CR-130 set **auto_merge:false** deliberately (P1 lock-acquisition rewrite; the "except critical" carve-out + matches prior testbridge-lock CR handling).

## Conclude result

| Issue | Requirement (AC) | Fix CR | auto_merge |
|---|---|---|---|
| issue-92 (P2 bug) | req-99 (pre-existing) | CR-127 (pre-existing) | true |
| issue-95 (P3) | req-100 (pre-existing) | CR-128 (pre-existing) | true |
| issue-96 (P3) | req-101 new (ac-362/363) | CR-129 new | true |
| issue-97 (P1 bug) | req-102 new (ac-364/365/366) | CR-130 new | **false** |

All 4 issues stamped `iteration: keel/iteration-30`. iteration-30 inbound refs = issues 92/95/96/97 + CR-129/130 (CR-127/128 absent per Blocker 1).

## Uncommitted files

None — git tree clean; all work is in gold.

## Recent commits

- 378f87c init-skills
- 1804670 Small changes
- f0e2f9a Merge branch 'cr-126'

## Context

Records live in gold, product `keel` (`mcp__gold__*`). Never local markdown. Plan `keel/dd_plan-1`, epic `keel/epic-1` (issue-92 rolls up to `keel/epic-2`). Every CR goes through the change-control loop; green gate = `go run ./cmd/keel-dev ci` (85% floor, target 90%).
