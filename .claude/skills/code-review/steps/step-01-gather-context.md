# Step 1: Gather Context

## SESSION VARIABLES

These are set as this workflow runs and read by later steps. They live in the session, not in any file.

| Variable | Set in | Meaning |
|---|---|---|
| `{diff_output}` | Step 1 | The unified diff under review |
| `{subject_ref}` | Step 1 | Ref of the record under review (e.g. `change_request-<n>`, `story-<n>`), or empty |
| `{review_mode}` | Step 1 | `full` when `{subject_ref}` is set, otherwise `no-spec` |
| `{failed_layers}` | Step 2 | Comma-separated review layers that failed or returned empty |
| `{review_ref}` | Step 4 | Ref of the `formal_review` record this run creates |

## RULES

- The prompt that triggered this workflow IS the intent — not a hint.
- Do not modify any files. This step is read-only.

## INSTRUCTIONS

1. **Find the review target.** The conversation context before this skill was triggered IS your starting point — not a blank slate. Check in this order — stop as soon as the review target is identified:

   **Tier 1 — Explicit argument.**
   Did the user pass a PR, commit SHA, branch, record ref, or diff source this message?

   - PR reference → resolve to branch/commit via `gh pr view`. If resolution fails, ask for a SHA or branch.
   - Commit or branch → use directly.
   - Record ref (`change_request-<n>`, `story-<n>`, `issue-<n>`) → set `{subject_ref}`. Read the record; if it names a branch or a baseline commit, use it as the diff source. If it does not, continue the cascade — a record alone does not identify a diff source.
   - Also scan the argument for diff-mode keywords that narrow the scope:
     - "staged" / "staged changes" → Staged changes only
     - "uncommitted" / "working tree" / "all changes" → Uncommitted changes (staged + unstaged)
     - "branch diff" / "vs main" / "against main" / "compared to \<branch\>" → Branch diff (extract base branch if mentioned)
     - "commit range" / "last N commits" / "\<from-sha\>..\<to-sha\>" → Specific commit range
     - "this diff" / "provided diff" / "paste" → User-provided diff (do not match bare "diff" — it appears in other modes)
   - When multiple keywords match, prefer the most specific (e.g. "branch diff" over bare "diff").

   **Tier 2 — Recent conversation.**
   Do the last few messages reveal what the user wants reviewed? Look for record refs, commit refs, branches, PRs, or descriptions of a change. Apply the same diff-mode keyword scan and routing as Tier 1.

   **Tier 3 — Record query.**
   Call `list_change_request` with `filter={"status":"in_review"}` scoped to the active product, then `list_story` the same way. Scan the results:

   - **Exactly one `in_review` record:** Set `{subject_ref}` to its ref. Suggest it: "I found \<ref\> in `in_review` status. Would you like to review its changes? [Y] Yes / [N] No, let me choose". If confirmed, use the record to determine the diff source (its branch, or uncommitted changes). If declined, clear `{subject_ref}` and fall through.
   - **Multiple `in_review` records:** Present them as numbered options alongside a manual choice option. Wait for user selection. If a record is selected, set `{subject_ref}` and use it to determine the diff source. If manual choice is selected, clear `{subject_ref}` and fall through.
   - **None:** Fall through.

   **Tier 4 — Current git state.**
   If version control is unavailable, skip to Tier 5. Otherwise check the current branch and HEAD. If the branch is not `main` (or the default branch), confirm: "I see HEAD is `\<short-sha\>` on `\<branch\>` — do you want to review this branch's changes?" If confirmed, treat as a branch diff against `main`. If declined, fall through.

   **Tier 5 — Ask.**
   Fall through to instruction 2.

   Never ask extra questions beyond what the cascade prescribes. If a tier above already identified the target, skip the remaining tiers and proceed to instruction 3.

2. HALT. Ask the user: **What do you want to review?** Present these options:

   - **Uncommitted changes** (staged + unstaged)
   - **Staged changes only**
   - **Branch diff** vs a base branch (ask which base branch)
   - **Specific commit range** (ask for the range)
   - **Provided diff or file list** (user pastes or provides a path)

3. Construct `{diff_output}` from the chosen source.

   - For **staged changes only**: run `git diff --cached`.
   - For **uncommitted changes** (staged + unstaged): run `git diff HEAD`.
   - For **branch diff**: verify the base branch exists before running `git diff`. If it does not exist, HALT and ask the user for a valid branch.
   - For **commit range**: verify the range resolves. If it does not, HALT and ask the user for a valid range.
   - For **provided diff**: validate the content is non-empty and parseable as a unified diff. If it is not parseable, HALT and ask the user to provide a valid diff.
   - For **file list**: validate each path exists in the working tree. Construct `{diff_output}` by running `git diff HEAD -- <path1> <path2> ...`. If any paths are untracked (new files not yet staged), use `git diff --no-index /dev/null <path>` to include them. If the diff is empty (files have no uncommitted changes and are not untracked), ask the user whether to review the full file contents or to specify a different baseline.
   - After constructing `{diff_output}`, verify it is non-empty regardless of source type. If empty, HALT and tell the user there is nothing to review.

4. **Set the acceptance context.**

   - If `{subject_ref}` is already set (from Tier 1, 2, or 3): read the record, follow it one hop to its acceptance contract (a `change_request` to its `requirements` and their `ac` records; a `story` to its acceptance criteria), and set `{review_mode}` = `full`.
   - Otherwise ask the user: **Is there a record that provides the acceptance context for these changes?**
     - If yes: set `{subject_ref}` to the ref given, read it and its acceptance contract, and set `{review_mode}` = `full`.
     - If no: set `{review_mode}` = `no-spec`.

5. If `{review_mode}` = `full`, also load the records the subject links out to (`related`, `depends_on`, the parent issue for a fix CR). Report any ref that does not resolve rather than silently skipping it.

6. Sanity check: if `{diff_output}` exceeds approximately 3000 lines, warn the user and offer to chunk the review by file group.

   - If the user opts to chunk: agree on the first group, narrow `{diff_output}` accordingly, and list the remaining groups for the user to note for follow-up runs.
   - If the user declines: proceed as-is with the full diff.

### CHECKPOINT

Present a summary before proceeding: diff stats (files changed, lines added/removed), `{review_mode}`, `{subject_ref}`, and the acceptance criteria loaded. HALT and wait for user confirmation to proceed.

## NEXT

Read fully and follow `./step-02-review.md`
