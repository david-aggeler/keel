<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 MD025 MD041 -->
# Step 1: Security Review Initialization

## MANDATORY EXECUTION RULES

- 🛑 NEVER generate threats, control gaps, or scores in this step — those are Steps 2–5
- 📖 Read this entire step file before taking any action
- 🚪 CHECK for an existing security review document first — if found, load step-02-continue.md immediately
- ✅ SPEAK in `{communication_language}` for all user-facing output
- 📋 Your only job here is scoping, document discovery, and initialization

## CONTEXT BOUNDARIES

- gold architecture tree is **required** — do not proceed without a product `architecture_description` root
- OpenAPI and AsyncAPI specs are **required** — they're the actual contract callers depend on; the threat surface is undercounted without them
- PRD and project-context are valuable but optional
- Scope can be the full system or a named subsystem; clarify if ambiguous
- The product `threat_model` record is the working review artifact; `failure_mode` rows are the durable threat register

## YOUR TASK

Initialize the security review workflow: detect continuation, load inputs, confirm scope, create or load the product threat_model.

---

## INITIALIZATION SEQUENCE

### 1. Check for Existing Security Review

Call `list_threat_model product=<slug>` and locate the active product threat model.

- **Found with in-progress details** → load `./step-02-continue.md` immediately. Stop here.
- **Found without in-progress details** → load it as the current baseline and continue.
- **Not found** → proceed with fresh initialization below.

### 2. gold Architecture Discovery

Call `list_architecture_description product=<slug>` and locate the active root record. Then call `get_architecture_description` for the root and every chapter listed by the root's `chapters` field, in order. This root plus ordered chapters is the gold architecture_description tree and is the only architecture input.

Do not search for or read local architecture files. Local architecture markdown is not canonical for this workflow.

Discover and load optional interface/supporting documents from: `api/`, `docs/`

| Document | Pattern | Required? |
|----------|---------|-----------|
| Architecture | gold `architecture_description` root + chapters | **Yes** — abort without it |
| OpenAPI spec | `api/openapi.yaml` | **Yes** — every HTTP endpoint is part of the attack surface |
| AsyncAPI spec | `api/asyncapi.yaml` | **Yes** — every event channel is part of the attack surface |
| PRD | `*prd*.md` | Recommended — context for what data matters |
| Project Context | `**/project-context.md` | Recommended |
| DFMEA (if exists) | `*dfmea*.md` | Optional — cross-reference reliability findings to avoid double-counting |
| UX Design | `*ux-design*.md` | Optional |

The API specs are the authoritative contract for what the system promises to callers and consumers. Load them fully — they're where authn/authz coverage gaps, error-code leakage, and unbounded inputs become visible. The architecture document alone won't surface those.

If a DFMEA exists, load its risk register so you can cross-reference: failure modes Vera already mitigated should be acknowledged, not re-litigated. Sera's findings focus on **adversarial** paths; Vera's focus on **reliability** paths. Overlap exists (e.g., DoS) but the framing differs.

**If the gold architecture root is not found:**
> "I can't start the security review without a gold architecture_description root for this product. Please run `architecture-create` first so the architecture is authored in gold."
> Stop. Do not proceed.

**If OpenAPI or AsyncAPI specs are missing**, warn the user but offer to proceed with reduced coverage — the API/event surface findings will be marked "spec not loaded — review skipped" rather than guessed.

### 3. Confirm Inputs and Define Scope

Present what was found and ask one question:

```
I found the following inputs:
- Architecture:   gold architecture_description root [ref] ✓
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

### 4. Create or Update Threat Model

Fetch the `threat_model-template` with `get_template_for dto_type=threat_model`.

If no current threat_model exists, call `create_threat_model` for the product. If one exists, call `update_threat_model` to record:
- scope from the user's answer
- MVP baseline mode
- score thresholds
- architecture_description root/chapter refs
- loaded interface/support documents
- steps completed: `[1]`

If full-compliance mode is on (mvp_baseline_mode = false), note in the threat_model details: "Full-compliance mode — regulatory items are scored alongside MVP findings."

### 5. Report and Hand Off

```
Security review initialized.

Threat model: [threat_model ref]
Scope: [user-confirmed scope]
MVP-baseline mode: [on/off]
Architecture loaded: gold architecture_description root [ref]
API specs loaded: [openapi: yes/no, asyncapi: yes/no]
DFMEA cross-reference: [yes/no]

Next: I'll map the attack surface — components, trust boundaries,
data flows, identities, and external dependencies.

[C] Continue to attack-surface mapping
```

Wait for `[C]`.

## SUCCESS METRICS

✅ Existing review detected and handed to step-02-continue correctly
✅ gold architecture tree loaded (or workflow aborted cleanly)
✅ API spec status confirmed (loaded or warned-and-skipped)
✅ Scope confirmed with user
✅ Threat model created or updated with correct initialization details
✅ `stepsCompleted: [1]` recorded in the threat_model

## FAILURE MODES

❌ Proceeding without the gold architecture_description tree
❌ Silently skipping API specs without warning the user that coverage will be reduced
❌ Creating a duplicate threat_model when one already exists for the product
❌ Generating any threats or control findings in this step
❌ Proceeding to Step 2 without user pressing `[C]`

## NEXT STEP

After `[C]`: load `./step-03-surface.md`
