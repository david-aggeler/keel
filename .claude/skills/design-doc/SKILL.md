---
name: design-doc
description: "Author or extend one of a product's design-documentation trees in the SoR — the architecture (architecture_description) tree for internal structure, or the interface spec (interface_spec) tree for boundary contracts. Both are a root + ordered chapter tree, one authoring engine. Basis: existing SoR records and the codebase. Pick the document, then two modes — Create (no root) and Extend (root exists) — with two gates: scope and review. Use when the user says: '/design-doc', '/architecture', 'create architecture', 'architecture document', 'technical architecture', 'design the architecture', 'extend the architecture', '/interface-spec', 'create interface spec', 'interface specification', 'interface control document', 'spec the interfaces', 'extend the interface spec'"
allowed-tools: mcp__gold__get_template_for, mcp__gold__list_architecture_description, mcp__gold__get_architecture_description, mcp__gold__search_architecture_description, mcp__gold__create_architecture_description, mcp__gold__update_architecture_description, mcp__gold__list_interface_spec, mcp__gold__get_interface_spec, mcp__gold__search_interface_spec, mcp__gold__create_interface_spec, mcp__gold__update_interface_spec, mcp__gold__list_design_decision, mcp__gold__get_design_decision, mcp__gold__search_design_decision, mcp__gold__create_design_decision, mcp__gold__update_design_decision, mcp__gold__list_glossary_term, mcp__gold__search_glossary_term, mcp__gold__create_glossary_term, mcp__gold__update_glossary_term, mcp__gold__list_vision, mcp__gold__get_vision, mcp__gold__list_requirement, mcp__gold__get_requirement, mcp__gold__search_requirement, mcp__gold__list_user_need, mcp__gold__get_user_need, mcp__gold__search_user_need, mcp__gold__list_epic, mcp__gold__get_epic, mcp__gold__list_environment
targets_templates:
  - architecture_description-template
  - interface_spec-template
  - design_decision-template
x-openbrain-source: design-doc/v1
x-openbrain-content-source-hash: sha256:9025830b3d7ba435003d044005aa0c7a09c86f84abafaacec2a9dadac149e8fe
x-openbrain-content-hash: sha256:ef609a021bceb98a5456ded22062fc114fd59b3fad85438566ad2f0f020f473e
---

# Design Doc

Author or extend one of the product's design-documentation trees in gold — the canonical output. Two document types, one authoring engine:

- **architecture** — an `architecture_description` root + chapter tree: the product's *internal* structure (context, components, runtime, deployment, cross-cutting concepts).
- **interface spec** — an `interface_spec` root + chapter tree: the product's *boundary* contracts (the surface register, compatibility policy, per-surface hand-owned contracts, cross-surface conventions).

Both are one ROOT (overview + index, carrying `chapters[]`) plus ordered CHAPTER records of the same type. You facilitate; the user decides. Product **keel**, primary language **Go** — settled, don't re-open.

## Document selection (first, always)

Decide which tree this pass authors: **architecture** (`architecture_description`) or **interface spec** (`interface_spec`). Take it from the trigger — `/architecture` → architecture, `/interface-spec` → interface spec — or ask in one line when neither the trigger nor the request settles it. Call the chosen type `<type>` below; every step is parameterized by it. One pass authors one tree; if the user wants both, do them in sequence, architecture first (its §5 components + §3 context are inputs to the interface register).

## Basis

What already exists: **SoR records** and **the codebase** (real boundaries, interfaces, deployment). If the SoR or the code can answer a question, read/explore instead of asking. Ask only what neither holds: intent, priorities, undecided trade-offs.

- **architecture** — inputs are `vision`, `requirement`, `user_need`, `epic`, `design_decision`, `interface_spec`, `environment`, `glossary_term`, the existing tree; and the code's boundaries, entry points, and deployment artifacts.
- **interface spec** — inputs are the `architecture_description` tree (its §3 context neighbors and §5 components ARE the exposed/consumed surfaces), every **generated** contract that already exists (tool snapshot, generated OpenAPI/AsyncAPI, wire types, drift-gated docs — the api-contract/Verity skill owns those), the code's actual endpoints/handlers/clients, plus `design_decision`, `environment`, `glossary_term`. The two trees cross-reference: a surface in the register maps to an architecture §3 neighbor or §5 component.

## Ground rules

