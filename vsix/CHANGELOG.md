# Changelog

Newest first. The Keel Test Bridge extension and the `github.com/david-aggeler/keel`
Go module ship on one tag at one version — an entry here describes the extension
side of that tag.

## v0.8.1

### Breaking

- The test-bridge config version rises from 3 to 5. `.vscode/test-bridge.json`
  gains a `display` block whose per-class toggles select what the extension
  renders, and version 5 adds the `display.ordinal` toggle to it. The migration
  ladder runs on activation, so an older workspace file is upgraded in place.
- The discovery item's `limitations` prose array leaves the wire. The producer's
  prose now travels as the scalar `description` string and carries nothing else;
  the machine facts that used to ride the array travel as the typed `last_run`
  and `findings` fields. A document still carrying `limitations` is refused by
  name, so a producer that has not migrated fails loudly instead of rendering
  blank.
- Item labels no longer carry an ordinal prefix, and `sort_text` is no longer a
  producer input. The order in which the producer emits its items is the one
  ordering fact: the extension derives the sort key from the emission index, and
  renders a label prefix only where a workspace sets `display.ordinal` to true.

### Added

- `cr-221`: discovery items can carry typed `description`, `findings` and
  `last_run` fields. Producers can now send lane duration, last-run exit code
  and validation findings as structured facts instead of embedding them in prose.
- `cr-222`: `.vscode/test-bridge.json` gains display toggles for the description
  classes the extension composes, so a workspace can choose which secondary
  facts appear in Test Explorer while keeping one shared render order.
- `cr-224`: persistent non-result conditions have a dedicated wire channel and
  render through `TestItem.error`, so parse failures, blocked lanes and
  error-severity findings no longer masquerade as ordinary run results.

### Changed

- `cr-219`: the Go module's `exec/codex` and `exec/claude` adapters share one
  outcome contract. Below the output ceiling, a non-zero exit or a failing
  terminal event fails the run, and callers get the decoded result back with the
  error where the CLI produced one.
- `cr-223`: the Breaking entries above describe its wire-shape change: config
  version 5, removal of `limitations`, and emission-index ordering with optional
  ordinal rendering.
- `cr-224`: warning-severity findings stay in the composed description, while
  error-severity findings move to the persistent error surface. Several
  persistent conditions on one item accumulate into a single error value instead
  of overwriting each other.

### Development

- `cr-220`: every `keel-dev ci` stage is also invocable on its own by name, with
  the same verdict and failure text it has inside the full gate. Bare
  `keel-dev ci` still runs the complete battery in order.

## v0.8.0

### Breaking

- The declared minimum VS Code version rises from `^1.102.0` to `^1.125.0`. Every
  tracked manifest in the repository now states that one value, and the policy gate
  reads them all. Upgrade VS Code to 1.125 or later before installing this VSIX.
- The desired-state wire document changes shape. `mutually_exclusive`, `active`,
  `current` and `action` leave the `Limitations` display array and become typed
  fields covered by the JSON schemas. That left `Limitations` carrying prose
  alone; v0.8.1 retired the array outright in favour of the scalar `description`.
  The extension no longer recovers state by substring match.
- A row's `active` fact is derived from the row's own probe, in every group. A
  producer can no longer declare a row active while its probe reports the resource
  unsatisfied.

### Added

- `src/protocol.ts` is pinned to the embedded schemas inside the VSIX gate, so the
  TypeScript half of the wire contract cannot go stale silently.
- The external-run mirror's stale-close deadline is a workspace setting. It was a
  module literal with no way to change it.

### Changed

- Run events render as the producer writes them. The bridge streams each event
  while the child is still running, in place of a post-exit burst.
- An exclusive desired-state group is invalidated at run **start**: every peer is
  stamped skipped before the devtool child spawns, and the group settles on
  bridge-served truth on every abort path. Peers previously kept a stale passed
  icon for the whole reconcile.
- A test inside a lane navigates to its source. The `covers` aliases now carry the
  canonical item's URI and range.
- The mirror skips the spool of the run the editor itself executed. One Test
  Explorer run previously produced two run-history entries.
- The mirror's stale close names producer silence rather than stream truncation,
  and a terminal event that arrives after the close reconciles the item.

### Development

- Toolchain moves to `@types/vscode` 1.125, `@types/node` 24, and TypeScript 7.
  The Node major is anchored to the runtime VS Code 1.125 ships, against a citable
  source.

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
