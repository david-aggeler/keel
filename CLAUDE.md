# CLAUDE.md

Guidance for Claude Code sessions rooted in this repo.

## What this is

keel is ONE public Apache-2.0 Go module (`github.com/david-aggeler/keel`)
— the shared process-execution + logging foundation for openbrain and
vela. Subpackages: `log`, `exec`, `exec/claude`, `exec/codex`. One tag,
one version, zero internal replaces, zero external deps. Anonymous
`go get` must always work: never introduce GOPRIVATE/token/netrc or
Docker build secrets on any path.

## Dev records live in gold, product `keel`

All requirements, ACs, change requests, epics, and the iteration plan
(`keel/dd_plan-1`, `keel/epic-1`) are records in the gold OpenBrain
instance under product **keel** — authored via `mcp__gold__*` tools or
`openbrain-client` on PATH. Never write dev records as local markdown.
Every record call takes `product: "keel"`. Search before creating.

## Transitional bridge (iterations 1–4)

Until keel's own CI + release loop are trusted, openbrain's `go.work`
carries `use /projects/keel` so openbrain's gates (`openbrain-dev ci
static-tools`, `test unit`) compile and run keel's tests. Keel is
source of truth for the moved code; openbrain's `pkg/logging`,
`pkg/procexec`, `pkg/claudecli`, `pkg/codexcli` remain as the
consumers' import target until iteration 4, then get deleted. Bridge
exit is iteration 5 (see `keel/dd_plan-1` in gold).

## Change control (owner-ratified 2026-07-07)

Two tiers; every code change gets a gold record and a merge SHA either way:

- **change_request** (draft→approved→in_progress→…→closed): anything that
  changes behavior, adds/removes surface, touches contracts, the gate
  definition, or the release loop. The CR's value is ex-ante: scope
  negotiation, decisions table, owner approval before code, AC mapping at
  close. **Before closing a CR, perform a literal AC-by-AC conformance
  check** — map the diff to each AC and record the evidence; a green gate
  alone is not conformance (CR-2 was reopened for exactly this).
- **Quick-change** (issue + issue_fix, `change_request: null`): cosmetic,
  docs, or test-only deltas with no scope to negotiate and no AC impact.
  The issue carries evidence; the issue_fix carries root cause, fix,
  verification, and the commit SHA.

Post-merge defects found by anyone: file an issue first, then fix under
the right tier. Never fix silently.

## Conventions

- **Move heritage:** code was moved content-faithfully from openbrain
  `pkg/`; error-string prefixes keep their historical package names
  (`procexec:`, `codexcli:`, …). Don't churn them without cause.
- **Aliased internal imports** (`logging "…/keel/log"`,
  `procexec "…/keel/exec"`) are deliberate — they avoid `os/exec` /
  stdlib-`log` identifier collisions and kept the move diff minimal.
- **DHF annotations:** `DHF-REQ:` / `DHF-TEST:` comments reference gold
  records; keel-owned requirements use `keel/requirement-N`, consumer
  obligations keep their `openbrain/...` refs.
- Validate with `gofmt -l .`, `go build ./...`, `go vet ./...`,
  `go test ./...`. (keel-dev CLI with `ci`/`release` verbs arrives in
  iteration 2 and becomes the canonical gate.)
- Tests must stay hermetic: the codex/claude adapters test against stub
  binaries; live smoke runs are env-gated. CI must never need a real
  codex/claude install.
