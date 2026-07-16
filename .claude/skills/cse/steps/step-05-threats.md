<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 4: Threat Enumeration (STRIDE)

## MANDATORY EXECUTION RULES

- 🤖 Generate threats **autonomously** for every component on the surface map — don't ask the user component-by-component
- 🎭 For each component, walk **all six STRIDE categories** — Spoofing, Tampering, Repudiation, Information Disclosure, DoS, Elevation of Privilege
- 🔗 Each threat names: the **attacker** (capability + position), the **asset**, and the **path** (how the attack happens) — vague threats produce vague mitigations
- 📥 Inherit gaps from Step 3 — every `❌ Missing` and `⚠️ Weak` control is the seed of at least one threat
- 🛑 Do NOT score yet — Step 5 handles scoring
- 🛑 Do NOT propose mitigations yet — Step 6 handles them

## YOUR TASK

For every component on the attack surface, enumerate concrete threats using STRIDE. Load `../references/stride.md` now — it has the category definitions, common patterns, and Vela-specific examples.

For every concrete threat that survives this pass, create or update a `failure_mode` row whose `identified_in_review` points at the product `threat_model`. The threat_model remains the session anchor; the failure_mode rows are the durable threat register.

A good threat is **falsifiable**: someone reading it can say "yes, that's possible here" or "no, X prevents that." Bad threats are vague ("the API could be attacked"). Be specific.

---

## ENUMERATION APPROACH

### Per-component STRIDE walk

For each component on the surface map, walk these six questions:

| Letter | Question |
|---|---|
| **S — Spoofing** | Can an attacker present themselves as a different identity (user, service, appliance, tenant) to this component? |
| **T — Tampering** | Can an attacker modify data in transit, at rest, in memory, or in the configuration that this component trusts? |
| **R — Repudiation** | Can an actor (legitimate or hostile) take an action and later deny it because the audit log doesn't capture it? |
| **I — Info Disclosure** | Can an attacker read data they shouldn't — through a response, a side channel, an error message, or a log? |
| **D — Denial of Service** | Can an attacker exhaust the component's resources or crash it? Note: DoS that overlaps with Vera's reliability findings should cross-reference, not duplicate. |
| **E — Elevation of Privilege** | Can an attacker who already has limited access expand it (cross-tenant, role-up, escape from appliance to host)? |

Not every category produces a meaningful threat for every component — but don't skip a category just because it seems unlikely. Write `none — [reason]` if a category genuinely doesn't apply.

### Threat seeds from Step 3

Every gap in the Control Review Findings is a threat seed. Walk that list and turn each gap into a STRIDE threat:

| Control gap (Step 3) | STRIDE seed |
|---|---|
| Missing default-deny middleware | **E**: any unauthenticated request reaches authn-required code paths |
| Existence-leakage in 403 vs 404 | **I**: tenant enumeration via probe-and-compare |
| Unbounded request body | **D**: memory exhaustion via large POST |
| No idempotency key on mutating endpoint | **T**: double-submission creates two resources |
| Token revocation gap | **S**: stolen token usable for full TTL after compromise |
| Audit log skips reads on sensitive resources | **R**: snooping is non-attributable |
| Secrets in env vars | **I**: process listing or memory dump leaks credentials |
| Verbose error responses | **I**: stack traces reveal internal structure |
| Tenant ID not enforced on AsyncAPI channel | **E**: cross-tenant event observation |

### Cross-reference DFMEA (if loaded)

If Vera's DFMEA was loaded in Step 1, scan its risk register for any failure mode that is *also* an adversarial threat (most commonly DoS or partial-success-leading-to-stuck-state). For those, cite the DFMEA item ID in your threat row's notes column rather than rewriting the analysis. The two documents should compose, not duplicate.

### Asset and attacker specificity

For each threat, name:

- **Attacker** — *who can do this and from where?* "Authenticated tenant user", "internet-anonymous attacker", "compromised appliance", "operator with read-only role", "compromised dependency (Proxmox / IdP)". Pick one — most threats have one obvious attacker class. If genuinely several, pick the most capable.
- **Asset** — *what gets harmed?* Customer data, audit log integrity, tenant isolation, availability of API X, secret material, control plane state.
- **Path** — *how does the attack happen, in two sentences?* Concrete: "Attacker calls POST /vms with `org_id=other-tenant` in the request body. Server doesn't cross-check against the JWT's tenant claim, creates VM in target tenant's quota."

---

## OUTPUT

Populate the threat register in the product `threat_model` and maintain one `failure_mode` row per concrete threat. Use these columns in the threat_model details:

| # | Component | STRIDE | Attacker | Asset | Path | Source | Notes |
|---|---|---|---|---|---|---|---|

- `Source`: which Step 3 gap or which DFMEA item seeded this (or "novel" if neither)
- `Notes`: cross-references, prerequisites, scope qualifiers

Aim for breadth over depth in this step — get every plausible threat written down. Step 5 scoring will rank them; trivial threats fall to the bottom naturally and don't need to be filtered upfront.

After populating, call `update_threat_model` to record `stepsCompleted: [1, 2, 3, 4]` and the current threat/failure_mode refs.

## REPORT AND HAND OFF

```
Threat enumeration complete.

Threats:           N total
By component:      [top 3 components by threat count]
By STRIDE:         S=N · T=N · R=N · I=N · D=N · E=N
DFMEA cross-refs:  N (where reliability and adversarial overlap)

Largest concentrations:
- [1-3 components or boundaries with the most threats]

Next: I'll score Likelihood × Impact for each threat (1..25)
and produce the prioritized risk register.

[C] Continue to scoring
```

Wait for `[C]`.

## SUCCESS METRICS

✅ Every component on the surface map has been walked through all 6 STRIDE categories
✅ Every Step 3 control gap produced at least one threat
✅ DFMEA cross-references made where applicable (no duplication)
✅ Each threat names attacker, asset, and path concretely
✅ `stepsCompleted: [1, 2, 3, 4]` recorded in the threat_model
✅ Wait for `[C]`

## FAILURE MODES

❌ "Auth could be bypassed" without naming the attacker class or path
❌ Skipping STRIDE categories silently rather than writing `none — [reason]`
❌ Re-litigating DFMEA failure modes as security threats
❌ Trying to score or mitigate in this step
❌ Treating breadth as low quality — broad enumeration is the point of this step

## NEXT STEP

After `[C]`: load `./step-06-scoring.md`
