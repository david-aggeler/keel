---
name: design-doc
description: "Author or extend one of a product's design-documentation trees in the SoR — the architecture (architecture_description) tree for internal structure, or the interface spec (interface_spec) tree for boundary contracts. Both are an overview root + ordered chapter tree, one authoring engine. Basis: existing SoR records and the codebase. Pick the document, then two modes — Create (no root) and Extend (root exists) — with two gates: scope and review. Use when the user says: '/design-doc', '/architecture', 'create architecture', 'architecture document', 'technical architecture', 'design the architecture', 'extend the architecture', '/interface-spec', 'create interface spec', 'interface specification', 'interface control document', 'spec the interfaces', 'extend the interface spec'"
allowed-tools: mcp__gold__get_template_for, mcp__gold__list_architecture_description, mcp__gold__get_architecture_description, mcp__gold__search_architecture_description, mcp__gold__create_architecture_description, mcp__gold__update_architecture_description, mcp__gold__list_interface_spec, mcp__gold__get_interface_spec, mcp__gold__search_interface_spec, mcp__gold__create_interface_spec, mcp__gold__update_interface_spec, mcp__gold__list_design_decision, mcp__gold__get_design_decision, mcp__gold__search_design_decision, mcp__gold__create_design_decision, mcp__gold__update_design_decision, mcp__gold__list_glossary_term, mcp__gold__search_glossary_term, mcp__gold__create_glossary_term, mcp__gold__update_glossary_term, mcp__gold__list_vision, mcp__gold__get_vision, mcp__gold__list_requirement, mcp__gold__get_requirement, mcp__gold__search_requirement, mcp__gold__list_user_need, mcp__gold__get_user_need, mcp__gold__search_user_need, mcp__gold__list_epic, mcp__gold__get_epic, mcp__gold__list_environment
targets_templates:
  - architecture_description-template
  - interface_spec-template
  - design_decision-template
x-openbrain-source: design-doc/v2
x-openbrain-content-source-hash: sha256:618cf6d1705e9ed0aab56f74a199dbc8211b50b6a7f496adebaeadaf0df144b2
x-openbrain-content-hash: sha256:a950f4a37a257daec96c39fea5c7ab9fa857f454bcdb2fbc07df3c5011793fee
---

# Design Doc

Author or extend one of the product's design-documentation trees in gold — the canonical output. Two document types, one authoring engine:

- **architecture** — an `architecture_description` root + chapter tree: the product's *internal* structure (context, components, runtime, deployment, cross-cutting concepts).
- **interface spec** — an `interface_spec` root + chapter tree: the product's *boundary* contracts (the surface register, compatibility policy, per-surface hand-owned contracts, cross-surface conventions).

Both are one ROOT — **an overview chapter and nothing more** — plus ordered CHAPTER records of the same type. You facilitate; the user decides. Product **openbrain**, primary language **Go** — settled, don't re-open.

## Document selection (first, always)

Decide which tree this pass authors: **architecture** (`architecture_description`) or **interface spec** (`interface_spec`). Take it from the trigger — `/architecture` → architecture, `/interface-spec` → interface spec — or ask in one line when neither the trigger nor the request settles it. Call the chosen type `<type>` below; every step is parameterized by it. One pass authors one tree; if the user wants both, do them in sequence, architecture first (its component-inventory and context chapters are inputs to the interface register).

## Basis

What already exists: **SoR records** and **the codebase** (real boundaries, interfaces, deployment). If the SoR or the code can answer a question, read/explore instead of asking. Ask only what neither holds: intent, priorities, undecided trade-offs.

- **architecture** — inputs are `vision`, `requirement`, `user_need`, `epic`, `design_decision`, `interface_spec`, `environment`, `glossary_term`, the existing tree; and the code's boundaries, entry points, and deployment artifacts.
- **interface spec** — inputs are the `architecture_description` tree (its context neighbors and inventory components ARE the exposed/consumed surfaces), every **generated** contract that already exists (tool snapshot, generated OpenAPI/AsyncAPI, wire types, drift-gated docs — the api-contract/Verity skill owns those), the code's actual endpoints/handlers/clients, plus `design_decision`, `environment`, `glossary_term`. The two trees cross-reference: a surface in the register maps to an architecture context neighbor or inventory component.

