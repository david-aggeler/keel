---
outputFile: './implementation-readiness-report-{date}.md'
---

# Step 5: Epic Quality Review

## STEP GOAL

To validate epics and units against the best practices defined in create-epics-and-units workflow, focusing on user value, independence, dependencies, and implementation readiness.

## MANDATORY EXECUTION RULES (READ FIRST)

### Universal Rules

- 🛑 NEVER generate content without user input
- 📖 CRITICAL: Read the complete step file before taking any action
- 🔄 CRITICAL: When loading next step with 'C', ensure entire file is read
- 📋 YOU ARE A FACILITATOR, not a content generator
- ✅ YOU MUST ALWAYS SPEAK OUTPUT In your Agent communication style with the config `{communication_language}`

### Role Reinforcement

- ✅ You are an EPIC QUALITY ENFORCER
- ✅ You know what good epics look like - challenge anything deviating
- ✅ Technical epics are wrong - find them
- ✅ Forward dependencies are forbidden - catch them
- ✅ Units must be independently completable

### Step-Specific Rules

- 🎯 Apply create-epics-and-units standards rigorously
- 🚫 Don't accept "technical milestones" as epics
- 💬 Challenge every dependency on future work
- 🚪 Verify proper unit sizing and structure

## EXECUTION PROTOCOLS

- 🎯 Systematically validate each epic and unit
- 💾 Document all violations of best practices
- 📖 Check every dependency relationship
- 🚫 FORBIDDEN to accept structural problems

## EPIC QUALITY REVIEW PROCESS

### 1. Initialize Best Practices Validation

"Beginning **Epic Quality Review** against create-epics-and-units standards.

I will rigorously validate:

- Epics deliver user value (not technical milestones)
- Epic independence (Epic 2 doesn't need Epic 3)
- Unit dependencies (no forward references)
- Proper unit sizing and completeness

Any deviation from best practices will be flagged as a defect."

### 2. Epic Structure Validation

#### A. User Value Focus Check

For each epic:

- **Epic Title:** Is it user-centric (what user can do)?
- **Epic Goal:** Does it describe user outcome?
- **Value Proposition:** Can users benefit from this epic alone?

**Red flags (violations):**

- "Setup Database" or "Create Models" - no user value
- "API Development" - technical milestone
- "Infrastructure Setup" - not user-facing
- "Authentication System" - borderline (is it user value?)

#### B. Epic Independence Validation

Test epic independence:

- **Epic 1:** Must stand alone completely
- **Epic 2:** Can function using only Epic 1 output
- **Epic 3:** Can function using Epic 1 & 2 outputs
- **Rule:** Epic N cannot require Epic N+1 to work

**Document failures:**

- "Epic 2 requires Epic 3 features to function"
- Units in Epic 2 referencing Epic 3 components
- Circular dependencies between epics

### 3. Unit Quality Assessment

#### A. Unit Sizing Validation

Check each unit:

- **Clear User Value:** Does the unit deliver something meaningful?
- **Independent:** Can it be completed without future units?

**Common violations:**

- "Setup all models" - not a USER story
- "Create login UI (depends on Unit 1.3)" - forward dependency

#### B. Acceptance Criteria Review

For each unit's ACs:

- **Given/When/Then Format:** Proper BDD structure?
- **Testable:** Each AC can be verified independently?
- **Complete:** Covers all scenarios including errors?
- **Specific:** Clear expected outcomes?

**Issues to find:**

- Vague criteria like "user can login"
- Missing error conditions
- Incomplete happy path
- Non-measurable outcomes

### 4. Dependency Analysis

#### A. Within-Epic Dependencies

Map unit dependencies within each epic:

- Unit 1.1 must be completable alone
- Unit 1.2 can use Unit 1.1 output
- Unit 1.3 can use Unit 1.1 & 1.2 outputs

**Critical violations:**

- "This unit depends on Unit 1.4"
- "Wait for future unit to work"
- Units referencing features not yet implemented

#### B. Database/Entity Creation Timing

Validate database creation approach:

- **Wrong:** Epic 1 Unit 1 creates all tables upfront
- **Right:** Each unit creates tables it needs
- **Check:** Are tables created only when first needed?

### 5. Special Implementation Checks

#### A. Starter Template Requirement

Check if Architecture specifies starter template:

- If YES: Epic 1 Unit 1 must be "Set up initial project from starter template"
- Verify unit includes cloning, dependencies, initial configuration

#### B. Greenfield vs Brownfield Indicators

Greenfield projects should have:

- Initial project setup unit
- Development environment configuration
- CI/CD pipeline setup early

Brownfield projects should have:

- Integration points with existing systems
- Migration or compatibility units

### 6. Best Practices Compliance Checklist

For each epic, verify:

- [ ] Epic delivers user value
- [ ] Epic can function independently
- [ ] Units appropriately sized
- [ ] No forward dependencies
- [ ] Database tables created when needed
- [ ] Clear acceptance criteria
- [ ] Traceability to FRs maintained

### 7. Quality Assessment Documentation

Document all findings by severity:

#### 🔴 Critical Violations

- Technical epics with no user value
- Forward dependencies breaking independence
- Epic-sized units that cannot be completed

#### 🟠 Major Issues

- Vague acceptance criteria
- Units requiring future units
- Database creation violations

#### 🟡 Minor Concerns

- Formatting inconsistencies
- Minor structure deviations
- Documentation gaps

### 8. Autonomous Review Execution

This review runs autonomously to maintain standards:

- Apply best practices without compromise
- Document every violation with specific examples
- Provide clear remediation guidance
- Prepare recommendations for each issue

## REVIEW COMPLETION

After completing epic quality review:

- Update {outputFile} with all quality findings
- Document specific best practices violations
- Provide actionable recommendations
- Load ./step-06-final-assessment.md for final readiness assessment

## CRITICAL STEP COMPLETION NOTE

This step executes autonomously. Load ./step-06-final-assessment.md only after complete epic quality review is documented.

---

## 🚨 SYSTEM SUCCESS/FAILURE METRICS

### ✅ SUCCESS

- All epics validated against best practices
- Every dependency checked and verified
- Quality violations documented with examples
- Clear remediation guidance provided
- No compromise on standards enforcement

### ❌ SYSTEM FAILURE

- Accepting technical epics as valid
- Ignoring forward dependencies
- Not verifying unit sizing
- Overlooking obvious violations

**Master Rule:** Enforce best practices rigorously. Find all violations.
