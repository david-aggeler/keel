---
outputFile: './implementation-readiness-report-{date}.md'
---
<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 -->

# Step 3: Epic Coverage Validation

## STEP GOAL

To validate that all Functional Requirements from the PRD are captured in the epics and units document, identifying any gaps in coverage.

## MANDATORY EXECUTION RULES (READ FIRST)

### Universal Rules

- 🛑 NEVER generate content without user input
- 📖 CRITICAL: Read the complete step file before taking any action
- 🔄 CRITICAL: When loading next step with 'C', ensure entire file is read
- 📋 YOU ARE A FACILITATOR, not a content generator
- ✅ YOU MUST ALWAYS SPEAK OUTPUT In your Agent communication style with the config `{communication_language}`

### Role Reinforcement

- ✅ You are an expert Product Manager
- ✅ Your expertise is in requirements traceability
- ✅ You ensure no requirements fall through the cracks
- ✅ Success is measured in complete FR coverage

### Step-Specific Rules

- 🎯 Focus ONLY on FR coverage validation
- 🚫 Don't analyze unit quality (that's later)
- 💬 Compare PRD FRs against epic coverage list
- 🚪 Document every missing FR

## EXECUTION PROTOCOLS

- 🎯 Load epics document completely
- 💾 Extract FR coverage from epics
- 📖 Compare against PRD FR list
- 🚫 FORBIDDEN to proceed without documenting gaps

## EPIC COVERAGE VALIDATION PROCESS

### 1. Initialize Coverage Validation

"Beginning **Epic Coverage Validation**.

I will:

1. Query epic and unit records via `list_epic` and `list_change_request`
2. Extract FR coverage information from epic/unit `details`
3. Compare against PRD FRs from previous step
4. Identify any FRs not covered in epics"

### 2. Load Epic and Unit Records

Call `list_epic` to retrieve all epics. For each epic ref returned, call `list_change_request filter={"parent":"<ref>"}` to retrieve the full unit list. Read the `details` and `summary` fields of each record to find FR coverage information. Look for sections like "FR Coverage Map" or similar within the record content.

### 3. Extract Epic FR Coverage

From the epic and unit records:

- Find FR coverage mapping or list
- Extract which FR numbers are claimed to be covered
- Document which epics cover which FRs

Format as:

```
## Epic FR Coverage Extracted

FR1: Covered in Epic X
FR2: Covered in Epic Y
FR3: Covered in Epic Z
...
Total FRs in epics: [count]
```

### 4. Compare Coverage Against PRD

Using the PRD FR list from step 2:

- Check each PRD FR against epic coverage
- Identify FRs NOT covered in epics
- Note any FRs in epics but NOT in PRD

Create coverage matrix:

```
## FR Coverage Analysis

| FR Number | PRD Requirement | Epic Coverage  | Status    |
| --------- | --------------- | -------------- | --------- |
| FR1       | [PRD text]      | Epic X Unit Y | ✓ Covered |
| FR2       | [PRD text]      | **NOT FOUND**  | ❌ MISSING |
| FR3       | [PRD text]      | Epic Z Unit A | ✓ Covered |
```

### 5. Document Missing Coverage

List all FRs not covered:

```
## Missing FR Coverage

### Critical Missing FRs

FR#: [Full requirement text from PRD]
- Impact: [Why this is critical]
- Recommendation: [Which epic should include this]

### High Priority Missing FRs

[List any other uncovered FRs]
```

### 6. Add to Assessment Report

Append to {outputFile}:

```markdown
## Epic Coverage Validation

### Coverage Matrix

[Complete coverage matrix from section 4]

### Missing Requirements

[List of uncovered FRs from section 5]

### Coverage Statistics

- Total PRD FRs: [count]
- FRs covered in epics: [count]
- Coverage percentage: [percentage]
```

### 7. Auto-Proceed to Next Step

After coverage validation complete, immediately load next step.

## PROCEEDING TO UX ALIGNMENT

Epic coverage validation complete. Read fully and follow: `./step-04-ux-alignment.md`

---

## 🚨 SYSTEM SUCCESS/FAILURE METRICS

### ✅ SUCCESS

- Epic and unit records queried completely via `list_epic` / `list_change_request`
- FR coverage extracted accurately
- All gaps identified and documented
- Coverage matrix created

### ❌ SYSTEM FAILURE

- Not querying all epic and unit records via `list_epic` / `list_change_request`
- Missing FRs in comparison
- Not documenting uncovered requirements
- Incomplete coverage analysis

**Master Rule:** Every FR must have a traceable implementation path.
