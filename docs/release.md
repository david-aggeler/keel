# Cutting a keel release and bumping consumers

keel ships as **one** public Apache-2.0 Go module,
`github.com/david-aggeler/keel`, plus the Keel Test Bridge VSIX from `vsix/`,
with one tag/version per release. Anonymous `go get` must always work — never
add GOPRIVATE, tokens, netrc, or Docker build secrets on any path.

The whole loop is driven by keel's own CLI, `keel-dev` — keel dogfooding
keel/log and keel/exec. Record operations (issues, CRs, requirements) are **not**
part of this loop; use `openbrain-client` from PATH for those. The root
`VERSION` file is the release-of-record and must be bumped before releasing.

## 1. Cut the release

From a clean checkout of `main`:

```sh
printf 'X.Y.Z\n' > VERSION
git add VERSION
git commit -m "chore: bump VERSION to X.Y.Z"
just publish vX.Y.Z
```

The `release` verb runs, in order, and **aborts before creating any tag** if a
preflight step fails:

1. **Version check** — `vX.Y.Z` must be a strict semver tag (`v0.1.0`,
   `v1.2.3-rc.1`).
2. **VERSION match** — root `VERSION` must contain `X.Y.Z` exactly. A mismatch
   aborts before any tag or GitHub release is created.
3. **Clean tree** — `git status --porcelain` must be empty.
4. **Tag absent** — `vX.Y.Z` must not already exist locally.
5. **Stamp + commit the VSIX version** — `vsix/package.json` is stamped from
   the release tag and the stamp is **committed**, so the gates, tag, and
   release asset all use the same version (one version, no dirty-stamp drift).
6. **Green core gate** — the full `keel-dev ci` sequence (gofmt, `go build ./...`,
   `go vet ./...`, the compiled-in lint policies, `go test ./...`) must pass.
7. **Green VSIX gate** — `keel-dev vsix ci` runs pnpm compile/lint and the
   headless VS Code extension suite. It fails loudly if Node, pnpm, or xvfb is
   absent.
Only then does it:

8. **VSIX asset build** — `pnpm --dir vsix run package:vsix` builds the
   release asset from that committed state into `bin/`.
9. Create the annotated tag `vX.Y.Z` and push it — plus the release (stamp)
   commit via `git push origin HEAD` — to `origin`, so `origin/main` carries
   the stamped manifest the tag points at.
10. Create the GitHub release with `gh release create ... --generate-notes`,
   attaching `bin/keel-test-bridge-X.Y.Z.vsix`.
11. **Verify anonymous resolution** — in a throwaway module with a fresh
   `GOMODCACHE` and every private-access escape hatch scrubbed
   (`GOPRIVATE`/`GOINSECURE`/`GONOSUMDB` empty, global git config ignored), run
   `go get github.com/david-aggeler/keel@vX.Y.Z` and fail loudly if it does not
   resolve. Retries a few times to absorb proxy.golang.org propagation lag.

keel runs no GitHub Actions CI — the `release` verb's own clean-cache fetch check
(step 11) is the proof that the tag is publicly fetchable. To re-check a tag later,
run `keel-dev verify vX.Y.Z` (see below).

### Prerequisites

- `git`, `go`, `gh` (authenticated: `gh auth status`), Node, pnpm, and xvfb on
  PATH.
- Push access to `origin` and permission to create GitHub releases.

## 2. Sync gold product_version

<!-- DHF-REQ: keel/requirement-112 -->

After the release succeeds, advance gold's `product_version` for product `keel`
through the gold MCP admin product-plane tools (a release is driven from a
session that has the gold MCP; no export/import round-trip, no hand-edited
JSON bundle):

1. `admin_list_product_versions(product: "keel")` — locate the row for `X.Y.Z`
   (its status should be `in_development`).
2. `admin_update_product_version` — write the release evidence onto that row:
   tag `vX.Y.Z`, stamp commit, VSIX asset name, the anonymous `go get` result,
   and the `release_notes` ref when one exists (fields `Body` and
   `ReleaseNotes`).
3. `admin_advance_product_version_status` — advance the row to `released`.
4. Ensure exactly one later row is `in_development` for follow-up work —
   create it with `admin_create_product_version` when opening the next line.
5. Re-run `admin_list_product_versions(product: "keel")` and confirm the
   `X.Y.Z` row reads `released` and the invariant of exactly one open
   development line holds.

The surface writes `Status`, `Body`, `ReleaseNotes`, `GitTag`, and
`ReleaseDate`; row IDs and timestamps are server-owned. Executing the
`openbrain-client` binary from keel tooling is permitted, but the
product_version sync does not use it — and no SoR client code ever enters
`keel-dev` or keel's compile graph.

If the gold MCP is unavailable in the releasing session, stop and hand back to
the owner with that blocker; do not add SoR client code to `keel-dev`.

## 3. Verify an existing tag

To re-check a tag without cutting anything (what `keel-dev verify` proves —
keel runs no CI service; this is an on-demand check):

```sh
go run ./cmd/keel-dev verify vX.Y.Z
```

## 4. Bump a consumer

In each consumer repo that depends on keel:

```sh
go get github.com/david-aggeler/keel@vX.Y.Z
go mod tidy
```

Then confirm the build is green **with no local `replace`/`use` directive** for
keel and **no credentials** in the Docker build — the Docker stage must resolve
keel anonymously (`go get` with no secrets mounted) and build green. If the
consumer carries a transitional local `use`/`replace` pointing at a keel checkout,
migrating it onto the tagged release means removing that directive so the tagged
module resolves from the proxy, then running the consumer's full gate.

### Bridge exit

Once keel's own pipeline is green and at least one consumer builds on a tagged
release, remove any transitional local `use`/`replace` directive that points a
consumer's build at a keel checkout. keel then stands alone on its own CI +
release loop.

## Versioning

Semantic versioning. Pre-1.0, breaking changes bump the minor. The subpackages
(`log`, `exec`, `exec/claude`, `exec/codex`) share the single module version —
there is no per-package tag.

## v0.5 migration notes

### keel/log sink verbosity fields

`log.Config.Level` was removed for v0.5. Use the explicit sink-specific fields:

- `log.Config.ConsoleVerbosity` controls the console sink minimum severity. Nil
  preserves the previous default of `slog.LevelInfo`.
- `log.Config.FileVerbosity` controls both file sinks (`TextDir` and
  `JSONLDir`). Nil preserves the previous forensic default of
  `slog.LevelDebug`.

String configuration should use `log.LevelFromString` and `log.LevelToString`.
The accepted vocabulary is `debug`, `info`, `warn`, and `error`; an empty string
maps to `info`, and unknown non-empty strings return an error.
