---
dto_type: threat_model
product: keel
title: Keel Threat Model
summary: >-
  Crown jewels: module integrity for anonymous consumers and the release/tag
  path; top open risk: workspace-trust adapter execution + VSIX Node supply
  chain; draft — initial STRIDE pass pending, coverage matrix seeded.
status: draft
related:
  - keel/architecture_description-1   # root (to-gold sibling)
  - keel/interface_spec-1             # to-gold sibling
---

# Threat Model: Keel Threat Model

## 1. System under analysis — "What are we working on?"

Keel at its pre-tree-restructure state (architecture root: to-gold sibling,
drafted 2026-07-12; no baseline yet — records pending in gold). Software-
centric stance, anchored on the architecture root's component inventory: the
Go module packages (`keel/log`, `keel/log/otel`, `keel/exec`, `exec/claude`,
`exec/codex`, `keel/cli`, `keel/vscode`), the `cmd/keel-dev` binary (gate,
release, bridge adapter), and the `Keel Test Bridge VSIX` running inside
VS Code. Data stores are plain directories on the workspace host: `.logs/`
(log sinks), `.devtools/` (run streams, run lock, demo state), plus the git
repo itself. There is no service, no network listener, no database — the
interesting flows are subprocess spawns and artifact publication.

## 2. Assets — the crown jewels

1. **Module integrity for anonymous consumers** — the published
   `github.com/david-aggeler/keel` tree at each tag. Grants: code execution in
   every downstream build (vela, openbrain, future consumers). Resides:
   GitHub + module proxies. Legitimate writers: the owner via the release
   verb. Sensitivity: highest — the product's #1 quality goal is that
   consumers fetch it blind, with no credential channel to compromise but also
   no private gate between repo and consumers.
2. **Release/tag capability** — the `gh`-authenticated identity and the local
   host that runs `keel-dev release`. Grants: minting asset #1 and the VSIX
   users install. Resides: owner workstation/dev hosts. Sensitivity: highest.
3. **The development host + repo working tree** — where coding agents operate
   with write access and where the gate runs. Grants: silent source tampering
   ahead of release. Custodians: owner + change-control process (CR review,
   gitleaks in the gate).
4. **Subprocess execution capability** — `keel/exec` and its adapters spawn
   `claude`/`codex`; the bridge adapter spawns `go`/`pnpm`. Grants: arbitrary
   command execution *if* an attacker controls binary resolution (PATH) or
   arguments. Custodians: consumers' environments.
5. **Availability** — low stakes: nothing serves traffic. Worst outage is a
   blocked dev loop (stranded `run.lock`) — an annoyance with a maintenance
   recovery, not a security event.

Deliberately NOT crown jewels: log/run-stream contents (local, redaction
conventions in `keel/log`), the gold records (owned by openbrain's threat
model, reached only via `openbrain-client` at dev time).

## 3. Trust boundaries

| Boundary | Sides | What crosses | Enforced by |
|---|---|---|---|
| VS Code UI ↔ adapter subprocess | extension host (user trust) ↔ workspace-configured binary | the command named in `.vscode/test-bridge.json` gets executed with workspace cwd | VS Code **workspace trust** + activation gated on `test-bridge.json` presence; no other gate — opening a hostile repo and trusting it hands execution to `bin/keel-dev` *of that repo* |
| Repo content ↔ executed toolchain | untrusted checkout ↔ `go test` / `pnpm` | test code, generators, hooks run with user privileges | none beyond workspace trust — inherent to running any repo's tests |
| Dev host ↔ GitHub | local ↔ public internet | pushes, tags, release assets (out); module/dep fetches (in) | `gh` auth for writes; go module checksum DB + pnpm lockfile for reads |
| Agent sessions ↔ repo | coding agents ↔ working tree | diffs, commands | change control (CR approval before code), gate on merge, gitleaks sweep |
| Core module ↔ VSIX toolchain | zero-dep Go graph ↔ node_modules | nothing at build time (core gate is node-free) | `log-core-deps` gate step; separate `vsix ci` gate |
| Local processes ↔ VSIX `.devtools/` readers | anything on the host that can write files ↔ the extension's parsers | run-stream JSONLs consumed by the external-run mirror; demo-block state JSON read by the adapter | path-prefix check + JSON parse tolerance — that is ALL there is; these are unauthenticated local inputs |

