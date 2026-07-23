<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# STRIDE — Reference for Threat Enumeration

STRIDE is a six-category checklist for asking "how could this be attacked?". It's not a complete model on its own — it's a *coverage prompt* that keeps you from missing a category. Walk every component on the surface map through every category. Where a category genuinely doesn't apply, write `none — [reason]`; don't silently skip.

## The Six Categories

### S — Spoofing identity

The attacker presents themselves as someone (or some thing) they're not.

**Common patterns:**
- Stolen or replayed token usable beyond its intended scope
- Missing tenant claim cross-check (token says tenant A; request body says tenant B)
- Service-to-service trust based only on network position (no mTLS or signed transport)
- Node/agent bootstrapping with a shared secret that any compromised distributed image leaks
- AsyncAPI events trusted on channel name alone, with no producer-identity assertion

**Worked examples (illustrative):**
- Any operation that takes `org_id` / `tenant_id` from the request body without checking against the JWT
- Any internal API reachable from the same network as the public API
- Any agent-to-control-plane call that authenticates with a static credential

### T — Tampering with data

The attacker modifies data — in transit, at rest, in memory, or in configuration that a component trusts.

**Common patterns:**
- HTTP traffic without TLS or with TLS-not-required as an option
- DB rows directly editable by an unprivileged actor (mass-assignment via JSON body)
- Audit log records that the user who took the action can edit or delete
- Config files read at startup that unprivileged code can rewrite
- AMQP messages without producer signature; consumer trusts payload as-is
- Idempotency-key reuse: replay a captured request and double-execute

### R — Repudiation

An actor (legitimate or hostile) takes an action and later credibly denies it because the audit log doesn't capture enough to prove otherwise.

**Common patterns:**
- Reads on sensitive resources (PII, secrets metadata) not logged
- Audit log lacks request-id / correlation-id, so events can't be tied to a session
- Asynchronous action: API call returns 202; the actual write happens later under a different identity (e.g. system worker), and only the worker is logged
- Log truncation: oversized field clipped, attacker uses big input to push useful evidence out of the record
- Log-field collision: user-supplied field name overlaps a structured field, masking the real value

### I — Information disclosure

The attacker reads data they shouldn't.

**Common patterns:**
- 403 vs 404 differential exposes existence of resources outside the caller's scope (tenant enumeration)
- Verbose error responses with stack traces, internal path names, ORM SQL fragments
- Timing-based existence leakage (lookup hits index → fast; miss → slow)
- Logs contain tokens, passwords, full request bodies with secrets
- Process listing or memory dump exposes secrets read from env vars
- Side channels: cache hit/miss timing, response size, status-code differential
- AsyncAPI: events on a multi-tenant channel without per-event tenant scoping

### D — Denial of Service

The attacker exhausts the component's resources or crashes it.

**Common patterns:**
- Unbounded inputs: arrays / strings without `maxItems` / `maxLength`
- Unauthenticated expensive operations (anything that hits the DB, an external infrastructure backend, or external APIs without a rate limit and an auth check first)
- Pagination without page-size caps
- Recursive structures in JSON (zip-bomb-style nested objects)
- Single-tenant exhaustion of a shared resource (one tenant DoSing the whole control plane)
- Slowloris-style: long-lived idle connections

**Cross-reference Vera (DFMEA):** if a DoS threat overlaps with a reliability availability finding, cite the DFMEA item ID and don't re-litigate. The framing is different — *adversarial* vs *random* — but the mitigation often overlaps.

### E — Elevation of Privilege

An attacker who already has limited access expands it.

**Common patterns:**
- Cross-tenant: tenant A reads/writes/deletes tenant B's resources
- Role-up: read-only role can take a write action through a side door
- Indirect privileged access: user who can edit a workflow that runs as a privileged service account
- Path traversal in any field that becomes a file path
- SSRF via URL inputs (an external infrastructure-backend adapter, image-import flows, webhook callbacks)
- Admin endpoints accessible without separate authn step (one token grants everything)
- Workload escape: code in an isolated workload influences control-plane state (reverse trust flow)

## Walking a Component

For each component on the surface map, ask each of the six questions in order. Write what you find — including `none — [reason]`. Look at the **trust boundaries** that touch this component: most threats live at a boundary, not inside the component.

A high-quality threat has all three:
- **Attacker class** (anonymous internet, authed tenant user, compromised node/agent, compromised dependency, etc.)
- **Asset** (specific data, specific control, specific availability target)
- **Path** (concrete request, concrete state, concrete sequence)

If you can't fill all three, the threat is too vague — refine it before scoring it.
