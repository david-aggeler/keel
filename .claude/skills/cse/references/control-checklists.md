# Control Checklists — Step 3 Scaffolding

Sera walks each of these four checklists in Step 3, producing a verdict per item: ✅ Present · ⚠️ Weak · ❌ Missing · 🔜 Deferred-to-Growth · ❓ Unclear. Each `❌`/`⚠️` row lists what would close it; each `❓` becomes an Open Item.

The lists are deliberately concrete and product-shaped. They are *not* a CIS Benchmark or a NIST control catalog — they're the controls that map to the keel MVP cybersecurity baseline plus the next ring of pragmatic security hygiene.

---

## A. Authentication & Authorization

| # | Item | What "Present" looks like |
|---|------|--------------------------|
| A1 | Every OpenAPI operation has a `security` requirement (or is explicitly anonymous and justified) | Spectral / linter enforces it; anonymous endpoints have a list comment |
| A2 | Default-deny middleware applies before handler code | A new handler without auth annotation gets rejected by middleware, not by handler check |
| A3 | Tenant claim is cross-checked on every multi-tenant operation | `body.org_id == jwt.org_id` (or scoped query that filters by the JWT) — no path where the body wins |
| A4 | Authorization decision is centralized | Single policy module / OPA / casbin / explicit RBAC matrix — not scattered `if user.role == ...` |
| A5 | Token revocation path exists and is documented | Step-by-step "kill this user's token in N minutes" — actually achievable |
| A6 | Token TTL is bounded | TTL is documented; rotation cadence is documented; no infinite tokens |
| A7 | Service-to-service auth uses something stronger than network position | mTLS, signed JWTs with short TTL, or cert pinning — not "it's on the internal network so it's fine" |
| A8 | Node/agent bootstrap authentication is per-node | Each node has its own credential / cert, not a shared image-baked secret |
| A9 | Admin / dangerous actions require a separate credential or step-up | Either separate role with separate token, or interactive re-auth at the moment of action |
| A10 | (Growth) MFA-claim enforcement at IdP | `amr` claim trusted; policy requires MFA for privileged ops — Deferred-to-Growth under MVP-baseline mode |

---

## B. API Contract Security

| # | Item | What "Present" looks like |
|---|------|--------------------------|
| B1 | All input objects have `additionalProperties: false` | Spec-level — silently-accepted unknown fields are a tampering vector |
| B2 | Strings have `maxLength`; arrays have `maxItems` | Bounded everywhere a user can supply input |
| B3 | Mutating endpoints (POST/PUT/DELETE) declare an idempotency mechanism | `Idempotency-Key` header documented and enforced |
| B4 | Every operation declares 401 / 403 / 429 / 5xx response shapes | And those shapes don't leak internal structure |
| B5 | Error responses use a stable, minimal shape | RFC 9457 problem+json or equivalent; no stack traces, no internal IDs in messages |
| B6 | 403 vs 404 discipline holds | Existence-leakage prevention — preferred 404 for "not found OR not yours" |
| B7 | AsyncAPI: every channel documents authentication and authorization | Not just bindings — actual security requirements |
| B8 | AsyncAPI: events on multi-tenant channels carry tenant ID | Consumers can verify producer scope |
| B9 | AsyncAPI: replay / duplicate / out-of-order is documented | "What does the consumer do if it sees this event twice?" has an answer |
| B10 | Pagination has a server-enforced max page size | Not "client may pass any number" |

---

## C. Audit Logging

| # | Item | What "Present" looks like |
|---|------|--------------------------|
| C1 | All CUD operations on customer-visible resources are logged | An action without a log entry is a known exception, not the default |
| C2 | Reads on sensitive resources are logged | Tokens, secrets metadata, audit log itself, role assignments |
| C3 | Audit record schema is stable and structured | Subject, object/type, verb, timestamp, request-id — all five always present |
| C4 | User-supplied strings are escaped before log emission | Test with `\n`, `{`, ANSI codes — output remains valid JSON |
| C5 | Token values, passwords, full request bodies are not logged | Verifiable via grep on the log stream |
| C6 | Audit-log writes are atomic with the action they audit | If the action committed, the log entry is durable |
| C7 | Audit log integrity is protected | Normal users can't modify or delete; preferably append-only storage |
| C8 | A retention statement exists | "We keep N days" with a concrete N — even minimal counts |
| C9 | Async-action chains preserve identity | If an API call defers work to a worker, the worker's log entry retains the original subject |
| C10 | Request-id / correlation-id flows end-to-end | A single ID ties HTTP entry, internal calls, and worker actions together |

---

## D. Secrets & Transport

| # | Item | What "Present" looks like |
|---|------|--------------------------|
| D1 | TLS-only on all external endpoints | HTTP plaintext returns connection error or 301 to https |
| D2 | TLS minimum version is pinned | Documented; no SSLv3/TLS1.0/TLS1.1 |
| D3 | Service-to-service uses mTLS or signed transport where it crosses a trust boundary | "Internal network" is not a control |
| D4 | Secrets live in a secret store | Vault / KMS / SOPS-encrypted — not env vars committed to git, not yaml-with-passwords |
| D5 | Secret-store access is itself authenticated | Bootstrap path is documented (chicken-and-egg has an answer) |
| D6 | Secret rotation has a documented path | Even if manual; not "we never rotate" |
| D7 | DB-stored secrets are encrypted at rest | And the encryption keys live in the secret store |
| D8 | Node/agent trust bootstrap is non-shared | Per-node credential (overlaps with A8 — score once, cite both) |
| D9 | Secrets are not in logs, error messages, or stack traces | Test path: deliberately include a secret-shaped value in an error and verify it's redacted |
| D10 | Credential exposure path on compromise is documented | "If env file leaks, here's our recovery procedure" |

---

## How to use these checklists

- Each row gets exactly one verdict (✅ ⚠️ ❌ 🔜 ❓).
- For `❌` and `⚠️`: write a one-sentence "what would close it" — concrete, testable.
- For `🔜 Deferred-to-Growth`: capture the row in the Deferred-to-Growth section with its likely framework driver.
- For `❓ Unclear`: write the question into Open Items rather than guessing.

If a row is genuinely n/a for the current scope (e.g. no AsyncAPI surface in scope), say so and move on. Don't pad.
