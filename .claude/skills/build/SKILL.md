---
name: build
description: Local green/red gate for keel — compile, unit-test, and build the release artifacts (binaries + VSIX), all through the Justfile. Run when the user says "/build", "does it build?", "is it green?", "build the binaries", "compile and test", "verify green". keel has no container or deploy step; the outward step is /publish (keel-dev release). Commands live in the Justfile — this skill only names `just` recipes.
---

# Build

Local green/red gate for keel: compile every package, run the unit tests, and build the
release artifacts (the admitted `cmd/` binaries + the VSIX package). Every step is a
Justfile recipe — this skill names recipes, it never embeds raw `go`/`pnpm`/`keel-dev`
commands.

> keel is one Go module with a VSIX under `vsix/` — no containers, no `go.work`, no
> deploy target. The full verification gate is `just ci` (`keel-dev ci`); the outward
> release step is **`/publish`** (`keel-dev release`). `/build` sits below both: if
> `/build` is green, the artifacts are producible.

## Steps

**Step 1 — compile every package**

    just go-build

Host-only compile over the whole module. If it fails, report the package and stop.

**Step 2 — unit tests**

    just test

The fast unit pass. The full gate (gofmt / vet / lint / coverage floor) is `just ci` —
run that via `/ci` when you need the gate, not just a build check. If a test fails,
surface the failing package + test name and stop before building artifacts.

**Step 3 — build the release artifacts**

    just build

Builds `keel-dev`, `keel-demo`, `keel-demo-dev` into `./bin` and packages the VSIX. If a
binary or the VSIX package fails, report which one.

## Report format

    Compile: OK / FAILED (which package)
    Tests:   OK / FAILED (list failing tests)
    Build:   OK / FAILED (which artifact)

If everything is green, say so in one line and note the artifacts are producible (ready
for `/publish`). If anything is red, say exactly what failed and at which step.

## What this skill never does

- Embeds raw `go` / `pnpm` / `keel-dev` commands — every step is a `just` recipe
- Proceeds to a later step after an earlier one failed
- Substitutes for the full gate (`just ci`) or the release preflight (`/publish`)
