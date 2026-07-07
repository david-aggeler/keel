# Step 3: Failure Mode Analysis

## MANDATORY EXECUTION RULES

- 🤖 Generate failure modes **autonomously** for all components — do not ask the user component-by-component
- 📋 Create a **complete set** of failure_mode records before presenting the summary
- 🧠 Think adversarially: what would a hostile environment, buggy dependency, or careless operator do to each component?
- ✅ Cover ALL four failure domains for each component (availability, integrity, security, performance)
- 🛑 Do NOT assign S/O/D scores in this step — leave severity/occurrence/detection unset (Step 4 handles scoring)
- 🔄 After creating records, present the count summary and invite additions before proceeding

## CONTEXT

Load `../references/risk-scales.md` now to understand failure categories and the ISO 14971 hazard taxonomy — you need this to classify failure modes correctly even though you won't score them yet.

Also load `../references/iso-context.md` for IEC 62304 classification guidance — both files are loaded unconditionally (no toggle).

## YOUR TASK

For every component in the map (Step 2), generate a comprehensive set of failure modes. A failure mode is a specific, observable way that a component can deviate from its intended function. Be concrete — not "API fails" but "POST /vms returns 200 with a stale VM state after a concurrent delete."

Fetch the failure_mode template before creating records: `get_template_for dto_type=failure_mode`.

---

## FAILURE MODE GENERATION APPROACH

### Systematic coverage: four domains per component

For every component, consider each domain:

| Domain | Question to ask |
|--------|----------------|
| **Availability** | How can this component become unreachable, unresponsive, or stop processing? |
| **Data Integrity** | How can this component produce, store, or pass wrong, corrupt, or stale data? |
| **Security** | How can this component be abused to escalate privilege, leak data, or bypass controls? |
| **Performance** | How can this component degrade to the point where SLAs are violated or cascading failures occur? |

Not every domain produces a meaningful failure mode for every component — use judgement. But don't skip a domain just because it seems unlikely.

### Additional patterns to check

- **Concurrency hazards**: race conditions, TOCTOU (time-of-check to time-of-use), double-write
- **Dependency failures**: what if the component's upstream (DB, message queue, hypervisor API) is down, slow, or returning errors?
- **Configuration drift**: what if a config value is wrong, missing, or changed without restart?
- **Partial success**: what if an operation partially succeeds (e.g., VM created but DNS not registered)?
- **Silent failure**: what if the component fails but returns no error, leaving the system in an inconsistent state?
- **Input edge cases**: null, empty, oversized, malformed, or maliciously crafted input
- **State machine violations**: what if the component receives an event it doesn't expect in its current state?

### API contract mining (if OpenAPI / AsyncAPI specs loaded)

The API specs are ground truth for what callers and consumers expect. Mine them for failure modes the architecture document alone won't surface:

- **Missing error codes**: are all realistic failure paths in the spec's response definitions? Anything absent means callers get an unexpected response shape.
- **Underspecified responses**: optional fields callers likely treat as required; nullable fields callers may not handle.
- **Idempotency gaps**: mutating endpoints (POST/PUT/DELETE) with no idempotency key — double-submission is a concrete failure mode.
- **Event ordering**: AsyncAPI channels where consumers may receive events out of order or miss events during a reconnect window.
- **Schema drift risk**: places where the implementation could diverge from the spec under partial failure (e.g., 200 with a subset of declared fields populated).

### Effect identification

For each failure mode, identify:

1. **Immediate effect**: what happens to this component
2. **System effect**: what the user or downstream system observes
3. **Worst-case effect**: the most severe plausible consequence (this is what drives the Severity score in Step 4)

---

## CREATING RECORDS

For each failure mode, call `create_failure_mode` with:

```json
{
  "product": "<product-slug>",
  "title": "<concise failure mode title>",
  "status": "identified",
  "function": "<function that can fail>",
  "mode": "<how it fails — be specific>",
  "component": "<component name from step-03 map>",
  "effects": ["<immediate effect>", "<system effect>", "<worst-case effect>"],
  "causes": ["<specific cause mechanism>"],
  "prevention_controls": ["<control that reduces likelihood of the cause>"],
  "detection_controls": ["<control that catches the failure before user impact>"],
  "identified_in_review": "<formal_review ref from step-01-init>"
}
```

After creating each failure_mode, call `update_formal_review` on the session anchor to accumulate `subject_refs` with the new failure_mode ref.

Leave `severity`, `occurrence`, `detection`, `iec_class`, and `hazard_category` unset — Step 4 handles scoring and classification.

---

## USER REVIEW

After creating all records, present a summary:

```text
Failure mode records created.

Total failure modes: X
By component:
  S1.1 [Component]: X failure modes
  S1.2 [Component]: X failure modes
  ...

By domain:
  Availability: X
  Data Integrity: X
  Security: X
  Performance: X

Please review and let me know:
- Any failure modes I missed?
- Any that are out of scope or not credible?
- Any causes or effects you'd correct?

[C] Records look good, proceed to scoring
```

Wait for `[C]`.

## SUCCESS METRICS

✅ All four failure domains considered for every in-scope component
✅ Each failure_mode record created with component, effects, causes, prevention_controls, detection_controls, identified_in_review
✅ Effects described at two levels: immediate and worst-case
✅ Causes are specific (not "software bug" but the actual mechanism)
✅ Session anchor subject_refs updated after each create
✅ User reviewed and confirmed before proceeding

## FAILURE MODES (meta)

❌ Skipping a domain because it seems unlikely
❌ Vague effects ("system degrades") — effects must be observable and specific
❌ Assigning S/O/D scores in this step
❌ Auto-advancing without user confirmation

## NEXT STEP

After `[C]`: load `./step-05-scoring.md`
