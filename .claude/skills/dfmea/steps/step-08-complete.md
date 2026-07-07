# Step 7: Finalise and Complete

## YOUR TASK

Derive the executive summary from records, close the formal_review session anchor, and hand off to the user.

---

## SEQUENCE

### 1. Load Final Record State

Call `list_failure_mode` filtered by `identified_in_review=<review-ref>` to get the full picture.

Compute statistics in-conversation:

- Total failure modes: count all records
- Risk distribution: count by RPN class (Critical ≥ 100, Major ≥ 50, Minor < 50)
- Top 3 by RPN
- Highest severity items (S ≥ 9)
- Class C items without mitigations (flag for the user — see step-07)

### 2. Write Executive Summary

Compose the executive summary — 5–7 bullet points a stakeholder can read in 60 seconds:

- Scope of analysis (components, subsystems)
- Total failure modes identified
- Risk distribution (Critical / Major / Minor counts)
- Top 3 risks by RPN with a one-line description each
- Highest-severity items (S ≥ 9) even if low RPN
- Total estimated effort to address all Critical items (sum effort scores: s=1, m=2, l=4, xl=8)
- IEC 62304 note: any Class C items still without mitigations

Present the summary to the user.

### 3. Close the Session Anchor

Call `get_formal_review` on the session anchor to confirm current state.

Build the final `subject_refs` list from all failure_mode refs created in this session.

Call `update_formal_review` with:

```json
{
  "status": "completed",
  "outcome": "approved | approved_with_actions | follow_up_required",
  "subject_refs": ["<all failure_mode refs>"],
  "details": "<AI-written executive summary — concise, no placeholders>"
}
```

Choose `outcome`:
- `approved`: all Critical items have mitigations and no Class C items lack coverage
- `approved_with_actions`: Critical items have mitigations but some follow-up is needed
- `follow_up_required`: Class C items without mitigations, or unresolved Critical-severity items

### 4. Generate Handoff Brief

Synthesise the DFMEA findings into a concise brief for follow-up review with Winston and Vera — write it directly to the user so they can copy-paste it.

The brief must include:

- One-sentence scope reminder
- Critical and Major failure modes that require architecture-level changes (from step-07), with their RPN and the specific change needed
- Any Class C items that still lack mitigations
- Two or three sharp questions Vera wants Winston to answer about the architecture — the most contentious or unresolved points, not softballs

Format: plain bullet list, terse, no preamble. Winston will see this cold.

### 5. Final Handoff

```text
DFMEA complete.

Session anchor: <formal_review ref> (status: completed)

─── Summary ────────────────────────────────
Scope:     [scope]
Components analyzed: X
Failure modes: X total (X Critical · X Major · X Minor)
Top risk:  [title] — RPN [score]

Mitigation roadmap: X items
  Blockers:      X  (must address before construction)
  Recommended:   X
  Improvements:  X

[IEC 62304 note if Class C items without mitigations]
────────────────────────────────────────────

Next: discuss the architecture implications with Winston.

Paste the brief above as your opening message to Winston and Vera.

To reopen this DFMEA later: invoke dfmea — it will detect the in_progress session
(or create a new one once this is completed).
```

## SUCCESS METRICS

✅ Final record state loaded via `list_failure_mode`
✅ Executive summary derived from records (not from a worksheet)
✅ `update_formal_review` called with status=completed, outcome, final subject_refs, and details summary
✅ Class C items without mitigations flagged
✅ Handoff brief generated — sharp questions included, ready to paste

## WORKFLOW COMPLETE
