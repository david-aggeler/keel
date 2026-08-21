---
name: run-queue
description: "Resident-session orchestrator that drains a batch of approved change_requests one CR at a time. Each single CR is driven by a SEPARATE LLM instance (a fresh `openbrain-client run-queue` child), while the resident session supervises: it resolves the target into a CR set, reuses the client's qualify (approved+agent) + depends_on dependency-order contract, enforces an auto_merge:true stop-and-ask preflight, dispatches one `--list change_request-<n>` child at a time (never `--epic`), and decides what to do when a child halts. Use when the user says: '/run-queue', 'run-queue iteration-2', 'run-queue epic-4', 'run-queue cr-4', 'run-queue cr-4 using claude', 'drain the queue', 'run the approved CRs of', 'work the queue for'"
allowed-tools: mcp__gold__list_change_request, mcp__gold__get_change_request, mcp__gold__search_change_request, mcp__gold__get_epic, mcp__gold__get_iteration, mcp__gold__update_iteration, mcp__gold__list_inbound_refs, mcp__gold__list_relations_for, mcp__gold__update_change_request, mcp__gold__create_issue, mcp__gold__create_action_item
x-openbrain-source: run-queue/v5
x-openbrain-content-source-hash: sha256:1d8bf18abf6646a4ddf56cfd58f9752c1e739148159662c0a1fefd499e794c7c
x-openbrain-content-hash: sha256:c53f11226c02680845dd5048896fe347f7813d1da0b1a31d2867ef167f71f8e5
---

# Run Queue

Drive a batch of **approved, agent-executable** change_requests toward `merged`, **one CR at a
time**. The resident session **supervises**; the actual work on each single CR is delegated to a
**separate LLM instance** — a fresh `openbrain-client run-queue` child that spins up its own
codex/claude session for that one unit. The resident's job is orchestration: resolve the set,
order it, gate it, dispatch one child, watch the outcome, decide the next move.

This is the in-session twin of `openbrain-client run-queue`'s own `--epic` drain. The client tool
can drain a whole target unattended, but it parks on the first problem it cannot resolve and moves
on. This skill keeps a supervisor in the loop between units: it reuses the tool's exact **qualify +
dependency-order** contract, but dispatches **a single CR per child invocation** so control returns
to the supervisor between units — and a halt gets a deliberate decision instead of a silent park.

> **Direction of travel.** This guidance is the seed of a future `openbrain-client loop` verb: `loop`
> reads this skill as its playbook and calls another `openbrain-client` instance with a single CR per
> unit. Keeping the per-CR drive in a separate LLM instance (rather than the supervisor doing the dev
> work itself) is the design that has worked best. This skill may also be surfaced as a `method`
> record in the catalog so the same playbook is reachable as process guidance, not only as a skill.

## Usage

| Invocation | Target set | Child executor |
|---|---|---|
| `/run-queue cr-4` | the single CR `cr-4` | codex (default) |
| `/run-queue epic-4` | qualifying child CRs of `epic-4` | codex (default) |
| `/run-queue iteration-2` | CRs belonging to `iteration-2` | codex (default) |
| `/run-queue cr-4 using claude` | `cr-4` (or any target above) | claude (`run-queue-claude`) |

The **default child executor is codex** (`openbrain-client run-queue`). Use the claude executor
(`openbrain-client run-queue-claude`) **only when the user appends "using claude"** (or "with claude"
/ "-claude").

> **The skill argument and the CLI identifier are different tokens.** `cr-4` above is the *skill's*
> shorthand — the form the user types and the form branches and worktrees use. The **CLI does not
> accept it**. Resolve it to the canonical record ref `change_request-<n>` before you build any
> `openbrain-client` command line. See § Contract for the two ways this bites.

## Contract

- **The child owns the worktree.** `run-queue` / `run-queue-claude` create a per-unit worktree
  themselves, named `cr-<n>` (the canonical ref with the `change_request-` prefix stripped).
  **Never run `worktree-up` yourself** for a queued CR — a second worktree root corrupts the run.
  If you must inspect, do it in the child's own worktree.
- **`--no-worktree` is the one exception, and it is single-item only.** It runs the verb sessions in
  the current checkout instead of a per-unit worktree. Passing it with `--epic`, `--iteration`, or a
  multi-id `--list` is rejected (`--no-worktree is only allowed for a single change_request id`).
  Use it only when you deliberately want the unit driven in the checkout you are already in — a
  resume of work already sitting there, or a repo state a fresh worktree would not reproduce. It
  makes the run's blast radius your current checkout, and the verb sessions will not be on a
  `cr-<n>` branch, which their own worktree check expects.
- **One CR per child invocation.** Always pass `--list change_request-<n>` with a single id. Do
  **not** hand the whole batch to `--epic` or `--iteration` — either collapses it into one unsupervised
  child and surrenders the between-unit control this skill exists to keep.