## Ground rules

1. **Template is the skeleton.** `get_template_for dto_type=<type>` first; its ROOT and CHAPTER skeletons, required-chapter list, and quality criteria win over this file.
2. **The root is an overview chapter.** The first record carries only the overview material and the chapter index — architecture: purpose/quality goals, constraints, chapter index, product-wide decisions + glossary; interface spec: overview & audiences, chapter index, decisions + glossary. Everything with substance is a chapter, including the ones the template marks required (architecture: context & scope, solution strategy, component inventory, runtime scenarios, deployment, cross-cutting concepts, risks & debt; interface spec: surface register, compatibility & evolution policy, cross-surface conventions). Writing a component table, a deployment topology, or the surface register into the root is the anti-goal.
3. **Records over prose.** Decisions → `design_decision`; terms → `glossary_term`. Link, never restate.
4. **Never hand-duplicate a generated spec** *(interface spec — the prime directive).* Where a machine-maintained spec exists, the tree points at it as authoritative and owns only the layer above (register, policy, concepts, auth, errors, limits). Writing an operation table for a surface that has a generator is the anti-goal. (For architecture: the equivalent is "no code echoes" — link signatures/schemas/env vars, never restate them.)
5. **Honest scaffolds.** Uninvestigated sections are marked _(scaffold)_ with what's missing — never padded or silently omitted.
6. **Receipts.** Each `create_*` needs a fresh same-type `search_*` receipt + the `template_receipt`. Receipts are consumed per create.
7. **No time estimates.**

## Body rules — HTML, headings, diagrams

Author every body — root and chapters alike — as HTML: `details_format=html` on every `create_*` and `update_*`. The chaptered design docs render richer as HTML; markdown is the fallback, not the default here. Keep the template's sections; only the format changes.

