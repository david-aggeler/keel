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
> gate). `/publish` is the outward-facing step above them. The root `VERSION` file is the
> release-of-record: `keel-dev release <version>` refuses before tagging unless `VERSION`
> equals the release argument without the leading `v`.

## Step 1: Determine the version

keel releases take an explicit semver tag (`vX.Y.Z`, or `vX.Y.Z-rcN`). Show the latest
release and the commits since it so the user (or you) can choose the bump:

    just release-commits

If the user gave an explicit version, validate the `vX.Y.Z` / `vX.Y.Z-rcN` shape and use it.
Otherwise propose the next version from the commit types (a `feat:` since the last tag -> a
minor bump; only `fix:`/`chore:`/`docs:` -> a patch bump) and ask the user to confirm before
proceeding. Before release, confirm the root `VERSION` file contains the same bare semver
(`X.Y.Z`) and has already been committed.

## Step 2: Confirm scope

Show a short summary and ask for an explicit go-ahead:

- Version: `<version>`
- Root `VERSION`: `X.Y.Z` already committed
- Commits since the last tag: (count/list from Step 1)
- What will run: `keel-dev release <version>` — clean-tree + no-existing-tag + green-gate
  preflight, then stamp & build the VSIX, tag, create the GitHub Release with the VSIX
  attached, and verify anonymous `go get`.
- Gold sync: after a successful release, `/publish` advances gold `product_version` for
  product `keel` to `X.Y.Z` through the gold MCP admin product-plane tools. This sync
  stays outside `keel-dev`; do not add SoR client code to the binary.
- Release notes: after a successful release, `/publish` brings the line's `release_notes`
  record current — coverage range, breaking changes, migration rows — and sets it
  `released`. A shipped version never leaves its note at `draft`.

Ask: "Proceed with release `<version>`? This tags the repo and cuts a public GitHub Release.
(yes/no)". Do not proceed without an explicit "yes".

> No separate pre-flight is needed here: `keel-dev release` **refuses** on a VERSION/tag
> mismatch, a dirty tree, an existing tag, a red core gate, or a red VSIX gate before it
> changes anything. Running `just ci` first is optional reassurance, not a requirement.

## Step 3: Run the release

    just publish <version>

Stream the output. `keel-dev release <version>` runs the full pipeline:

1. Preflight — refuse on VERSION/tag mismatch / dirty tree / existing tag / red `keel-dev ci` / red VSIX gate.
2. Stamp and build the VSIX release asset (one version across module + VSIX).
3. Create the annotated tag.
4. Create the GitHub Release with the VSIX attached.
5. Anonymous-fetch check — a clean-cache `go get github.com/david-aggeler/keel@<version>`
   must resolve.

## Step 4: Sync gold product_version

<!-- DHF-REQ: keel/requirement-112 -->

After `just publish <version>` succeeds, advance gold's `product_version` for
product `keel` through the gold MCP admin product-plane tools — no record
export/import round-trip, no hand-edited JSON bundle:

1. `admin_list_product_versions(product: "keel")` — locate the `X.Y.Z` row (it
   should be `in_development`).
2. `admin_update_product_version` — write the release evidence onto that row's
   `Body` and `ReleaseNotes`: tag `vX.Y.Z`, stamp commit, VSIX asset name,
   anonymous `go get` result, and the release notes ref when one exists.
3. `admin_advance_product_version_status` — advance the row to `released`.
4. Ensure exactly one later row remains `in_development` for follow-up work;
   create it with `admin_create_product_version` when opening the next line.
5. Re-run `admin_list_product_versions(product: "keel")` and confirm the
   `X.Y.Z` row reads `released` with exactly one open development line.

The surface writes `Status`, `Body`, `ReleaseNotes`, `GitTag`, and
`ReleaseDate`; row IDs and timestamps are server-owned.

If the gold MCP is unavailable in this session, stop and hand back to the
owner with that exact blocker; do not implement a gold client in `keel-dev`.

## Step 5: Bring the release notes current

<!-- DHF-REQ: keel/requirement-112 -->

The `release_notes` record is what a consumer actually reads. A shipped version whose
note still says `draft`, or whose coverage stops at an earlier unit, is a stale document
with a release behind it. Bring it current **before** reporting success.

