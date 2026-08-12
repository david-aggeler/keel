# Step 2: Review

## RULES

- The Blind Hunter subagent receives NO project context — diff only.
- The Edge Case Hunter subagent receives diff and project read access.
- The Acceptance Auditor subagent receives diff, the subject record, and its acceptance criteria.
- All review subagents must run at the same model capability as the current session.

## INSTRUCTIONS

1. If `{review_mode}` = `no-spec`, note to the user: "Acceptance Auditor skipped — no acceptance context provided."

2. Launch the review layers as parallel subagents, each without conversation context.

   - **Blind Hunter** — receives `{diff_output}` only. No acceptance criteria, no context records, no project access. Spawn the `adversarial-reviewer` subagent (Cassandra) with that constrained input.
   - **Edge Case Hunter** — receives `{diff_output}` and read access to the project. Invoke via the `edge-case-hunter` skill.
   - **Acceptance Auditor** (only if `{review_mode}` = `full`) — receives `{diff_output}`, the subject record, and its acceptance criteria. Its prompt:
     > You are an Acceptance Auditor. Review this diff against the acceptance criteria and context records. Check for: unmet acceptance criteria, deviations from stated intent, specified behavior that is missing or stubbed, and contradictions between a stated constraint and the actual code. Output findings as a Markdown list. Each finding: one-line title, which AC or constraint it violates, and evidence from the diff.

3. **If subagents are not available**, do not write prompt files into the repository. Print each reviewer prompt above inline, in full, one fenced block per role, and HALT. Ask the user to run each in a separate session (ideally a different LLM) and paste the findings back. When findings are pasted, resume from this point and proceed to step 3.

4. **Subagent failure handling**: if any subagent fails, times out, or returns empty results, append the layer name to `{failed_layers}` (comma-separated) and proceed with findings from the remaining layers.

5. Collect all findings from the completed layers.

## NEXT

Read fully and follow `./step-03-triage.md`
