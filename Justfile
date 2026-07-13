# keel task runner — thin wrappers around keel-dev and go.
# Commands live HERE. Skills call `just <recipe>`; they never embed raw
# go/pnpm/keel-dev/git commands.

# List available recipes.
default:
    @just --list

# Compile every package in the module (host-only, no artifacts).
go-build:
    go build ./...

# Run the unit tests (fast). The full gate is `just ci`.
test:
    go test ./...

# Build admitted cmd/ binaries and the VSIX release artifact into ./bin + vsix/.
build:
    mkdir -p bin
    # DHF-REQ: keel/requirement-27
    go build -o bin/keel-dev ./cmd/keel-dev
    go build -o bin/keel-demo ./cmd/keel-demo
    go build -o bin/keel-demo-dev ./cmd/keel-demo-dev
    # DHF-REQ: keel/requirement-45
    pnpm --dir vsix run package:vsix

# Run the verification gate (canonical: keel-dev ci — gofmt, build, vet, lint, test, coverage).
ci:
    go run ./cmd/keel-dev ci

# Run the VSIX sibling gate (node-backed: pnpm build/lint/headless suite).
vsix:
    go run ./cmd/keel-dev vsix ci

# Show what a branch would merge into main (stat summary).
merge-diff branch:
    git --no-pager diff --stat main...{{branch}}

# Guarded merge of a unit branch into main: --no-ff, post-merge full gate, auto-revert on red.
merge-branch branch:
    bash .claude/skills/merge/scripts/merge-branch.sh {{branch}}

# Show the latest release tag and the commits a release would include since it.
release-commits:
    #!/usr/bin/env bash
    set -euo pipefail
    git fetch --tags --quiet
    latest=$(git tag --sort=-version:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n1 || true)
    if [ -z "$latest" ]; then
        echo "No release tag yet — all commits would be included:"
        git --no-pager log --oneline
    else
        echo "Latest release: $latest"
        echo "Commits since $latest:"
        git --no-pager log --oneline "$latest..HEAD"
    fi

# Publish a keel release: preflight, build+attach VSIX, tag, GitHub release, anon-fetch check.
publish version:
    go run ./cmd/keel-dev release {{version}}

# Re-verify anonymous module fetch for an existing tag.
verify version:
    go run ./cmd/keel-dev verify {{version}}

# Push the current branch to origin (never force). Used by /merge after a guarded merge.
push:
    git push

# Remove build artifacts.
clean:
    rm -rf bin
