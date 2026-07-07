---
title: Security Review (Threat Model + Control Review)
project: "keel"
version: "1.0"
status: "Draft"
author: ""
reviewed_by: ""
review_date: ""
approved_by: ""
approval_date: ""
mvp_baseline_mode: true
score_critical: 15
score_major: 8
scope: ""
inputDocuments: []
stepsCompleted: []
---
<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->

# Security Review — keel

## Document Control

| Field | Value |
|-------|-------|
| Document ID | CSE-keel-001 |
| Version | 1.0 |
| Status | Draft |
| Scope | _(populated in Step 1)_ |
| Architecture Ref | _(populated in Step 1)_ |
| API Contracts | _(populated in Step 1)_ |
| MVP-baseline mode | _(populated in Step 1)_ |
| Scoring Refs | STRIDE · Likelihood × Impact (1..25) |

---

## Executive Summary

_(populated in Step 8)_

---

## Attack Surface

_(populated in Step 2)_

### Components

| Name | Type | Trust Zone | Reachable From | Exposes |
|------|------|------------|---------------|---------|
| - | - | - | - | - |

### Trust Boundaries

| # | From → To | Why it's a boundary | Existing controls |
|---|-----------|---------------------|------------------|
| - | - | - | - |

### Security-relevant Data Flows

| # | Source → Sink | Data | Boundaries Crossed |
|---|---------------|------|--------------------|
| - | - | - | - |

### Identities and Credentials

| Identity | What it is | How it authenticates | What it can do |
|---------|------------|---------------------|----------------|
| - | - | - | - |

### External Dependencies

| Dependency | Trust assumption | Failure-of-trust impact |
|-----------|------------------|-------------------------|
| - | - | - |

---

## Control Review Findings

_(populated in Step 3)_

### A. Authentication & Authorization

| # | Checklist Item | Verdict | Evidence | What would close it |
|---|----------------|---------|----------|---------------------|
| - | - | - | - | - |

### B. API Contract Security

| # | Checklist Item | Verdict | Evidence | What would close it |
|---|----------------|---------|----------|---------------------|
| - | - | - | - | - |

### C. Audit Logging

| # | Checklist Item | Verdict | Evidence | What would close it |
|---|----------------|---------|----------|---------------------|
| - | - | - | - | - |

### D. Secrets & Transport

| # | Checklist Item | Verdict | Evidence | What would close it |
|---|----------------|---------|----------|---------------------|
| - | - | - | - | - |

---

## Threat Register

_(populated in Step 4 with rows; scored in Step 5)_

> **Column key:** STRIDE = category · L = Likelihood (1..5) · I = Impact (1..5) · Score = L×I · Severity per `mvp_baseline_mode` thresholds · Source = Step-3 gap ID, DFMEA item, or "novel"

<!-- THREAT_TABLE_START -->

| # | Component | STRIDE | Attacker | Asset | Path | L | I | Score | Severity | Source | Rationale | Notes |
|---|-----------|--------|----------|-------|------|---|---|-------|----------|--------|-----------|-------|
| - | - | - | - | - | - | - | - | - | - | - | - | - |

<!-- THREAT_TABLE_END -->

---

## Risk Distribution Summary

_(populated in Step 5)_

```
Critical (Score ≥ 15):  X items
Major    (Score 8–14):  X items
Minor    (Score < 8):   X items
Total:                  X items

Top 5 threats by score: [list]
Components with ≥1 Critical: [list]
```

---

## Mitigation Plan

_(populated in Step 6)_

> Sorted by Cost/Benefit (Score Δ ÷ Effort). Effort: S=1 · M=2 · L=4 · XL=8. Type: P=Prevent, D=Detect, R=Redesign, PD=Both.

| Priority | Threat # | Mitigation | Type | Effort | Residual L | Residual I | Residual Score | Score Δ | C/B | Owner |
|---------|----------|-----------|------|--------|-----------|-----------|---------------|--------|-----|-------|
| - | - | - | - | - | - | - | - | - | - | - |

---

## MVP Cybersecurity Baseline Compliance

_(populated in Step 6)_

> The Vela MVP cybersecurity floor. Anything `Gap` is an MVP blocker.

| # | Baseline Item | State | Evidence | Closing Mitigation(s) |
|---|--------------|-------|----------|----------------------|
| 1 | TLS-only on all external endpoints | - | - | - |
| 2 | Default-deny middleware on all routes | - | - | - |
| 3 | Existence-leakage prevention (403 vs 404 discipline) | - | - | - |
| 4 | Basic audit log (subject → object/type → CRUD verb) | - | - | - |
| 5 | Structured JSON logging | - | - | - |
| 6 | Revocable tokens (with documented revocation path) | - | - | - |
| 7 | Secrets in a secret store (not env / config / source) | - | - | - |
| 8 | Appliance config API authenticated | - | - | - |

---

## Deferred to Growth (Post-MVP Regulatory)

_(populated in Step 6)_

> Items surfaced during review that are real but **out of MVP scope** under `mvp_baseline_mode = true`. Captured here so they're not lost when ISO 27001 / NIS2 / EU CRA / SOC 2 prep begins.

| # | Item | Likely driver | What it would require | Pre-req in MVP? |
|---|------|---------------|----------------------|-----------------|
| - | - | - | - | - |

---

## Architecture Feedback

_(populated in Step 7)_

> Items requiring a design-level change (not just implementation controls). `Urgency`: Blocker (MVP-baseline gap or Critical threat) · Recommended (Major threat or Partial baseline) · Improvement (future-proofing).

| # | Concern | Required Architecture Change | Threat IDs Addressed | Urgency |
|---|---------|------------------------------|---------------------|---------|
| - | - | - | - | - |

---

## Open Items / Decisions Required

_(updated throughout workflow)_

| # | Item | Owner | Due |
|---|------|-------|-----|
| - | - | - | - |

---

## Revision History

| Rev | Date | Author | Summary |
|-----|------|--------|---------|
| 1.0 | {date} | {user_name} | Initial draft |
