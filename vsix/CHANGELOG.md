# Changelog

Newest first. The Keel Test Bridge extension and the `github.com/david-aggeler/keel`
Go module ship on one tag at one version — an entry here describes the extension
side of that tag.

## v0.7.3

### Changed

- Discovery and desired-state reads share one exported document-size bound
  instead of two inline literals. The bound is 16777216 bytes and is now stated
  in `docs/wire-schema.md`, so a producer can find it without reading this
  extension's source.
- A document that exceeds the bound reports the byte limit and attributes the
  failure to the producer's document size. The previous message was a generic
  Node error paired with a `just build-dev` remedy that had nothing to do with
  the cause.
- A failed discovery refresh now clears the published test tree on **every**
  failure path — size breach, non-zero exit, malformed JSON, missing binary —
  rather than leaving one branch clearing and another silently keeping stale
  items.
- Ancestor (group) items are no longer stamped `skipped` when a descendant
  passes. VS Code's own state rollup is the sole author of ancestor state. This
  changes no icon: the stamp was verified rendering-identical at every moment it
  could occur.
- Run execution scope enqueues leaf items only; desired-state rows are excluded.

### Development

- devDependencies updated, including `@vscode/test-electron` 2.x → 3.1.0,
  `@vscode/vsce`, `mocha`, and TypeScript 5.9.3. No runtime dependencies — the
  extension ships none.

## v0.7.2

No extension changes. Version stamp only, to keep the VSIX and the Go module on
one version.

## v0.7.1

No extension changes. Version stamp only.

## v0.7.0

### Breaking

- Bridge-owned Test Bridge discovery ids move from `keel::maintenance::*` to
  `testbridge::maintenance::*`; rebuild devtools and the VSIX from the same tag.

## v0.4.0

- Keel takes ownership of the VS Code extension as `aggeler.keel-test-bridge`.
- Activate from `.vscode/test-bridge.json` and remove VS Code settings fallback.
- Add config initialization and migration through `keel-dev test-bridge config`.
- Remove the demo-toggle command and `vscode demo` wire path; blocked-lane demos
  now run as ordinary `keel-demo-dev` maintenance items.
