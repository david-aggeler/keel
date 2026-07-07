<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 5: Scoring — Likelihood × Impact

## MANDATORY EXECUTION RULES

- 🤖 Score every threat in the register autonomously
- 📏 Use the 1–5 Likelihood × 1–5 Impact scales in `../references/risk-scales.md` — load it now
- 🎯 Score = Likelihood × Impact (1..25), Severity bands defined by `15` and `8`
- 🔍 Justify each score — even a one-line rationale prevents arbitrary numbers
- 🛑 Do NOT propose mitigations yet — Step 6 handles them
- 🛑 Do NOT lower scores because "we can fix that later" — score the **as-designed** state

## YOUR TASK

Quantify each threat's risk so the team can triage. Read `../references/risk-scales.md`, then walk every row in the Threat Register and assign:

- **Likelihood** (1..5) — how plausible is the attack path given the **current as-designed** controls?
- **Impact** (1..5) — what's the worst-case consequence to confidentiality, integrity, availability, tenant trust, or audit posture?
- **Score** = Likelihood × Impact
- **Severity** — Critical if Score ≥ `15`, Major if Score ≥ `8` (and below Critical), else Minor

---

## SCORING DISCIPLINE

### Score the as-designed state

The score reflects the architecture and specs as they exist today. Don't pre-discount because "we'll add a control later" — that mitigation belongs in Step 6, where the residual score (post-mitigation) is computed separately.

### Likelihood: capability × access × known-pattern

A high-likelihood threat usually has:
- Low attacker capability required (no zero-day, no insider position)
- Direct access to the surface (no chained prerequisites)
- A known, documented attack pattern (not a novel exploit)

Conversely, a 1-likelihood threat usually requires nation-state-grade capability, multiple chained compromises, or pre-existing access that itself would already be Critical.

### Impact: blast radius and recoverability

A high-impact threat usually has:
- Cross-tenant or cross-customer blast radius
- Affects integrity or confidentiality of authoritative data (not just availability of one feature)
- Difficult or impossible to detect after the fact
- Difficult to recover from (irreversible state change, leaked secrets that can't be rotated cheaply)

A 1-impact threat is something like "user can crash their own session" — only the attacker is affected.

### Avoid the everything-is-3 anti-pattern

If half your threats land at Likelihood=3, Impact=3, you're not scoring — you're shrugging. When in doubt, force a tie-breaker: "is this more like the prior threat (which was a 4) or the next one (which was a 2)?"

---

## OUTPUT

For each row in the Threat Register, fill the existing columns:

| Column | Notes |
|---|---|
| L | Likelihood 1..5 |
| I | Impact 1..5 |
| Score | L × I |
| Severity | Critical / Major / Minor |
| Rationale | One-line "why this number" — cite the control state or the missing control |

Sort the table by Score descending so the highest-risk items are at the top.

Populate the **Risk Distribution Summary** section with counts and the top items. Use this template:

```
Critical (Score ≥ 15):  N items
Major    (Score ≥ 8, < critical): N items
Minor    (Score < 8):     N items
Total:                                N items

Top 5 threats by score:
1. #X [component] [STRIDE] — Score N — [one-line description]
2. ...

Components with ≥1 Critical: [list]
```

After populating, write to frontmatter: `stepsCompleted: [1, 2, 3, 4, 5]`.

## REPORT AND HAND OFF

```
Scoring complete.

Critical: N · Major: N · Minor: N
Top concern: #X — Score N — [one-line]
Components carrying Critical items: [list]

Next: I'll write the prioritized mitigation plan
(cost vs benefit), the MVP-baseline checklist, and
the Deferred-to-Growth section for post-MVP regulatory items.

[C] Continue to mitigations
```

Wait for `[C]`.

## SUCCESS METRICS

✅ Every threat scored with L, I, and rationale
✅ Severity assigned per `15` and `8` thresholds
✅ Threat Register sorted by Score descending
✅ Risk Distribution Summary populated
✅ `stepsCompleted: [1, 2, 3, 4, 5]` in frontmatter
✅ Wait for `[C]`

## FAILURE MODES

❌ Defaulting most scores to L=3, I=3 (the "everything is medium" anti-pattern)
❌ Pre-discounting scores because "the fix is easy"
❌ Skipping rationale (an unjustified score is unfalsifiable)
❌ Forgetting to sort the table — the user reads top-down
❌ Proposing mitigations in this step

## NEXT STEP

After `[C]`: load `./step-07-mitigations.md`
