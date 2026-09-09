# `.claude/` — agent configuration

Skills, agents, and settings for coding agents working in this repo.

## Materialized skills are not tracked; a worktree gets them by replication

`.claude/skills/` holds two classes of file under one directory:

| Class | Count | Tracked? | Source of truth |
|---|---|---|---|
| catalog-materialized | 22 | no — gitignored | gold, via `openbrain-client init-skills` |
| locally authored | 5 | yes | this repo, under the CLAUDE.md change-control rule |

The locally-authored set is `asd-ste100`, `build`, `decide`, `merge`, `publish`.

**Telling them apart:** the stamp is the test. A materialized skill carries
`x-openbrain-content-hash` in its front matter; a locally-authored one does not.
`.gitignore` cannot read that stamp — a gitignore rule matches paths, not
contents — so the five local skills are re-included by name in a hand-maintained
negation list. Adding a catalog skill needs no change there; adding a
locally-authored one needs its own negation line, or git never sees it.

### Why untracking is safe now

A worktree created by `git worktree add` contains exactly what git tracks. That
is what made untracking unsafe before: keel/issue-200 untracked the materialized
skills on 2026-08-20, and every run-queue worktree created afterwards carried
only the five locally-authored ones. The dev verb's skill-currentness gate found
the rest missing and aborted each unit with `outcome=skill_stale` before any work
started (keel/issue-201). The tracking was restored the next day as a workaround.

keel/requirement-157 removed the need for that workaround. Worktree bring-up now
copies every item that a committed manifest declares **and** git ignores. The
declaration lives in `openbrain-client.yaml` under `keel.worktree.replicate`, the
same policy plane both invokers read:

```yaml
keel:
  worktree:
    replicate:
      presets: [claude, codex]
```

The `claude` preset expands to `.claude/**` and `.mcp.json`. Both invokers honour
it — `keel-dev worktree up`, and `openbrain-client worktree up`, which is the path
run-queue uses. Measured on 2026-09-09 against client 1.6.7.7560: the client
reported `.claude/**` as `copied`, 536 of 536 eligible items.

**The matched-AND-ignored rule is what keeps the two classes apart.** A tracked
file is never copied, so the five locally-authored skills always arrive through
git, on the branch's own version, and a replicated copy can never shadow them.
The 22 materialized trees are ignored, so they arrive by copy. Neither class
needs the mechanism to know which is which.

### What this buys, and what it costs

**Buys:** a withdrawal at the source propagates by definition. Gold stops serving
a skill, `init-skills` stops materializing it, and it leaves this repo without a
`git rm`. That orphan class is what keel/issue-200 filed and what tracking could
never fix — under the old arrangement, `init-skills` restored an unfetchable
skill from `.claude/legacy/` rather than deleting it.

**Costs:** a fresh clone has no materialized skills until `openbrain-client
init-skills` runs in it. A worktree is covered by replication; a clone is not.

**An edit here is still not a fix.** Editing a materialized skill locally only
produces drift that the next `init-skills` overwrites. Fix a materialized skill
in gold and re-export. Fix a locally-authored one here, under the repo
change-control rule in `CLAUDE.md`.

## `.claude/agents/` is not tracked either

Same reasoning, simpler rule. Every agent projection carries the
`x-openbrain-content-hash` stamp — there is no locally-authored agent — so
`.gitignore` ignores the directory outright with no negation list. Do not add
negation machinery here by analogy with the skills block above; it would imply a
distinction that does not exist.

The seven gold serves are `adversarial-reviewer`, `api-contract`, `architect`,
`coder`, `dfmea`, `reviewer`, `ux-designer`. `init-skills` fetches them and
replication carries them into every worktree.

### Deleted projections

`cse.md` and `tester.md` were **deleted** on 2026-09-09 (keel/issue-236). Both
were stamped, but `materialization.json` had stopped listing them: their skills
are live in gold, the agent projection is not. They had been carried since
2026-08-21 as deliberate stale exports. Untracking alone would not have removed
them — nothing re-materializes what gold does not serve — so they were deleted on
the owner's ruling. The `cse` and `tester` agent types are no longer available in
this project; the skills are unaffected.

`product-manager.md` was **deleted** on 2026-08-21, for the same reason
(keel/issue-200).

**Expect `init-skills` to try to restore all three from `.claude/legacy/`.** An
unfetchable directory is treated as locally authored, so the reconcile restores
rather than removes. They are now untracked, so a reappearance is not a git diff
and nothing will flag it — check `ls .claude/agents` against the served list
above after a reconcile, and delete again if one returns.

`.claude/legacy/` and `.claude/materialization.json` stay untracked. They are
per-checkout reconcile state, not content.
