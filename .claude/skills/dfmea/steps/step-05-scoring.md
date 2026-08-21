# Step 4: Risk Scoring (S / O / D)

## MANDATORY EXECUTION RULES

- 📖 Load `../references/risk-scales.md` and `../references/iso-context.md` — do not score from memory; use the defined scales
- 🤖 Score ALL records autonomously first, then present for review — don't ask about each record individually
- 💬 For every score, provide a one-line rationale (why this number, not that one)
- ⚠️ When in doubt, score conservatively (higher risk) — the user can relax scores with domain knowledge
- 🛑 Do NOT compute RPN or plan mitigations in this step — that is Step 5

## CONTEXT

Load `../references/risk-scales.md` now.
Load `../references/iso-context.md` now.

## YOUR TASK

Call `list_failure_mode` filtered by `identified_in_review=<review-ref>` to get all records created in step-04.

For each record, call `update_failure_mode` with:

```json
{
  "severity": <1-10>,
  "occurrence": <1-10>,
  "detection": <1-10>
}
```

Also set `iec_class` and `hazard_category` when you can determine them:

- `iec_class`: `a`, `b`, or `c` per IEC 62304 classification rules in `iso-context.md`. Omit if unknown — these are optional fields; empty is the "unknown" state.
- `hazard_category`: free-form ISO 14971 category from the taxonomy in `risk-scales.md` (DAT, SEC, AVL, CFG, INT, PER, AUD, OPR). Omit if not determinable.

No toggle, no branch — set these fields when you can, leave them unset when you cannot. Presence of the fields on some records and absence on others is expected.

---

## SCORING APPROACH

### Severity (S)

Score the **worst-case effect on the user or system** if the failure occurs and is not caught before impact. Ask: "If this happens and nobody notices for 24 hours, what is the worst outcome?"

Key calibration anchors:

- Data loss / security breach → S 9–10
- Core feature down for all users → S 7–8
- Core feature degraded or intermittent → S 5–6
- Non-critical feature affected → S 3–4
- No user-visible impact → S 1–2

Do not let the fact that a failure seems unlikely reduce the Severity score — Severity is about impact if it happens, not how often it happens.

### Occurrence (O)

Score the likelihood this failure mode occurs **given the current design, without counting detection controls**. Think about: how well-understood is this failure domain? Does the architecture have known weak points here? Is this a concurrency-heavy path? A third-party dependency?

Key calibration anchors:

- No synchronisation on shared state → O 7+
- Unvalidated input from external source → O 7+
- Third-party API with no retry/circuit-breaker → O 5–7
- Well-tested deterministic path → O 2–3
- Theoretical-only (requires hardware failure + race condition + bad config simultaneously) → O 1–2

### Detection (D)

Score based on what **actually exists today** in the design — not planned tests, not hoped-for monitoring. If the step-01-init documents don't describe a specific test or monitor for this failure mode, assume it doesn't exist.

Key calibration anchors:

- No tests, no monitoring → D 9–10
- Only manual log review → D 8–9
- Some unit tests, no integration tests on this path → D 6–7
- Integration tests + basic health checks → D 4–5
- Comprehensive tests + production alerting → D 2–3

---

## OUTPUT FORMAT

After updating all records, present the scored summary:

```text
Scoring complete. Summary of notable scores:

High severity (S ≥ 8) items:
  [title]  S=9 (auth bypass possible)
  [title]  S=8 (compliance trail broken)
  ...

High occurrence (O ≥ 7) items:
  [title]  O=7 (no connection limit in design)
  ...

Weak detection (D ≥ 8) items:
  [title]  D=9 (no concurrent-state tests described)
  ...

Anything you'd score differently?

[C] Scores look right, proceed to RPN and mitigations
```

Wait for `[C]`.

---

## CORRECTION HANDLING

If the user corrects a score: accept the correction, call `update_failure_mode` with the revised S/O/D, note the rationale they gave. User domain knowledge overrides model assumptions — the user knows things about implementation constraints, deployment context, and risk tolerance that aren't visible in the architecture document.

## SUCCESS METRICS

✅ Every record has S, O, D scores from the defined scale
✅ Every score has a one-line rationale
✅ `iec_class` and `hazard_category` set where determinable (omitted when unknown — no toggle, no branch)
✅ High-risk items explicitly called out in the summary
✅ User reviewed and confirmed before proceeding

## FAILURE MODES (meta)

❌ Scoring from memory instead of the reference scales
❌ Letting low occurrence reduce severity ("it probably won't happen so S=3")
❌ Giving detection credit for controls that don't exist yet in the design
❌ Computing RPN or classifying risk in this step

## NEXT STEP

After `[C]`: load `./step-06-mitigations.md`
