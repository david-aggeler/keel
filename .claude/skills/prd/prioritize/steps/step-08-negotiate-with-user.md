<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 -->
# Step 8: Negotiate the Negotiable Items with the User

**Goal:** Present the draft plan to the user and iterate on the NEGOTIABLE steps until they accept it. Never silently reorder — every change happens with explicit user blessing.

## The presentation

Present the plan as a numbered list. Each step entry shows:

- Step number
- Step title
- Label: `[TECHNICAL NECESSITY]` (with the downstream dependency named) or `[NEGOTIABLE]`
- One-paragraph summary (the ~50–80 words from the draft)
- FRs covered (rough list)
- Depends on (the earlier steps it needs)

Format (as Winston, on a whiteboard):

```
🏗️ Here's the draft execution plan. {N} steps, {K} negotiable.

Step 1 — Base Infrastructure  [TECHNICAL NECESSITY — required by everything]
{paragraph + FR refs + deps}

Step 2 — Deployment-Feedback Loop  [TECHNICAL NECESSITY — required by everything]
{paragraph + FR refs + deps}

Step 3 — vApp CRUD against the mock  [NEGOTIABLE]
{paragraph + FR refs + deps}

Step 4 — IPAM and DNS  [TECHNICAL NECESSITY — required by Step 5 (RDP/VNC)]
{paragraph + FR refs + deps}

...

Step {N} — {final step}  [NEGOTIABLE]
{paragraph + FR refs + deps}

The {K} negotiable steps are: 3, 6, 7, 9, ...

Trade-offs you might want to weigh in on:
- {step A vs step B — why one might go first}
- {step C — could ship earlier if you're willing to accept {tradeoff}}
- {step D — could defer if {capability} is not needed yet}

Which negotiable items would you reorder, drop, or add? Or is this draft acceptable as-is?
```

## The negotiation loop

Wait for the user's response. They have four kinds of moves:

1. **Accept as-is.** "Looks good, ship it." → Proceed to Step 9 (write output).
2. **Reorder.** "Move step 7 to step 4." → Verify the move doesn't violate any TECHNICAL NECESSITY ordering. If it does, push back: "Step 7 depends on Step 5's contract; can't move it before Step 5. Want to move it to position 5+1?". If the move is legal, apply it, renumber the affected steps, re-display the plan, ask again.
3. **Drop.** "Step 12 is post-v1 for me — drop it." → Move it to the "out of scope at this time" list with a noted reason. Re-display, ask again.
4. **Add.** "I want a step for {capability} between 6 and 7." → Treat it as a new cluster. Estimate its FRs (or ask the user to point at the PRD section). Apply the dependency test from Step 7's rules to label it. Insert. Re-display. Ask again.

Loop until the user explicitly accepts.

## Important behaviours

- **Never reorder silently.** Even if the user's request implies a downstream change you can see is necessary, surface it: "Moving Step 7 means Step 9 (which depends on 7) shifts to 8. OK?" — wait for blessing.
- **Push back on dependency violations explicitly.** If a user wants something that breaks the dependency graph, name the conflict ("Step 5 depends on Step 4's IPAM contract; can't ship 5 before 4") and offer alternatives.
- **Track the deltas from the draft.** When the user lands on a final order, note which steps moved from the original draft and why. This goes in the output file's "Negotiation history" section.
- **Be measured, not insistent.** Winston voice. Trade-offs not verdicts. The user is the decision-maker.

## Actions

1. **Display the plan** in the format above.

2. **Ask explicitly:** "Which negotiable items would you reorder, drop, or add?" Wait for response.

3. **Iterate** through reorder/drop/add moves until the user accepts. Track the history of changes.

4. **Confirm before proceeding.** Final ask: "Plan locked? I'll write it to `{default_output_file}` as input for the epic and sprint creators." Wait for explicit yes.

## Hand-off

Once the user accepts, proceed to `steps/step-09-write-output.md`.
