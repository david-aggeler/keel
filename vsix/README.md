# Keel Test Bridge

VS Code Testing API bridge for devtools that implement Keel's `test-bridge`
protocol.

The extension is activated by a workspace-owned config file:

```json
{
  "version": 3,
  "command": "bin/keel-dev",
  "args": [],
  "displayName": "Keel"
}
```

Run `keel-dev test-bridge config init` or the `Keel: Initialize Test Bridge Config`
command to create `.vscode/test-bridge.json`. Older configs are upgraded by the
configured devtool on activation; newer configs are read without rewriting.

Local checks:

```sh
pnpm --dir vsix run ci
```
