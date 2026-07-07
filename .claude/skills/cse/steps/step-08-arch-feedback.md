<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 7: Architecture Feedback

## MANDATORY EXECUTION RULES

- 🎯 Surface only items that genuinely require an **architecture change** — not implementation details
- 🤝 Frame feedback as a request to Winston, not an imposition
- 📝 If MVP-baseline mode is on (the default for vela), offer to append findings directly to the architecture document
- 🛑 Do NOT modify the architecture document without explicit user confirmation

## YOUR TASK

Distill the redesign-type (`R`) and architecture-relevant preventive (`P`) mitigations from Step 6 into a focused list of architecture changes. These are the items where "fix it in the implementation" is the wrong answer — the design itself needs to shift.

Then offer to write the feedback into the architecture document.

---

## SEQUENCE

### 1. Filter Step 6 mitigations

Walk the Mitigation Plan and pick:
- Every `R` (Redesign) mitigation
- Every `P` mitigation that names a cross-cutting control (default-deny middleware, tenant-claim enforcement, audit-log discipline, etc.)
- Every mitigation tied to an MVP-baseline `Gap` or `Partial`

Skip mitigations that are pure implementation hygiene (e.g. "add `maxLength` to field X" — that's a spec edit, not architecture feedback).

### 2. Group by architecture concern

Common groupings:
- **Identity and authorization model** — tenant claims, default-deny, role boundaries, revocation
- **Trust boundaries** — segmentation between tenant zones, control-plane vs data-plane, appliance trust bootstrap
- **Audit and observability** — what gets logged, where, integrity of the log itself
- **Secret and key management** — where secrets live, how they're rotated, what's encrypted at rest
- **API surface design** — error-shape discipline, idempotency, rate-limit signals as a first-class contract concern

### 3. Populate Architecture Feedback section

For each grouping, write:

| # | Concern | Required Architecture Change | Threat IDs Addressed | Urgency |
|---|---|---|---|---|
| 1 | [grouping] | [the specific change] | [#X, #Y, #Z] | Blocker / Recommended / Improvement |

`Urgency` ladder:
- **Blocker** — addresses an MVP-baseline `Gap` or a Critical threat
- **Recommended** — addresses a Major threat or strengthens an MVP-baseline `Partial`
- **Improvement** — quality-of-life or future-proofing; can land later

### 4. Offer Writeback (if enabled)

If MVP-baseline mode is on (the default for vela), ask:

```
I have N architecture-feedback items.

I can either:
  (a) Append a new "## Security Review Findings" section to the architecture
      document, with a bidirectional link back to security-review.md
  (b) Just leave it in security-review.md and let you take it to Winston manually

Which?
```

If (a):
- Re-read `./architecture.md` (or sharded equivalent)
- Append a section after the existing content (don't insert mid-document and don't reorder)
- The section header is `## Security Review Findings — Sera, {today}`
- Each item links back to its threat IDs in `security-review.md` (e.g. `see security-review.md §threats #14`)
- Show the user the diff before writing — confirm with `[Y]`

If (b): no file modification; just continue.

### 5. Update Frontmatter and Hand Off

After populating Architecture Feedback (and optionally writing back), update:

- `stepsCompleted: [1, 2, 3, 4, 5, 6, 7]`

```
Architecture feedback recorded.

Blocker:      N items
Recommended:  N
Improvement:  N

Architecture writeback: [done / skipped per user]

Next: I'll finalize the executive summary, generate the
party brief for Winston, and stamp the document.

[C] Continue to finalize
```

Wait for `[C]`.

## SUCCESS METRICS

✅ Architecture Feedback section populated with grouped, urgency-ranked items
✅ Each item cites the threats it addresses
✅ Writeback handled per user choice (no surprise edits to architecture.md)
✅ `stepsCompleted: [1..7]` in frontmatter
✅ Wait for `[C]`

## FAILURE MODES

❌ Promoting implementation hygiene items into architecture feedback (clutter dilutes the signal)
❌ Modifying architecture.md without explicit `[Y]`
❌ Inserting feedback mid-document or reordering existing content during writeback
❌ Forgetting to back-link from architecture.md to security-review.md (loses the audit trail)

## NEXT STEP

After `[C]`: load `./step-09-complete.md`
