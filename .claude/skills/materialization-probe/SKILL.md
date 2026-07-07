---
name: materialization-probe
description: "Permanent smoke-test skill for the OpenBrain materialize pipeline: placeholder substitution (variant A), direct-text passthrough (variant B), and content-hash stamping. Seeded in_development; materialized only via the advanced-token dev path. Use when the user says: '/materialization-probe', 'materialization smoke test'"
allowed-tools: Read
x-openbrain-source: materialization-probe/v1
x-openbrain-content-source-hash: sha256:c64ab44afbf4969b89f831aa9a5f0160738fe93c2203837ffdad3a06c4736527
x-openbrain-content-hash: sha256:8f7024dbef553bc831e5295d21e0d3424ab41f17979b65f89fe3a958e9514638
---

# Materialization Probe

This skill is the permanent smoke-test ground for the OpenBrain materialize pipeline.
It is seeded `in_development` and materialized only via the `include_unreleased` + advanced-token dev path (`init --dev`).

## Variant A — Placeholder substitution

The following tokens are baked at materialize time by the reconcile pass:

- Product: keel
- MCP instance: gold

After materialization, these markers must be replaced with the resolved values from the project marker file (`openbrain-client.local.yaml`).

## Variant B — Direct-text passthrough

The following block contains literal text that must survive materialize verbatim:

```
VERBATIM_MARKER_DO_NOT_SUBSTITUTE
This text must appear byte-for-byte in the materialized output.
No placeholder tokens here.
VERBATIM_MARKER_DO_NOT_SUBSTITUTE
```

Both variants are exercised in a single materialize pass when this skill is fetched via `init --dev` with a valid advanced-ingestion token.
