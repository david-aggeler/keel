---
dto_type: interface_spec
product: keel
title: Keel Interface Specification
summary: >-
  13 surfaces (7 exposed, 6 consumed); Go module APIs additive-only under one
  tag, bridge protocol schema'd and drift-gated, lanes file devtool-owned
  (spec drafted, rev 2); draft.
status: draft
related:
  - keel/architecture_description-1   # root (to-gold sibling)
  - keel/exploration-2
---

# Interface Specification: Keel Interface Specification

## 1. Overview & audiences

Keel exposes two families of interfaces: **Go module APIs** (`log`, `log/otel`,
`exec`, `exec/claude`, `exec/codex`, `cli`, `vscode`) consumed by sibling
products (vela, openbrain) and by keel's own devtool; and the **Test Bridge**
family — the keel-dev CLI verbs, the versioned JSON protocol documents the
VSIX consumes, and the workspace config files users hand-edit. Audiences:
partner-internal (sibling products importing the module), tool-internal (the
VSIX ↔ adapter pair, one release unit), and human developers (CLI, config
files). Nothing here is public-anonymous *except* module fetch itself, which
is the product's #1 quality goal.

## 2. Surface inventory

| Surface | Direction | Provider → Consumers | Audience | Auth | Machine spec (authoritative) | Lifecycle | Compat regime | Owner |
|---|---|---|---|---|---|---|---|---|
| Go APIs: `keel/log`, `keel/exec`, `exec/claude`, `exec/codex` | exposed | keel → vela, openbrain, keel-dev | partner-internal | n/a (module fetch is anonymous by policy) | godoc from source; surface pinned for `log` only (`log/api_surface_test.go`) — exec/adapter surface pins PENDING (gap) | stable | additive-only (§3) | David |
| Go API: `keel/log/otel` | exposed | keel → consumers opting into OTLP | partner-internal | n/a | godoc from source | preview — may split to its own module | additive-only; module split reserved | David |
| Go APIs: `keel/cli`, `keel/vscode` | exposed | keel → keel-dev, future consumer devtools | tool-internal | n/a | godoc from source | preview | additive-intent, may still break with the tool | David |
| Bridge protocol documents (discovery, setup plan, run events, run lock) | exposed | keel-dev adapter → Keel Test Bridge VSIX | tool-internal (cross-binary, same release) | none — local subprocess, workspace trust (threat_model §3) | embedded JSON Schemas `vscode/schemas/*.json`, drift-gated by `schema_drift_test.go` + `wire_stability_test.go` | stable | versioned documents (`version` int per doc); additive within a version | David |
| `.vscode/test-bridge.json` | exposed (read) | user → VSIX + adapter | human developers | — | `vscode/schemas/test-bridge-config.json`; `vscode config init/upgrade` migrates | stable (v2) | versioned; upgrade verb owns migration | David |
| `.vscode/test-lanes.json` *(planned)* | exposed (read/write) | keel-dev ↔ user (both write); VSIX watches the path only, never parses | human developers | — | none (devtool-owned contract — §4) | preview (spec rev 2.3; implementing unit keel/change_request-55, approved) | versioned file (`version` int); additive fields = warning-tolerated | David |
| keel-dev CLI verbs | exposed | keel-dev → humans, VSIX, consumer Justfiles | tool-internal | — | `keel-dev help` (generated from `cli.CommandSpec`) | stable core (`ci`, `release`), preview (`vscode lanes *` planned) | verbs never repurposed; new verbs additive | David |
| Go toolchain | consumed | golang.org → keel | — | — | pinned `go 1.25.1` in go.mod | pinned | tolerate patch releases; minor bumps via CR | David |
| Node + pnpm (VSIX builds only) | consumed | npm registry → vsix/ | — | — | `vsix/pnpm-lock.yaml` (lockfile-pinned) | pinned | lockfile updates via CR; never touches core gate | David |
| GitHub (module fetch, releases, tags) | consumed | GitHub → keel | — | release verb uses `gh` auth; module fetch anonymous | GitHub REST via `gh`; go module checksum DB on fetch | pinned by usage | release path changes via CR | David |
| claude / codex CLIs (runtime adapters) | consumed | user-installed CLIs → `exec/claude`, `exec/codex` | — | none (argv spawn, no shell) | deliberately unpinned; hermetic contract tests encode tolerated behaviors; live smokes env-gated (`*_LIVE_SMOKE=1`), never gate CI | tracked by usage | adapters tolerate output drift; upstream breaks detected by on-demand smokes | David |
| Static-tool battery (golangci-lint, govulncheck, cspell, gitleaks, shellcheck, shfmt, deadcode) | consumed | third-party binaries → gate runs on the dev/release host | — | — | `pinnedTools` manifest (`cmd/keel-dev/tools.go`) — version-pinned at install | pinned | bumps via CR; presence/version checked by the gate | David |
| OTLP endpoint (`log/otel` egress) | consumed | keel client → consumer-configured collector | — | consumer-configured (endpoint + creds belong to the consumer) | OTLP/HTTP per pinned otel SDK version in go.mod | pinned SDK | keel ships client only; endpoint trust is the consumer's; the product's only network egress besides GitHub | David |

## 3. Compatibility & evolution policy

- **Backward-compatibility stance.** Productive surfaces (stable rows) are
  additive-only: exported Go APIs may gain symbols but never lose or change
  signatures within the module's major version; protocol documents may gain
  optional fields, never remove fields or repurpose values; the VSIX tolerates
  unknown fields (and must continue to). Breaking a stable surface requires a
  CR that names every known consumer.
