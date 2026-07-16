<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 2: Project Context Analysis

## MANDATORY EXECUTION RULES (READ FIRST):

- 🛑 NEVER generate content without user input

- 📖 CRITICAL: ALWAYS read the complete step file before taking any action - partial understanding leads to incomplete decisions
- 🔄 CRITICAL: When loading next step with 'C', ensure the entire file is read and understood before proceeding
- ✅ ALWAYS treat this as collaborative discovery between architectural peers
- 📋 YOU ARE A FACILITATOR, not a content generator
- 💬 FOCUS on understanding project scope and requirements for architecture
- 🎯 ANALYZE loaded documents, don't assume or generate requirements
- ⚠️ ABSOLUTELY NO TIME ESTIMATES - AI development speed has fundamentally changed
- ✅ YOU MUST ALWAYS SPEAK OUTPUT In your Agent communication style with the config `{communication_language}`

## EXECUTION PROTOCOLS:

- 🎯 Show your analysis before taking any action
- ⚠️ Present A/P/C menu after generating project context analysis
- 💾 ONLY save when user chooses C (Continue)
- 📖 Update frontmatter `stepsCompleted: [1, 2]` before loading next step
- 🚫 FORBIDDEN to load next step until C is selected

## COLLABORATION OPTIONS:

This step will generate content and present choices:

- **R (Refine)**: Dive deeper into specific architectural implications or add detail before saving
- **C (Continue)**: Save the content to the document and proceed to next step

## CONTEXT BOUNDARIES:

- Current document and frontmatter from step 1 are available
- Input documents already loaded are in memory (PRD, epics, UX spec, etc.)
- Focus on architectural implications of requirements
- No technology decisions yet - pure analysis phase

## YOUR TASK:

Fully read and Analyze the loaded project documents to understand architectural scope, requirements, and constraints before beginning decision making.

## CONTEXT ANALYSIS SEQUENCE:

### 1. Review Project Requirements

**From PRD Analysis:**

- Extract and analyze Functional Requirements (FRs)
- Identify Non-Functional Requirements (NFRs) like performance, security, compliance
- Note any technical constraints or dependencies mentioned
- Count and categorize requirements to understand project scale

**From Epics/Stories (if available):**

- Map epic structure and user stories to architectural components
- Extract acceptance criteria for technical implications
- Identify cross-cutting concerns that span multiple epics
- Estimate story complexity for architectural planning

**From UX Design (if available):**

- Extract architectural implications from UX requirements:
  - Component complexity (simple forms vs rich interactions)
  - Animation/transition requirements
  - Real-time update needs (live data, collaborative features)
  - Platform-specific UI requirements
  - Accessibility standards (WCAG compliance level)
  - Responsive design breakpoints
  - Offline capability requirements
  - Performance expectations (load times, interaction responsiveness)

### 2. Project Scale Assessment

Calculate and present project complexity:

**Complexity Indicators:**

- Real-time features requirements
- Multi-tenancy needs
- Regulatory compliance requirements
- Integration complexity
- User interaction complexity
- Data complexity and volume

### 3. Reflect Understanding

Present your analysis back to user for validation:

"I'm reviewing your project documentation for openbrain.

{if_epics_loaded}I see {epic_count} epics with {story_count} total stories.{/if_epics_loaded}
{if_no_epics}I found {fr_count} functional requirements organized into {fr_category_list}.{/if_no_epics}
{if_ux_loaded}I also found your UX specification which defines the user experience requirements.{/if_ux_loaded}

**Key architectural aspects I notice:**

- [Summarize core functionality from FRs]
- [Note critical NFRs that will shape architecture]
- {if_ux_loaded}[Note UX complexity and technical requirements]{/if_ux_loaded}
- [Identify unique technical challenges or constraints]
- [Highlight any regulatory or compliance requirements]

**Scale indicators:**

- Project complexity appears to be: [low/medium/high/enterprise]
- Primary technical domain: [web/mobile/api/backend/full-stack/etc]
- Cross-cutting concerns identified: [list major ones]

This analysis will help me guide you through the architectural decisions needed to ensure AI agents implement this consistently.

Does this match your understanding of the project scope and requirements?"

### 4. Generate Project Context Content

Prepare the content to append to the document:

#### Content Structure:

```markdown
## Project Context Analysis

### Requirements Overview

**Functional Requirements:**
{{analysis of FRs and what they mean architecturally}}

**Non-Functional Requirements:**
{{NFRs that will drive architectural decisions}}

**Scale & Complexity:**
{project_scale_assessment}

- Primary domain: {technical_domain}
- Complexity level: {complexity_level}
- Estimated architectural components: {component_count}

### Technical Constraints & Dependencies

{known_constraints_dependencies}

### Cross-Cutting Concerns Identified

{concerns_that_will_affect_multiple_components}
```

### 5. Present Content and Menu

Show the generated content and present choices:

"I've drafted the Project Context Analysis based on your requirements. This sets the foundation for our architectural decisions.

**Here's what I'll add to the document:**

[Show the complete markdown content from step 4]

**What would you like to do?**
[R] Refine - Dive deeper into specific architectural implications
[C] Continue - Save this analysis and begin architectural decisions"

### 6. Handle Menu Selection

#### If 'R' (Refine):

- Ask the user what aspect they want to develop further
- Process the additional input and update the content
- Return to the R/C menu

#### If 'C' (Continue):

- Append the final content to the active `architecture_description` chapter with `update_architecture_description`
- Update frontmatter: `stepsCompleted: [1, 2]`
- Load `./step-04-starter.md`

## APPEND TO DOCUMENT:

When user selects 'C', append the content directly to the document using the structure from step 4.

## SUCCESS METRICS:

✅ All input documents thoroughly analyzed for architectural implications
✅ Project scope and complexity clearly assessed and validated
✅ Technical constraints and dependencies identified
✅ Cross-cutting concerns mapped for architectural planning
✅ User confirmation of project understanding
✅ A/P/C menu presented and handled correctly
✅ Content properly appended to document when C selected

## FAILURE MODES:

❌ Skimming documents without deep architectural analysis
❌ Missing or misinterpreting critical NFRs
❌ Not validating project understanding with user
❌ Underestimating complexity indicators
❌ Generating content without real analysis of loaded documents
❌ Not presenting A/P/C menu after content generation

❌ **CRITICAL**: Reading only partial step file - leads to incomplete understanding and poor decisions
❌ **CRITICAL**: Proceeding with 'C' without fully reading and understanding the next step file
❌ **CRITICAL**: Making decisions without complete understanding of step requirements and protocols

## NEXT STEP:

After user selects 'C' and content is saved to document, load `./step-04-starter.md` to evaluate starter template options.

Remember: Do NOT proceed to step-04 until user explicitly selects 'C' from the A/P/C menu and content is saved!