- **Two spellings the CLI rejects — get both right or the dispatch dies before any work starts.**
  - **Double-dash for every long flag.** Since the keel/cli POSIX migration, `-list` is parsed as the
    short flag `-l` and fails `unknown flag "-l"` (exit 2). The same applies to `--epic`,
    `--transition-gate`, `--product`, `--continue-on-error`.
  - **Canonical ref, not the `cr-N` shorthand.** `--list cr-921` fails
    `invalid --list entry "cr-921" (want change_request-<n>)` (exit 64). Pass `change_request-921`.

  When in doubt, read the binary rather than this page: `openbrain-client --mode ai run-queue --help`
  is the authority on the flag surface.
- **Division of labor.** The inner child LLM does the CR's initial implementation and drives its
  tail. The outer (resident/supervisor) LLM resolves/orders/gates the set **and performs the
  corrective actions when a child halts** — it does not do a unit's initial implementation, but it
  is the one that fixes what a halted child left behind.
- **Sparse writes.** When you record a park blocker via `update_change_request`, pass only the changed
  keys in `fields:` — a top-level update is a full REPLACE that drops every field you omit.
- **No fabrication.** Never report a CR as merged you did not observe reach `status=merged`. An honest
  halt with a recorded blocker is success; a fabricated "done" is the one unrecoverable failure.

## Steps

### 1 — Resolve the target into a CR set

- **`cr-N`** → that one CR, as the canonical ref `change_request-<n>`. (Still runs the auto-merge
  preflight before dispatch.)
- **`epic-N`** → its child CRs: `list_inbound_refs(epic-N)` and keep the change_requests whose
  `parent` points at the epic. (Epic children link via the child's `parent` field, not the epic's
  `related[]`.)
- **`iteration-N`** → CRs whose `iteration` field equals `iteration-N` (`search_change_request` /
  `list_change_request`, filter on `iteration`).

### 2 — Qualify and order (the run-queue contract)

- **Qualify:** keep only CRs with `status = approved` **and** `executor = agent`. List every CR you
  drop and why (`status=draft`, `executor=human`, already `merged`/`closed`, …) so the user sees the
  full picture — but do not dispatch them.
- **Order:** topologically sort the qualified set by `depends_on` — a CR runs only after every in-set
  record it depends on has reached `merged`/`closed`; break ties by ascending sequence number.
- **Park:** a CR that depends on an **out-of-set record that is not closed**, or that sits in a
  dependency **cycle**, is parked (not dispatched). Report it with its blocker.

### 3 — Auto-merge preflight (STOP-and-ASK)

Every qualifying CR **must** carry `auto_merge: true`. **If any qualifying CR has `auto_merge` false
or absent, STOP before dispatching anything and ask the user** — list the offending CRs. (Do not
silently proceed: the child would do all the dev/review work and only then park the unit for a human
at `ready_to_merge`, wasting the run.) Proceed only once the user confirms or flips the flags.

### 3b — Mark the lane active before the first dispatch

A CR that is about to be worked belongs to a lane that is about to be **drained**, and `active` is
what says so. Do this **before** the first child launches, not after the loop — the point is to
fence the roster while the run is in flight.

**This is keyed on the CR's own `iteration` field, not on the target form.** A `cr-N` target drained
out of a lane marks that lane active exactly as an `iteration-N` target does; a CR with no
`iteration` has no lane and this step is a no-op for it. Collect the distinct lanes across the
qualified set and, for each:

```text
update_iteration(product, id=iteration-N, fields: {
  status: "active",
  last_edited_by: "claude",
})
```

- **Sparse `fields:` only** — a top-level update is a full REPLACE that drops every omitted field.
- **Idempotent.** Already `active` → do nothing.
- `draft` or `ready` → set `active`. Both are roster-open states; the drain is what ends that.
- **Never reopen a finished lane.** `completed` or `closed` with a member you are about to dispatch
  is a bookkeeping contradiction, not something to silently fix by flipping the status back. Report
  it and let the user decide whether the lane or the CR's `iteration` field is wrong.

**Why it matters beyond bookkeeping.** `issue-grooming` step 4 targets only `draft` and `ready`
lanes, so an `active` lane stops receiving new members — a roster that changed under a run which
had already qualified its set would leave the new member neither worked by the run in flight nor
visible as waiting. Marking the lane `active` here is what raises that fence. The reverse direction
stays open: `issue-grooming` may still move records **out** of an active lane, which is how a lane
that is nearly done gets cleared to complete.

### 4 — Dispatch one child per CR

For each CR in dependency order, launch a single-item child run:

```bash
# default (codex)
openbrain-client --mode ai run-queue --list change_request-<n>
# "using claude"
openbrain-client --mode ai run-queue-claude --list change_request-<n>
```

