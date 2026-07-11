# keel VS Code Test Bridge

`keel-dev vscode tests` is keel's producer for the neutral VS Code test-runner
protocol in `github.com/david-aggeler/keel/vscode`.

## Commands

```sh
go run ./cmd/keel-dev vscode tests discover --format json
go run ./cmd/keel-dev vscode tests plan --format json --id keel::lane::test-fast
go run ./cmd/keel-dev vscode tests run --id keel::lane::test-fast
```

The lane ids are:

- `keel::lane::lint`
- `keel::lane::test-fast`
- `keel::lane::test-coverage`

## Protocol Streams

During `vscode` verbs, stdout is reserved for protocol JSON or JSONL only. The
keel/log console sink routes to stderr for these verbs, while the `.logs/` human
log and `.jsonl` file sinks stay enabled.

Run event streams are also written under `.devtools/vscode-runs/<run-id>.jsonl`
through the same event stamper used for stdout. Files older than seven days are
removed opportunistically when a new run starts.

## Fixture Contract

The VS Code extension and keel engine use the in-repo fixtures from this
checkout. There is no peer fixture-sync step: a single commit carries the engine,
extension, and fixture state atomically.

## Demo Block

`KEEL_VSCODE_DEMO_BLOCK=<lane-id>` makes the named lane report a synthetic
blocked prerequisite. It is inert when unset and exists so the structured
lane-blocked path can be demonstrated without breaking a real toolchain.
