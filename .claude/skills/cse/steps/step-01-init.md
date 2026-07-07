<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 1: Security Review Initialization

## MANDATORY EXECUTION RULES

- 🛑 NEVER generate threats, control gaps, or scores in this step — those are Steps 2–5
- 📖 Read this entire step file before taking any action
- 🚪 CHECK for an existing security review document first — if found, load step-02-continue.md immediately
- ✅ SPEAK in `{communication_language}` for all user-facing output
- 📋 Your only job here is scoping, document discovery, and initialization

## CONTEXT BOUNDARIES

- Architecture document is **required** — do not proceed without it
- OpenAPI and AsyncAPI specs are **required** — they're the actual contract callers depend on; the threat surface is undercounted without them
- PRD and project-context are valuable but optional
- Scope can be the full system or a named subsystem; clarify if ambiguous
- `.` is the output folder — security review document lives here

## YOUR TASK

Initialize the security review workflow: detect continuation, load inputs, confirm scope, create the output document.

---

## INITIALIZATION SEQUENCE

### 1. Check for Existing Security Review

Look for `./*security-review*.md` or `./*threat-model*.md` (including sharded folders with `index.md`).

- **Found with `stepsCompleted` in frontmatter** → load `./step-02-continue.md` immediately. Stop here.
- **Found without `stepsCompleted`** → treat as fresh (may be a stale file from an aborted run); confirm with user before overwriting.
- **Not found** → proceed with fresh initialization below.

### 2. Input Document Discovery

Discover and load documents from: `./`, `./`, `docs/`, `api/`

| Document | Pattern | Required? |
|----------|---------|-----------|
| Architecture | `*architecture*.md` | **Yes** — abort without it |
| OpenAPI spec | `api/openapi.yaml` | **Yes** — every HTTP endpoint is part of the attack surface |
| AsyncAPI spec | `api/asyncapi.yaml` | **Yes** — every event channel is part of the attack surface |
| PRD | `*prd*.md` | Recommended — context for what data matters |
| Project Context | `**/project-context.md` | Recommended |
| DFMEA (if exists) | `*dfmea*.md` | Optional — cross-reference reliability findings to avoid double-counting |
| UX Design | `*ux-design*.md` | Optional |

The API specs are the authoritative contract for what the system promises to callers and consumers. Load them fully — they're where authn/authz coverage gaps, error-code leakage, and unbounded inputs become visible. The architecture document alone won't surface those.

If a DFMEA exists, load its risk register so you can cross-reference: failure modes Vera already mitigated should be acknowledged, not re-litigated. Sera's findings focus on **adversarial** paths; Vera's focus on **reliability** paths. Overlap exists (e.g., DoS) but the framing differs.

For sharded documents (folder + `index.md`): load the index first, then all section files.

**If the architecture document is not found:**
> "I can't start the security review without the architecture document. Please run `architecture-create` first, or provide the path."
> Stop. Do not proceed.

**If OpenAPI or AsyncAPI specs are missing**, warn the user but offer to proceed with reduced coverage — the API/event surface findings will be marked "spec not loaded — review skipped" rather than guessed.

### 3. Confirm Inputs and Define Scope

Present what was found and ask one question:

```
I found the following inputs:
- Architecture:   [filename] ✓
- OpenAPI spec:   [filename or "not found — HTTP API coverage will be limited"]
- AsyncAPI spec:  [filename or "not found — event/message coverage will be limited"]
- DFMEA:          [filename or "not found — no reliability cross-reference"]
- PRD:            [filename or "not found"]
- Project Context:[filename or "not found"]

Posture: MVP-baseline mode is [on/off]. [If on:]
  Findings beyond Vela's MVP cybersecurity baseline (formal SBOM signing,
  ISO 27001 / NIS2 / EU CRA / SOC 2 controls, etc.) will be parked in the
  Deferred-to-Growth section, not in MVP findings.

Before I begin, one question: should I review the full system, or focus on
a specific subsystem (e.g. just the appliance config API, just the
hypervisor adapter)?
(Press Enter or type "full system" to review everything)
```

Wait for the user's response.

### 4. Create Output Document

Copy `../security-review-template.md` to `./security-review.md`.

Populate frontmatter:
- `project`: from config or architecture doc title
- `scope`: the user's answer from step 3
- `mvp_baseline_mode`: from `True`
- `score_critical`: from `15`
- `score_major`: from `8`
- `inputDocuments`: list of loaded files
- `stepsCompleted`: `[1]`

If full-compliance mode is on (mvp_baseline_mode = false), note in the document header: "Full-compliance mode — regulatory items are scored alongside MVP findings."

### 5. Report and Hand Off

```
Security review workspace initialized.

Document: ./security-review.md
Scope: [user-confirmed scope]
MVP-baseline mode: [on/off]
Architecture loaded: [filename]
API specs loaded: [openapi: yes/no, asyncapi: yes/no]
DFMEA cross-reference: [yes/no]

Next: I'll map the attack surface — components, trust boundaries,
data flows, identities, and external dependencies.

[C] Continue to attack-surface mapping
```

Wait for `[C]`.

## SUCCESS METRICS

✅ Existing review detected and handed to step-02-continue correctly
✅ Architecture document loaded (or workflow aborted cleanly)
✅ API spec status confirmed (loaded or warned-and-skipped)
✅ Scope confirmed with user
✅ Output document created from template with correct frontmatter
✅ `stepsCompleted: [1]` written to frontmatter

## FAILURE MODES

❌ Proceeding without an architecture document
❌ Silently skipping API specs without warning the user that coverage will be reduced
❌ Overwriting an existing security review without user confirmation
❌ Generating any threats or control findings in this step
❌ Proceeding to Step 2 without user pressing `[C]`

## NEXT STEP

After `[C]`: load `./step-03-surface.md`
