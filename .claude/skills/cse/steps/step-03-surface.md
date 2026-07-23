<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 2: Attack Surface Mapping

## MANDATORY EXECUTION RULES

- 🤖 Generate the surface map **autonomously** — don't ask the user component-by-component
- 🌐 Map every externally reachable entry point: HTTP endpoints, AMQP channels, CLI/UI, device- or agent-facing API
- 🔒 Mark every **trust boundary** — wherever a request crosses from a less-trusted zone to a more-trusted one
- 🗺️ Capture **identities and roles** — who can call what, with what credential
- 📦 Capture **external dependencies** — every system keel trusts (external infrastructure backend, IdP, AMQP broker, DB, secret store, DNS resolver)
- 🛑 Do NOT enumerate threats yet — that's Step 4. Right now you're just naming the surface.

## YOUR TASK

Produce a complete attack-surface map: components, trust boundaries, data flows, identities, and external dependencies. This is the substrate Steps 3 and 4 work against — every threat names something on this map, so coverage here directly bounds coverage there.

---

## WHAT TO MAP

### A. Component inventory

For every component in the architecture, record:

| Field | Notes |
|---|---|
| Name | Use the architecture doc's name verbatim |
| Type | service / database / queue / external system / node-agent / CLI |
| Trust zone | where it sits (public, DMZ, internal, node-agent, external-third-party) |
| Reachable from | which other components or external actors can call it |
| Exposes | HTTP endpoints / AMQP channels / CLI / file system / none |

### B. Trust boundaries

A trust boundary is a line where you'd want a control (authn, authz, validation, encryption). Identify every place a request crosses from less-trusted to more-trusted:
- Internet → public HTTP API
- Public HTTP API → internal services
- Tenant A → Tenant B (multi-tenant boundary)
- keel control plane → device- or agent-facing API
- keel control plane → external compute/infrastructure backend adapter
- Operator → audit log (read boundary)
- Application → secret store
- Application → database

### C. Data flows that matter

Don't enumerate every flow — pick the **security-relevant** ones:
- Where credentials, tokens, or secrets are read or written
- Where audit-relevant actions happen (CRUD on customer data, role assignments)
- Where untrusted input crosses a trust boundary
- Where data leaves the system (egress, logs, third-party APIs)

For each, record: source → sink, what data, what trust boundaries crossed.

### D. Identities and credentials

| Identity | What it is | How it authenticates | What it can do |
|---|---|---|---|
| End user (human) | tenant operator | IdP → token | full or scoped CRUD per role |
| Service account | machine principal | mTLS / API key / OIDC client-credentials | scoped tasks |
| Node/agent | an out-of-process config agent | shared secret / cert pinning | read its own config |
| Internal service-to-service | between keel components | mTLS / network-layer trust | internal API |

Be honest if anything in this table is not yet decided — mark it `TBD` and surface it as an Open Item rather than guessing.

### E. External dependencies (the supply chain you trust)

Each row should answer: "If this system is compromised or misbehaves, what can it do to keel?"

| Dependency | Trust assumption | Failure-of-trust impact |
|---|---|---|
| External compute/infrastructure backend | keel controls the backend's management API | full compromise of all workloads in scope |
| IdP | Issues tokens we honour | impersonate any user / arbitrary role |
| AMQP broker | Delivers events with integrity | inject events, replay, observe |
| Database | Stores authoritative state | full data breach + tamper |
| Secret store | Holds credentials | full secret breach |
| DNS resolver | Names resolve correctly | redirect keel traffic to attacker |

---

## OUTPUT

Populate the **Attack Surface** section of `security-review.md` with the five subsections above. Use tables — they're skimmable and stable across edits.

After populating, write to frontmatter: `stepsCompleted: [1, 2]`.

## REPORT AND HAND OFF

```
Attack surface mapped.

Components:        N
Trust boundaries:  N
Identities:        N
External deps:     N
Open items (TBD):  [list any unresolved identity/auth questions]

Next: I'll walk the control review pass —
authentication, API contract security, audit logging,
and secrets/TLS — and produce a control-gap list.

[C] Continue to control review
```

Wait for `[C]`.

## SUCCESS METRICS

✅ Every architecture component appears on the surface map
✅ Every external dependency named with its trust-failure impact
✅ Trust boundaries listed (not implied)
✅ TBD items surfaced as Open Items, not silently filled in
✅ `stepsCompleted: [1, 2]` in frontmatter
✅ Wait for `[C]`

## FAILURE MODES

❌ Listing only the components keel owns and skipping the external deps
❌ Conflating "service-to-service" links with trust boundaries (or vice versa)
❌ Filling in `TBD` identity decisions with plausible-sounding guesses
❌ Generating threats in this step

## NEXT STEP

After `[C]`: load `./step-04-controls.md`
