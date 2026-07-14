# Changelog

## Unreleased

- Keel takes ownership of the VS Code extension as `aggeler.keel-test-bridge`.
- Activate from `.vscode/test-bridge.json` and remove VS Code settings fallback.
- Add config initialization and migration through `keel-dev test-bridge config`.
- Remove the demo-toggle command and `vscode demo` wire path; blocked-lane demos
  now run as ordinary `keel-demo-dev` maintenance items.