- **Versioning mechanism.** One tag versions the module AND the VSIX — there
  is no per-surface version. Protocol documents carry integer `version`
  fields; a consumer seeing a higher major document version must fail loudly,
  not guess. Config files version independently of the tag.
  `test-bridge.json` migrates via the `config upgrade` verb — invoked by hand
  OR automatically by the VSIX at activation when the file's version is below
  current (that auto-migration is part of the contract, §4). The planned
  `test-lanes.json` is devtool-owned: keel-dev writes it (`lanes detect`) and
  humans hand-edit it; both are first-class writers (go.mod model), and
  neither is a "silent rewrite" — every write is an explicit user action.
- **Consumed-side policy.** Go toolchain and pnpm lockfile are pinned in-repo;
  bumps are ordinary CRs gated by the same commands. The claude/codex CLIs are
  deliberately NOT pinned (user-installed); the adapters' contract tests
  encode the tolerated output behaviors, and live smokes exist to detect
  upstream drift on demand — they never gate CI. openbrain's `go.work` bridge
  consumes keel at HEAD until iteration 5 (accepted transitional risk,
  architecture root §9).

## 4. Non-generated contracts

### `.vscode/test-lanes.json` (planned)

The one non-generated contract — **owned 100% by the consumer devtool**
(go.mod model: keel-dev writes it via `lanes detect`, humans hand-edit it,
the VSIX only watches the path). Normative contract: **Test Lanes Interface
Specification rev 2.3** (attached to `keel/exploration-2` as
`test-lanes-spec.md`; carried by keel/requirement-51…54, implemented by
keel/change_request-55). Contract essentials: lanes declare *member sets*
(Go package globs, framework roots, per-file vsix selections, lane refs) —
never commands; composition is a DAG with union+dedup semantics,
depth ≤ 8; validation errors suppress the offending lane (or file) with a
visible diagnostic item and can never take down discovery; the file is
watched — any write re-renders the tree without a refresh action or restart;
system lanes always render regardless of file state. Timing: watcher-driven
re-discovery is bounded by the discovery implementation — seconds today,
~1 s only after the go/parser discovery upgrade (keel/change_request-55
`depends_on` keel/change_request-54 for exactly this). Lane runs serialize
on `run.lock`. Failure scenario: malformed
file ⇒ system lanes + one non-runnable diagnostic item carrying the parse
error.

### Adapter invocation contract

The VSIX performs FOUR invocation shapes against the `test-bridge.json`
command, all with the workspace root as cwd — a conforming devtool must
answer all of them:

1. `command [...args, 'discover', '--format', 'json']` and
   `command [...args, 'plan', '--format', 'json', ('--id', id)…]` — exactly
   one JSON document on stdout, exit 0.
2. `command [...args, 'run', ('--id', id)…]` — run-event JSONL streamed on
   stdout until a terminal `run_finished`.
3. **Demo verbs perform args surgery**: the last `tests` token in the
   configured args is spliced to `demo` (or `demo` is appended when absent)
   before `status` / `block <lane>` / `unblock` — a devtool whose args do not
   end in a `tests`-style token must still tolerate this shape.
4. **Auto-migration at activation**: when the config's `version` is below
   current, the VSIX invokes the HARDCODED verb path
   `command ['vscode', 'config', 'upgrade']`, ignoring the configured args
   entirely. A devtool with a different verb layout must still accept this
   exact invocation (or ship a current-version config so it never fires).
   This is the auto-migration named in §3.

stderr is free-form logging on every shape. Idempotency: discover/plan/
status are read-only; run takes the lock. Event ordering: **scoped to leaf
`test` items** — a leaf test's events arrive after its `test_started`;
lane/rollup-level events (e.g. the terminal lane `passed`) are exempt and may
appear without a prior `test_started`. Buffer ceilings per shape are in §5.
These rules are load-bearing for any non-keel devtool implementing the
bridge.

## 5. Cross-surface conventions

- **Error contract.** Go APIs return wrapped errors prefixed with the owning
  package path (`keel/exec:` — never bare `exec:`); `log.OperationalError`
  carries structured, redaction-safe metadata. Protocol failures surface as
  `errored` events plus a non-zero `run_finished.exit_code` — the VSIX never
  parses stderr for semantics.
- **Auth conventions.** None by design on exposed surfaces (local subprocess
  + anonymous fetch). The only credential anywhere is `gh` auth used by the
  release verb — and the no-credentials-in-build-path policy is absolute.
- **Limits.** Per-verb extension-host buffer ceilings: `discover` and `plan`
  ≤ 16 MiB; demo `status`/`block`/`unblock` and `config upgrade` ≤ **1 MiB**
  — exceeding one kills the call and the user sees an opaque
  `maxBuffer exceeded` error, so chatty devtools must keep stdout lean.
  Run-event streams are mirrored to `.devtools/vscode-runs/` with 7-day
  retention. There is currently NO cap on captured child-process output
  (`MaxOutputBytes` is declared but unwired — keel/issue-40; also a threat
  model §6 gap and an architecture root §9 risk).
- **Deprecation procedure.** Verbs and protocol fields are deprecated in the
  release notes of the tag introducing the replacement, kept for ≥ one minor
  release, then removed via CR naming known consumers. Config-file migrations
  ship as `config upgrade` behavior (hand-invoked or the §4 activation
  auto-migration); lanes-file writes belong to the lanes verbs. Every config
  write traces to an explicit, documented mechanism.

## 6. Linked decisions

Pending design_decision records: single-tag module+VSIX versioning;
additive-only stance; protocol-over-subprocess (no VS Code settings surface,
`testBridge.*` intentionally unsupported); member-set lanes over opaque
commands (keel/exploration-2 Round 11).
