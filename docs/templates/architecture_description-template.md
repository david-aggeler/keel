# Architecture Description: {title}

An `architecture_description` record is one node of a product's architecture **tree**: exactly one ROOT per product (overview + index) plus any number of CHAPTER records (one focused topic each). Both shapes use this same DTO type and template; which shape you are writing is determined by structure, not a field — a record listed in the root's `chapters[]` is a chapter, the record carrying a non-empty `chapters[]` is the root.

**The governing rule — document what the code cannot tell you.** Context, drivers, quality goals, constraints, rationale, and the reasoning between components live here; anything mechanically recoverable from the repo (signatures, schemas, env vars, endpoint lists) is linked, never restated. This is the primary defense against stale docs.

**Fields to set (both shapes):**

- `title`: root — "{Product} Architecture"; chapter — the topic in a few words (e.g. *"Write path & vault canonicality"*), never "Chapter 3". *(mandatory)*
- `summary`: root — "{what the system is and its #1 quality goal}; {status}". chapter — "{what this chapter covers}; {status}". Update on status change.
- `status`: draft | in_review | approved | closed *(mandatory)*. `approved` = current structure reviewed; content keeps evolving in approved. Chapters carry independent status.
- `related`: design_decision refs, quality-goal requirement refs, soft see-also links.
- `details`: body per the applicable skeleton below.

**Root-only field:**

- `chapters[]`: ordered refs to this product's chapter records (same type). The order IS the reading order. Adding a chapter = create the record, append its ref here, add its annotated summary to root §10.

> **One tree per product.** Never a second root; grow or split chapters. Per-version editions come from release-cut baselines (`read_at_baseline`), not duplicate records; `supersedes` only for a genuine coexisting redesign.
>
> **Sections are a reading order, not a form to fill.** Mandatory core: §1 (with quality goals), §3, §4, §5, §7. Everything else scales with the product — an honest "nothing architecturally notable" one-liner beats padded prose.
>
> **Records over prose.** Decisions are design_decision records; quality requirements are requirement/ac records; risks are failure_mode records; terms are glossary_term records. The architecture describes what IS, and links the why/how-verified — it never duplicates those populations.

---

## ROOT skeleton (`details`)

### 1. Introduction & quality goals *(mandatory — the section everything else answers to)*

- **Purpose**: what the product is and does, three sentences, newcomer-first.
- **Top quality goals**: the **top 3–5** architecture-driving quality goals, **prioritized**, each concrete and testable — no bare "-ilities". Weak: *"the system is reliable."* Strong: *"a crash between vault commit and index write must never lose an accepted record (durability over availability)."* Every goal names the requirement record that carries it, or states the assumption explicitly if none exists yet.
- **Stakeholders**: who reads/judges this architecture (owner, agent personas, operators, consuming products) and what each expects of it — one line each.

| # | Quality goal (concrete) | Priority | Carried by |
|---|---|---|---|
| 1 | {scenario-flavored statement} | highest | {requirement ref or "assumption — record pending"} |

### 2. Constraints

What the architecture was NOT free to choose — technical (mandated stack, protocols, resource ceilings), organizational (team size, solo-operator + agents, release cadence), and regulatory/process (standards followed, license policy). One table row each: constraint → consequence for the design. Omit only if genuinely unconstrained (rare — say so explicitly).

### 3. Context & scope *(mandatory — C4 context level)*

The system as a black box: every neighboring actor and system, what crosses each boundary (domain-level, not protocol-level), and what is explicitly OUTSIDE the boundary. A context diagram (Mermaid/attachment) plus the table:

| Neighbor | In/out | What crosses the boundary | Interface detail |
|---|---|---|---|
| {actor/system} | {→ / ←} | {data/commands, domain terms} | {interface_spec row or chapter ref} |

### 4. Solution strategy *(mandatory — the shortest, densest section)*

The handful of fundamental choices that shape everything: core technology decisions, top-level decomposition pattern, and the key mechanisms by which the architecture achieves each §1 quality goal. Table form; every row traces back to a quality goal or constraint and forward to the design_decision that settled it:

