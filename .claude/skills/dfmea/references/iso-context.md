# Compliance Context for DFMEA

Reference for compliance-relevant fields: `iec_class` and `hazard_category` on `failure_mode` records.
Load this file during step-04 and step-05 to inform classification decisions.

---

## ISO 14971 — Medical Device Risk Management

**Scope:** Risk management process for medical devices throughout the lifecycle.
**Relevance:** Infrastructure software potentially used for R&D that feeds into medical device development workflows. As a *software tool* in that supply chain, its risk posture must be documentable.

**What the DFMEA satisfies:**

- Clause 4.3: Hazard identification
- Clause 4.4: Estimation of risk for each hazardous situation
- Clause 5: Risk evaluation
- Clause 6: Risk control (mitigations)
- Clause 9: Residual risk evaluation

**Output requirements for ISO 14971 conformance:**

- Risk management file must include: scope, hazard identification, risk estimation, risk evaluation, risk control measures, and residual risk evaluation
- The `failure_mode` records and the `formal_review` session anchor together constitute the risk analysis record

---

## IEC 62304 — Medical Device Software Lifecycle

**Scope:** Software development lifecycle processes for medical device software.
**Relevance:** If the system's output is used in the development, test, or operation of software-controlled medical devices, it may be subject to IEC 62304 tool qualification requirements.

**Software Safety Classification:**

| Class | Wire value | Risk level | Description |
|-------|-----------|-----------|-------------|
| A | `a` | No injury | Software failure cannot contribute to a hazardous situation |
| B | `b` | Non-serious injury | Software failure can contribute to a hazardous situation resulting in non-serious injury |
| C | `c` | Serious injury / death | Software failure can contribute to a hazardous situation resulting in serious injury or death |

**Classification assignment rules (for DFMEA):**

- Assign based on the **worst-case downstream use** of the data, config, or environment that this component manages
- Example: a quota enforcement failure (wrong VM count) is Class A if the VM is dev-only; Class B or C if the VM runs code that generates clinical decisions
- When use context is unknown: default to Class B (conservative)
- Class C components require the full IEC 62304 lifecycle process (detailed design, unit test documentation, traceability matrix)

**Setting on records:** Use the `iec_class` field on each `failure_mode` record — values `a`, `b`, or `c` (lowercase). Omit when class is not determinable for this record. No toggle is needed: an empty `iec_class` means "not yet classified" and is valid.

**Class C flag in step-07/08:** Any Class C failure_mode with no mitigations must be explicitly flagged before the DFMEA session is marked complete.

---

## ISO 13485 — Medical Devices QMS

**Scope:** Quality management system requirements for medical device organizations.
**Relevance:** Tool validation under ISO 13485 §4.1.6 (control of software used in QMS) and §7.6 (control of monitoring and measurement equipment).

**Tool validation implications:**

- The DFMEA is part of the design history file (DHF) that supports tool validation
- Risk analysis must be performed and documented before the tool is used in regulated workflows
- The `failure_mode` records and `formal_review` produced by this workflow constitute evidence-grade output suitable for inclusion in a validation package
- Required evidence: risk identification (records), risk controls (mitigations), verification testing (test protocols), residual risk sign-off (`post_mitigation` ratings)

---

## Practical Guidance for the Workflow

**When to set `iec_class` and `hazard_category`:**

Set them when you can determine the class or category from the failure mode's description, function, and effects. Omit when the classification is not determinable from the available context — empty is the correct "unknown" state, not a default placeholder.

**When to flag items:**

1. Any failure mode with S ≥ 8 involving data integrity, audit trail loss, or access control
2. Any component that processes, stores, or transmits data that could feed a clinical decision
3. Any configuration path where a misconfiguration could silently produce wrong results

**Residual risk statement (for step-07):**

After implementing mitigations, set `post_mitigation` on each mitigated record:

```json
{
  "post_mitigation": {
    "severity": <usually same as pre-mitigation>,
    "occurrence": <estimated after prevention mitigations>,
    "detection": <estimated after detection mitigations>,
    "rpn": <new_S * new_O * new_D>
  }
}
```

**Linking to architecture document:**

The architecture feedback generated in step-07 should be cross-referenced in the architecture document. The `formal_review` record's `subject_refs` provide end-to-end traceability from the session anchor to each identified failure mode.
