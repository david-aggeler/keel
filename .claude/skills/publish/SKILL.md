---
name: publish
description: Publish a keel release — cut a tag and a GitHub Release with the VSIX attached, via the canonical `keel-dev release` verb. Use when the user says "/publish", "publish a version", "publish vX.Y.Z", "cut a release", "make a release", "ship a release". keel is one Go module + a VSIX; there are no OCI images, no GHCR, no homelab/coal. `keel-dev release` owns the whole pipeline (clean-tree/tag/gate preflight → stamp+build VSIX → tag → GitHub Release with VSIX attached → anonymous go-get check). Commands live in the Justfile — this skill only names `just` recipes.
---

# Publish

Publish a new keel version: cut the tag and a GitHub Release with the VSIX asset attached.
The entire pipeline is owned by one verb, `keel-dev release` (exposed as `just publish
<version>`) — this skill's job is to pick the version, confirm scope, run it, and report.

> keel is a single public Go module (`github.com/david-aggeler/keel`) plus the Keel Test
> Bridge VSIX under `vsix/`. **One tag, one version across the module and the VSIX.** There
> are no container images, no GHCR, and no homelab/coal deploy — publishing makes the tag
> and VSIX available via GitHub Releases and anonymous `go get`. Anonymous `go get` must
> always resolve; `keel-dev release` verifies this as its final step.
>
> Sibling: **`/build`** (compile + unit-test + build artifacts) and **`just ci`** (the full
> gate). `/publish` is the outward-facing step above them.

## Step 1: Determine the version

keel releases take an explicit semver tag (`vX.Y.Z`, or `vX.Y.Z-rcN`). Show the latest
release and the commits since it so the user (or you) can choose the bump:

    just release-commits

If the user gave an explicit version, validate the `vX.Y.Z` / `vX.Y.Z-rcN` shape and use it.
Otherwise propose the next version from the commit types (a `feat:` since the last tag → a
minor bump; only `fix:`/`chore:`/`docs:` → a patch bump) and ask the user to confirm before
proceeding.

## Step 2: Confirm scope

Show a short summary and ask for an explicit go-ahead:

- Version: `<version>`
- Commits since the last tag: (count/list from Step 1)
- What will run: `keel-dev release <version>` — clean-tree + no-existing-tag + green-gate
  preflight, then stamp & build the VSIX, tag, create the GitHub Release with the VSIX
  attached, and verify anonymous `go get`.

Ask: "Proceed with release `<version>`? This tags the repo and cuts a public GitHub Release.
(yes/no)". Do not proceed without an explicit "yes".

> No separate pre-flight is needed here: `keel-dev release` **refuses** on a dirty tree, an
> existing tag, a red core gate, or a red VSIX gate before it changes anything. Running
> `just ci` first is optional reassurance, not a requirement.

## Step 3: Run the release

    just publish <version>

Stream the output. `keel-dev release <version>` runs the full pipeline:

1. Preflight — refuse on dirty tree / existing tag / red `keel-dev ci` / red VSIX gate.
2. Stamp and build the VSIX release asset (one version across module + VSIX).
3. Create the annotated tag.
4. Create the GitHub Release with the VSIX attached.
5. Anonymous-fetch check — a clean-cache `go get github.com/david-aggeler/keel@<version>`
   must resolve.

## Step 4: Report results

On success:
- Report the GitHub Release URL and confirm the VSIX asset is attached.
- Confirm the tag `<version>` was created and the anonymous `go get` check passed.

On failure, identify which preflight/step failed and suggest remediation:

| Failure | Likely cause | Remediation |
|---|---|---|
| Preflight: dirty tree | Uncommitted changes | `git status`; commit or stash, then re-run |
| Preflight: tag exists | Previous aborted release | `git push origin :refs/tags/<version>` and `git tag -d <version>`, then re-run |
| Preflight: red gate | `keel-dev ci` or VSIX gate failing | Run `just ci` (and `just vsix`) to reproduce; fix, then re-run |
| GitHub Release | `gh` not installed / not authed / network | `gh auth status`; `gh auth login`; re-run |
| Anonymous fetch | Module proxy lag or a private-path leak | Confirm no GOPRIVATE/token was introduced (never allowed); retry the fetch check |

## What this skill never does

- Bypasses `keel-dev release` — the verb owns the pipeline; this skill never hand-rolls
  tagging, VSIX build, or GitHub Release creation
- Embeds raw `go` / `gh` / `git` commands — version/preflight/release steps are `just` recipes
- Runs the release without explicit user confirmation
- Adds GOPRIVATE, tokens, or any private build path (anonymous `go get` must always work)
- Force-pushes or force-tags
