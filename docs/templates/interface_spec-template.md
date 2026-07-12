# Interface Specification: {title}

The `interface_spec` record is the product's **interface overview and control document** — ONE record per product. It is deliberately NOT an API reference: wherever a machine-maintained spec exists (tool snapshot, generated OpenAPI/AsyncAPI, generated wire types, drift-gated docs), this record **points at it as the authoritative contract** and states the guarantees, ownership, and evolution policy around it. Hand-written contract detail is reserved for surfaces that have no generator.

It is a *control* document in the ICD sense: it records who owns each interface, who consumes it, and under what compatibility promise — the agreement between the two sides, not just the shape of the wire.

**Fields to set:**

- `title`: "{Product} Interface Specification" *(mandatory)*
- `summary`: "{N surfaces, exposed/consumed split}; {status}" — update on status change.
- `status`: draft | in_review | approved | closed *(mandatory)*
- `related`: design_decision refs governing interface policy; the architecture_description root; soft see-also links.
- `details`: body per the skeleton below.

> **Prime directive — never hand-duplicate a generated spec.** The single-source-of-truth rule: any fact maintained in two places drifts. If a surface has a generator, this record links it and owns only the map, the policy, and the prose the generator cannot produce (concepts, auth flow, error semantics, limits, deprecation state). If you are writing an operation table for a surface that has a generator, stop and link instead.
>
> **What the code cannot tell you.** The generated reference gives shapes; this document gives the layer above it — product overview, getting-started orientation, authentication/authorization model, status/error semantics, rate limits/thresholds, and the compatibility contract. Those are the sections a human overview owes.

---

## Skeleton (`details`)

### 1. Overview & audiences

What interfaces this product exposes and consumes, in one paragraph, and who the consumers are (agent clients, human operators, sibling products, external services). Name the audience class per surface — an internal-only channel and a published contract earn different promises, and the reader needs to know which they are looking at.

### 2. Surface inventory *(the core — the interface register)*

Every interface the product exposes or consumes, one row each. This table is also the attack-surface enumeration input for the threat_model.

| Surface | Direction | Provider → Consumers | Audience | Auth | Machine spec (authoritative) | Lifecycle | Compat regime | Owner |
|---|---|---|---|---|---|---|---|---|
| {e.g. MCP tool surface} | exposed | {service} → {consumer classes} | {internal / partner / public} | {scheme} | {pointer + how it is drift-gated, e.g. "tool snapshot, gated by X"} | {stable / preview / deprecated} | {additive-only / versioned} | {who agrees to changes} |
| {e.g. admin REST} | exposed | … | … | … | … | … | … | … |
| {e.g. upstream Postgres} | consumed | {external} → {service} | — | … | {their spec / pinned version} | {pinned vX} | {what we tolerate} | {who tracks upstream} |

Column intent (borrowed from Backstage catalog + ICD register practice):

- **Machine spec** — the authoritative contract pointer, typed by format; "none (hand-owned in §4)" is a valid explicit answer, never blank.
- **Lifecycle** — stable / preview / deprecated. A deprecated row must carry its sunset in §5.
- **Compat regime** — which evolution promise this surface operates under (see §3). Per surface, not global — that is the whole point of the column.
- **Owner** — who must agree before this interface changes. "Unknown consumers" is a finding; an interface with no named owner is a finding.

### 3. Compatibility & evolution policy

The promise, stated once, that the *Compat regime* column references. Cover:

- **Backward-compatibility stance** — the default (recommended: additive-only; breaking changes forbidden on productive surfaces). The concrete additive rules, e.g.: inputs may gain only *optional* fields and never lose fields or add required ones; outputs may gain fields but must not extend enum ranges consumers may not handle; consumers must tolerate unknown fields.
- **Versioning mechanism** — how a surface versions when it must break (media-type / date-stamped `api-version` / path — pick and state it), and which surfaces are frozen vs. still evolving.
- **Consumed-side policy** — for each consumed dependency: the pinned version, the behaviors tolerated, and the upstream-change-handling procedure. What the product depends on breaks the product.

### 4. Non-generated contracts

For each surface whose *Machine spec* column is "none" — the hand-owned contract, and only those. Per surface: operations/messages, shapes, invariants (idempotency, ordering, atomicity), and — in the ICD tradition that modern API docs forget — **timing** (timeouts, rates), **state/atomicity** aspects, and **failure scenarios**. Keep to what a consumer must know to integrate correctly.

#### {surface name}

{contract prose / tables}

### 5. Cross-surface conventions

Rules that hold across all surfaces, so they are stated once and not per row:

- **Error contract** — the structured error envelope every surface returns (recommend a documented shape: stable machine-readable `code` + human `message` + optional `target`/`details`; error codes are part of the contract). Reference the design_decision that fixed it.
- **Auth conventions** — token schemes, scopes, identity model.
- **Limits** — pagination, rate limits, size ceilings, and what a consumer sees when it hits one.
- **Deprecation procedure** — the governed steps: reflect deprecation in the spec, signal it on the wire (deprecation/sunset markers), notify/agree with named consumers, monitor usage, then remove. Deprecation is a process this document owns, not just a lifecycle label.

### 6. Linked decisions

design_decision refs for interface-policy choices (via `related`), a half-line each.

---

## Quality criteria (review checklist)

- **Inventory completeness beats row depth** — a missing surface is worse than a thin row; the threat model enumerates attack surface from §2, so an unlisted interface is an unanalyzed one.
- **Every row states where the truth lives** — Machine spec pointer present, or an explicit "none (§4)". No blanks.
- **Every exposed surface has a named owner and known consumers** — unknowns are findings, not omissions.
- **Compat regime is per surface** — and every surface has one; an interface with no stated evolution promise is a broken contract waiting to happen.
- **No hand-duplication** — nothing in §4 restates what a generator in §2 already produces.
- **Consumed interfaces are contracts too** — each has a pinned version and an upstream-change policy.
- **The layer-above test** — a newcomer can authenticate, make a first call, interpret an error, and learn the limits from this document without opening the generated reference.
