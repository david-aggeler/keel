# DFMEA Risk Rating Scales — Software Systems

These scales are adapted from AIAG/VDA DFMEA 1st Edition for software and infrastructure systems.
Apply them consistently throughout the analysis. When in doubt, score conservatively (higher risk).

---

## Severity (S) — Impact of the failure effect on the user or system

Score based on the **worst credible effect** if no detection or mitigation exists.

| S | Label | Criteria for software/infrastructure systems |
|---|-------|---------------------------------------------|
| 10 | Catastrophic | Unrecoverable data loss, safety-critical system failure, severe security breach (credential exfiltration, privilege escalation to root/admin), or regulatory non-compliance causing legal action. Affects all users. No workaround. |
| 9 | Critical | Significant data corruption (non-trivial recovery required), authentication/authorization bypass, prolonged total outage (>4 h), or loss of audit trail in a compliance-regulated context. |
| 8 | Major — Loss of primary function | Core feature completely unavailable, partial data corruption affecting a meaningful subset of records, silent wrong output that may propagate before detection. |
| 7 | Major — Degraded primary function | Core feature available but producing incorrect or incomplete results. Users must work around the defect. Affects multiple users. |
| 6 | Moderate — Loss of secondary function | Non-critical feature completely unavailable, or intermittent failures in a primary feature. Single-user or low-frequency impact. |
| 5 | Moderate — Degraded secondary function | Non-critical feature partially working. Noticeable to users but not blocking core workflows. |
| 4 | Low — Cosmetic or performance | UI glitch, slow response (>3× expected), or minor inaccuracy with no downstream consequence. |
| 3 | Very Low | Barely noticeable. Affects only edge-case inputs or very infrequent paths. |
| 2 | Negligible | Effect is theoretical; no user or data impact observed in practice. |
| 1 | No effect | Failure has no discernible effect on system behaviour or output. |

**IEC 62304 mapping guidance** (see also `iso-context.md`):

- S 9–10 → Class C candidate (serious injury / death risk in downstream use)
- S 6–8  → Class B candidate (non-serious injury risk)
- S 1–5  → Class A candidate (no injury possible)

Set `iec_class` on each `failure_mode` record when you can determine the class; omit when unknown. No toggle — optional fields with empty value are the "unknown" state.

---

## Occurrence (O) — Likelihood the failure cause produces the failure mode

Score based on expected frequency **given current design, without detection controls**.

| O | Label | Frequency / Rate |
|---|-------|-----------------|
| 10 | Almost certain | > 1 in 2 operations. Failure is the default without active mitigation. |
| 9 | Very high | 1 in 3 to 1 in 10. Fails frequently in normal use. |
| 8 | High | 1 in 20. Regularly observed in integration or load testing. |
| 7 | Moderately high | 1 in 50. Observed in stress testing or infrequent real conditions. |
| 6 | Moderate | 1 in 100. Likely to appear at least once per release cycle under production load. |
| 5 | Low-moderate | 1 in 500. Appears in extended soak testing or edge-case inputs. |
| 4 | Low | 1 in 2,000. Rare; triggered by unusual but realistic conditions. |
| 3 | Very low | 1 in 10,000. Theoretically possible; rarely if ever seen in practice. |
| 2 | Remote | 1 in 100,000. Would require a combination of very unlikely conditions. |
| 1 | Extremely remote | < 1 in 1,000,000. Near-impossible under any realistic scenario. |

**Calibration tips:**

- Concurrency bugs (race conditions, TOCTOU): O 6–8 without explicit synchronisation
- Unvalidated external input paths: O 7–9
- Well-tested happy paths with no concurrency: O 2–4
- Third-party dependency failures (network, cloud API): O 5–7 for unretried calls

---

## Detection (D) — Likelihood the current controls catch the failure before it affects the user

Lower D = easier to detect = lower risk contribution. Score based on controls **as designed today**, not aspirational controls.

| D | Label | Current detection capability |
|---|-------|------------------------------|
| 10 | No detection | No test, monitor, log, or alert exists for this failure mode. Discovered by user complaint only. |
| 9 | Very unlikely | Manual log review only; no automated detection. Low-frequency review cadence. |
| 8 | Remote | Ad-hoc inspection or very limited unit tests. No integration tests covering this path. |
| 7 | Very low | Some unit tests exist but do not cover this failure mode. No system/integration tests. |
| 6 | Low | Integration tests exist but with limited coverage. No production monitoring for this specific failure. |
| 5 | Moderate | Meaningful test coverage. Basic health checks in production. Failure usually caught in CI. |
| 4 | Moderately high | Good test coverage (unit + integration). Structured error logging. Failure caught in staging or early in prod. |
| 3 | High | Comprehensive tests plus production alerting. Failure caught within minutes in production. |
| 2 | Very high | Automated detection with near-zero false-negative rate. Tests + monitoring + structured error reporting cover this path. |
| 1 | Almost certain | Failure cannot reach production undetected. Hard guarantee via compile-time checks, formal verification, or equivalent. |

---

## RPN — Risk Priority Number

**RPN = S × O × D** (conventional range: 1–1000)

| RPN | Risk Class | Default action |
|-----|-----------|----------------|
| ≥ 100 | **Critical** | Mitigation required before construction of this component begins |
| 50–99 | **Major** | Mitigation recommended; defer only with documented rationale |
| < 50 | **Minor** | Log for awareness; address opportunistically |

> Note: RPN alone does not capture the full picture. A failure with S=10, O=1, D=1 (RPN=10) is still worth flagging because the severity is catastrophic even if rare. Always apply engineering judgement to high-severity items regardless of RPN.

---

## ISO 14971 Hazard Categories

Classify each failure mode under the most appropriate hazard category when determinable. Set `hazard_category` on the `failure_mode` record (free-form string). Omit when not determinable.

| Code | Category |
|------|----------|
| DAT | Data integrity / data loss |
| SEC | Security / access control |
| AVL | Availability / continuity of service |
| CFG | Configuration error / misconfiguration |
| INT | Interface / integration failure |
| PER | Performance / resource exhaustion |
| AUD | Audit trail / traceability |
| OPR | Operator error / usability |

These are guidance categories for product-specific use. The `hazard_category` field is free-form — adapt to the product's ISO 14971 risk file if the team uses different categories.
