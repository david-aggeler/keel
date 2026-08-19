# keel VS Code Test Bridge

`keel-dev test-bridge` is keel's canonical producer for the neutral VS Code
test-runner protocol in `github.com/david-aggeler/keel/vscode`. The Keel Test
Bridge VSIX lives in this repo under `vsix/` and rides the same release tag as
the Go module.

## Commands

```sh
go run ./cmd/keel-dev test-bridge discover --format json
go run ./cmd/keel-dev test-bridge desired-state --format json --id keel::lane::test-fast
go run ./cmd/keel-dev test-bridge run --id keel::lane::test-fast
go run ./cmd/keel-dev test-bridge config-init
go run ./cmd/keel-dev test-bridge config-upgrade
go run ./cmd/keel-dev vsix ci
```

Run `testbridge::maintenance::detect-lanes` once from the Test Explorer to populate
`.vscode/test-lanes.json`. The seeded gate lane ids are:

- `keel::lane::lint`
- `keel::lane::test-fast`
- `keel::lane::test-coverage`

## Protocol Streams

During `test-bridge` verbs, stdout is reserved for protocol JSON or JSONL only. The
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
read or fall back to `testBridge.*` VS Code settings. Consumer mapping is a
config-file concern, never a settings one.

One VS Code setting exists, in the `keel.tests.*` namespace the extension already
owns: `keel.tests.externalRunStaleMs` (default `60000`) is how long the
external-run mirror waits for a producer to write again before it closes a
mirrored run as errored. Raise it for producers that legitimately run quiet for
longer. A producer that writes its terminal event after the deadline reconciles
the run to its own outcome either way — the deadline changes when the mirror
gives up, not who has the last word.

```json
{
  "version": 4,
  "command": "bin/keel-dev",
  "args": [],
  "displayName": "Keel",
  "env": {
    "OPTIONAL_KEY": "optional value"
  },
  "display": {
    "description": true,
    "lastRun": true,
    "desiredState": true,
    "findings": true
  }
}
```

`version`, `command`, `args`, and `displayName` are required. `env` and
`display` are optional.

`display` carries one toggle per rendered fact class — `description`,
`lastRun`, `desiredState`, `findings`, in that rendered order. An absent block,
and an absent key inside a present block, both mean enabled, so upgrading a
workspace hides nothing that was visible beforehand. An unknown key is refused
by name at config read rather than ignored. The toggles are repo-scoped because
this file is committed; `testBridge.*` VS Code settings stay unsupported
(keel/design_decision-8).
The JSON Schema is embedded in `keel/vscode` as `test-bridge-config` and is
drift-checked against the Go type. `CurrentConfigVersion` is the schema version
constant.

`keel-dev test-bridge config-init` writes the default template. `keel-dev
test-bridge config-upgrade` migrates supported older configs forward, preserves
user values, and is byte-idempotent. If the config version is newer than the
binary, upgrade refuses without writing. The extension follows the same rule on
activation: older configs are upgraded by invoking the configured devtool's
`test-bridge config-upgrade` verb and notifying the user to inspect the git
diff; newer configs are read tolerantly and never rewritten.

## Demo Content

Blocked-lane demos are ordinary consumer content. Build `cmd/keel-demo-dev`,
point `.vscode/test-bridge.json` at that binary, then run the advertised
maintenance items through the canonical run verb:

```bash
go run ./cmd/keel-demo-dev test-bridge run --id keel-demo-dev::maintenance::block-bad-lane
go run ./cmd/keel-demo-dev test-bridge run --id keel-demo-dev::lane::go-fail
go run ./cmd/keel-demo-dev test-bridge run --id keel-demo-dev::maintenance::unblock-bad-lane
```

The VSIX no longer emits a demo-family invocation. Its wire surface is the four
canonical shapes above: discover, desired-state, run, and config-upgrade.
