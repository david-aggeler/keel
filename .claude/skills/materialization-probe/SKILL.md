---
name: materialization-probe
description: "Permanent smoke-test skill for the OpenBrain materialize pipeline: placeholder substitution (variant A), direct-text passthrough (variant B), and content-hash stamping. Seeded in_development; materialized only via the advanced-token dev path. Use when the user says: '/materialization-probe', 'materialization smoke test'"
allowed-tools: Read
x-openbrain-source: materialization-probe/v4
x-openbrain-content-source-hash: sha256:7dfd8f5bc44484a4564405f279ca530d113c4eb7c0d12a3eab464f8b137f4ca9
x-openbrain-content-hash: sha256:15c632878822ee91c40b15d1094cd92de1185bad014016c82d8b887384842617
---

# Materialization Probe

This skill is the permanent smoke-test ground for the OpenBrain materialize pipeline.
It is seeded `in_development` and materialized only via the `include_unreleased` + advanced-token dev path (`init --dev`).

## Variant A — Placeholder substitution

The block below is generated from `sordata.SkillMaterializationVocabulary()` — the single source every fixture tree's probe is derived from — and the `ci-e2e-skill-materialization` gate reds if any probe drifts from it. It carries every substitutable token so the corpus-coverage gate has a full corpus to check.

<!-- generated from sordata.SkillMaterializationVocabulary(); do not hand-edit -->

- `build_command`: just build
- `mcp_instance`: gold
- `primary_language`: Go
- `process`: HELIX01
- `product_name`: keel

<!-- end generated token block -->

After materialization, none of these markers may survive literally — each is replaced with the resolved init value or the operator placeholder from the project marker file (`openbrain-client.local.yaml`).

## Variant B — Direct-text passthrough

The following block contains literal text that must survive materialize verbatim:

```
VERBATIM_MARKER_DO_NOT_SUBSTITUTE
This text must appear byte-for-byte in the materialized output.
No placeholder tokens here.
VERBATIM_MARKER_DO_NOT_SUBSTITUTE
```

Both variants are exercised in a single materialize pass when this skill is fetched via `init --dev` with a valid advanced-ingestion token.
