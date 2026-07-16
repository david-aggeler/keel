<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 8: Finalize and Complete

## YOUR TASK

Write the executive summary, validate record completeness, create the cybersecurity_summary formal_review, generate the party brief for Winston, and hand off.

---

## SEQUENCE

### 1. Write Executive Summary

Populate the Executive Summary in the product `threat_model` details. Keep it to 6–8 bullets a stakeholder can read in 60 seconds:

- Scope of review (components / subsystems analysed)
- Total threats identified and the Critical / Major / Minor split
- MVP-baseline status: `met / partial / gap` count out of 8 — and **whether any baseline `Gap` blocks construction**
- Top 3 threats by score, one line each
- Top 2 architecture-level changes recommended (from Step 7)
- Total estimated effort to address all Critical mitigations
- Deferred-to-Growth: count of items captured (one-line: "tracked separately, no MVP impact")
- DFMEA cross-references (if Vera's work was loaded): how many threats overlap with reliability findings

If `mvp_baseline_mode` is true and any baseline item is `Gap`, lead the summary with that — it's the most consequential thing on the page.

### 2. Validate Threat Model Completeness

Check every threat_model section:
- ✅ Document Control (title, scope, version, date, mvp_baseline_mode flag visible)
- ✅ Executive Summary
- ✅ Attack Surface (components, trust boundaries, data flows, identities, external deps)
- ✅ Control Review Findings (all four areas)
- ✅ Threat Register (every row scored, sorted by Score)
- ✅ Risk Distribution Summary
- ✅ Mitigation Plan
- ✅ MVP Cybersecurity Baseline Compliance
- ✅ Deferred to Growth (Post-MVP Regulatory)
- ✅ Architecture Feedback
- ✅ Open Items
- ✅ Revision History

If any section has placeholder text, fill it now.

### 3. Update Threat Model

Call `update_threat_model` with:
- status/details marker: `Draft - Pending Review`
- `stepsCompleted`: `[1, 2, 3, 4, 5, 6, 7, 8]`
- version marker: `1.0`
- all linked failure_mode refs

### 3b. Create Cybersecurity Summary Review

Fetch the formal_review template, then call `create_formal_review` with `type=cybersecurity_summary`, `status=completed`, `product=<slug>`, and `subject_refs` containing the threat_model plus the material failure_mode rows. If a release baseline/product_version is already known, include those refs in `materials`; otherwise state that the summary is pre-release and must be linked during `/publish`.

### 4. Generate Handoff Brief

Synthesise the findings into a concise brief for follow-up review with Winston (and optionally Vera). Write it directly to the user so they can copy-paste it. Format: plain bullet list, terse, no preamble. Winston will see it cold.

The brief must include:
- One-sentence scope reminder
- Every MVP-baseline `Gap` (these are MVP blockers — must be resolved before construction)
- Critical and Major threats that map to architecture-level mitigations (with threat ID and the specific change)
- Two or three sharp questions Sera wants Winston to answer about the architecture — the contentious or unresolved points, not softballs
- One question to Vera (only if DFMEA was loaded) about overlap between adversarial DoS and reliability availability findings

### 6. Final Handoff

```
Security review complete. Threat model: [threat_model ref]

─── Summary ────────────────────────────────
Scope:       [scope]
Threats:     X total (X Critical · X Major · X Minor)
MVP baseline: X/8 met · X partial · X gap
            [If any gap: ⚠️ MVP BLOCKER — see baseline section]
Top concern: #N [one-line] — Score N

Mitigation plan: X items
  Blockers (architecture-level): X
  Recommended:                    X
  Improvements:                   X

Deferred to Growth: X items captured (post-MVP regulatory)
────────────────────────────────────────────

Next: discuss the architecture implications with Winston.

Paste the brief above as your opening message to Winston (and Vera if a DFMEA exists).

To reopen this review later: invoke cse and it will resume from here.
```

## SUCCESS METRICS

✅ Executive summary written, MVP-baseline gaps surfaced first if any
✅ All threat_model sections populated, no placeholders remaining
✅ Threat_model records `stepsCompleted: [1..8]`, `status: Draft - Pending Review`
✅ formal_review type=cybersecurity_summary created with threat_model and failure_mode refs
✅ Party brief generated — sharp questions, ready to paste
✅ `on_complete` hook executed if configured

## WORKFLOW COMPLETE