- **Never repeat the title in the body.** The renderer already prints `title` above the body. No opening `<h1>{title}</h1>`, no bold restatement. The body opens with its first section heading.
- **Headings start at h1.** Top-level sections are `<h1>`, subsections `<h2>`, then `<h3>`. Do not start at `<h2>` or `<h3>` "to leave room" for the title — there is no title in the body.
- **No numbered headings, at any level.** Not the sections, not the sub-sections. Write `<h1>Constraints</h1>`, `<h2>Upstream pinning</h2>` — never `2. Constraints`, `2.3 Upstream pinning`, `Chapter 4 — …`. Order lives only in the root's `chapters[]` and its chapter index; cross-reference a sibling by topic, never by number.
- **Diagram as much as feasible, as inline SVG.** A fenced ` ```mermaid ` block does NOT render inside an HTML body. Draw with inline `<svg>` (very large or reused figures as attachments). Every section describing structure, sequence, topology, ownership, state, or lifecycle gets a figure; prose alone there is a finding, not a style choice.
- **Colored and annotated.** Fix one palette per tree, one hue per axis (component kind / direction / lifecycle / audience), the same concept the same color in every figure, with an in-figure legend. Label every box, label every edge with what crosses it, and add callouts on what a reader would misread — the invariant, the failure point, the trust boundary. Color as decoration is worse than monochrome.
- **Never use `style=`.** The renderer strips `style=` attributes, `<style>` blocks, and `class=`, and the figure comes out black. Use presentation attributes only: `fill`, `fill-opacity`, `stroke`, `stroke-width`, `stroke-dasharray`, `font-family`, `font-size`, `font-weight`, `text-anchor`, `opacity`, `marker-end`. Use `viewBox` + `width="100%"` (never a fixed pixel width), `font-size` ≥ 12, mid-tone fills with dark text, and a `<title>` first child.

## Mode selection

`list_<type> product=<slug>`: no root → **Create**; root exists → **Extend**. One root per product per type, ever.

## Mode A — Create

1. **Orient.** Template (keep receipt). Load the basis for `<type>` summaries-first (`list_*` with `include_summary=true`; paginate past 100); read the richest-signal records fully (`design_decision` for architecture; the `architecture_description` tree + any generated specs for interface spec). Glossary once; use its vocabulary. Explore the code. If the basis is empty of the load-bearing inputs (architecture: vision/requirements/user needs; interface spec: no architecture tree and no discoverable surfaces) → stop, name the gap, offer to proceed from code + conversation; don't invent inputs.
2. **Scope gate (stop).** One message: scope of this pass, chapter plan (titles + one-liners, typed per template — survey/component/flow/concept for architecture; survey/surface/concept for interface spec), which of the template's required chapters this pass covers and which are deferred, anchoring records and code areas, and the figure plan (which chapters get which diagrams). **Create nothing until agreed.**
3. **Author.** Root (`draft`) as an overview chapter only, then chapters; `chapters[]` in reading order, the root chapter index in sync. Cite the carrying record for every claim (`related[]` + text). Missing rationale → `design_decision` first. New terms → `glossary_term`. **Must cover** (workflow contract):
   - **architecture** — the template's required chapters exist (context & scope, solution strategy, component inventory, runtime scenarios, deployment, cross-cutting concepts, risks & debt), plus **Testing strategy** (what's tested where, mock-vs-real, gated lanes) and **Deployment & merge gate** (topology, dev stack, CI gate) in their owning chapters.
   - **interface spec** — the template's required chapters exist, the **surface register is complete** (every exposed AND consumed surface, one row each) and every row states where its truth lives (machine-spec pointer or a chapter, never blank); **cross-surface conventions** (auth, error contract, limits, deprecation) present; every exposed surface has a named owner and known consumers.

   Absent the type's must-cover → not done.
4. **Review gate (stop).** Record IDs, tree, self-check vs template criteria; name every open scaffold and every section still carrying prose where a figure belongs. Status advances only on user say-so.

## Mode B — Extend

1. **Orient.** Load root + chapters fully — a decided baseline: extend, don't restart. Diff against SoR and code: for architecture — unreflected decisions, missing inventory components, drifted chapters, stale index; for interface spec — surfaces present in code/architecture but missing from the register, rows whose machine-spec pointer rotted, a generated surface that gained a hand-written duplicate, missing owners, stale index. For both — root sections that should have been chapters, bodies repeating their title, bodies starting below h1 or carrying numbered headings, figures using `style=`, and sections with a shape but no figure.
   - **Float conflicts.** SoR vs code vs tree disagreements: don't pick a winner silently — present evidence + recommended resolution. Blockers immediately; the rest at the scope gate.
2. **Scope gate (stop).** The delta in one message: chapters/rows to add/revise/split, root material to lift into chapters, root updates, the observed drift motivating each; separate drift repair from new design. **Change nothing until agreed.**
3. **Author.** New chapters per Mode A §3. Revisions via `update_<type>`; keep the tree's closure invariants current (architecture: inventory ↔ *Affected components*; interface spec: register rows ↔ chapters' *Surface(s) covered*) and the chapter index in sync. The type's must-cover contract applies tree-wide.
4. **Review gate (stop).** Per Mode A §4, plus before/after of the tree.

## Anti-patterns (each broke a real run)

- ✗ Second root because the first looks incomplete — grow or split chapters instead.
- ✗ A root that carries a component inventory, a deployment topology, a runtime sequence, a risk register, or the surface register — those are chapters.
- ✗ A body that opens by repeating its own title, or whose headings start at `<h2>`/`<h3>`.
- ✗ A numbered heading anywhere — top-level or sub.
- ✗ A `style=` attribute (or `<style>` / `class=`) in an SVG — it is stripped and the figure renders black.
- ✗ A structure or flow described in prose where a figure would carry it.
- ✗ An unlabeled edge, or a color that means nothing.
- ✗ Asking what the SoR or code already answers.
- ✗ Restating decision rationale, requirement text, or glossary definitions inline.
- ✗ Hand-writing an operation/message table for a surface that has a generator (interface spec).
- ✗ A register surface with no named owner or unknown consumers left as an omission rather than a finding (interface spec).
- ✗ Claiming completeness a scaffold doesn't have.
- ✗ Canonical output anywhere but the gold tree.
