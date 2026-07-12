# Cutting a keel release and bumping consumers

keel ships as **one** public Apache-2.0 Go module,
`github.com/david-aggeler/keel`, plus the Keel Test Bridge VSIX from `vsix/`,
with one tag/version per release. Anonymous `go get` must always work — never
add GOPRIVATE, tokens, netrc, or Docker build secrets on any path.

The whole loop is driven by keel's own CLI, `keel-dev` — keel dogfooding
keel/log and keel/exec. Record operations (issues, CRs, requirements) are **not**
part of this loop; use `openbrain-client` from PATH for those.

## 1. Cut the release

From a clean checkout of `main`:

```sh
just publish vX.Y.Z
```

The `release` verb runs, in order, and **aborts before creating any tag** if a
preflight step fails:

1. **Version check** — `vX.Y.Z` must be a strict semver tag (`v0.1.0`,
   `v1.2.3-rc.1`).
2. **Clean tree** — `git status --porcelain` must be empty.
3. **Tag absent** — `vX.Y.Z` must not already exist locally.
4. **Green core gate** — the full `keel-dev ci` sequence (gofmt, `go build ./...`,
   `go vet ./...`, the compiled-in lint policies, `go test ./...`) must pass.
5. **Green VSIX gate** — `keel-dev vsix ci` runs pnpm compile/lint and the
   headless VS Code extension suite. It fails loudly if Node, pnpm, or xvfb is
   absent.
Only then does it:

6. **Stamp + commit the VSIX version** — `vsix/package.json` is stamped from
   the release tag and the stamp is **committed**, so the tag's tree carries
   the same version as the release asset (one version, no dirty-stamp drift).
7. **VSIX asset build** — `pnpm --dir vsix run package:vsix` builds the
   release asset from that committed state into `bin/`.
8. Create the annotated tag `vX.Y.Z` and push it — plus the release (stamp)
   commit via `git push origin HEAD` — to `origin`, so `origin/main` carries
   the stamped manifest the tag points at.
9. Create the GitHub release with `gh release create ... --generate-notes`,
   attaching `bin/keel-test-bridge-X.Y.Z.vsix`.
10. **Verify anonymous resolution** — in a throwaway module with a fresh
   `GOMODCACHE` and every private-access escape hatch scrubbed
   (`GOPRIVATE`/`GOINSECURE`/`GONOSUMDB` empty, global git config ignored), run
   `go get github.com/david-aggeler/keel@vX.Y.Z` and fail loudly if it does not
   resolve. Retries a few times to absorb proxy.golang.org propagation lag.

keel runs no GitHub Actions CI — the `release` verb's own clean-cache fetch check
(step 10) is the proof that the tag is publicly fetchable. To re-check a tag later,
run `keel-dev verify vX.Y.Z` (see below).

### Prerequisites

- `git`, `go`, `gh` (authenticated: `gh auth status`), Node, pnpm, and xvfb on
  PATH.
- Push access to `origin` and permission to create GitHub releases.

## 2. Verify an existing tag

To re-check a tag without cutting anything (what the tag CI does):

```sh
go run ./cmd/keel-dev verify vX.Y.Z
```

## 3. Bump a consumer

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
