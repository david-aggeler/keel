<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Risk Scales — Likelihood × Impact

Sera scores threats on a 1..25 scale: Likelihood (1..5) × Impact (1..5). The scale is deliberately simple — finer granularity (1..10) doesn't improve decisions, it just creates the illusion of precision.

## Likelihood (1..5)

How plausible is the attack path **as designed today**, before any new mitigation?

| L | Label | Meaning |
|---|-------|---------|
| 5 | Near-certain | Known attack pattern; trivial capability; direct surface. Documented in OWASP / CWE / public CVEs against this exact pattern. Will happen in production within months of launch. |
| 4 | Likely | Common pattern; modest capability; surface is reachable. Will happen against any internet-exposed system; specific to Vela once anyone tries. |
| 3 | Plausible | Reasonable capability or chained access required. Targeted attacker who knows what they're looking for. |
| 2 | Unlikely | Significant capability needed (insider position, leaked secrets, novel exploit). Rare in non-targeted attacks. |
| 1 | Implausible | Nation-state or pre-existing compromise of higher-value target. If they got here, you have larger problems. |

### Likelihood scoring tips

- "Documented attack pattern + reachable surface" defaults to **4 or 5** — don't talk yourself down from a known-bad pattern.
- Multiple independent prerequisites usually drop one notch each, but only if each prerequisite is genuinely independent. "Attacker needs a token AND knows the URL" is one prerequisite (the token) — the URL is enumerable.
- An authenticated-only threat is not automatically lower-likelihood than an unauthenticated one. If any tenant user can reach it, it's still wide.

## Impact (1..5)

What's the worst-case consequence to confidentiality, integrity, availability, tenant trust, or audit posture?

| I | Label | Meaning |
|---|-------|---------|
| 5 | Catastrophic | Cross-tenant breach; full data exfiltration; control-plane takeover; secrets leaked that can't be cheaply rotated; irreversible. |
| 4 | Severe | Single-tenant full compromise OR cross-tenant partial leak OR multi-day outage of a core function. Recoverable but expensive. |
| 3 | Moderate | One feature or one tenant degraded; partial data exposure (metadata, listing, but not contents); audit-trail gaps that complicate forensics. |
| 2 | Limited | One user's session affected; contained nuisance; recoverable in minutes by the affected user. |
| 1 | Minimal | Attacker only harms themselves (e.g. crashes own session); no third party affected. |

### Impact scoring tips

- **Cross-tenant blast radius is almost always 4 or 5** in a multi-tenant system. Vela's tenants are the customer trust boundary; crossing it is severe by definition.
- **Audit-log integrity loss is at least 3** — even if no data was stolen, repudiation breaks accountability for everything else.
- **Availability of a core function** (creating vApps, viewing inventory) is 3–4 depending on duration and scope. A degraded but working system is 2; a hard outage is 4.
- **Recoverability matters.** A leak you can detect and rotate within an hour is lower-impact than a leak that cements a long-term foothold.

## Score and Severity Bands

```
Score = Likelihood × Impact   (1..25)

Critical: Score ≥ score_critical  (default 15)
Major:    Score ≥ score_major     (default 8, < critical)
Minor:    Score < score_major
```

The defaults map to:
- **Critical** = (L=5, I=3+) or (L=4, I=4+) or (L=3, I=5)
- **Major** = (L=2, I=4+) or (L=4, I=2+) or (L=3, I=3+)
- **Minor** = everything else

The thresholds are configurable (`score_critical`, `score_major` inlined into the cse step files (currently 15 and 8)). Lower them when stakes rise (tighter triage); raise them only with a written reason.

## Avoiding the everything-is-3 anti-pattern

The most common scoring failure is collapsing every threat to L=3, I=3. That's not scoring — it's hedging. When a row tempts you toward 3,3:

1. Ask: "is this more like row #X (which I scored 4,4) or row #Y (which I scored 2,2)?" Force a tie-break to one side.
2. If you genuinely can't, the threat is probably under-specified — go back and tighten the path or attacker class.
3. If after that it's truly 3,3, fine — but only after a real attempt to break the tie.

## Residual scoring (Step 6)

When estimating the post-mitigation score, score the **state after the mitigation is implemented and in production**, not the design intent. A mitigation that says "add validation" without naming what or how doesn't lower the residual score — phrase the mitigation tightly enough that you can estimate L and I with confidence.

Residual score is rarely 0. Admit it. The point is to be honest about what's left, not to claim the threat is gone.
