<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 2: Resume Existing Security Review

## YOUR TASK

An existing security review document was detected. Determine where the previous run left off and resume from the correct step — do not start over.

---

## SEQUENCE

### 1. Read Frontmatter

Load the existing `security-review.md` and read its frontmatter, especially:
- `stepsCompleted` — array of completed step numbers
- `scope` — the previously confirmed scope
- `mvp_baseline_mode` — the previously chosen posture
- `inputDocuments` — what was loaded last time

### 2. Refresh Inputs

Re-read all `inputDocuments` from disk. They may have changed since the last run — the architecture or API specs evolve. If a new file exists in `api/` or `./` that wasn't in `inputDocuments`, mention it.

### 3. Determine Resume Point

Find the largest step number in `stepsCompleted`. The next step is `max + 1`.

| Last completed | Resume at |
|---|---|
| 1 | step-03-surface.md |
| 3 | step-04-controls.md |
| 4 | step-05-threats.md |
| 5 | step-06-scoring.md |
| 6 | step-07-mitigations.md |
| 7 | step-08-arch-feedback.md |
| 8 | step-09-complete.md |
| 9 | already complete — see below |

If `stepsCompleted` already contains `9`, the review is complete. Offer the user three options:
- **Re-finalize** (regenerate executive summary and party brief from current data)
- **Update a section** (name the section; you'll regenerate it and any downstream sections)
- **Start fresh** (archive the existing file as `security-review.archived-{date}.md` and run step-01-init.md from scratch)

Note: step-02 (this file) is the resume handler and is not stored in `stepsCompleted`. Stored values are integers from step-01=1, step-03=3, step-04=4, …, step-09=9.

### 4. Brief the User

```
Resuming security review.

Document: ./security-review.md
Scope: [scope from frontmatter]
MVP-baseline mode: [on/off]
Last completed step: [N]
[If any input file changed since last run: "Input changed: [file] — I'll reflect this in the next pass."]

Next: [name of next step]

[C] Continue
```

Wait for `[C]`, then load the next step file.

## SUCCESS METRICS

✅ Frontmatter read correctly
✅ Inputs refreshed from disk
✅ Resume point computed from `stepsCompleted`
✅ User briefed on where we are and what's next
✅ Wait for `[C]` before advancing

## FAILURE MODES

❌ Restarting from step 1 when later steps are already complete
❌ Skipping the input-refresh check (stale specs lead to stale findings)
❌ Auto-advancing past `[C]`
