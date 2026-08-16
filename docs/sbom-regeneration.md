<!-- DHF-REQ: keel/requirement-121, keel/requirement-123 -->

# Regenerating the committed SBOM

The software bill of materials under `docs/auto-generated/sbom/` is a compliance
artifact: `grype` sweeps it for CVEs, and `licenses/` is the redistribution
attribution keel ships. Two requirements govern it, and they constrain the
*environment* of the regeneration, not only its output:

- **`keel/requirement-121` — completeness.** The inventory lists every component
  the module links against, and is regenerated **only in a checkout where every
  ecosystem's dependencies are installed**. syft scans the filesystem, so a
  checkout without `vsix/node_modules` silently drops ~398 npm components and
  blanks every npm license cell (`keel/issue-146`).
- **`keel/requirement-123` — convergence.** Regenerating from the same commit in
  a different checkout location produces byte-identical committed content, so a
  refresh diff shows only real inventory change (`keel/issue-149`).

This page is the single statement of the procedure. It replaces the prose
scattered across those records and supersedes `keel/change_request-188`'s
instruction to regenerate "in the primary checkout", which is wrong — see below.

## 1. Choose the checkout — both constraints bind at once

The regeneration must run in a checkout that is **both**:

1. **Dependency-complete** — `vsix/node_modules` installed, Go module cache warm.
   (requirement-121)
2. **Free of a nested `worktrees/` directory** — nothing matching
   `worktrees/*/go.mod` beneath it. (requirement-123)

These pull against each other on this machine, which is the trap. `/projects/keel`
is the long-lived dependency-complete checkout, but run-queue roots every in-flight
unit at `/projects/keel/worktrees/<unit>/`, so it usually fails constraint 2.
Either of these satisfies both:

- **A unit worktree, after installing the VSIX dependencies** (the normal case):

  ```sh
  cd /projects/keel/worktrees/<unit>
  pnpm --dir vsix install --frozen-lockfile
  ```

  A worktree holds no `worktrees/` directory of its own, and the install makes it
  dependency-complete. This is where `keel/change_request-190` regenerated.

- **The primary checkout with the queue drained** — `/projects/keel` when
  `ls worktrees/` is empty. Verify it, do not assume it.

Confirm before generating:

```sh
find . -maxdepth 3 -name go.mod -not -path './vendor/*' -not -path './.git/*'
ls -d vsix/node_modules
```

The `find` must print exactly `./go.mod`. Its expression is copied from the
generator on purpose — see the next section for why.

## 2. Why constraint 2 is not covered by `.syft.yaml`

`.syft.yaml` excludes `./worktrees/**`, and `grype` runs against the SBOM syft
produces under that config, so **the component inventory is already correct from
either checkout** — the two runs in `keel/issue-149` produced byte-identical
`linked-components.md`.

The license-extraction step is a **separate code path that never reads
`.syft.yaml`**. It enumerates modules with
`find . -maxdepth 3 -name go.mod` (`~/.claude/skills/sbom/scripts/generate.sh`,
at both `:123` and `:150`), which in the primary checkout also matches
`worktrees/<unit>/go.mod` and writes one `docs/auto-generated/sbom/licenses/go-<unit>/`
tree per open worktree — 189 Go license files against 27, all duplicate copies of
keel's own dependency set. That split between the two code paths is why the defect
survived `9ba9df0`, the commit that added the syft exclusion.

The generator is a chezmoi-managed user-level skill, outside keel's change control
(`keel/change_request-184`). Keel's guard is this procedure plus the post-run
check in section 4 — not a fix to the script. The upstream shape (read the
exclusion list from `.syft.yaml` so both code paths share one control point) is
recorded in `keel/issue-149` for whoever changes the dotfile repo.

## 3. Generate

```sh
bash ~/.claude/skills/sbom/scripts/generate.sh
```

`raw/filesystem.cyclonedx.json` is written but **not committed** — it is
gitignored by `docs/auto-generated/sbom/.gitignore`, because syft records each
component's absolute path and the file therefore states which directory produced
it. The `linked-components.md` and `cve.md` headers still name it as their source;
that is provenance for a file the generator produces locally on every run, not a
pointer to a committed artifact.

