---
priority: P2
status: next-up
branch: main
last_commit: 778a05f
cwd: /projects/keel
created: 2026-08-13T19:36:00+02:00
---

# Grooming aftermath — requirement-114 split question, blocked issues, iteration-40 draining in parallel

Session ran the full `issue-grooming` sweep (8 open issues → 5, then 3), a grill-me
on the backlog, a three-way unit consolidation, and a long record-repair arc after
several of my own writes were built on wrong premises. All SoR state is in gold;
this file carries only the open threads.

## Next action

Answer the standing question the session ended on: **split `keel/requirement-114`
into its constituent obligations, or keep working within it as-is.**

Analysis already in this session (re-derive with `get_requirement keel
requirement-114` and its inbound ACs): the statement carries four independent
"shall" obligations — verb-family exposure, CLI-invariant conformance,
single-implementation canonicality, catalog-path independence, branch-removal
shape — against the template's one-sentence rule. That compound shape is why every
worktree AC (10 active) lands in one bucket and every worktree `fix` unit inherits
all of them. Of the three consolidated defects in `keel/issue-133`, only the
tear-down-policy divergence violates an approved clause (single-implementation);
the glob abort (ac-436) and stale comments (ac-437) contradict nothing normative.
A split is record surgery over an approved requirement → owner decision, then a
docs-tier path if approved. **Do not start this while iteration-40 is mid-drain**
— cr-175's review resolves its contract through requirement-114.

## Blockers

- **keel/issue-132** (`blocked`): combo — fix spans repo prose
  (`openbrain-client.yaml:65-68`, `CLAUDE.md:68-69`) AND the gold record
  `keel/requirement-89` ("Where the gate runs" + amendment history). Needs the
  owner to split it into one issue per bucket before it can be groomed. Stale
  `iteration` pointer (keel/iteration-39, closed) could not be cleared — empty-ref
  rejected, `remove_fields` needs the maintenance token.
- **keel/issue-135** (`blocked`): owner decision — amend `keel/ac-428` to five
  `run.skipped` reasons (recording the rollup rationale from evidence item 6), or
  change `neutralAncestorItemsForRunEvent` under a new unit (breaks
  `extension.test.ts:1283`). Do not widen reason (c) silently.
- **iteration-40 is draining in a parallel session** (owner said so at handoff
  time). Members: `keel/change_request-163` (fix → issue-124 → requirement-115 →
  ac-422/423/424/438) and `keel/change_request-175` (fix → issue-133 →
  requirement-114 → ac-436/437/439). Both approved, `executor: agent`,
  `auto_merge: true`. **Re-read gold and `git log` before touching any of these
  records or `cmd/keel-dev/worktree.go` / `vsix/` — statuses and line numbers in
  this session's records go stale as the drain merges.** cr-175's body pins slice
  order: policy → glob walk → comments.

## Decisions made this session (owner rulings, all already in gold)

1. **The acceptance-contract rule is intended, consistent design and will not
   change** — a unit owes every active AC of every requirement it reaches, proven
   in its own branch diff. `keel/issue-134` closed `wontfix` carrying the
   statement. (Its Resolution text still reasons from my superseded "requirement
   is the unit of work" framing rather than from `kind`; iteration-40's body flags
   this. Optional cleanup, low priority.)
2. **The fix shape is `CR → Issue → 1 Req → 1..n AC`; feature is `CR → n Requirements`.**
   `kind` decides where the contract lives (structural leg every write, state leg
   at approved+, frozen at approval). Owner directed "fix the structure" over my
   proposed fix/feature split: the consolidated chain stands as one fix.
3. **Consolidation over splitting**: issue-121 + issue-123 closed `duplicate` into
   issue-133; cr-173 + cr-174 closed `superseded` by cr-175. issue-122 closed
   `obsolete` (both drifts already fixed at HEAD). issue-134 closed `wontfix`.
4. **Auto-progress stopped by owner instruction** — do not chain writes past an
   owner answer; surface, then wait.

## Dead ends (do not retry)

- **Three parallel fix units over shared requirements** (cr-173/174/175 v1): each
  owed the others' new unimplemented ACs; none could pass review. Cause: I minted
  ACs across two shared requirements during grooming.
- **`keel/requirement-117`** (tear-down policy as its own feature requirement):
  minted, then **deleted** on the owner's structure ruling. Do not re-mint without
  an explicit owner ask; ac-439 lives under requirement-114 (the
  single-implementation clause is requirement-114's statement, not 113's).
- **`one-unit-per-requirement` memory**: written, rewritten twice, judged bad,
  deleted (owner confirmed removal). The durable lesson is decision 2 above — in
  gold and in this file, not in auto-memory.
- **Re-parenting ac-439 to requirement-113** and narrowing then re-widening
  issue-133's `related_requirements`: churn from acting between owner messages.

## Uncommitted files

None — working tree clean at `778a05f` throughout; the only filesystem writes were
two probe dirs under `worktrees/` (reproduction for issue-121/ac-436), since
removed by the owner and verified gone.

## Recent commits

Repo unchanged this session. HEAD `778a05f` "Refresh gold skill catalog; ignore
issue-grooming". The parallel drain will be adding merges — re-check `git log`
on resume.

## Context

- Gold record inventory this session: created iteration-40, ac-436/437/438/439,
  cr-173/174/175 (173/174 since superseded), requirement-117 (since deleted);
  amended requirement-113/114/115, issue-121/122/123/124/132/133/134/135, cr-163,
  iteration-40. All current-state consistent per the end-of-session audit; the
  clean approval chain is cr-163 → issue-124 → requirement-115 → ac-422/423/424/438.
- ac-438 (deterministic failed-refresh tree state) was added to requirement-115
  and folded into cr-163's contract via an amendment note in its body.
- Verify-issues note for later: ac-436's live reproduction is recorded in
  issue-133 Section A (probe@1/probeb A/B at exit 64); the fix test must sort the
  bad entry first or it proves nothing.
- openbrain object-model refs that settled the kind rules: user_need-83 (CR),
  user_need-96 (issue: `related_requirements` = what the defect violates),
  user_need-102 (requirement lifecycle), user_need-79 (AC: approved through its
  parent, active|retired only).
