# Step 3: Triage

## RULES

- Be precise. When uncertain between categories, prefer the more conservative classification.

## INSTRUCTIONS

1. **Normalize** findings into a common format. Expected input formats:

   - Blind Hunter: Markdown list of descriptions
   - Edge Case Hunter: JSON array with `location`, `trigger_condition`, `guard_snippet`, `potential_consequence` fields
   - Acceptance Auditor: Markdown list with title, AC/constraint reference, and evidence

   If a layer's output does not match its expected format, attempt best-effort parsing. Note any parsing issues for the user.

   Convert all to a unified list where each finding has:

   - `id` — sequential integer
   - `source` — `blind`, `edge`, `auditor`, or merged sources (e.g. `blind+edge`)
   - `title` — one-line summary
   - `detail` — full description
   - `location` — file and line reference (if available)

2. **Deduplicate.** If two or more findings describe the same issue, merge them into one:

   - Use the most specific finding as the base (prefer edge-case JSON with a location over adversarial prose).
   - Append any unique detail, reasoning, or location reference from the other finding(s) into the surviving `detail` field.
   - Set `source` to the merged sources (e.g. `blind+edge`).

3. **Classify** each finding into exactly one bucket:

   - **decision_needed** — an ambiguous choice that requires human input. The code cannot be correctly patched without knowing the user's intent. Only possible if `{review_mode}` = `full`.
   - **patch** — a code issue fixable without human input. The correct fix is unambiguous.
   - **defer** — a pre-existing issue not caused by the current change. Real, but not this change's work.
   - **dismiss** — noise, false positive, or handled elsewhere.

   If `{review_mode}` = `no-spec` and a finding would otherwise be `decision_needed`, reclassify it as `patch` (if the fix is unambiguous) or `defer` (if not).

4. **Drop** all `dismiss` findings. Record the dismiss count for the summary.

5. If `{failed_layers}` is non-empty, report which layers failed before announcing results. If zero findings remain after dropping dismissed AND `{failed_layers}` is non-empty, warn the user that the review may be incomplete rather than announcing a clean review.

6. If zero findings remain after triage (all dismissed or none raised) and `{failed_layers}` is empty, state: "Clean review — all layers passed."

## NEXT

Read fully and follow `./step-04-present.md`