1. **Template is the skeleton.** `get_template_for dto_type=<type>` first; its ROOT and CHAPTER skeletons and quality criteria win over this file.
2. **Records over prose.** Decisions → `design_decision`; terms → `glossary_term`. Link, never restate.
3. **Never hand-duplicate a generated spec** *(interface spec — the prime directive).* Where a machine-maintained spec exists, the tree points at it as authoritative and owns only the layer above (register, policy, concepts, auth, errors, limits). Writing an operation table for a surface that has a generator is the anti-goal. (For architecture: the equivalent is "no code echoes" — link signatures/schemas/env vars, never restate them.)
4. **Honest scaffolds.** Uninvestigated sections are marked _(scaffold)_ with what's missing — never padded or silently omitted.
5. **Receipts.** Each `create_*` needs a fresh same-type `search_*` receipt + the `template_receipt`. Receipts are consumed per create.
6. **HTML body.** Author the `details` of both trees as HTML — set `details_format=html` on every `create_*` and `update_*` (root and chapters alike). The chaptered design docs render richer as HTML; markdown is the fallback, not the default here. Keep the template's sections and headings; only the body format changes. Note: inside an HTML body a fenced ` ```mermaid ` block will not render — embed diagrams as inline SVG or as attachments.
7. **Drawings.** Diagram expectations are the template's — follow its "show the shape" rule and matching quality criterion.
8. **No time estimates.**

## Mode selection

`list_<type> product=<slug>`: no root → **Create**; root exists → **Extend**. One root per product per type, ever.

## Mode A — Create

1. **Orient.** Template (keep receipt). Load the basis for `<type>` summaries-first (`list_*` with `include_summary=true`; paginate past 100); read the richest-signal records fully (`design_decision` for architecture; the `architecture_description` tree + any generated specs for interface spec). Glossary once; use its vocabulary. Explore the code. If the basis is empty of the load-bearing inputs (architecture: vision/requirements/user needs; interface spec: no architecture tree and no discoverable surfaces) → stop, name the gap, offer to proceed from code + conversation; don't invent inputs.
2. **Scope gate (stop).** One message: scope of this pass, chapter plan (titles + one-liners, typed per template — component/flow/concept chapters for architecture; surface/concept chapters for interface spec), anchoring records and code areas. **Create nothing until agreed.**
3. **Author.** Root (`draft`), then chapters; `chapters[]` in reading order, the root chapter-index section in sync. Cite the carrying record for every claim (`related[]` + text). Missing rationale → `design_decision` first. New terms → `glossary_term`. **Must cover** (workflow contract):
   - **architecture** — **Testing strategy** (what's tested where, mock-vs-real, gated lanes) and **Deployment & merge gate** (topology, dev stack, CI gate), root or owning chapter.
   - **interface spec** — the **surface register is complete** (every exposed AND consumed surface, one row each) and every row states where its truth lives (machine-spec pointer or a chapter, never blank); **cross-surface conventions** (auth, error contract, limits, deprecation) present; every exposed surface has a named owner and known consumers.

   Absent the type's must-cover → not done.
4. **Review gate (stop).** Record IDs, tree, self-check vs template criteria; name every open scaffold. Status advances only on user say-so.

## Mode B — Extend

1. **Orient.** Load root + chapters fully — a decided baseline: extend, don't restart. Diff against SoR and code: for architecture — unreflected decisions, missing §5 components, drifted chapters, stale index; for interface spec — surfaces present in code/architecture but missing from the register, rows whose machine-spec pointer rotted, a generated surface that gained a hand-written duplicate, missing owners, stale index.
   - **Float conflicts.** SoR vs code vs tree disagreements: don't pick a winner silently — present evidence + recommended resolution. Blockers immediately; the rest at the scope gate.
2. **Scope gate (stop).** The delta in one message: chapters/rows to add/revise/split, root updates, the observed drift motivating each; separate drift repair from new design. **Change nothing until agreed.**
3. **Author.** New chapters per Mode A §3. Revisions via `update_<type>`; keep the tree's closure invariants current (architecture: §5 ↔ *Affected components*; interface spec: register rows ↔ chapters' *Surface(s) covered*) and the chapter index in sync. The type's must-cover contract applies tree-wide.
4. **Review gate (stop).** Per Mode A §4, plus before/after of the tree.

## Anti-patterns (each broke a real run)

- ✗ Second root because the first looks incomplete — grow or split chapters instead.
- ✗ Asking what the SoR or code already answers.
- ✗ Restating decision rationale, requirement text, or glossary definitions inline.
- ✗ Hand-writing an operation/message table for a surface that has a generator (interface spec).
- ✗ A register surface with no named owner or unknown consumers left as an omission rather than a finding (interface spec).
- ✗ Claiming completeness a scaffold doesn't have.
- ✗ Canonical output anywhere but the gold tree.