## 4. Attack surface

Walking interface_spec §2 plus the non-interface vectors:

- **Go module APIs** — exposure: every consumer build. Assets reachable: #1.
  Vector class: malicious/compromised commit reaching a tag (supply chain of
  ourselves). Auth: none (by design).
- **Bridge protocol + adapter invocation** — exposure: local. Assets: #4.
  Vectors: hostile workspace config (`test-bridge.json` pointing at an
  attacker binary — the classic malicious-repo vector), oversized/malformed
  protocol documents against the VSIX parser, symlinked `.devtools/` paths.
- **Config files (`test-bridge.json`, planned `test-lanes.json`)** — parsed by
  Go and TS; lanes-file globs become `go test` arguments — argument-injection
  shape (glob strings must never be shell-interpreted; keel/exec uses argv
  arrays, no shells, which must stay true for lane expansion).
- **keel-dev CLI** — exposure: local operator. Assets: #2 (release verb), #3.
- **Consumed:** Go toolchain and module proxy (checksummed), **npm/pnpm tree
  for the VSIX** — the largest third-party surface in the product and fully
  outside the zero-dep policy; claude/codex CLIs (user-installed, spawned
  with caller-controlled args); GitHub (release path); **the pinned
  static-tool battery** (golangci-lint, govulncheck, cspell, gitleaks,
  shellcheck, shfmt, deadcode) — third-party binaries executed on the
  dev/release host by every gate run, version-pinned via the `pinnedTools`
  manifest, adjacent to crown jewels #2/#3.
- **OTLP egress (`log/otel`)** — the product's only network egress besides
  GitHub: structured log content leaves the host toward a consumer-configured
  collector. Vectors: log-content exfiltration via a hostile endpoint config,
  endpoint spoofing, TLS/credential posture — all owned by the consumer, but
  the surface must be enumerated here because keel ships the client.
- **Operational access** — owner workstation, SSH keys to dev hosts, `gh`
  token; backups/state dirs contain no secrets by convention (gitleaks +
  redaction), but `.logs/` may capture subprocess output verbatim.
- **Stored/replayed payloads** — `.devtools/vscode-runs/*.jsonl` are re-read
  by the external-run mirror; a crafted stream is a parser input.

## 5. Threat enumeration — "What can go wrong?" → failure_mode rows

- **Approach:** STRIDE-per-surface over §4 (which enumerates interface_spec §2
  row by row), with §3 boundaries as the crossing points.
- **Status: initial pass PENDING.** No failure_mode records exist yet — this
  document is staged before its DTO type exists in gold; filing the rows is
  part of concluding keel/exploration-2's CR work. Candidate rows already
  visible from design work (to be filed, scored exploitability-style):
  hostile-workspace adapter execution (S/E); tag-time source tamper via agent
  session (T); VSIX dependency compromise (T, supply chain); lanes-glob
  argument injection (T/E); crafted run-stream vs. mirror parser (D);
  stranded run.lock as denial-of-dev-loop (D, low).
- **Coverage matrix** (surface × STRIDE). Legend: `—` = not yet analyzed
  (unknown, honestly); `C` = candidate failure_mode identified (unscored,
  row pending); `n/a` = category argued inapplicable. Most cells are `—`
  because the first sweep has not run — that is the point of showing the
  matrix now.

  | Surface | S | T | R | I | D | E |
  |---|---|---|---|---|---|---|
  | Go module APIs (published tree) | — | C tag-time source tamper | — | — | — | — |
  | Bridge protocol + adapter invocation | C hostile workspace | — | — | — | — | C hostile workspace |
  | Config files (test-bridge, lanes planned) | — | C glob argument injection | — | — | — | C glob argument injection |
  | keel-dev CLI (incl. release verb) | — | — | — | — | — | — |
  | Go toolchain + module proxy | — | — | — | — | — | — |
  | VSIX npm/pnpm tree | — | C dependency compromise | — | — | — | — |
  | Static-tool battery (gate binaries) | — | — | — | — | — | — |
  | claude/codex CLIs | — | — | — | — | — | — |
  | GitHub / release path | — | — | — | — | — | — |
  | OTLP egress | — | — | — | C log exfiltration (candidate) | — | — |
  | `.devtools/` readers (mirror, demo state) | — | C crafted run-stream | — | — | C crafted run-stream / stranded lock | — |
  | Operational access (host, gh token, SSH) | — | — | — | — | — | — |

