# Cutting a keel release and bumping consumers

keel ships as **one** public Apache-2.0 Go module,
`github.com/david-aggeler/keel`, with one tag per release. Anonymous `go get`
must always work — never add GOPRIVATE, tokens, netrc, or Docker build secrets
on any path.

The whole loop is driven by keel's own CLI, `keel-dev` — keel dogfooding
keel/log and keel/exec. Record operations (issues, CRs, requirements) are **not**
part of this loop; use `openbrain-client` from PATH for those.

## 1. Cut the release

From a clean checkout of `main`:

```sh
go run ./cmd/keel-dev release vX.Y.Z
```

The `release` verb runs, in order, and **aborts before creating any tag** if a
preflight step fails:

1. **Version check** — `vX.Y.Z` must be a strict semver tag (`v0.1.0`,
   `v1.2.3-rc.1`).
2. **Clean tree** — `git status --porcelain` must be empty.
3. **Tag absent** — `vX.Y.Z` must not already exist locally.
4. **Green gate** — the full `keel-dev ci` sequence (gofmt, `go build ./...`,
   `go vet ./...`, `go test ./...`) must pass.

Only then does it:

5. Create the annotated tag `vX.Y.Z` and push it to `origin`.
6. Create the GitHub release with `gh release create ... --generate-notes`.
7. **Verify anonymous resolution** — in a throwaway module with a fresh
   `GOMODCACHE` and every private-access escape hatch scrubbed
   (`GOPRIVATE`/`GOINSECURE`/`GONOSUMDB` empty, global git config ignored), run
   `go get github.com/david-aggeler/keel@vX.Y.Z` and fail loudly if it does not
   resolve. Retries a few times to absorb proxy.golang.org propagation lag.

Pushing the `vX.Y.Z` tag also triggers `.github/workflows/release.yml` on the
self-hosted runner, which independently runs `keel-dev verify vX.Y.Z` — a second,
mechanical proof that the tag is publicly fetchable.

### Prerequisites

- `git`, `go`, and `gh` (authenticated: `gh auth status`) on PATH.
- Push access to `origin` and permission to create GitHub releases.

## 2. Verify an existing tag

To re-check a tag without cutting anything (what the tag CI does):

```sh
go run ./cmd/keel-dev verify vX.Y.Z
```

## 3. Bump the consumers

keel is consumed by **vela** (first) and **openbrain**. In each consumer repo:

```sh
go get github.com/david-aggeler/keel@vX.Y.Z
go mod tidy
```

Then confirm the build is green **with no local `replace`/`use` directive** for
keel and **no credentials** in the Docker build:

- **vela** — build the module and its image; the Docker stage must resolve keel
  anonymously (`go get` with no secrets mounted) and build green.
- **openbrain** — until the bridge exit (keel/dd_plan-1 iteration 5), openbrain's
  `go.work` still carries `use /projects/keel`. Migrating a consumer onto a
  tagged keel means removing that `use` line (and any `replace`) so the tagged
  module is resolved from the proxy, then running openbrain's full gate.

### Bridge exit

Once keel's own pipeline is green and at least one consumer builds on a tagged
release, remove the transitional `use /projects/keel` (and any keel `replace`)
from openbrain's `go.work`. keel then stands alone on its own CI + release loop.

## Versioning

Semantic versioning. Pre-1.0, breaking changes bump the minor. The subpackages
(`log`, `exec`, `exec/claude`, `exec/codex`) share the single module version —
there is no per-package tag.
