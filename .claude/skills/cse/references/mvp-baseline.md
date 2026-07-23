<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# keel MVP Cybersecurity Baseline

The floor. Every item below must be `Met` before construction is considered ready. Any `Gap` is an MVP blocker.

These are deliberately tight, testable controls — not framework-sized programs. The bar is "not stupid." Formal compliance work (ISO 27001, NIS2, EU CRA, SOC 2) is captured in `Deferred to Growth` and is **not** part of the baseline.

## The 8 Items

### 1. TLS-only on all external endpoints

**What it means:** Every endpoint reachable from outside the orchestrator process refuses plaintext HTTP. mTLS or signed transport on service-to-service where required.

**Testable:** A request to `http://...` returns a connection error or a 301 to https. TLS minimum version pinned. Cert store is documented.

**Common gap:** "We'll add TLS at the load balancer" — fine, but does the orchestrator itself reject HTTP if the LB is misconfigured?

### 2. Default-deny middleware on all routes

**What it means:** The HTTP framework rejects unauthenticated requests by default, before reaching handler code. Anonymous endpoints are an explicit, audited list — not a missing decorator.

**Testable:** Add a new endpoint without an auth annotation; the test suite or middleware rejects it. Anonymous endpoints are enumerated in code review.

**Common gap:** "Every handler checks auth" — fragile pattern. One forgotten check = one open endpoint.

### 3. Existence-leakage prevention (403 vs 404 discipline)

**What it means:** A response can't be used to enumerate resources outside the caller's scope. Either: return 404 for "not found OR not yours" (preferred), or return 403 only after a constant-time existence check.

**Testable:** Probe an existent resource you don't own and a nonexistent ID — responses are indistinguishable in code, body, and timing.

**Common gap:** Response body differs ("Not found" vs "Forbidden") even when status code matches.

### 4. Basic audit log

**What it means:** Every CUD on customer-visible resources writes an audit record with at least: subject (who) → object/type (what) → CRUD verb → timestamp → request-id. Reads on sensitive resources also logged.

**Testable:** Take any user-driven write action; an audit record exists with all five fields. Schema is stable enough that a downstream tool could parse it.

**Common gap:** Logs go to stdout but aren't structured; or a half of the actions go through a worker queue and the worker doesn't log.

### 5. Structured JSON logging

**What it means:** All application logs are JSON, with stable keys. User-supplied strings are escaped (no log injection). Tokens, passwords, and full request bodies are not logged.

**Testable:** A log line is parseable JSON. A test feeds malicious input (`"\n", "{", ANSI codes`) — output remains a single valid JSON record. Grep the log stream for token-like strings — none.

**Common gap:** Mixing JSON and plain-text log lines, or stuffing the entire request into a single field.

### 6. Revocable tokens

**What it means:** A path exists to invalidate a stolen token before its TTL expires. Whether revocation list, short TTL + rotation, or session store — pick one and document it.

**Testable:** Document a step-by-step "this user's token is compromised, here's how we kill it within N minutes." Try it.

**Common gap:** Long-lived JWTs with no revocation mechanism. The "we'll just rotate the signing key" story doesn't hold up — that revokes everyone.

### 7. Secrets in a secret store

**What it means:** Every secret (DB credentials, signing keys, third-party API tokens, infrastructure-backend admin creds) lives in a secret store, not in env files committed to git, not in `*.yaml` config.

**Testable:** `git grep` for secret-shaped strings turns up nothing. `printenv` on the running orchestrator shows references (paths, tokens to fetch) but not the actual secret values where avoidable.

**Common gap:** Bootstrap chicken-and-egg: how does the orchestrator authenticate to the secret store? Answer that explicitly.

### 8. Device- or agent-facing API authenticated

**What it means:** An out-of-process agent authenticates back to the control plane with a credential that's not shared across nodes. Stolen credentials from one node don't impersonate another.

**Testable:** Steal the credential from node A, try to call as node B → rejected.

**Common gap:** A shared secret baked into a distributed image. Every node has the same credential.

---

## How to use this in the workflow

- **Step 3** marks each baseline item `Met / Partial / Gap` as part of the control review.
- **Step 6** writes the MVP Baseline Checklist with evidence and pointer to mitigations.
- **Step 8** surfaces any `Gap` in the Executive Summary as an MVP blocker — it's the most consequential thing on the page.

## What's NOT here (Deferred to Growth)

These are real and important — but **not MVP**:
- Formal SBOM signing chain (sigstore/cosign on artefacts) — driven by EU CRA
- MFA-claim enforcement at the IdP — driven by ISO 27001 / SOC 2
- Vuln-disclosure policy with timelines — driven by EU CRA / NIS2
- Retention policies tied to a specific framework — driven by ISO 27001
- Data-residency controls — driven by NIS2
- Formal pen-test cadence — driven by SOC 2
- Threat-intel feed integration — driven by NIS2

Capture findings on these in `Deferred to Growth` so the work is visible when the time comes, but don't promote them into MVP scope.
