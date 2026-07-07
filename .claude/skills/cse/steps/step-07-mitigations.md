<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 6: Mitigation Plan + MVP Baseline + Growth Backlog

## MANDATORY EXECUTION RULES

- 🤖 Propose mitigations for **every Critical and Major** threat; for Minor, group them or skip with `accept residual` if the cost outweighs the benefit
- 🧪 Each mitigation must be **testable** — phrase it so an engineer can write a test that proves it works
- 💰 Score effort and estimate the residual score after the mitigation is in place
- 🚪 Sort by cost/benefit: prefer mitigations with high score-reduction per unit effort
- 📋 Populate the **MVP Cybersecurity Baseline** checklist (load `../references/mvp-baseline.md`)
- 🔜 Populate the **Deferred to Growth** section with any regulatory-flavored items found during the review (don't lose them, but don't promote them to MVP either)

## YOUR TASK

Turn the prioritized threat list into a plan that's actually shippable. Three outputs:

1. **Mitigation table** — one row per Critical/Major threat (Minor optional), with mitigation, type, effort, residual score, cost/benefit
2. **MVP Cybersecurity Baseline** — checklist of the eight MVP-floor items in `../references/mvp-baseline.md`, each marked `met / partial / gap`, with evidence
3. **Deferred to Growth** — list of post-MVP regulatory items surfaced during review, with what they'd require if/when promoted

Load `../references/mvp-baseline.md` now.

---

## A. Mitigation Table

For each threat in priority order:

| Field | Notes |
|---|---|
| Mitigation | What to do — concrete, testable. Not "improve auth" but "enforce tenant claim cross-check on POST /vms; return 403 if `body.org_id != jwt.org_id`" |
| Type | `P` = Preventive (lowers Likelihood), `D` = Detective (lowers Impact via early detection), `R` = Redesign (lowers both — usually architecture-level), `PD` = Both prevent and detect |
| Effort | S=1 / M=2 / L=4 / XL=8 (relative units) |
| Residual L | Estimated Likelihood after mitigation is in place |
| Residual I | Estimated Impact after mitigation is in place |
| Residual Score | Residual L × I |
| Score Δ | Original Score − Residual Score |
| Cost/Benefit | Score Δ ÷ Effort |
| Owner | Suggested owner (often `architecture` or `implementation`) |

Sort by Cost/Benefit descending — the top of the list is what to do first.

### Mitigation hygiene

- **Don't write mitigations that are just restatements of the threat** ("attacker bypasses authn → add authn"). Name the specific control.
- **Prefer redesign for repeating patterns.** If you find yourself writing the same preventive control on five threats, promote it to a redesign item — it's an architecture change.
- **Watch for compensating controls that aren't really compensating.** "Add detection" doesn't compensate for "missing prevention" if detection happens after the data is exfiltrated.

### Architecture-level mitigations (`R` type)

Items typed `R` are candidates for the **Architecture Feedback** section in Step 7. Examples:
- "Move tenant ID enforcement out of per-handler code into middleware"
- "Replace shared-secret appliance auth with cert-pinning bootstrap"
- "Make audit-log writes synchronous with the action they log (currently async; window for repudiation)"

---

## B. MVP Cybersecurity Baseline Checklist

Walk the eight items from `../references/mvp-baseline.md` and mark each:

| State | Meaning |
|---|---|
| ✅ Met | Architecture or spec satisfies the item with a testable control |
| 🟡 Partial | Control exists but a known gap remains (cross-reference threat IDs) |
| ❌ Gap | Item not addressed; this is an MVP blocker |
| n/a | Item doesn't apply to current scope (justify) |

Each row cites:
- The architecture / spec / threat IDs that support the verdict
- For `Partial` and `Gap`: the mitigation rows from section A that would close it

A `Gap` on any baseline item is an **MVP blocker** — flag it loudly. The user said "ship a not-stupid product"; the baseline is the floor.

---

## C. Deferred to Growth

During the review you'll have surfaced items that are real but **post-MVP** under `mvp_baseline_mode = true`. Capture them here so they're not lost:

| Item | Driver (likely) | What it'd require | Pre-req in MVP? |
|---|---|---|---|
| Formal SBOM signing chain | EU CRA | sigstore / cosign on release artefacts; verification at install | Decent SBOM generation in MVP makes this cheaper later |
| MFA-claim enforcement at IdP | ISO 27001, SOC 2 | Trust the IdP's `amr` claim; policy says require MFA for privileged ops | Token has `amr`-shaped claim space in MVP |
| Vuln-disclosure policy + timelines | EU CRA, NIS2 | Published security.txt; coordinated disclosure SLA; response runbook | Have a security@ inbox |
| Retention policy tied to framework | ISO 27001 | Audit-log retention 365d + customer-data retention rules | MVP has *some* retention statement |
| Data-residency controls | NIS2 | Storage tags + scheduling rules to keep customer data in region | MVP doesn't preclude (no global multi-region yet) |

The point of this section: when the user later asks "what does ISO 27001 prep look like?", this section is the starting list. It is **not** part of MVP scope.

---

---

## D. Requirement Records for Accepted Mitigations

After the user accepts the mitigation plan (section A), for each mitigation with status `accepted`:

1. Call `search_requirement product=keel query="<mitigation action summary>"`.
2. If a matching requirement already exists that captures the same behavioral constraint:
   - Call `update_requirement` to add a GWT atom in `acceptance_criteria` (one entry per mitigation: `Given <context> When <trigger> Then <constraint enforced>`). Append a note citing the CSE mitigation ID. Do not duplicate an atom that is already present.
3. If no matching requirement exists:
   - Call `get_template_for dto_type=requirement` to get the authoritative template.
   - Call `create_requirement` with:
     - `type: constraint`
     - `title`: concise statement of the behavioral constraint the mitigation enforces
     - `description`: full mitigation action, cross-referenced to the threat ID
     - `acceptance_criteria`: GWT atom — `Given <context> When <trigger> Then <the specific control is in place and testable>`
     - `source`: `cse`
   - Record the returned requirement ref for traceability (include it in the security review document's mitigation row).

Rejected and deferred mitigations create no requirement records.

Re-runs are update-don't-duplicate: `search_requirement` first, update if found, create only if absent.

---

## OUTPUT

Populate three sections of `security-review.md`:

1. **Mitigation Plan** — table from section A, sorted by Cost/Benefit
2. **MVP Cybersecurity Baseline Compliance** — checklist from section B
3. **Deferred to Growth (Post-MVP Regulatory)** — table from section C

After populating, write to frontmatter: `stepsCompleted: [1, 2, 3, 4, 5, 6]`.

## REPORT AND HAND OFF

```
Mitigation plan written.

Critical mitigated: N (residual score ≤ 8)
Major mitigated:    N
Accepted residual:  N (Minor items not worth the effort)

MVP Baseline:
  Met:     N / 8
  Partial: N
  Gap:     N  ← MVP blockers (must address before construction)

Deferred to Growth: N items captured (no MVP impact)

Architecture-level mitigations (`R` type): N
  → I'll offer to write these back to the architecture doc next.

[C] Continue to architecture feedback
```

Wait for `[C]`. If there are MVP-baseline `Gap`s, surface them again in the report — they're the most important thing on the page.

## SUCCESS METRICS

✅ Every Critical and Major threat has at least one mitigation
✅ Every mitigation is testable (an engineer could write an assertion)
✅ Effort and residual scores assigned consistently
✅ MVP-baseline checklist populated with evidence per row
✅ Deferred-to-Growth items captured (none lost, none promoted)
✅ `stepsCompleted: [1, 2, 3, 4, 5, 6]` in frontmatter
✅ Wait for `[C]`

## FAILURE MODES

❌ Mitigations that just restate threats
❌ Pretending residual score is 0 (residual risk is always > 0; admit it)
❌ Putting MFA-claim enforcement, formal SBOM signing, or framework-pinned retention into MVP findings (those are Growth items under `mvp_baseline_mode = true`)
❌ Conversely: hiding a real MVP-baseline gap by labelling it "Deferred"
❌ Skipping the cost/benefit sort

## NEXT STEP

After `[C]`: load `./step-08-arch-feedback.md`
