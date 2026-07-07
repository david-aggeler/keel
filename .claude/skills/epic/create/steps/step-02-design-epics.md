<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 -->
# Step 2: Design Epic List

## STEP GOAL

To design and get approval for the epics_list that will organize all requirements into user-value-focused epics. Each approved epic will be created as a slim `epic` record via `create_epic`.

## MANDATORY EXECUTION RULES (READ FIRST)

### Universal Rules

- NEVER generate content without user input
- CRITICAL: Read the complete step file before taking any action
- CRITICAL: When loading next step with 'C', ensure entire file is read
- YOU ARE A FACILITATOR, not a content generator
- YOU MUST ALWAYS SPEAK OUTPUT In your Agent communication style with the config `{communication_language}`

### Role Reinforcement

- You are a product strategist and technical specifications writer
- If you already have been given communication or persona patterns, continue to use those while playing this new role
- We engage in collaborative dialogue, not command-response
- You bring product strategy and epic design expertise
- User brings their product vision and priorities

### Step-Specific Rules

- Focus ONLY on creating the epics_list and the epic records
- FORBIDDEN to create individual units in this step (units are dispatched to `/unit create` in Step 3)
- Organize epics around user value, not technical layers
- GET explicit approval for the epics_list before calling `create_epic`
- **CRITICAL: Each epic must be standalone and enable future epics without requiring future epics to function**

## EXECUTION PROTOCOLS

- Design epics collaboratively based on extracted requirements
- On user approval, call `get_template_for dto_type=epic` then `create_epic` for each approved epic
- Document the FR coverage mapping in working context
- FORBIDDEN to load next step until user approves epics_list and all epic records are created

## EPIC DESIGN PROCESS

### 1. Review Extracted Requirements

Review the working context from Step 1:

- **Functional Requirements:** Count and review FRs
- **Non-Functional Requirements:** Review NFRs that need to be addressed
- **Additional Requirements:** Review technical and UX requirements

### 2. Explain Epic Design Principles

**EPIC DESIGN PRINCIPLES:**

1. **User-Value First**: Each epic must enable users to accomplish something meaningful
2. **Requirements Grouping**: Group related FRs that deliver cohesive user outcomes
3. **Incremental Delivery**: Each epic should deliver value independently
4. **Logical Flow**: Natural progression from user's perspective
5. **Dependency-Free Within Epic**: Units within an epic must NOT depend on future units

**CRITICAL PRINCIPLE:**
Organize by USER VALUE, not technical layers:

**CORRECT Epic Examples (Standalone and Enable Future Epics):**

- Epic 1: User Authentication and Profiles (users can register, login, manage profiles) — **Standalone: Complete auth system**
- Epic 2: Content Creation (users can create, edit, publish content) — **Standalone: Uses auth, creates content**
- Epic 3: Social Interaction (users can follow, comment, like content) — **Standalone: Uses auth + content**
- Epic 4: Search and Discovery (users can find content and other users) — **Standalone: Uses all previous**

**WRONG Epic Examples (Technical Layers or Dependencies):**

- Epic 1: Database Setup (creates all tables upfront) — **No user value**
- Epic 2: API Development (builds all endpoints) — **No user value**
- Epic 3: Frontend Components (creates reusable components) — **No user value**
- Epic 4: Deployment Pipeline (CI/CD setup) — **No user value**

**DEPENDENCY RULES:**

- Each epic must deliver COMPLETE functionality for its domain
- Epic 2 must not require Epic 3 to function
- Epic 3 can build upon Epic 1 and 2 but must stand alone

### 3. Design Epic Structure Collaboratively

**Step A: Identify User Value Themes**

- Look for natural groupings in the FRs
- Identify user journeys or workflows
- Consider user types and their goals

**Step B: Propose Epic Structure**
For each proposed epic:

1. **Epic Title**: User-centric, value-focused
2. **User Outcome**: What users can accomplish after this epic
3. **FR Coverage**: Which FR numbers this epic addresses
4. **Implementation Notes**: Any technical or UX considerations

**Step C: Create the epics_list**

Format the epics_list as:

```
## Epic List

### Epic 1: [Epic Title]
[Epic goal statement — what users can accomplish]
**FRs covered:** FR1, FR2, FR3, etc.

### Epic 2: [Epic Title]
[Epic goal statement — what users can accomplish]
**FRs covered:** FR4, FR5, FR6, etc.

[Continue for all epics]
```

### 4. Present Epic List for Review

Display the complete epics_list to user with:

- Total number of epics
- FR coverage per epic
- User value delivered by each epic
- Any natural dependencies

### 5. Create Requirements Coverage Map

Create {requirements_coverage_map} showing how each FR maps to an epic:

```
### FR Coverage Map

FR1: Epic 1 — [Brief description]
FR2: Epic 1 — [Brief description]
FR3: Epic 2 — [Brief description]
...
```

This ensures no FRs are missed.

### 6. Collaborative Refinement

Ask user:

- "Does this epic structure align with your product vision?"
- "Are all user outcomes properly captured?"
- "Should we adjust any epic groupings?"
- "Are there natural dependencies we've missed?"

### 7. Get Final Approval

**CRITICAL:** Must get explicit user approval:
"Do you approve this epic structure for proceeding to unit creation?"

If user wants changes:

- Make the requested adjustments
- Update the epics_list
- Re-present for approval
- Repeat until approval is received

## CREATE EPIC RECORDS

After approval, call `get_template_for dto_type=epic` to get the authoritative field list, then for each approved epic call `create_epic` with:

- `title`: epic title
- `summary`: epic goal statement (one paragraph)
- `details`: epic context — FR coverage, user outcomes, implementation notes
- `plan`: reference to the development plan (dd_plan ref if available, or a brief plan note)
- `status`: `planned`

Store the returned epic refs in working context (e.g., `epic-1-ref`, `epic-2-ref`) — Step 3 creates thin units per epic.

Also store the requirements_coverage_map in working context.

### 8. Confirm and continue

Confirm with the operator that all epic records are created and the requirements coverage map is complete.

Ask: "Are these epics correct? Ready to proceed to unit decomposition? [yes/no/edit]"

Wait for confirmation. When confirmed, read fully and follow: `./step-03-create-units.md`

## CRITICAL STEP COMPLETION NOTE

ONLY WHEN C is selected and all epic records are created via `create_epic`, will you then read fully and follow: ./step-03-create-units.md to begin unit creation dispatch.

---

## SYSTEM SUCCESS/FAILURE METRICS

### SUCCESS

- Epics designed around user value
- All FRs mapped to specific epics
- epics_list created and formatted correctly
- Requirements coverage map completed
- User gives explicit approval for epic structure
- All epic records created via `create_epic` with status `planned`

### FAILURE

- Epics organized by technical layers
- Missing FRs in coverage map
- No user approval obtained
- Epic records not created

**Master Rule:** Skipping steps, optimizing sequences, or not following exact instructions is FORBIDDEN and constitutes SYSTEM FAILURE.
