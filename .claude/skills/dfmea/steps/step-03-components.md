# Step 2: System Component Mapping

## MANDATORY EXECUTION RULES

- 📖 Read the architecture document fully before mapping — partial reads produce incomplete coverage
- 🔍 Extract EVERY distinct component within the defined scope — coverage gaps here become blind spots in the analysis
- ✅ Organize by subsystem so the user can spot omissions easily
- 🛑 Do NOT start generating failure modes — that is Step 3
- 🚫 Do NOT proceed without user confirmation of the component map

## YOUR TASK

Parse the architecture document and produce a structured component map as an in-conversation artifact. Every component in scope that could fail in a way that matters should appear here. When in doubt, include it — it's easier to remove an item in review than to rediscover it later.

The component map is a conversational artifact, not a record: it is scaffolding to ensure coverage. The durable output of this step is the `component` string stamped on each `failure_mode` record in step-03. If this session is resumed, coverage can be reconstructed by grouping `list_failure_mode` results by `component`.

---

## COMPONENT EXTRACTION

### What to extract

From the architecture document, extract:

1. **Subsystems / layers** — logical groupings (e.g., API layer, data layer, event system)
2. **Components within each subsystem** — individual services, modules, adapters, daemons
3. **Key functions per component** — what does it do that matters? (1–3 functions max per component; focus on externally observable behaviour)
4. **Integration points** — external systems, third-party dependencies, inter-component interfaces

### Component types to look for

- API endpoints / handlers
- Data stores (databases, caches, queues)
- Background workers / goroutines
- Authentication / authorization modules
- Configuration management
- Network infrastructure (routing, DNS, firewalls)
- Hypervisor / cloud adapters
- Monitoring / observability components
- Deployment / provisioning pipelines
- External integrations

### Scoping

If the user specified a subsystem scope in Step 1, extract only components within that scope. Still note the integration points with out-of-scope components — failures at those boundaries need to be analyzed.

---

## OUTPUT FORMAT

Present the component map to the user in conversation:

```text
## System Component Map

### [Subsystem Name]
| ID | Component | Key Functions | Integration Points |
|----|-----------|--------------|-------------------|
| S1.1 | [name] | [fn1], [fn2] | [ext system or component] |
| S1.2 | ... | ... | ... |

### [Next Subsystem]
...

Total components in scope: X
Out-of-scope integration points: X (listed at bottom)
```

Component IDs use the format `S{subsystem_index}.{component_index}` (e.g., S1.1, S2.3). These IDs are used as conversational labels throughout the DFMEA; the durable value is the free-form `component` string stamped on each failure_mode record.

---

## USER REVIEW

After presenting the component map:

```text
This is the component inventory I'll analyze in Step 3.

Before I proceed:
- Are any significant components missing?
- Should anything be removed from scope?
- Are any component names or functions described incorrectly?

[C] Looks good, continue to failure mode analysis
[E] Let me make edits first (then type [C] when ready)
```

Wait for `[C]`.

## SUCCESS METRICS

✅ Every in-scope component from the architecture document appears in the map
✅ Each component has at least one function and one integration point identified
✅ Component IDs assigned and consistent
✅ User confirmed the map before proceeding

## NEXT STEP

After `[C]`: load `./step-04-failure-modes.md`
