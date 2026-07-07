# Step 6: Architecture Feedback

## MANDATORY EXECUTION RULES

- 🔍 Distinguish architecture changes from implementation controls — they are different things
- 📝 Present the architecture findings derived from records, then offer to append to the architecture doc
- 🛑 Do NOT rewrite the architecture document — append a focused "DFMEA Risk Findings" section only
- 🚫 Do NOT auto-append without explicit user confirmation

## KEY DISTINCTION

Not all mitigations require an architecture change. This step is specifically about failure modes where the recommended action cannot be handled at implementation time — where the *design* needs to change.

**Implementation-time controls** (handled in code, not architecture):

- Adding error handling, retries, timeouts
- Writing tests
- Adding log statements
- Input validation
- Null checks and defensive coding

**Architecture-level controls** (require a design decision):

- Adding a new infrastructure component (message queue, cache, circuit breaker, HSM)
- Changing a data flow (sync → async, push → pull)
- Redesigning a protocol or state machine
- Adding a new cross-cutting concern (audit logging as a service, centralised secrets management)
- Splitting or merging subsystems
- Changing consistency/availability trade-off for a data store
- Introducing redundancy (active-passive, quorum)

## YOUR TASK

Call `list_failure_mode` filtered by `identified_in_review=<review-ref>` to get all records.

Focus on records with mitigations where `type="redesign"` or `effort="xl"` — these signal architecture-level changes. For each mitigation that requires an architecture change:

- State which failure mode it addresses
- Describe the specific architecture change needed (one clear sentence)
- Estimate the impact on the architecture document (new section? modify existing decision?)
- Assign urgency: **Blocker** (must resolve before construction), **Recommended** (should resolve before first release), **Improvement** (can address in a later iteration)

---

## SEQUENCE

### 1. Identify Architecture-Level Findings

Review the mitigation roadmap from step-06. Classify each mitigation: architecture change or implementation control?

### 2. Present Architecture Findings

```text
Architecture-level findings:

| # | Failure Mode | Required Architecture Change | Urgency |
|---|-------------|------------------------------|---------|
| 1 | [title] | [specific architecture change] | Blocker |
| 2 | [title] | [specific architecture change] | Recommended |
...
```

### 3. Residual Risk Assessment

For all Critical and Major failure modes, estimate post-mitigation ratings and update each record via `update_failure_mode`:

```json
{
  "post_mitigation": {
    "severity": <usually same as pre-mitigation — severity is worst-case impact>,
    "occurrence": <estimated after prevention mitigations>,
    "detection": <estimated after detection mitigations>,
    "rpn": <new_S * new_O * new_D>
  }
}
```

Flag any failure_mode where `iec_class="c"` has no mitigations item — these require attention before the DFMEA can be considered complete for compliance use:

> "⚠️ [title] is IEC 62304 Class C with no mitigations. This must be addressed before this analysis can be used for compliance purposes."

### 4. Offer Architecture Writeback

```text
I've identified X items that require architecture-level changes:
  [1 Blocker, X Recommended, X Improvement]

Would you like me to append a "DFMEA Risk Findings" section to the architecture document?
This would be a new section at the end of ./architecture.md summarising
the architecture changes needed, for traceability between the records and the design.

[Y] Yes, append to architecture.md
[N] No, I'll handle it manually
```

If the user confirms `[Y]`:

- Read `./architecture.md`
- Append a new `## DFMEA Risk Findings` section with: date, formal_review ref, list of architecture changes with urgency
- Do NOT modify any existing section — append only
- Confirm to the user: "Appended to architecture.md."

### 5. Continue

```text
Architecture feedback complete.

[C] Continue to final completion
```

Wait for `[C]`.

## SUCCESS METRICS

✅ Every mitigation classified as architecture-level or implementation-level
✅ Architecture findings derived from `list_failure_mode` (mitigations with type=redesign or effort=xl)
✅ `post_mitigation` set on all Critical and Major records via `update_failure_mode`
✅ Class C items without mitigations flagged
✅ User given clear choice about architecture writeback
✅ If writeback confirmed: architecture.md updated with append-only change

## FAILURE MODES (meta)

❌ Calling implementation controls "architecture changes" (muddies the architecture document)
❌ Auto-appending to architecture.md without user confirmation
❌ Modifying or reorganising existing architecture sections
❌ Advancing to Step 7 without user pressing `[C]`

## NEXT STEP

After `[C]`: load `./step-08-complete.md`
