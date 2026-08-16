---
priority: P1
status: blocked
branch: main
last_commit: df20ae4
cwd: /projects/keel
created: 2026-08-16T20:34:26+02:00
---

# iteration-49 editor-state reference model — interface_spec-7 reworked, awaiting owner review

## Next action

**The owner reviews the reworked `keel/interface_spec-7`.** Nothing else in the lane
proceeds until that lands, because `architecture_description-5/6/7` and the
`interface_spec-3` event section all cite it.

Read the four changed places (record is `draft`, so diffing is by reading):

1. Provenance paragraph — the repo notebook is declared retired.
2. **F13** — rewritten from one verification proxy to three, and it now states that the
   behavioural-contract proxy is blind to rendered effect.
3. **F17 / F18** — two new rows appended after F16.
4. Version pinning & drift watch — the manual byte check becomes a refusing gate step.

## Blockers

- **Owner review of `interface_spec-7`.** Blocking the whole lane.
- **Model scope is still undecided.** The grill reached this question and the owner
  redirected to "rework if-7 first". F13(c) is currently written as covering
  F7, F8, F11, F12, F15, F16, F17, F18 — the "run-state core" option. If reload
  persistence (F9/F10/F14) or tree surgery (F4/F5/F6) belong in the model, F13(c)'s
  list must extend and the units grow.
- **Test-only vs shipped** — never answered. Assumed test-only throughout.
- `keel/iteration-49` stays `draft` until the two above are settled.

## Decisions made this session

Three decided in the grill, all recorded in `interface_spec-7`:

1. **Gate the byte check.** The F8 and F15 workbench literals get an automated check;
   the chapter already mandated re-running it manually, so this enforces a stated
   procedure rather than inventing policy.
2. **The gate fixture is the executable home**, `interface_spec-7` the narrative.
   Expected literals commit as a fixture; the chapter cites it instead of repeating bytes.
   This is what makes the repo notebook deletable with no loss.
3. **Refuse, don't skip.** No VS Code install, or a version outside the audited set,
   reds the gate. Runs in `keel-dev vsix ci`, not core `ci` — the host is a gitignored
   download the node-free core gate cannot see.

Earlier in the session, and already in the SoR:

- **Covers stays Unset** — owner accepted losing cross-lane coverage marks so a lane
  that never ran cannot render green. Recorded as a Limitation on `keel/requirement-54`.
- `keel/issue-155` step 0 **reproduced live**; `cr-196` returned to `approved`
  (and has since merged — see Context).

## Dead ends

Worth recording so nobody re-derives them:

- **The `_lane` sentinel node stamped `queued`.** Cannot work: VS Code's
  `markTaskComplete` sweeps everything still Queued or Running to Skipped at
  `run.end()` (F8). The node would silently become skipped, which is *weaker* than
  passed in the rollup, so the false green returns. It would also be a fifth
  `run.skipped` reason against `keel/ac-428` — the shape `cr-183` deleted.
- **Green leaves with a neutral parent.** Not expressible while covers is a descendant
  of the lane: F15 takes the max-priority descendant and ignores Unset, and no
  persistent state outranks passed without asserting something false.
- **`GoJSONResultBelongsToSelection`'s selection-kind gate as the cause of `issue-173`.**
  Wrong — the lane path never calls that function. The real cause is the unconditional
  `event.Test != ""` discard at `cmd/keel-dev/vscode.go:1966`.
- **"The mock is the problem."** Also wrong. The suite already runs real VS Code under
  `xvfb-run` with a genuine `TestController`; even `red-spec.test.ts:135` asserts on a
  recorded `calls` array. The constraint is F1 — no readback — which is why a model,
  not a better mock, is the answer.

## Uncommitted files

None. Working tree clean at `df20ae4`.

This handoff is the only new file; everything else this session was written to gold.

## Recent commits

```
df20ae4 Merge branch 'cr-207'
b19db70 cr-207 register the coinage the new remedy test introduces
770621b cr-207 name the dictionary and the registration action in the cspell stage failure
4e0489c Merge branch 'cr-196'
fef2671 cr-196 carry the initiating surface on the run-event source enum so the mirror
        skips the editor's own run (keel/requirement-36, keel/ac-485, keel/ac-486)
64342e8 Merge branch 'cr-206'
```

## Context

### main moved during this session — re-read before trusting any citation

Every record written today cites HEAD **`64342e8`**. HEAD is now **`df20ae4`**. Line
numbers in `issue-173`, `issue-174`, `cr-209`, `cr-210` and the `requirement-54`
Limitation were correct at `64342e8` and must be re-verified before use.

### `cr-196` merged, and it bears directly on F18

`fef2671` landed the run-event `source` widening: **the mirror now skips a run the
editor itself started.** F18's concurrency attribution rests on the mirror opening a
second concurrent `TestRun` for every editor-started run — which `cr-196` was built to
stop.

So the decisive experiment F18 names may already be moot, or may have become trivial:
re-run a lane at `df20ae4` and see whether queued/running icons now appear. **If they
do, F18's cause is confirmed and the row can be upgraded from UNPROVEN. If they do not,
the concurrency hypothesis is falsified and F18 needs a different cause.** Either
outcome is cheap and should be the first thing done after the review.

### Records written this session

| record | state |
|---|---|
| `keel/issue-155` | step 0 observation appended; reproduced live |
| `keel/change_request-196` | `on_hold` → `approved`; since merged. **Stale `block_reason: needs_owner_input` could not be cleared** — needs the advanced-ingestion maintenance token |
| `keel/requirement-54` | Limitation section (covers run-scope) + `ac-506` |
| `keel/requirement-71` | `ac-505` |
| `keel/issue-173` / `cr-209` | lane run settles packages only — `reviewed` / `approved`, `iteration-48` |
| `keel/issue-174` / `cr-210` | covers aliases carry no URI/Range — `reviewed` / `approved`, `iteration-48` |
| `keel/iteration-49` | created `draft` — the model lane, with the doc work as members |
| `keel/interface_spec-7` | reworked; F13 amended, F17/F18 added, drift watch gated |

### Not filed, deliberately

The **covers-alias run-scope defect** (a run stamping aliases outside its scope —
a live `requirement-71` violation) is described in the `requirement-54` Limitation with
its three sites (`vsix/src/extension.ts:1227`, `:1147`, `:1130`) and the enqueue trap at
`:339`, but has **no issue record**. The owner chose to leave it there for now.

### Lane members still to file

`iteration-49` names six candidate units and none is filed: the model, retargeting the
existing assertions, the red specs, the `interface_spec-3` event section, the
`architecture_description-5/6/7` reconciliation, and the notebook deletion
(`docs/mutex/vscode-object-model.md` — repoint `vsix/src/extension.ts:489` and remove the
`docs/mutex/README.md:32` index row first).

Sequencing trap already recorded on the iteration: AD-6 and AD-7 describe behaviour
`cr-209` will change. Reconciling them first writes prose that is wrong on merge.