So for the skill argument `cr-921`, the dispatch is
`openbrain-client --mode ai run-queue --list change_request-921 --transition-gate unit`. (The bare
positional form `run-queue change_request-921` is also accepted, but prefer `--list` — it is the
form the rest of this skill's contract is written against.)

Pass `--transition-gate <rung>` only when the operator explicitly overrides the CR's
declared `transition_gate`; otherwise let the child read the record. The rungs are
`prose`, `static`, `unit`, `integration`, and `system`. Run the child as a
foreground/normal task and read its result — do **not** background it behind a `| tail`
that swallows the exit status.

### 5 — Decide what to do when a child halts

System-gate CRs and codex/claude quota pauses commonly halt **before** the tail reaches `merged`.
When a child does not land the unit clean, **the outer LLM performs the corrective action** —
diagnose, then choose:

- Read the child's own JSON log — `<repo-root>/.logs/openbrain-client-<DATE>.jsonl` — and grep for
  `tail halt` / `pipeline halted` **first** (not the raw codex/claude rollouts).
- **Clean resume** (the child stopped mid-tail with no code fix needed, e.g. quota pause, verb
  timeout): re-launch a fresh child on the same single CR to resume the tail — no outer edit required.
- **Corrective fix** (gate red, unmet postcondition, a bug the child left behind): the outer LLM fixes
  it in the child's worktree, then either re-dispatches a fresh child or finishes the unit by hand.
- **Runner-owned gate that never finished:** the outer LLM may run the same runner-owned
  stages once, in the child's worktree and in the foreground; when green, resume the tail
  at the next verb rather than inventing a status transition.
- **Real regression vs. orthogonal red:** distinguish a genuine regression from a **pre-existing
  orthogonal red on main** (a full `ci run` failing on something the CR's diff cannot affect). File
  the orthogonal red as its own issue — **never chase it inside this CR**.
- **Genuinely stuck:** record the blocker on the CR (sparse `update_change_request`) and leave it
  parked — do not force it.

### 6 — Pick the next one

Once the current CR reaches `merged` (or is deliberately parked with a recorded blocker), advance to
the next CR in dependency order. **Skip the transitive dependents** of a CR that failed or parked
(they cannot run until it lands) unless the user directs otherwise.

When the set is drained, report a short GFM table: each CR → outcome (`merged` / `parked: <reason>` /
`skipped: <reason>`).

### 7 — Close the iteration when its CRs are all done (iterations only)

**This step applies to `iteration-N` targets only — never to `cr-N` or `epic-N`.** A single CR or an
epic is not an activity this loop is entitled to close; an iteration is.

When the target is an `iteration-N` **and every CR in the resolved set reached `merged`/`closed`**
(nothing parked, nothing skipped, nothing left in a dependency it never satisfied), close the
iteration record itself:

```text
update_iteration(product, id=iteration-N, fields: {
  status: "closed",
  close_reason: "merged",
  closed_by: "claude",
  closed_at: <now, ISO8601>,
})
```

- **Sparse `fields:` only** — a top-level update is a full REPLACE that drops every omitted field.
- **All-or-nothing gate.** If **any** CR in the set parked, skipped, or otherwise did not reach
  `merged`/`closed`, **leave the iteration open** — do not close it. The whole point of the gate is
  that a closed iteration means the iteration's work actually landed. Report which CR held it open.
- **Idempotent.** If the iteration is already `closed`, do nothing.
- **Qualify carve-outs don't block closure.** CRs you dropped at qualify time because they were
  *already* `merged`/`closed` still count as done; a CR dropped as `executor=human` or `draft` is
  **not** done and keeps the iteration open.

### 8 — Leave no dangling context (capture before you clear)

Like an ACR (`automated-change-request`), **this loop must leave nothing important living only in the
session context.** Before the loop ends, every loose thread must be durably captured so the context
can be cleared without losing a thing. Route each thread by **who resolves it**:

- **An agent can resolve it →** file an **issue** at **reviewed quality**: a concrete title, the
  problem, and **objective evidence captured** (the child's log line, the failing gate output, the
  commit/CR it surfaced under — HEAD-cited, not "seemed flaky"). A thin, evidence-free issue does not
  count as captured.
- **A human must resolve it →** file an **action_item** (a decision, a flag flip, an out-of-band
  follow-up).
- **A no-op stage / the CR itself needs more work →** record the blocker on the CR and, where the
  work belongs back in the unit, reopen/leave it parked with that reason.

Concretely, the following must all be captured, never left implicit: parked CRs and their blockers;
each orthogonal red you filed; any corrective fix you made outside a CR; any "we should also…"
observation the run surfaced. Only once every thread is an `issue` / `action_item` / CR blocker is
the loop done and the context safe to clear.
