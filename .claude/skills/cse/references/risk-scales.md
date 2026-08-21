<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Risk Scales — Severity × Occurrence × Detection, plus CVSS

Sera scores every threat row on the `failure_mode` record's own axes: **Severity**, **Occurrence** and **Detection**, each 1–10, with **RPN = S × O × D** (1–1000). These are the same axes and the same bands Vera uses for DFMEA — one register, one triage language. Because cybersecurity has no historical-frequency data, **Occurrence is scored as exploitability** (how reachable, how easy), not probability. Rows backed by a concrete vulnerability additionally carry a **CVSS 3.1 base vector** in `cvss_vector`.

## Severity (S) — blast radius and recoverability, 1–10

Worst-case consequence to confidentiality, integrity, availability, tenant trust, or audit posture — **as designed today**.

| S | Label | Meaning |
|---|-------|---------|
| 10 | Catastrophic | Cross-tenant breach at scale; control-plane takeover; secrets leaked that can't be cheaply rotated; irreversible. |
| 9 | Extreme | Full data exfiltration of one class of authoritative data; persistent foothold that survives remediation attempts. |
| 8 | Severe | Single-tenant full compromise; cross-tenant partial leak; integrity loss on authoritative records. |
| 7 | Very high | Multi-day outage of a core function; audit-trail destruction that breaks accountability broadly. |
| 6 | High | Partial data exposure (contents, not just metadata); privileged action performed without authorization, recoverable. |
| 5 | Moderate | One feature or one tenant degraded; metadata/listing exposure; audit-trail gaps that complicate forensics. |
| 4 | Moderate-low | Contained integrity nuisance (double-execution, duplicate resources); recoverable with operator effort. |
| 3 | Low | One user's session affected; contained nuisance; recoverable in minutes by the affected user. |
| 2 | Very low | Cosmetic or informational; no third party affected beyond noise. |
| 1 | Minimal | Attacker only harms themselves (e.g. crashes own session). |

**Tips:**

- **Cross-tenant blast radius is almost always 8–10** in a multi-tenant system. Tenants are the customer trust boundary; crossing it is severe by definition.
- **Audit-log integrity loss is at least 5** — even if no data was stolen, repudiation breaks accountability for everything else.
- **Recoverability matters.** A leak you can detect and rotate within an hour sits notches below a leak that cements a long-term foothold.

## Occurrence (O) — exploitability, 1–10

How reachable and how easy is the attack path **as designed today**, before any new mitigation? This is CVSS-style exploitability reasoning (attack vector, complexity, privileges, interaction), not a frequency claim.

| O | Label | Meaning |
|---|-------|---------|
| 10 | Trivial | Known attack pattern; no capability needed; unauthenticated, network-reachable surface. Will be found by scanners. |
| 9 | Very easy | Documented pattern (OWASP / CWE / public CVEs against this exact shape); direct surface; commodity tooling. |
| 8 | Easy | Common pattern; any authenticated tenant user can reach it; no timing or race required. |
| 7 | Straightforward | Modest capability; reachable surface; one enumerable prerequisite (an ID, a URL). |
| 6 | Moderate-high | Requires a valid low-privilege credential plus a known technique. |
| 5 | Moderate | Targeted attacker who knows what they're looking for; reasonable capability or a chained step. |
| 4 | Difficult | Multiple genuinely independent prerequisites; insider position or a leaked secret needed. |
| 3 | Hard | Significant capability (novel exploit, race window, physical or network adjacency). |
| 2 | Very hard | Chained compromises of independently-secured components. |
| 1 | Implausible | Nation-state capability or pre-existing compromise of a higher-value target. If they got here, you have larger problems. |

**Tips:**

- "Documented attack pattern + reachable surface" defaults to **8–10** — don't talk yourself down from a known-bad pattern.
- Multiple prerequisites drop the score only if each is genuinely independent. "Attacker needs a token AND knows the URL" is one prerequisite (the token) — the URL is enumerable.
- An authenticated-only threat is not automatically lower than an unauthenticated one. If any tenant user can reach it, it's still wide.

## Detection (D) — will the current controls catch it? 1–10

