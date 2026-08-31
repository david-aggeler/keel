# `.claude/` — agent configuration

Skills, agents, and settings for coding agents working in this repo.

## Skills and agents are tracked, and worktrees are why

`.claude/skills/` and `.claude/agents/` are **committed**, including the
catalog-materialized ones OpenBrain owns.

That is a deliberate exception to the usual rule. Gold is the source of truth for
every materialized skill; this repo holds a copy, and a copy can drift. The
exception exists because of worktrees.

### The failure it prevents

`run-queue` drives each approved change_request in its own `worktrees/cr-N/`
checkout, created by `git worktree add`. A new worktree contains exactly what git
tracks. It does not inherit untracked files from the primary checkout.

So when `keel/issue-200` untracked the catalog skills on 2026-08-20 (commit
`8a94209`), every worktree created afterwards carried only the five
locally-authored skills. The dev verb's skill-currentness gate found the rest
missing and aborted the unit with `outcome=skill_stale` **before any work
started**. The supervisor had to run `openbrain-client init-skills` inside the
worktree by hand and re-dispatch — once per unit, every unit
(`keel/issue-201`).

Nothing in the worktree bring-up path materializes skills, and that path lives in
`openbrain-client`, not in this repo. keel cannot fix it at the source. Owner
ruling, 2026-08-21: track the skills, so a fresh worktree is usable the moment
`git worktree add` returns.

keel-dev worktree bring-up now reads the committed `keel.worktree.replicate`
declaration in `openbrain-client.yaml` and copies the declared gitignored items
into new checkouts. That replication covers per-checkout agent and tool state;
it does not change the tracked-skills ruling above.

### What this costs you

**A withdrawal is not automatic.** A skill withdrawn in gold does not leave this
repo on its own. `openbrain-client init-skills` cannot fetch it, so the reconcile
restores it from `.claude/legacy/` instead of deleting it. Removing it takes a
deliberate `git rm` and a commit.

**An edit here is not a fix.** A materialized skill carries an
`x-openbrain-content-hash` stamp in its front matter. Editing it locally only
produces drift that the next `init-skills` overwrites. Fix a materialized skill in
gold and re-export. Fix a locally-authored one here, under the repo
change-control rule in `CLAUDE.md`.

**Telling them apart:** the stamp is the test. A materialized skill has
`x-openbrain-content-hash` in its front matter; a locally-authored one does not.
The locally-authored set today is `asd-ste100`, `build`, `decide`, `merge`,
`publish`.

### Known drift, as of 2026-08-21

`.claude/agents/` tracks 9 projections. `materialization.json` lists 7 that gold
currently serves: `adversarial-reviewer`, `api-contract`, `architect`, `coder`,
`dfmea`, `reviewer`, `ux-designer`. Two more are tracked deliberately:

| Agent | Its skill in gold | Why it is here |
|---|---|---|
| `cse` | live, but gold no longer projects an agent for it | in active use; stale export, kept on purpose |
| `tester` | live, but gold no longer projects an agent for it | in active use; stale export, kept on purpose |

Both carry an `x-openbrain-content-hash` stamp, so do not read their presence as
evidence that gold still serves them. Re-check this table against
`materialization.json` whenever the catalog is re-exported.

`product-manager.md` was **deleted** on 2026-08-21. Its skill was withdrawn in
gold and removed by `keel/issue-200`; the projection had outlived it. Expect
`openbrain-client init-skills` to try to restore it from `.claude/legacy/`,
because an unfetchable directory is treated as locally authored — that is the
restore-not-remove behaviour `keel/issue-200` documents. If it reappears,
`git rm` it again rather than committing it.

`.claude/legacy/` and `.claude/materialization.json` stay untracked. They are
per-checkout reconcile state, not content.
