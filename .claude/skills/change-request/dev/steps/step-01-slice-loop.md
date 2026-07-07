# Step 01 — Slice Loop Setup

**Goal:** For each requirement ref in `acceptance_criteria`, derive the GWT atom and public interface that will guide the tester subagent.

## Per-slice inputs

For each requirement ref:

1. Call `get_requirement ref=<req-ref>` to load the requirement.
2. Extract:
   - **GWT atoms** from `requirement.acceptance_criteria` (the Given/When/Then strings). These are the test oracle; the tester subagent must not be given implementation details.
   - **Public interface** — the observable API surface the test exercises (function signature, HTTP endpoint, MCP tool name, etc.). Derive from the requirement statement and the unit's Scope section. Do not leak internal design.
3. Record the GWT atoms and public interface as the slice spec.

## Slice ordering

Process requirement refs in the order they appear in `acceptance_criteria`. The tracer bullet is the first ref.

## Invariant

Never start a new slice while any test from the current slice is red. `dev` is sequential per slice; slice-parallelism is explicitly deferred.