- **Pass log:** none yet. First full sweep is due with the tree-restructure
  CR train (new surfaces: lanes file, lanes verbs), owner-run; it fills or
  n/a-justifies every `—` cell and files a scored failure_mode per `C`.

## 6. Controls map — "What are we going to do about it?"

Existing controls (preventive unless noted):

| Control | Serves threat class | Implemented / verified |
|---|---|---|
| No-credentials-in-build-path policy (no GOPRIVATE/tokens/netrc ever) | removes credential theft from the module-fetch path entirely | CLAUDE.md rule; release verb's anonymous-fetch check (detective) |
| Change control: CR approval before code; gate green before merge | agent-session tampering, silent fixes | gold CR process; `keel-dev ci` |
| gitleaks in the gate (`--redact`) | committed-secret leakage | gate step, keel/requirement-13 |
| govulncheck in the gate | known-vuln Go deps (small graph by design) | gate step, keel/requirement-12 |
| Zero-dep core + otel quarantine + node-free core gate | shrinks supply-chain surface of everything consumers import | `log-core-deps` step; module structure |
| pnpm lockfile pinning (VSIX only) | npm tree drift | `vsix/pnpm-lock.yaml`; `vsix ci` |
| argv-array subprocess spawning, no shells | argument/shell injection through exec paths | `keel/exec` design; must be preserved by lane expansion (planned) |
| VS Code workspace trust + activation on `test-bridge.json` | hostile-workspace adapter execution (partial — trust is user-granted) | VSIX activation events |
| Redaction in `log.OperationalError`; extension-host buffer ceilings (16 MiB discover/plan, 1 MiB demo/config) | information disclosure via logs; parser resource abuse on the VSIX side | keel/log tests; bridgeAdapter execFile buffers |
| Release preflight = the same gates + refuses dirty tree/existing tag | shipping unreviewed state | `keel-dev release`; docs/release.md |

Gaps (controls with no implementation yet): **child-process output capture is
unbounded** — `MaxOutputBytes` (4 MiB) is declared on the workspace profile
but has zero callers, and `keel/exec` buffers without a cap; tracked as
keel/issue-40 (a prior draft of this document listed the cap as an existing
control — that was false); emitted-discovery schema validation (drift
detection — lands with keel/change_request-53, requirement-46 AC);
lanes-file validation rules (spec §11 — keel/change_request-55);
provenance/signing for release assets (unassessed — candidate finding).
Two-way traceability to failure_mode rows starts when the rows exist (§5).

## 7. Residual risk — "Did we do a good job?"

Standing conclusion, honestly: **not yet demonstrated.** The control set is
real but the enumeration pass has not run, so residual risk is unquantified.
Top open risks by inspection: (1) hostile-workspace adapter execution is
mitigated only by VS Code workspace trust — accepted for now as the platform's
own model, justification to be recorded on its failure_mode row; (2) the VSIX
npm tree is the largest unreviewed dependency surface — lockfile-pinned but
unaudited. Definition of done for the first pass: every §4 surface × STRIDE
cell marked analyzed/N-A, every resulting row scored and either
control-linked or justification-accepted, and this section rewritten from the
rows.

## 8. Maintenance — update triggers & ownership

- **Triggers:** architecture change (tree restructure CR train — already
  queued), any interface_spec §2 row change (the planned `test-lanes.json`
  surface triggers re-analysis of the config-file vector), new dependency in
  either dependency tree, new deployment/distribution channel (e.g. VSIX
  marketplace publication would be a major trigger), auth change (any new
  credential anywhere is a policy exception AND a trigger), security
  incident.
- **Owner:** David (curates; agents may draft rows, owner approves scores and
  acceptances).
- **Last reviewed:** never — first sweep pending (see §5 pass log).

## 9. Linked decisions

Pending design_decision records: no-credentials policy; zero-dep core /
quarantine split; workspace-trust reliance for adapter execution; argv-only
subprocess policy. Until extracted, the deciding prose lives in CLAUDE.md and
keel/exploration-2.