| Strategy | Serves goal/constraint (§1/§2) | Settled by |
|---|---|---|
| {e.g. canonical store = git vault, DB is derived index} | {durability goal #1} | {design_decision ref} |

*Quality bar: a reader who knows only §1–§4 should be able to predict most of the rest of the document.*

### 5. Component inventory *(mandatory — building-block/container level, depth 1 only)*

The canonical component vocabulary — `failure_mode.component`, threat-model prose, and chapter *Affected components* lines spell names exactly as this table does. Include external dependencies (they fail too; DFMEA must see them). **Technology choice is payload at this level.** Stay at depth 1: internals of a component belong in its chapter, not in nested tables here.

| Component | Kind | Technology | Responsibility | Talks to |
|---|---|---|---|---|
| {name} | service \| module \| external | {runtime/stack} | {one line — what it alone is responsible for} | {components + channel} |

### 6. Runtime view — key scenarios

Behavior as **scenarios, not narrative**: pick the architecturally significant few (selection criterion: would a newcomer or reviewer misunderstand the system without it?). Mandatory coverage classes, at least one each where they exist:

1. the primary **happy-path** flow(s) end to end (e.g. the main write path, the main read path);
2. at least one **error/exception** path — what fails, what contains it, what the consumer observes;
3. one **operations** scenario — startup ordering, recovery, migration/re-index.

Sequence diagrams welcome; each scenario ≤ one screen at root level — deep flows get a chapter. *A runtime view with only happy paths is a finding.*

### 7. Deployment view *(mandatory)*

Where the software runs: processes/containers → nodes, ports, persistent volumes, init/startup ordering, external runtime dependencies, and per-environment differences worth knowing at the architecture level. Link generated operator docs (env vars, service matrix) — never restate machine-maintained detail.

### 8. Cross-cutting concepts

The rules that hold everywhere, one short subsection each, with a chapter pointer where depth exists: authentication/authorization · persistence & canonicality · error-handling conventions · logging/observability · configuration · validation/strictness posture. State the rule and its scope; mechanics belong in chapters or code.

### 9. Risks & technical debt

The curated, honest list a reviewer needs: known architectural risks, deliberate debt, and fragile seams — each with a one-line consequence and its tracking record (failure_mode / issue ref) or an explicit "accepted, untracked" with reason. This section ROLLS UP records; it does not replace them. An empty section means "we claim none" — say that only if you mean it.

### 10. Chapter summaries

The annotated table of contents — order matches `chapters[]`, one short paragraph per chapter: what it covers and when to read it. (Deliberately duplicates each chapter's scope line; the drift risk is the accepted price of a readable index. No bare link lists.)

**{chapter-1 title}** — {two-to-four-sentence summary}.

### 11. Linked decisions & glossary

- **Decisions**: the design_decision records shaping the top-level structure (product-wide only — chapter-scoped decisions link from their chapter), half a line each on what the decision constrains. Scope per the record population: important, expensive, critical, or risky choices.
- **Glossary**: link the product's glossary_term records for terms a newcomer would misread; define nothing inline that a record already defines.

---

## CHAPTER skeleton (`details`)

### Scope

One or two sentences: which slice of the architecture this chapter owns and what it deliberately leaves to sibling chapters.

### Affected components

Names spelled exactly as root §5. A chapter is usually one of: a **component zoom** (one §5 row's internals — this is where depth-2 decomposition lives), a **flow deep-dive** (one §6 scenario in full), or a **concept chapter** (one §8 rule end to end).

### Quality goals served

Which root §1 goals this chapter's design serves or trades off, one line each. If a chapter serves no quality goal, question the chapter.

### {Topic narrative}

The body — free-form in structure, but it owes the payload of its chapter type. Pick the matching checklist; cover each item or consciously skip it (a skipped item is a choice, not an oversight). Diagrams welcome; large ones as attachments. Link machine-maintained truth instead of restating it.

**Component zoom** (one §5 row's internals — depth-2 lives here):

- internal structure: the depth-2 building blocks and how they interact (the only place nested decomposition is allowed);
- state owned: what data/state this component alone holds, its lifecycle, and what happens to it on restart/crash;
- error containment: what can fail inside, what the blast radius is, and what the component guarantees its consumers despite failure;
- interfaces in detail: what it exposes/consumes beyond the §5 one-liner — contracts, idempotency, ordering assumptions (pointer to interface_spec rows where they exist).

**Flow deep-dive** (one §6 scenario in full):

- trigger & preconditions: what starts the flow and what must already be true;
- the sequence: step by step across components, naming the channel on every hop;
- failure points: at EACH step — what can fail, what compensates or contains it, and what the caller observes (the happy path alone fails review);
- guarantees & observability: what consistency/durability holds at the end, and which log lines/metrics prove the flow ran.

**Concept chapter** (one §8 rule end to end):

- the rule, stated normatively, and its scope (which components it binds, which are exempt and why);
- mechanics: how each bound component implements it;
- enforcement: what makes violations impossible or visible — server reject, gate, lint, review convention (unenforced conventions are named as such);
- exceptions: the sanctioned deviations and where they are recorded.

### Linked decisions

design_decision refs this chapter realizes or is constrained by (via `related`), half a line each on the connection. Important rationale with no record yet → create the design_decision first; never bury decision rationale in chapter prose.

---

## Quality criteria (review checklist)

- **Quality goals**: 3–5, prioritized, each concrete enough to test — and each §4 strategy row traces to one. A goal no strategy serves, or a strategy no goal justifies, is a finding.
- **Predictive front**: §1–§4 alone let a reader predict the rest.
- **Vocabulary closure**: every §5 component appears in ≥1 chapter's *Affected components*; every chapter component name resolves to §5.
- **Behavior honesty**: runtime coverage includes error/exception and operations paths, root and chapters both.
- **No code echoes**: nothing in the tree restates what a generator or the repo answers — pointers only.
- **Records, not prose**: no inline decision rationale, quality-requirement text, risk registers, or term definitions that duplicate their DTO populations.
- **Root stays thin**: any root section beyond ~2 screens wants to be a chapter; chapters are single-topic (an "and" in a chapter title usually means two chapters).
