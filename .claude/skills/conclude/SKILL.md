---
name: conclude
description: "Right after filing SoR records in gold, mature them in-session: collect objective evidence into the issue, then promote each issue to a change_request with requirements + acceptance criteria, and approve as far as the evidence allows. Use on '/conclude', '/consolidate', 'conclude these', 'consolidate these', 'conclude issue-xyz', 'consolidate issue-xyz'"
allowed-tools: mcp__gold__search_change_request, mcp__gold__create_change_request, mcp__gold__update_change_request, mcp__gold__get_change_request, mcp__gold__list_change_request, mcp__gold__get_issue, mcp__gold__update_issue, mcp__gold__list_issue, mcp__gold__search_requirement, mcp__gold__get_requirement, mcp__gold__create_requirement, mcp__gold__update_requirement, mcp__gold__list_requirement, mcp__gold__create_ac, mcp__gold__update_ac, mcp__gold__get_ac, mcp__gold__list_ac, mcp__gold__get_design_decision, mcp__gold__search_design_decision, mcp__gold__get_epic, mcp__gold__list_inbound_refs, mcp__gold__list_relations_for, mcp__gold__get_template_for
targets_templates:
  - change_request-template
  - requirement-template
  - ac-template
x-openbrain-source: conclude/v2
x-openbrain-content-source-hash: sha256:3cba68a39025722e8a0a9601b1899a2c6481e91173dcd2bc9379964d56494e64
x-openbrain-content-hash: sha256:ab708ad49be1bf660637704b814bbc2402b648c23b0eaac1ec4df81ae5ad89fd
---

# Conclude

Mature freshly-filed records **in this session**, while the context is hot — not later in a fresh session (e.g. an automated tail) that can't see the file:line you were just looking at.

**Objective evidence goes in the issue.** HEAD-cited (`git rev-parse --short HEAD` + a `file:line` or verbatim quote), checkable without asking you anything. Verify against the current repo/records, not the record's prose — if reality differs, fix or close the record.

Then for each issue:

- **Create a CR** (`parent` = the issue) — `search_change_request` first so you don't duplicate an in-flight unit. **Keep evidence DRY:** the objective evidence has one home — the issue. The CR's `details` section must contain the pointer statement (e.g. "Objective evidence: see `{issue}`") rather than restating the evidence. Don't copy or re-summarize it into the CR — that's both duplication and where it gets lost in the transcode. Use the server-provided template (`get_template_for change_request-template`).
- **A CR created without a parent issue must carry its own Objective Evidence section** in `details` (HEAD-cited: file:line / verbatim quotes). With no issue to point to, the CR *is* the evidence's home — it never ships without one.
- **Requirement with acceptance criteria** — create one or more (durable, behavioral; CR-specific counts go in the CR, not the requirement) or reference an existing one; wire it into the CR's `acceptance_criteria[]`.
- **Mature requirements before CR approval.** After filing or linking requirements, verify each requirement has at least one `acceptance_criteria` item. If the evidence in the current session supports it, fill missing requirement acceptance criteria before attempting approval. Then approve a requirement only when it has at least one acceptance criterion and the evidence supports approval; otherwise it leaves the requirement below approved with the blocking reason recorded in that requirement's `details`.
- **Respect the 1-hop invariant for CR approval.** Before setting a `change_request` to `approved`, verify its `acceptance_criteria[]` is non-empty and every ref points at an approved requirement. Then approve a change_request only when every acceptance_criteria ref points at an approved requirement. If any referenced requirement is missing, unapproved, or has no acceptance criteria, stop short and record the blocker in the CR `details`.
- **Hard refusal floor.** You must never approve a requirement without acceptance criteria, and never approve a change_request without at least one approved requirement ref.
- **Wire the related links — concretely.** A concluded CR is usually under-linked. Set every link that genuinely exists (don't invent any):
  - **Requirement(s)** → the CR's `acceptance_criteria[]` (the bullet above). This is the load-bearing link; never skip it.
  - **Governing `design_decision`** → link it *both ways*: the CR's `related: [design_decision-N]` **and** the requirement's `related_decisions: [design_decision-N]`. A CR that implements a decision but doesn't point at it is a miss.
  - **See-also `related[]`** on the CR → sibling issues/CRs, the originating `exploration`, a superseded record. Soft, non-directional — only real ones.
  - **CR → epic link, concretely.** A `change_request` has exactly ONE `parent` (an epic *or* an issue) and **no** separate `epic` field, so the path to the epic depends on whether the CR has a parent issue:
    - CR **with** a parent issue → set the CR's `parent: <issue>`, and put the epic on the *issue* (`issue.epic: <epic>`). The CR rolls up **transitively** (CR → issue → epic). Do **not** try to also set the epic as the CR's parent — `parent` is singular, and pointing it at the epic would drop the issue link.
    - CR **without** a parent issue → set the CR's `parent: <epic>` directly.
    - Never add the CR to the epic's `related[]`; the link is always the child pointing up (CR `parent`, `issue.epic`), which is what renders as the epic's **Inbound references**. `iteration` (the delivery lane) is a separate axis — set it via the CR's/issue's `iteration` field, not the epic.
    - **Verify the rollup:** `list_inbound_refs(<epic>)` must surface the parent issue (or the parentless CR), and `list_inbound_refs(<issue>)` must surface the CR. A missing hop = a broken rollup, even if each record *looks* assigned.
  - Quick self-check before moving on: does this CR point at (a) its requirement, (b) any design_decision that motivated it, (c) its parent issue/epic? If one exists and isn't linked, link it.
- **Verify content consistency along `cr → (issue) → req → ac` — not just that the links resolve.** A present link is not a consistent one. Read the actual text of every record in the chain and confirm they describe the *same* change without contradiction. The `(issue)` hop is optional — start at the CR when there is no parent issue.
  - **issue → cr:** the CR's `motivation`/scope addresses the issue's stated problem. Flag scope drift (the CR does materially more or less than the issue asks) and factual contradiction (e.g. the issue cites file X, the CR only touches Y).
  - **cr → req:** every requirement in `acceptance_criteria[]` actually covers part of the CR's scope, and the CR claims no scope that *no* referenced requirement covers. A requirement whose `statement` is broader or narrower than the CR misrepresents it — tighten one side. An rca-only requirement under an all-DTO CR (or vice-versa) is exactly this class of drift.
  - **req → ac:** each acceptance criterion is a concrete, checkable test *of that requirement's `statement`*. Flag ACs that test something the statement doesn't claim, and statement clauses left with no AC. CR-specific counts/thresholds belong in the CR, not the requirement — verify they don't contradict across records.
  - **cross-record terminology:** the same field names, enum values, type slugs, and identifiers are used consistently across issue, CR, requirement, and AC. A term renamed in one record but not the others is a drift bug.
  - **disposition:** auto-fix trivially-safe drift (typos, a stale ref, a wrong slug) in-session. For substantive drift (scope mismatch, an AC that doesn't test its requirement, a direct contradiction) do **not** approve — reconcile it now if the evidence is in front of you, otherwise record the specific inconsistency in the CR `details` as the named blocker.
- **Review, plan, approve** — advance the record as much as you can. Set `executor: agent` + `status: approved` only when the CR is fully specified and the 1-hop invariant above is satisfied; otherwise leave it below `approved` and name the blocker.
- **A dependency is not a reason not to approve.**

gold conventions: every `create_*` needs explicit `created_by`/`last_edited_by`/`status`; `update_*` is a full-payload replace — use the `fields:` sparse param for partial edits; link to an epic via the child's `epic`/`parent`, not the epic's `related[]`.
