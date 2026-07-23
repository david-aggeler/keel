<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 3: Control Review Pass

## MANDATORY EXECUTION RULES

- 🤖 Walk all four control areas autonomously — don't ask "should I check authentication next?"
- 📜 Use `../references/control-checklists.md` as your scaffolding — every item gets a verdict
- 📐 Compare findings against `../references/mvp-baseline.md` (the keel MVP cybersecurity baseline)
- 🎯 Each finding must name **what control is missing or weak**, **on what asset**, and **what testable state would close it**
- 🚪 If MVP-baseline mode is on (the default for keel): items that go beyond the MVP baseline get marked `Deferred-to-Growth` here, not flagged as MVP gaps
- 🛑 Do NOT enumerate threats or score risk in this step — Step 4 and Step 5 do that

## YOUR TASK

Walk four control areas in sequence — Authentication & Authorization, API Contract Security, Audit Logging, and Secrets & Transport — and produce a control-gap list per area. The threat enumeration in Step 4 leans heavily on these gaps, so be specific: a vague gap ("authn weak") generates vague threats.

Load `../references/control-checklists.md` and `../references/mvp-baseline.md` now.

---

## REVIEW SEQUENCE

For each of the four areas below, walk the corresponding checklist from `control-checklists.md`. For every checklist item produce a verdict:

| Verdict | Meaning |
|---|---|
| ✅ Present | The architecture or spec satisfies the item with a testable control. |
| ⚠️ Weak | A control exists but is partial, ambiguous, or untestable as written. |
| ❌ Missing | No control. Open gap. |
| 🔜 Deferred-to-Growth | Beyond MVP baseline; tracked but not flagged as an MVP gap. |
| ❓ Unclear | Cannot determine from current docs — surface as an Open Item. |

### A. Authentication & Authorization

Mine the architecture and OpenAPI for: who proves identity, how, on which endpoints; how authorization decisions are made; default-deny posture; revocation; multi-tenant isolation.

Focus signals:
- Every OpenAPI operation should have a `security` requirement (or be explicitly anonymous and justified)
- Default-deny middleware: is it described, or implied?
- Revocation path: how does keel invalidate a stolen token before its TTL expires?
- Tenant scoping: can a token from tenant A read tenant B's resources?
- Device- or agent-facing API: how does the out-of-process agent authenticate back?

### B. API Contract Security

Walk the OpenAPI and AsyncAPI specs (loaded in Step 1) endpoint by endpoint. Don't guess — read the spec.

Focus signals:
- **Existence leakage**: 403 vs 404 on resources outside the caller's scope
- **Verbose errors**: do error responses leak internal structure (stack traces, table names, file paths)?
- **Unbounded inputs**: arrays / strings / objects without `maxItems` / `maxLength`
- **Missing rate-limit signals**: 429 not in the response set on mutating endpoints
- **Idempotency**: mutating endpoints (POST/PUT/DELETE) without an idempotency key
- **AsyncAPI**: every channel has an `Authentication` and `Authorization` description; events on multi-tenant channels carry a tenant ID; replay/duplicate handling is documented

### C. Audit Logging

keel's MVP baseline calls for a basic audit log: subject → object/type → CRUD verb. Walk the architecture for what gets logged, where, and how the logs are protected.

Focus signals:
- **Coverage**: are all CUD operations on customer-visible resources logged? Reads on sensitive resources?
- **Schema**: is each event structured (JSON) with stable keys (subject, object, verb, timestamp, request-id)?
- **PII / secret leakage**: are token values, passwords, or full request bodies logged?
- **Log injection**: are user-supplied strings escaped before being written into JSON / structured fields?
- **Tamper resistance**: can a normal user modify or delete audit records?
- **Retention**: is there *any* policy stated, even if minimal? (Formal retention tied to ISO 27001 / NIS2 / EU CRA is Deferred-to-Growth — but a documented "we keep N days" is MVP.)

### D. Secrets & Transport

Focus signals:
- **Transport**: TLS-only on all external endpoints; mTLS or signed transport on service-to-service where required
- **Secret storage**: secrets in a secret store, not in env vars / config files / source
- **Secret rotation**: a path exists to rotate, even if manual
- **Key material at rest**: DB-stored secrets are encrypted; encryption keys themselves live in the secret store, not in DB
- **Node/agent trust bootstrap**: how does a node/agent prove identity on first contact? (Common gap.)

---

## OUTPUT

Populate the **Control Review Findings** section of `security-review.md` with four sub-tables (one per area). Each row is one checklist item with verdict, evidence (file/line or spec path), and — for `❌ Missing` and `⚠️ Weak` — a one-sentence "what would close this" hint.

Write all `❓ Unclear` items to the **Open Items** section as questions for the user.

After populating, write to frontmatter: `stepsCompleted: [1, 2, 3]`.

## REPORT AND HAND OFF

```
Control review complete.

Authentication/authorization: N items (X missing · Y weak · Z deferred)
API contract security:        N items (X missing · Y weak · Z deferred)
Audit logging:                N items (X missing · Y weak · Z deferred)
Secrets & transport:          N items (X missing · Y weak · Z deferred)

Top concerns going into threat enumeration:
- [1-3 most consequential gaps]

Open questions: [list of ❓ Unclear items the user needs to answer]

Next: I'll enumerate STRIDE threats per component, leaning on the
control gaps above as the starting set.

[C] Continue to threat enumeration
```

Wait for `[C]`. If there are blocking Open Questions (e.g. core authn model is `❓ Unclear`), say so plainly — Step 4 quality drops sharply if the auth model is unknown.

## SUCCESS METRICS

✅ All four control areas walked, every checklist item has a verdict
✅ MVP-baseline items distinguished from Deferred-to-Growth items
✅ Each `❌ Missing` and `⚠️ Weak` item names what would close it
✅ `❓ Unclear` items written to Open Items
✅ `stepsCompleted: [1, 2, 3]` in frontmatter
✅ Wait for `[C]`

## FAILURE MODES

❌ Skipping the OpenAPI / AsyncAPI walk (re-read the specs; don't infer from architecture alone)
❌ Marking everything `❌ Missing` without checking if a control exists in the spec
❌ Promoting Deferred-to-Growth items into MVP gaps (or vice versa)
❌ Producing vague gaps ("authn could be stronger") instead of specific ones
❌ Enumerating threats here

## NEXT STEP

After `[C]`: load `./step-05-threats.md`
