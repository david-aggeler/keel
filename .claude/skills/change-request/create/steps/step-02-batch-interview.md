# Step 02 — Front-Loaded Batch Interview

**Goal:** Elicit the owner's intent in one batch. Every question has a recommended default drawn from `dev_defaults`. **Present every row every time** — never drop a row because the default "obviously" applies. A row the unit does not deviate on is answered `n/a (default)`, not omitted. The owner sees the full slate and confirms or overrides each row explicitly.

Present all questions together; the owner confirms or overrides each default in a single reply. **Silence is not consent:** block on each row until the owner *explicitly* accepts the default or states an override. A row the owner did not answer is unresolved — do not infer acceptance from omission, and do not proceed to `step-03` with any row open. (This front-loaded completeness is what keeps `dev` fully AFK: every default is settled here, so no question surfaces mid-implementation.) **AFK escape:** if the owner is unavailable and rows remain open, leave the unit in draft with the open rows recorded — do not block indefinitely. This is the escape, not a consent proxy: the unit simply waits in draft until the owner returns.

## Must-ask (owner taste — always ask)

| # | Question | Recommended default |
|---|---|---|
| 1 | **Scope and cluster** — what is in, what is explicitly out or deferred? One unit or a cluster? **If the answer is a cluster, stop the interview** and propose splitting into N separate units, then **ask the owner to confirm the split before continuing** — each split unit gets its own `create` run and its own `create_change_request` DTO with its own batch interview. Do not silently scope to "this unit only". If the owner declines the split or is unavailable to confirm, leave this unit in draft — do not proceed and do not silently collapse to a single-unit scope. | MVP, aggressive deferral; edge-case findings fixed in-unit |
| 2 | **Lifecycle and vocabulary** — any user-facing renames or special status handling? | Standard 7-state path; no renames |
| 3 | **Strictness** — reject or coerce? Fail-fast or accumulate? What does the operator see on error? | Reject loudly; trim-then-validate; fail-fast (T1) |
| 4 | **Canonicality** — does anything become the single source of truth? Any dual-source collapse? | One source per artifact; search/update before create (T3) |
| 5 | **Migration posture** — additive, in-place, or clean break? Backfill? Destructive marker? | Pre-1.0: clean break (T11); Post-1.0: forward-only migrations |
| 6 | **Visibility and gating** — when do operators see this? Env var, config row, or no toggle? | No new knob when an existing one covers it (T5) |
| 7 | **Compat risk** — do external clients depend on the touched surface? Is breaking acceptable? | Flag if yes; default assume internal only |

## Should-ask (default usually suffices — still presented; answer `n/a (default)` if no deviation)

| # | Question | Recommended default |
|---|---|---|
| 8 | **Field intent** — required vs optional; immutable after create (status-gated)? | Workflow records are living; only `closed` is immutable (T9) |
| 9 | **Operational model** — one active or many concurrent? | One active unless a real second consumer is named |
| 10 | **Plan gating** — pre-flight audit blocks the plan, or ship-and-red? | Ship-and-red; pre-flight is explicit when the surface is sensitive |

## Gate rows (prefill from scope; confirm)

| Row | Question | Prefill rule |
|---|---|---|
| `transition_gate` | Gate rung: `prose` / `static` / `unit` / `integration` / `system`? | Ask one question — *what is the strongest class of evidence this change requires?* Documentation-only → `prose`; a change whose risk is caught by compilation and deterministic analysis alone → `static`; code changes carrying unit tests → `unit`; anything touching the store, a migration replay, or a real database → `integration`; DB-schema, MCP-API, cold boot, roundtrip, log gates, or a browser surface → `system`. The rungs are **cumulative**, so when the scope spans several the **highest matched rung wins**. Only when no rule matches does the fallthrough apply: propose `unit` and ask the owner to confirm. |
| `auto_merge` | Auto-merge eligible? | Off when the unit depends on another open unit; `system`-rung units are eligible for nightly e2e merges |

A deviation from a `dev_defaults` catalog row lands as a Decisions-table row referencing the overridden key. If the owner's answer is a novel deviation with **no matching `dev_defaults` key** to reference, mark that Decisions row `(owner)` — it is owner taste with no catalog precedent, not an override of an existing default. **Partial-match tie-break:** if the answer partly references a key (i.e. any matching `dev_defaults` key exists for the answer), treat it as an override and reference that key — do not mark `(owner)`. Mark `(owner)` only when no `dev_defaults` key matches the answer at all.

**Prefill fallthrough.** When the scope answer matches none of the rung rules above, propose `transition_gate: unit` and ask the owner to confirm — never silently leave the rung unset. When the scope spans multiple rungs, the **highest rung wins** (`system` over `integration` over `unit` over `static` over `prose`); because the rungs are cumulative, the highest one already contains the others, so nothing is lost by taking it. The no-match confirm is a create-batch row governed by G5: if the owner does not explicitly confirm, the row is unresolved and the unit stays in draft — do not treat the proposed `unit` as accepted by default.

**Pick the rung on one axis.** The value names the strongest class of evidence required — not the subject matter of the change, not a position on a ladder, not how much coverage it deserves. Answering three questions at once is what made the old tier field round down, shipping browser-affecting changes under a gate that never opened a browser. One judgement, one value.

**Auto-merge and fallthrough rungs.** A `transition_gate` resolved via fallthrough (no-match→`unit`, or highest-rung-wins yielding anything below `system`) is not `system` — only an explicitly `system`-rung unit is eligible for nightly e2e auto-merges. A fallthrough-derived lower rung is simply not auto-merge-eligible; the `auto_merge` row defaults to off for those rungs. This does not change the `system`-rung auto-merge semantics, which remain the owner-ruled default.