`raw/cve-filesystem.json` is gitignored for the same reason in a second form
(`keel/ac-496`): grype stamps `descriptor.timestamp` into it, so two runs of the
same commit differ by a wall-clock instant with an identical vulnerability
payload. Stripping that one field repo-side was the alternative and was rejected
— the file also carries about ninety CVE publication dates, which no shape-based
guard can tell apart from a generation stamp. `cve.md` and
`raw/cve-filesystem.txt` are the rendered forms review reads, both carry no
stamp, and both stay tracked.

## 4. Check the result before committing

Run all four. Each corresponds to an acceptance criterion; a failure means the
regeneration was run in the wrong place, not that the criterion should be relaxed.

**a. No worktree license trees** (`keel/ac-473`) — the check is only meaningful
with at least one worktree present somewhere under `/projects/keel`; without one
it passes for the wrong reason.

```sh
ls -d docs/auto-generated/sbom/licenses/go-* | grep -v '/go-root$' && echo FAIL || echo ok
```

**b. No absolute paths in the tracked set** (`keel/ac-472`):

```sh
git add -A docs/auto-generated/sbom
git ls-files docs/auto-generated/sbom | xargs grep -l "$(git rev-parse --show-toplevel)" && echo FAIL || echo ok
```

**c. Coverage did not shrink** (`keel/ac-468`) — the signature of a scan run
without dependencies installed. Compare the candidate against the committed
inventory before landing it:

```sh
git diff --stat docs/auto-generated/sbom/linked-components.md
```

The component row count must not fall, and no license cell that was populated may
have gone empty. If either happened, the checkout was not dependency-complete —
discard the run, fix the environment, regenerate.

**d. Every required module is listed** (`keel/ac-467`):

```sh
awk '/^require/,/^\)/' go.mod | grep -oE '^\s+[a-z0-9./-]+' |
  while read -r m; do grep -q "$m" docs/auto-generated/sbom/linked-components.md || echo "MISSING $m"; done
```

## 5. NOTICE — what the gate holds you to

`NOTICE` at the repository root is regenerated by the same run, and it carries the
**same defect class in two more places**. `build-notice.sh` sets the product name
from `basename` of the checkout (`:34`), so a run in `worktrees/cr-190` writes
"cr-190 — third-party software notice" into a public legal artifact; and it stamps
`Generated: <date>` (`:35`, `:57`), so `NOTICE` cannot converge across two days
even in the primary checkout. Neither `.syft.yaml` nor section 1's environment
rule reaches either one — both are derived from the machine, not from the
inventory.

Correcting them by hand was this page's instruction until `keel/change_request-201`.
It is no longer: the `sbom-convergence` step of `keel-dev ci` asserts both
properties over the tracked artifact set (`keel/ac-495`, `keel/ac-496`), so the
obligation is now the gate's, not the operator's memory. After a regeneration:

- **Delete the `Generated:` line** from `NOTICE` before staging. The generator
  still emits it; the gate reds on it.
- **Leave the product name alone unless the gate says otherwise.** If it reds,
  the name is the checkout's — set it to `keel` on both lines it names.

Everything else in `NOTICE` is derived from `licenses/` and needs no correction.
The gate reports every offending artifact in one run, so fix the whole list it
prints rather than re-running per finding.

The upstream shape (derive the product name from `go.mod` or an explicit flag;
drop the date) is recorded in `keel/issue-157` for whoever changes the dotfile
repo. Keel's guard holds whatever that script does.

## 6. Commit

Stage `NOTICE` with the SBOM tree. Then the usual gate:

```sh
go run ./cmd/keel-dev ci
```

Its `sbom-convergence` step is the only SBOM assertion in the gate, and it is
narrow on purpose: two named properties of the committed set, not that the set is
current. There is still deliberately **no gate step that reds on SBOM drift** —
an owner decision on 2026-08-15, recorded in `keel/requirement-121`'s scope note.
The checks in section 4 stay with the operator performing the regeneration.
