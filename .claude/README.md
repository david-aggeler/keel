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

## `.claude/agents/` is still tracked

All nine projections carry the `x-openbrain-content-hash` stamp and belong to the
same class as the materialized skills, so the same reasoning applies to them. They
were left tracked deliberately: keel/change_request-271 was scoped to skills.
keel/issue-235 records the follow-on.

### Known drift, as of 2026-08-21

`materialization.json` lists 7 agents gold currently serves:
`adversarial-reviewer`, `api-contract`, `architect`, `coder`, `dfmea`, `reviewer`,
`ux-designer`. Two more are tracked deliberately:

| Agent | Its skill in gold | Why it is here |
|---|---|---|
| `cse` | live, but gold no longer projects an agent for it | in active use; stale export, kept on purpose |
| `tester` | live, but gold no longer projects an agent for it | in active use; stale export, kept on purpose |

Both carry a stamp, so do not read their presence as evidence that gold still
serves them. Re-check this table against `materialization.json` whenever the
catalog is re-exported.

`product-manager.md` was **deleted** on 2026-08-21. Its skill was withdrawn in
gold and removed by keel/issue-200; the projection had outlived it. Expect
`openbrain-client init-skills` to try to restore it from `.claude/legacy/`,
because an unfetchable directory is treated as locally authored. If it reappears,
`git rm` it again rather than committing it.

`.claude/legacy/` and `.claude/materialization.json` stay untracked. They are
per-checkout reconcile state, not content.
