# Keel Test Bridge

VS Code Testing API bridge for devtools that implement Keel's `vscode tests`
protocol.

The extension is activated by a workspace-owned config file:

```json
{
  "version": 2,
  "command": "bin/keel-dev",
  "args": ["vscode", "tests"],
  "displayName": "Keel"
}
```

Run `keel-dev vscode config init` or the `Keel: Initialize Test Bridge Config`
command to create `.vscode/test-bridge.json`. Older configs are upgraded by the
configured devtool on activation; newer configs are read without rewriting.

Local checks:

```sh
pnpm --dir vsix run ci
```
