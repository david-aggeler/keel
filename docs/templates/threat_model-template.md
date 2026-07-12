# Threat Model: {title}

The `threat_model` record is the product's **living security analysis** — exactly ONE per product (a second create is rejected). It carries the system-level picture; **individual threats are NOT written here** — each is a `failure_mode` record (a threat is a failure mode with an adversarial cause), scored and lifecycle-tracked. This document frames those rows and rolls them up.

**Structure follows Shostack's four questions** — the canonical frame, used in their exact wording because rephrasing loses the intent:

1. **What are we working on?** → §1–§4 (system, assets, boundaries, attack surface)
2. **What can go wrong?** → §5 (the STRIDE pass → failure_mode rows)
3. **What are we going to do about it?** → §6 (controls map, mitigation traceability)
4. **Did we do a good job?** → §7 (residual risk, coverage)

The end-of-version **cybersecurity summary report is NOT this record** — it is a `formal_review` of type `cybersecurity_summary`, frozen at the release cut, referencing this threat_model and the baseline that pins the edition it assessed. This record stays alive; the report freezes.

**This is a LIVING document.** Its failure mode is decay — a static artifact while the system evolves. Update triggers are mandatory (see §8): architecture change, interface change, new dependency, new deployment surface, auth change. A threat model updated only at release has already failed.

**Fields to set:**

- `title`: "{Product} Threat Model" *(mandatory)*
- `summary`: "{crown-jewel assets and top open risk}; {status}" — update on status change.
- `status`: draft | in_review | approved | closed *(mandatory)*. A cybersecurity_summary review expects an `approved` threat model.
- `related`: the architecture_description root, the interface_spec, governing design_decisions.
- `details`: body per the skeleton below.

> **Row conventions (each threat = one failure_mode record):** set `identified_in_review` → this threat_model; `component` spelled per the architecture root's inventory; `hazard_category` carries the STRIDE class (`stride:spoofing`, `stride:tampering`, `stride:repudiation`, `stride:information_disclosure`, `stride:denial_of_service`, `stride:elevation_of_privilege`). Because cybersecurity risk has no historical-frequency data, score `occurrence` as **exploitability** (CVSS-style: how reachable/easy), not probability. Every mitigation terminates in a `requirement` ref (risk control as design input) or a `justification` (documented risk acceptance) — the server rejects `accepted_as_is` without justification and `done` without requirement/closure_evidence.

---

## Skeleton (`details`)

### 1. System under analysis — "What are we working on?"

Which product, at what architectural state (link the architecture root; note the baseline/version if this pass targets a cut). Describe the components and data stores **in prose** — this framing IS part of threat modeling; name components exactly as the architecture root's inventory does. A data-flow view (with trust zones marked) is welcome as an attachment; the component/flow prose is mandatory. State the analysis stance you took (software-centric here, given the architecture is the anchor).

### 2. Assets — the crown jewels

What an attacker wants, ranked. Do not enumerate everything — name what is worth defending:

- **Data classes** — secrets, credentials, tokens, user content, audit trails; where each lives.
- **Capabilities** — write access, code execution, identity assumption, admin/host reach.
- **Availability** — what must stay up, and what an outage costs.

For each: where it resides, who legitimately touches it, and its sensitivity. Weak: *"the database."* Strong: *"the signing key — grants license minting; custodied offline; only the air-gapped operator CLI touches it."*

### 3. Trust boundaries

Every line where privilege or trust changes: network edges, auth checkpoints, product/tenant isolation seams, human-vs-agent access splits, host/container boundaries. Per boundary: what is on each side, what crosses it, and what enforces the crossing. A boundary is where threats concentrate; a boundary-less diagram is a finding. Boundary diagram as attachment; the prose list is mandatory.

### 4. Attack surface

Walk the interface_spec's surface inventory row by row — **the attack surface IS the interface list** — plus the non-interface vectors modern threat models forget: supply chain (dependencies, build), stored/replayed payloads, and operational access (deploy creds, host, backups). For each entry: exposure, authentication, and which §2 assets are reachable through it.

### 5. Threat enumeration — "What can go wrong?" → failure_mode rows

The STRIDE pass. Do NOT tabulate threats here — each becomes a `failure_mode` (conventions above). This section records the **method and coverage** so the pass is auditable and repeatable:

- **Approach**: STRIDE-per-element or STRIDE-per-interaction — state which, and applied to what (each boundary from §3, each surface from §4).
- **Coverage matrix**: for each boundary/surface × STRIDE category, mark analyzed / not-applicable-because / gap. An unmarked cell is an unknown, and unknown coverage is the core auditability failure — "we looked at everything" without a matrix is not a coverage claim.
- **Pass log**: when the last full sweep ran, by whom, and against which architecture baseline.

Coverage gaps are findings, tracked like any other.

### 6. Controls map — "What are we going to do about it?"

Existing and planned controls mapped to the threats they address:

- **Preventive** (lower exploitability/occurrence) and **detective** (catch it in flight) controls, each with a pointer to where it is implemented and how it is verified. Defense-in-depth: name the layers.
- **Two-way traceability**: every enumerated threat names its control(s) or its accepted justification; every control names the threat(s) it serves. **A threat with no control is a finding; a control addressing no threat is a finding.**
- **Mitigation → requirement**: mitigations that become design inputs point at their `requirement` record (the risk-control-as-input loop); mitigations declined point at a `justification`. This is enforced on the rows, summarized here.

### 7. Residual risk — "Did we do a good job?"

The current standing conclusion: open threats (failure_mode rows not yet mitigated) with their scores, accepted risks with justifications, and the overall posture. This is the running view the next `cybersecurity_summary` formal_review freezes into the release report. State the **definition of done** you are holding this analysis to — the explicit criteria for "adequately analyzed and mitigated for this release" — so "good job" is measured, not asserted.

### 8. Maintenance — update triggers & ownership

- **Triggers** (mandatory re-analysis events): architecture change, interface/surface change, new dependency, new deployment target, auth/identity change, a security incident. Each trigger re-visits the affected boundaries/surfaces and the rows on them.
- **Owner**: who curates this model and triages the trigger queue.
- **Last reviewed**: date + architecture baseline of the last sweep (also in §5's pass log).

### 9. Linked decisions

Security-relevant design_decision refs (via `related`), a half-line each.

---

## Quality criteria (review checklist)

- **Assets are ranked crown jewels, not an inventory dump** — each names location, legitimate accessors, sensitivity.
- **Every trust boundary is enumerated** and every boundary has been STRIDE-swept — the §5 coverage matrix proves it; gaps are named, not hidden.
- **No threat prose without a row** — if it is worth describing, it is a scored failure_mode with a lifecycle. No unscored threat lists.
- **Controls trace both ways** — no threat without a control-or-acceptance, no control without a threat.
- **Scoring is exploitability-based** — occurrence reflects reachability/ease, not fictional probability.
- **Coverage is stated, not implied** — a threat model that cannot say what it did and did not analyze has failed its "did we do a good job" question.
- **It is alive** — §8 triggers present, last-reviewed baseline recent relative to the architecture's; a model last touched at the previous release is stale by definition.
