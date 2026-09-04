#!/usr/bin/env bash
# setup_repo.sh — repo-level bootstrap for keel.
# Assumes machine-level (setup_as_root.sh) and user-level (setup_user.sh)
# setup are already complete. Builds keel-dev, keel-demo, and keel-demo-dev and
# proves the gate is green.
#
# keel has no Docker stack, database, or .env — the whole "bring up services"
# flow that openbrain's setup_repo.sh runs does not apply here. The repo is
# ready when `keel-dev ci` passes.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

export PATH="/usr/local/go/bin:$PATH"

if ! command -v go >/dev/null 2>&1; then
	echo "go not on PATH — run scripts/setup_as_root.sh first." >&2
	exit 1
fi

echo "Building keel-dev, keel-demo, and keel-demo-dev into ./bin..."
mkdir -p bin
# The build number (commit count since the repository's first commit) is
# stamped so --version renders MAJOR.MINOR.PATCH.BUILD.
# DHF-REQ: keel/requirement-27, keel/requirement-110 (keel/ac-690)
BUILD_NUMBER="$(git rev-list --count HEAD 2>/dev/null || echo "")"
LDFLAGS="-X github.com/david-aggeler/keel.BuildNumber=${BUILD_NUMBER}"
go build -ldflags "$LDFLAGS" -o bin/keel-dev ./cmd/keel-dev
go build -ldflags "$LDFLAGS" -o bin/keel-demo ./cmd/keel-demo
go build -ldflags "$LDFLAGS" -o bin/keel-demo-dev ./cmd/keel-demo-dev

echo "Running the verification gate (keel-dev ci)..."
go run ./cmd/keel-dev ci

echo ""
echo "Repo bootstrap complete. The gate is green; keel-dev is at ./bin/keel-dev, keel-demo is at ./bin/keel-demo, and keel-demo-dev is at ./bin/keel-demo-dev."
