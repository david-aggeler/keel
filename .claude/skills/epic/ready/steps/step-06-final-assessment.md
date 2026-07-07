---
outputFile: './implementation-readiness-report-{date}.md'
---

# Step 6: Final Assessment

## STEP GOAL

Compile all findings from previous steps, determine overall readiness verdict, and record the assessment as a `formal_review` record in the SoR. No local file is written — the record IS the deliverable.

## MANDATORY EXECUTION RULES (READ FIRST)

### Universal Rules

- 🛑 NEVER generate content without user input
- 📖 CRITICAL: Read the complete step file before taking any action
- 📖 You are at the final step - complete the assessment
- 📋 YOU ARE A FACILITATOR, not a content generator
- ✅ YOU MUST ALWAYS SPEAK OUTPUT In your Agent communication style with the config `{communication_language}`

### Role Reinforcement

- ✅ You are delivering the FINAL ASSESSMENT
- ✅ Your findings are objective and backed by evidence
- ✅ Provide clear, actionable recommendations
- ✅ Success is measured by value of findings

### Step-Specific Rules

- 🎯 Compile and summarize all findings from steps 1–5
- 🚫 Don't soften the message — be direct
- 💬 Provide specific examples for problems
- 🎯 Map verdict to `outcome` field: READY → approved, NEEDS WORK → approved_with_actions, NOT READY → rejected
- 💾 Record the assessment with `create_formal_review` — do NOT write a local markdown file

## EXECUTION PROTOCOLS

- 🎯 Review all findings from previous steps
- 📖 Determine overall readiness status (READY / NEEDS WORK / NOT READY)
- 💾 Call `create_formal_review` with the full assessment
- 🚫 Complete and present summary to user

## FINAL ASSESSMENT PROCESS

### 1. Initialize Final Assessment

"Completing **Final Assessment**.

I will now:

1. Review all findings from previous steps
2. Determine overall readiness status
3. Record the assessment as a formal review record"

### 2. Review Previous Findings

Gather findings from the assessment conversation so far:

- File and FR Validation findings (step 1–2)
- UX Alignment issues (step 3)
- Epic Quality violations (step 4–5)

### 3. Determine Verdict

Based on the severity and count of issues:

| Verdict | Condition | `outcome` value |
|---|---|---|
| **READY** | No critical issues; all gaps are minor | `approved` |
| **NEEDS WORK** | Significant gaps but core structure is sound | `approved_with_actions` |
| **NOT READY** | Critical gaps that block implementation | `rejected` |

### 4. Build Assessment Content

Compose the full assessment summary to be stored as `details` in the formal review:

```
## Overall Readiness Status: [READY / NEEDS WORK / NOT READY]

## Critical Issues Requiring Immediate Action
[List most critical issues that must be addressed — or "None" if READY]

## Recommended Next Steps
1. [Specific action item]
2. [Specific action item]
...

## Summary
This assessment identified [X] issues across [Y] categories.
[Brief synthesis of the readiness picture.]
```

### 5. Record the Formal Review

Call `create_formal_review` with:

- `product_id` — the product this epic belongs to (from workflow context)
- `title` — "Implementation Readiness Review: {epic_title}"
- `type` — `other`
- `type_other` — "epic readiness review"
- `subject_refs` — array containing the epic ref (e.g., `epic/{epic_id}`)
- `outcome` — mapped from verdict (see table in step 3)
- `status` — `completed`
- `summary` — one-sentence verdict: "Epic {title} assessed as [READY/NEEDS WORK/NOT READY] for implementation."
- `details` — the full assessment content from step 4

### 6. Present Completion

Display:
"**Implementation Readiness Assessment Complete**

Verdict: **[READY / NEEDS WORK / NOT READY]**

The assessment has been recorded as a formal review record.

[If NEEDS WORK or NOT READY:]
Address the critical issues listed above before proceeding to implementation."

## WORKFLOW COMPLETE

The implementation readiness workflow is now complete. The formal review record contains all findings and recommendations.

Implementation Readiness complete. See `bin/vela-dev help` for the devtool surface, or `/prd`, `/epic`, `/unit` for workflow dispatchers.

---

## 🚨 SYSTEM SUCCESS/FAILURE METRICS

### ✅ SUCCESS

- All findings from steps 1–5 compiled and summarized
- Verdict clearly determined (READY / NEEDS WORK / NOT READY)
- `create_formal_review` called with correct fields
- `outcome` correctly mapped from verdict
- `subject_refs` includes the epic ref
- User sees clear completion summary

### ❌ SYSTEM FAILURE

- Writing a local markdown file instead of calling `create_formal_review`
- Not reviewing previous findings
- Not mapping verdict to correct `outcome` enum value
- Missing `subject_refs` (epic must be linked)
- Incomplete or vague `details` field

## On Complete

If the resolved `workflow.on_complete` is non-empty, follow it as the final terminal instruction before exiting.
