# Planning Workflow Integration

If the project uses a planning workflow that records its conventions in the System of Record, wire the API contract skill into that workflow with two narrow integrations so the spec stays authoritative across all planning stages:

1. A `design_decision` record carries the rule statement so every skill reads it: "the spec is the source of truth; generated code is committed; drift fails the gate."
2. Implementation-readiness checks run `scripts/validate.sh` and `scripts/drift.sh` before declaring any work item ready to merge.

If the project does not use a planning workflow, this skill stands alone — wire `make api-validate` / `make api-drift` into your existing CI gate stack.
