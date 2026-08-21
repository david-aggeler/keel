# Step 4: Present and Act

## RULES

- Findings land as records. Never write findings into a local Markdown file.
- Create the `formal_review` record before offering action choices, so nothing is lost if the session ends.
- `decision_needed` findings must be resolved before handling `patch` findings.
- Before authoring any record, fetch its template with `get_template_for dto_type=<type>` and write into that structure.

## INSTRUCTIONS

### 1. Open the review record

Call `create_formal_review` with:

- `title` — "Code review: \<subject title or diff description\>"
- `type` — `other`, with `type_other` = `code_review`
- `status` — `in_progress`
- `conducted_at` — today
- `subject_refs` — `[{subject_ref}]` when set; otherwise leave empty and put the diff description in `subject_text`
- `summary` — one line: what was reviewed and the headline result
- `details` — the triaged findings, grouped by bucket, each with title, detail, and location

Set `{review_ref}` to the returned ref.

If zero findings remain after triage, record the clean result in `details`, set `outcome` = `approved`, `status` = `completed`, and skip to section 5.

### 2. Present the summary

Announce what was recorded:

> **Code review complete.** \<D\> `decision_needed`, \<P\> `patch`, \<W\> `defer`, \<R\> dismissed as noise. Recorded as `{review_ref}`.

### 3. Resolve `decision_needed` findings

If `decision_needed` findings exist, present each one with its detail and the options available. The user must decide — the correct fix is ambiguous without their input. Walk through each finding (or batch related ones) and get the user's call. Once resolved, each becomes a `patch`, a `defer`, or is dismissed.

If the user chooses to defer, ask for a one-line reason and carry it into the record created in section 5.

**HALT** — wait for the user's decision on every `decision_needed` finding before continuing.

### 4. Handle `patch` findings

If `patch` findings exist (including any promoted in section 3), HALT and ask:

> **How would you like to handle the \<P\> `patch` findings?**
>
> 1. **Apply every patch** — fix all of them now, no per-finding confirmation. Defer and decision items are not touched.
> 2. **Record them** — leave the code alone; each becomes an issue in section 5.
> 3. **Walk through each patch** — show details for each before deciding.

**HALT** — wait for a numbered choice. Do not proceed until the user selects an option.

- **Apply every patch**: apply every `patch` finding without per-finding confirmation. Do not touch `defer` or `decision_needed` items. Present a summary of the changes made.
- **Record them**: no code changes; section 5 files each one.
- **Walk through each patch**: present each finding with full detail, diff context, and suggested fix. After the walkthrough, re-offer options 1 and 2.

### 5. Land every surviving finding as a record

One record per finding that was not applied and not dismissed. Route by who can resolve it:

| Finding state | Record | Key fields |
|---|---|---|
| Agent-solvable, not applied | `create_issue` | `executor` = `agent`, evidence embedded in the body, `related` = `[{review_ref}]` |
| Needs human judgment or out-of-band action | `create_action_item` | owner, the decision being asked for, `related` = `[{review_ref}]` |
| Deferred pre-existing defect | `create_issue` | `executor` = `agent`, body states it predates this change, `related` = `[{review_ref}]` |

If the review found that a coding stage did nothing at all — the change is a no-op against its stated scope — do not file a new record. Report it and reopen the originating change request instead.

Then call `update_formal_review` on `{review_ref}` with:

- `action_items` — one entry per record filed above, each carrying `description`, `severity`, `status` = `open`, and the record ref in `closure_evidence`
- `outcome` — `approved` (nothing outstanding), `approved_with_actions` (records filed, none blocking), or `follow_up_required` (a blocking finding is unresolved)
- `status` — `completed`

### 6. Completion summary

> **Review complete.**
>
> - Review record: `{review_ref}`
> - Patches applied: \<applied_count\>
> - Issues filed: \<issue_count\>
> - Action items filed: \<action_count\>
> - Dismissed as noise: \<R\>

### 7. Next steps

Present the user with follow-up options:

> **What would you like to do next?**
>
> 1. **Address the filed records** — pick up the issues just created
> 2. **Re-run code review** — review again after changes
> 3. **Done** — end the workflow

**HALT** — wait for the user's choice. Do not proceed until they select an option.

## On Complete

If the invoking workflow declared an `on_complete` instruction, follow it as the final terminal step before exiting.
