# keel VS Code Test Bridge

`keel-dev vscode tests` is keel's producer for the neutral VS Code test-runner
protocol in `github.com/david-aggeler/keel/vscode`. The Keel Test Bridge VSIX
lives in this repo under `vsix/` and rides the same release tag as the Go
module.

## Commands

```sh
go run ./cmd/keel-dev vscode tests discover --format json
go run ./cmd/keel-dev vscode tests plan --format json --id keel::lane::test-fast
go run ./cmd/keel-dev vscode tests run --id keel::lane::test-fast
go run ./cmd/keel-dev vscode config init
go run ./cmd/keel-dev vscode config upgrade
go run ./cmd/keel-dev vsix ci
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

## Workspace Config

The extension activates only when `.vscode/test-bridge.json` exists. It does not
read or fall back to `testBridge.*` VS Code settings.

```json
{
  "version": 2,
  "command": "bin/keel-dev",
  "args": ["vscode", "tests"],
  "displayName": "Keel",
  "env": {
    "OPTIONAL_KEY": "optional value"
  }
}
```

`version`, `command`, `args`, and `displayName` are required. `env` is optional.
The JSON Schema is embedded in `keel/vscode` as `test-bridge-config` and is
drift-checked against the Go type. `CurrentConfigVersion` is the schema version
constant.

`keel-dev vscode config init` writes the default template. `keel-dev vscode
config upgrade` migrates supported older configs forward, preserves user values,
and is byte-idempotent. If the config version is newer than the binary, upgrade
refuses without writing. The extension follows the same rule on activation:
older configs are upgraded by invoking the configured devtool's `vscode config
upgrade` verb and notifying the user to inspect the git diff; newer configs are
read tolerantly and never rewritten.

## Demo Block

`KEEL_VSCODE_DEMO_BLOCK=<lane-id>` makes the named lane report a synthetic
blocked prerequisite. It is inert when unset and exists so the structured
lane-blocked path can be demonstrated without breaking a real toolchain.

For persistent local demos, use:

```bash
go run ./cmd/keel-dev vscode demo block keel::lane::test-fast
go run ./cmd/keel-dev vscode demo status
go run ./cmd/keel-dev vscode demo unblock
```

The persistent state lives under `.devtools/vscode-demo-block.json`, which is
ignored by git. `KEEL_VSCODE_DEMO_BLOCK` remains the authoritative override:
when it is set, lane preparation uses the environment value and ignores the
persistent state. The Keel Test Bridge command `Keel: Toggle Demo Block` toggles
the persistent state and refreshes the test tree without editing
`.vscode/test-bridge.json`.