Lower D = easier to detect = lower risk contribution. Rate the detective controls **as designed today** — the Pass 3 `Present` rows, not aspirations. For an adversarial row, "detect" means *while it happens or promptly after*, attributably — an attacker-visible-only failure is a 10 even if the system "logs errors".

| D | Label | Current detection capability |
|---|-------|------------------------------|
| 10 | No detection | Nothing would notice; discovered by external report or breach disclosure only. |
| 9 | Very unlikely | Manual log review only, and the action doesn't produce a distinctive log line. |
| 8 | Remote | Logged, but indistinguishable from legitimate traffic; no alerting. |
| 7 | Very low | Attributable log line exists but nothing reviews it on any cadence. |
| 6 | Low | Audit record exists; review is periodic and manual. |
| 5 | Moderate | Structured audit trail plus basic anomaly visibility; caught in days. |
| 4 | Moderately high | Alerting on the relevant class of event; caught in hours. |
| 3 | High | Automated alerting tuned for this pattern; caught in minutes. |
| 2 | Very high | Blocking-plus-alerting; the attempt itself is flagged with near-zero false-negative rate. |
| 1 | Almost certain | The attempt cannot succeed silently — hard guarantee (constraint, signed chain, compile-time or protocol-level). |

## RPN — Risk Priority Number

**RPN = S × O × D** (conventional range: 1–1000). Bands match the DFMEA reference so both personas triage one register consistently:

| RPN | Band | Default action |
|-----|------|----------------|
| ≥ 100 | **Critical** | Mitigation required before construction of this component begins |
| 50–99 | **Major** | Mitigation recommended; defer only with documented rationale |
| < 50 | **Minor** | Log for awareness; address opportunistically |

**High-severity override:** RPN is a triage rank, not a magnitude. A row with S ≥ 9 stays flagged in §7 regardless of band — an S=10, O=1, D=1 row (RPN 10) is still a catastrophic outcome worth engineering judgement.

## CVSS 3.1 base vector (`cvss_vector`)

Every row backed by a **concrete vulnerability** — typically the STRIDE rows this workflow produces — also records a CVSS 3.1 base vector (`CVSS:3.1/AV:_/AC:_/PR:_/UI:_/S:_/C:_/I:_/A:_`). It gives auditors a portable, machine-comparable magnitude next to the in-model RPN.

**Consistency rules:**

- **AV/AC/PR/UI must track the Occurrence rationale.** O=9 "unauthenticated, network-reachable, documented pattern" pairs with `AV:N/AC:L/PR:N`; O=4 "needs insider credential" pairs with `PR:H` or `AC:H`. A vector that contradicts the occurrence score is a scoring bug.
- **C/I/A must track Severity and `effects`.** Cross-tenant data exfiltration is `C:H`; audit-trail destruction is `I:H`.
- **RPN and CVSS may legitimately rank rows differently** — RPN weights detectability, CVSS weights exploitability × impact. Note the divergence in §7 rather than forcing agreement.
- **Design-level rows with no clean CVSS mapping omit the field** and are scored by RPN alone. Never invent a vector for a row that isn't a concrete vulnerability.

## Avoiding the everything-is-5 anti-pattern

The most common scoring failure is collapsing every row to a mid-scale triple. That's not scoring — it's hedging. When a row tempts you toward 5/5/5:

1. Ask: "is this more like row Tx (which I scored 8) or row Ty (which I scored 3)?" Force a tie-break to one side.
2. If you genuinely can't, the threat is probably under-specified — go back and tighten the path or attacker class.
3. If after that it's truly mid-scale, fine — but only after a real attempt to break the tie.

## Estimated RPN on mitigations (Pass 6)

`mitigations[].estimated_rpn` is the analyst-forecast RPN **after the mitigation is implemented and in production**, not the design intent. A mitigation that says "add validation" without naming what or how doesn't lower the forecast — phrase the mitigation tightly enough that you can re-score S/O/D with confidence. `prevention` lowers O, `detection` lowers D, `redesign` lowers S. Estimated RPN is rarely near zero. Admit it — the point is to be honest about what's left, not to claim the threat is gone. (`post_mitigation` on the record is distinct: realized ratings, set only after mitigations are confirmed in place.)
