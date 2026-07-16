# Step 1: DFMEA Initialization

## MANDATORY EXECUTION RULES

- 🛑 NEVER generate failure modes or scores in this step — that is Steps 3 and 4
- 📖 Read this entire step file before taking any action
- 🚪 CHECK for an in-progress formal_review first — if found, load step-02-continue.md immediately
- ✅ SPEAK in `{communication_language}` for all user-facing output
- 📋 Your only job here is scoping, document discovery, and creating the session anchor record

## CONTEXT BOUNDARIES

- gold architecture tree is **required** — do not proceed without a product `architecture_description` root
- PRD and project-context are valuable but optional
- Scope can be full system or a named subsystem; clarify if ambiguous
- The `formal_review` record is the session anchor — create it before any failure_mode record

## YOUR TASK

Initialize the DFMEA workflow: check for an in-progress session, load inputs, confirm scope, create the formal_review session anchor.

---

## INITIALIZATION SEQUENCE

### 1. Check for Existing Session

Call `list_formal_review` with `type=dfmea status=in_progress product=<slug>` for the product being analyzed.

- **Exactly one result** → load `./step-02-continue.md` immediately. Stop here.
- **More than one result** → load `./step-02-continue.md` immediately. Stop here.
- **No results** → proceed with fresh initialization below.

### 2. gold Architecture Discovery

Call `list_architecture_description product=<slug>` and locate the active root record for the product. Then call `get_architecture_description` for the root and every chapter listed by the root's `chapters` field, in order. This root plus ordered chapters is the **gold architecture tree** and is the only DFMEA architecture input.

Do not search for or read local architecture files. Local architecture markdown is not canonical for this workflow.

Discover and load optional interface/supporting documents from: `api/`, `docs/`

| Document | Pattern | Required? |
|----------|---------|-----------|
| Architecture | gold `architecture_description` root + chapters | **Yes** — abort without it |
| OpenAPI spec | `api/openapi.yaml` | **Yes** — essential for API endpoint failure mode coverage |
| AsyncAPI spec | `api/asyncapi.yaml` | **Yes** — essential for event/message failure mode coverage |
| PRD | `*prd*.md` | Recommended |
| Project Context | `**/project-context.md` | Recommended |
| UX Design | `*ux-design*.md` | Optional |

The API specs are the authoritative contract for what the system promises to callers and consumers. Load them fully — they reveal interface failure modes (wrong response shapes, missing error codes, uncovered edge cases) that the architecture document alone does not surface.

**If the gold architecture root is not found:**
> "I can't start the DFMEA without a gold architecture_description root for this product. Please run `architecture-create` first so the architecture is authored in gold."
> Stop. Do not proceed.

### 3. Confirm Inputs and Define Scope

Present what was found and ask one question:

```text
I found the following inputs:
- Architecture:   gold architecture_description root [ref] ✓
- OpenAPI spec:   [filename or "not found — API endpoint coverage will be limited"]
- AsyncAPI spec:  [filename or "not found — event/message coverage will be limited"]
- PRD:            [filename or "not found"]
- Project Context:[filename or "not found"]

Before I begin, one question: should I analyze the full system, or focus on a specific subsystem or component set?
(Press Enter or type "full system" to analyze everything)
```

Wait for the user's response.

### 4. Create Session Anchor (formal_review)

Fetch the formal_review template first: `get_template_for dto_type=formal_review`.

Then call `create_formal_review` with:

```json
{
  "type": "dfmea",
  "status": "in_progress",
  "product": "<product-slug>",
  "title": "DFMEA — <scope-derived title>",
  "subject_text": "<scope description from user's answer above>",
  "participants": [{"name": "<user name if known>", "role": "analyst"}],
  "conducted_at": "<today's date>",
  "details": "Session initialized. Scope: <scope>. Architecture input: <root ref plus chapter refs>. Input documents: <list loaded support files>."
}
```

**Important:** `product` must be included explicitly — the server does not enforce it, but the step-01b resume filter (`list_formal_review type=dfmea status=in_progress product=<slug>`) silently misses this review if `product` is omitted.

Capture the returned ref — it is the session anchor for this entire DFMEA run.

### 5. Report and Hand Off

```text
DFMEA session initialized.

Session anchor: <formal_review ref>
Scope: [user-confirmed scope]
Architecture loaded: gold architecture_description root [ref]

Next: I'll map the system components from the architecture document.

[C] Continue to component mapping
```

Wait for `[C]`.

## SUCCESS METRICS

✅ Existing in-progress DFMEA session detected and handed to step-02-continue correctly
✅ gold architecture tree loaded (or workflow aborted cleanly)
✅ Scope confirmed with user
✅ `formal_review` record created with type=dfmea, status=in_progress, product, subject_text, participants
✅ Session anchor ref captured

## FAILURE MODES

❌ Proceeding without the gold architecture_description tree
❌ Creating a new formal_review when one already exists for this product
❌ Omitting `product` from the create_formal_review call
❌ Generating any failure modes or scores in this step
❌ Proceeding to Step 2 without user pressing `[C]`

## NEXT STEP

After `[C]`: load `./step-03-components.md`