keel keeps one line-level note per minor line, titled `keel vX.Y.x`, which accumulates as
units merge. Find it, then extend rather than replace:

- `list_release_notes(product="keel")` — locate the note whose title covers this line.
- Extend it with `update_release_notes(field="details", edits=[...])` using **anchored
  edits**. Never rewrite the whole `details` body: the note carries curated prose from
  earlier units, and a full replace silently drops it.
- If no note covers the line, create one with `create_release_notes` against the
  `release_notes` template.

What the note must carry for the version just cut:

| section | content |
|---|---|
| header | the unit range now covered, and a table of which version shipped which units |
| Highlights / New / Improved / Fixed | one line per unit, each naming its `change_request` |
| Breaking changes | one subsection per break: **what changed**, **why**, **what to do** |
| Upgrade & migration | a row per affected consumer, naming the file or setting to edit |
| Known issues | anything open at the cut, and any unit parked rather than landed |

Then set `status: released` on the note and refresh its `summary`.

**Advance the note's `product_version` pointer.** keel keeps ONE note per minor line, and
that note's `product_version` tracks the line's currently **in-development** version — not
the version just shipped. The note is created against an in-development version and
accumulates as units merge into it, so after Step 4 opens the next development line the
pointer must follow:

    product_version: keel/<the version Step 4 left in_development>

Leaving it on the version just released points the line's living document at a closed
version, and the next unit to merge accumulates against the wrong row. This field lives on
the gold record, not in the repository.

Two rules learned from live releases:

- **Name external consumers concretely.** A migration row that says "update your
  producer" is not actionable. Name the repository, the file, and the symbol where it is
  known — and state the commit you measured it at, so a reader can tell a stale reading
  from a current one.
- **A break at a patch level needs its reason in the note.** Semver alone will not carry
  it, so the note must say why the level was chosen and who chose it.

## Step 6: Report results

On success:
- Report the GitHub Release URL and confirm the VSIX asset is attached.
- Confirm the tag `<version>` was created and the anonymous `go get` check passed.
- Confirm gold `product_version` for product `keel` now reflects `X.Y.Z` and that the sync was
  performed through the gold MCP admin product-plane tools.
- Confirm the line's `release_notes` record covers this version, names every breaking change
  with its migration action, and is `released` rather than `draft`.

On failure, identify which preflight/step failed and suggest remediation:

| Failure | Likely cause | Remediation |
|---|---|---|
| Preflight: dirty tree | Uncommitted changes | `git status`; commit or stash, then re-run |
| Preflight: VERSION mismatch | `VERSION` was not bumped to `X.Y.Z` | Edit and commit `VERSION`, then re-run |
| Preflight: tag exists | Previous aborted release | `git push origin :refs/tags/<version>` and `git tag -d <version>`, then re-run |
| Preflight: red gate | `keel-dev ci` or VSIX gate failing | Run `just ci` (and `just vsix`) to reproduce; fix, then re-run |
| GitHub Release | `gh` not installed / not authed / network | `gh auth status`; `gh auth login`; re-run |
| Anonymous fetch | Module proxy lag or a private-path leak | Confirm no GOPRIVATE/token was introduced (never allowed); retry the fetch check |
| Gold sync | gold MCP unavailable in the session, or the `X.Y.Z` row is missing | Restore gold MCP access or create the row via `admin_create_product_version`; hand back if blocked; never add SoR client code to keel-dev |
| Release notes | no note covers the line, or an edit anchor no longer matches | Create the note from its template, or re-read `details` and re-anchor; never full-replace the body |

## What this skill never does

- Bypasses `keel-dev release` — the verb owns the pipeline; this skill never hand-rolls
  tagging, VSIX build, or GitHub Release creation
- Embeds raw `go` / `gh` / `git` commands — version/preflight/release steps are `just` recipes
- Runs the release without explicit user confirmation
- Adds GOPRIVATE, tokens, or any private build path (anonymous `go get` must always work)
- Reports a release as done while the line's `release_notes` record is still `draft` or
  stops short of the units just shipped
- Rewrites a `release_notes` body wholesale — curated prose from earlier units is extended
  by anchored edit, never replaced
- Force-pushes or force-tags
