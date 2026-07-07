# Step 5: RPN Calculation, Risk Classification & Mitigations

## MANDATORY EXECUTION RULES

- 🔢 Compute RPN = S × O × D for every record — no exceptions, no rounding
- 🚨 Flag ALL items with S ≥ 9 individually regardless of RPN — catastrophic severity is never just "Minor"
- 💰 Prioritise mitigations by cost/benefit, not RPN alone
- 📋 Propose concrete, implementable mitigations — not "improve testing" but "add integration test for concurrent DELETE + GET on /vms/{id}"
- ✅ Produce a prioritised mitigation roadmap the team can act on
- 🔄 After presenting, invite the user to adjust before finalising

## CONTEXT

Use `100` and `50` for classification thresholds (defaults: Critical ≥ 100, Major ≥ 50).

## YOUR TASK

Call `list_failure_mode` filtered by `identified_in_review=<review-ref>` to get all records with S/O/D from step-05.

For each record:
- Compute RPN = S × O × D in-conversation
- Classify: Critical (≥ 100), Major (≥ 50), Minor (< 50)
- Override: any item with S ≥ 9 is at minimum Major regardless of RPN

---

## STEP SEQUENCE

### 1. Design Mitigations

For each Critical and Major item, design one or more mitigations. Mitigations fall into three types:

| Type | Wire value | What it does | Effect on scores |
|------|-----------|-------------|-----------------|
| Prevention | `prevention` | Removes or reduces the cause | Lowers O |
| Detection | `detection` | Catches the failure before user impact | Lowers D |
| Redesign | `redesign` | Changes the design to reduce failure severity | Lowers S |

Good mitigations are:

- **Specific**: name the exact technique, component, or test to add
- **Bounded**: feasible within the project's tech stack and team size
- **Calibrated**: match the mitigation effort to the risk level

Effort values (wire values lowercase; display uppercase):

| Effort | Wire value | Score | Example |
|--------|-----------|-------|---------|
| Small | `s` | 1 | Add a single unit test, add a null check, add a log statement |
| Medium | `m` | 2 | Add an integration test, add a retry with backoff, add a health check endpoint |
| Large | `l` | 4 | Introduce a circuit breaker, add idempotency key pattern, add structured audit logging |
| Extra Large | `xl` | 8 | Redesign a subsystem, introduce a new infrastructure component, rewrite a protocol |

### 2. Cost/Benefit Prioritisation

For each mitigation:

- **RPN reduction** = current_RPN − estimated_rpn (the analyst-forecast RPN after this mitigation lands)
- **Cost/Benefit ratio** = RPN_reduction / effort_score
- Higher ratio = better return on investment

Sort the mitigation roadmap by Cost/Benefit ratio descending.

**Important nuance:** Don't chase the highest Cost/Benefit ratio mechanically. A mitigation for S=10 item may have a modest ratio but should still be in the first tranche because the consequence of inaction is unacceptable. Flag these explicitly.

### 3. Update Records

For each failure_mode record that needs mitigations, call `update_failure_mode` setting `mitigations[]`:

```json
{
  "mitigations": [
    {
      "action": "<specific mitigation action>",
      "type": "prevention",
      "effort": "m",
      "estimated_rpn": 42,
      "owner": "<person responsible>",
      "target_date": "<target date>",
      "status": "open"
    }
  ],
  "rpn": <S * O * D>
}
```

`estimated_rpn` is the analyst forecast of RPN after this mitigation lands — distinct from `post_mitigation.rpn` (the realized post-mitigation RPN set in step-07 once mitigations are confirmed).

---

---

### 4. Requirement Records for Accepted Mitigations

After the user confirms the roadmap (section above), for each mitigation the user accepts:

1. Call `search_requirement product=keel query="<mitigation action summary>"`.
2. If a matching requirement already exists that captures the same non-functional constraint:
   - Call `update_requirement` to add a GWT atom in `acceptance_criteria` (one entry: `Given <system state> When <failure trigger condition> Then <the mitigation control is in place and measurable>`). Append a note linking back to the failure_mode ref (e.g., `related: <failure_mode ref>`). Do not duplicate an atom already present.
3. If no matching requirement exists:
   - Call `get_template_for dto_type=requirement` to get the authoritative template.
   - Call `create_requirement` with:
     - `type: non_functional`
     - `title`: concise statement of the reliability or safety property the mitigation enforces
     - `description`: full mitigation action, cross-referenced to the failure_mode ref it mitigates
     - `acceptance_criteria`: GWT atom — `Given <system state> When <failure trigger condition> Then <the specific control reduces RPN to estimated_rpn or better>`
     - `source`: `dfmea`
   - Record the returned requirement ref; cross-reference it in the failure_mode record's `mitigations[].action` field (append `, req: <ref>`) for traceability.

Rejected and deferred mitigations create no requirement records. `failure_mode` records remain the primary output of the DFMEA analysis.

Re-runs are update-don't-duplicate: `search_requirement` first, update if found, create only if absent.

---

## USER REVIEW

```text
RPN calculation and mitigation roadmap complete.

Risk summary:
  Critical (RPN ≥ 100): X items
  Major    (RPN ≥ 50):  X items
  Minor:                X items

Top 3 by RPN: [title (RPN=X), ...]
Highest severity items: [any S≥9]

Mitigation roadmap:
  Priority 1: [title] — [mitigation action] (type: prevention, effort: M, est. new RPN: X)
  Priority 2: ...
  ...

Questions:
- Any mitigations that aren't realistic for this project?
- Any you'd add or merge?
- Any effort estimates that are off?

[C] Roadmap looks good, continue to architecture feedback
```

Wait for `[C]`.

## SUCCESS METRICS

✅ RPN computed in-conversation for every record (S × O × D)
✅ All S ≥ 9 items flagged as at least Major regardless of RPN
✅ Every Critical and Major item has at least one concrete mitigation
✅ Each mitigation carries type, effort, and estimated_rpn
✅ Cost/Benefit ratio computed and roadmap sorted accordingly
✅ High-severity items with modest C/B ratio explicitly called out
✅ User reviewed and confirmed before proceeding

## FAILURE MODES (meta)

❌ Vague mitigations ("improve error handling") — always name the specific technique
❌ Ignoring high-severity items because their RPN looks manageable
❌ Not accounting for effort when ordering mitigations
❌ Confusing estimated_rpn (forecast, per mitigation) with post_mitigation.rpn (realized, set in step-07)

## NEXT STEP

After `[C]`: load `./step-07-arch-feedback.md`
