---
dto_type: architecture_description
product: keel
title: VS Code Test Bridge & lanes
summary: >-
  Chapter: the protocol-over-subprocess bridge between the Keel Test Bridge
  VSIX and consumer devtools — wire documents, the lettered target tree,
  aggregation lanes with composition and measured cost; draft.
status: draft
related:
  - keel/exploration-2
  - keel/prototype-1
---

# Architecture Description: VS Code Test Bridge & lanes

*Chapter of the keel architecture root (see `chapters[]` there).*

## Scope

The bridge between VS Code's Test Explorer and a consumer repo's devtool: the
wire protocol and its schemas, the discovery tree structure, lane semantics
(including the planned lanes file and composition), run/event projection, and
the maintenance surface. Deliberately left to siblings: the gate and release
machinery (root §6/§8), the log/exec library internals.

## Affected components

`keel/vscode`, `cmd/keel-dev`, `Keel Test Bridge VSIX` (names per root §5).

## Quality goals served

- Goal 4 (SSH-first interactive loop) — the entire reason this chapter's
  design exists: the adapter is a subprocess on the workspace host, so the
  Explorer works identically over Remote-SSH, and the tree doubles as the
  merge-gate-sizing instrument (compose a lane, run it, read measured cost).
- Goal 3 (hermetic tests) — the VSIX headless suite runs against a synthetic
  `fake-adapter.js`; keel-dev's bridge verbs are tested without VS Code.
- Goal 1 (trade-off) — the VSIX's Node toolchain is the one large third-party
  surface; it is quarantined behind the separate `vsix ci` gate so the core
  module keeps its zero-dep promise.

## Topic narrative (concept chapter: the bridge rule end to end)

**The rule.** The VSIX contains zero toolchain knowledge. It executes the
workspace-configured adapter (`.vscode/test-bridge.json` → command + args) and
speaks only versioned JSON documents: a discovery document (the tree), a setup
plan (desired-state rows), and a run-event stream (JSONL). Everything the tree
shows — groups, ordering, lanes, maintenance actions, durations — is asserted
by the consumer's devtool, never by the extension. Binding: any consumer
devtool that answers the verbs gets the full Test Explorer experience;
per-consumer variation (lane sets, maintenance items) is data, not code.

**Mechanics — wire contract.** Types and embedded JSON Schemas live in
`keel/vscode` (`vscoderun.go`, `schemas/*.json`); the item model carries
`sort_text` (VS Code has no sorting concept — order is encoded as label prefix
plus sortText), `uri`/`range` (click-to-source), `limitations` (rendered as
description), `required_resources` (tags), and `canonical_id` (alias items,
the covers mechanism). Run events are projected onto canonical test ids by the
projectors in `keel/vscode`; the VSIX mirrors alias state via `canonical_id`.
Detail contract: interface_spec §2 (schema'd documents) and §4 (lanes file).

**Mechanics — target tree** (decided in keel/exploration-2; rendered by
keel/prototype-1 variant c):

```
a. Maintenance    a.1 detect lanes · a.2 unlock test bridge
                  a.3 clear test results · a.4 clear local test state
b. Lanes          b.1 lint · b.2 test-fast · b.3 test-coverage
                  b.10 vsix ci · b.30 ci   (+ file lanes from test-lanes.json)
                  each: measured last-run duration + covers subtree
d. Frameworks     parent for language-specific test trees
   d.1 Go         package → file → test (go/parser; uri + range)
   d.2 Mocha (vsix)   file items land with per-file vsix members (CR-55)
(c. deliberately unassigned — gap for a future group, no renumbering needed)
```

Letters order top-level groups, numeric children carry family gaps; ordinals
live only in labels + `sort_text`, never in item ids, so renumbering is free
and results survive scheme changes.

**Mechanics — lanes** *(planned — approved units keel/change_request-53
(tree + vsix-ci/ci lanes) and keel/change_request-55 (lanes file + verbs);
today exactly three system lanes are compiled — lint, test-fast,
test-coverage — and no `lanes` verbs exist)*.
Target state: system lanes lint, test-fast, test-coverage, vsix-ci, ci stay
compiled; file lanes come from `.vscode/test-lanes.json` — **100% owned by
the consumer devtool** (go.mod model: keel-dev writes it via `lanes detect`,
the human hand-edits it, the VSIX only watches the path) — and are defined by
member sets: Go package globs, framework roots, **per-file vsix selections**
(owner decision, Option 2), or other lanes (DAG composition, union + dedup),
never by opaque commands (vela's opaque-command registry forced a
hand-maintained covers switch that drifts; member sets make covers, run
fan-out, and cost attribution derive from one source). Planned verbs:
`vscode lanes list` (effective definitions incl. expanded members and
measured durations — the gate-sizing dataset) and `vscode lanes detect`
(idempotent, append-only category writes, also maintenance item a.1). Full
normative contract: the Test Lanes Interface Specification rev 2.3 (attached
to keel/exploration-2; carried by requirements keel/requirement-51…54).

**Enforcement.** Protocol stdout discipline via the `no-raw-stdout-stream`
lint and the vscode-verb sink arrangement; wire stability via
`wire_stability_test.go` + `schema_drift_test.go`; the run lock serializes
runs today (its stranding gets a maintenance recovery item, planned).
Planned: a malformed lanes file can never take down discovery — system lanes
always render, file errors become a diagnostic item.

**Exceptions.** `vscode demo block/unblock` exists purely for demoing blocked
lanes (keel/requirement-41). The VSIX headless suite uses the in-repo fixture
adapter only — no peer fixture-sync path.

## Linked decisions

Pending extraction into design_decision records once the DTO population
starts; the deciding dialogue is keel/exploration-2 (CONCLUDED 2026-07-12 —
letters ordering; covers subtrees with full expansion; measured cost hints
via requested-attribution; Maintenance group title + membership; member-set
lanes over opaque commands; devtool-owned lanes file (go.mod model, detect
writes); per-file vsix members; package-path globs as the Go category
mechanism; no VS Code tasks; no tree-driven config editing). The decisions
are normatively carried by keel/requirement-46…54 and implemented by
keel/change_request-53/54/55 (iteration-10).
