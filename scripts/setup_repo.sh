#!/usr/bin/env bash
# setup_repo.sh — repo-level bootstrap for keel.
# Assumes machine-level (setup_as_root.sh) and user-level (setup_user.sh)
# setup are already complete. Builds keel-dev and keel-demo and proves the gate
# is green.
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

echo "Building keel-dev and keel-demo into ./bin..."
mkdir -p bin
# DHF-REQ: keel/requirement-27
go build -o bin/keel-dev ./cmd/keel-dev
go build -o bin/keel-demo ./cmd/keel-demo

echo "Running the verification gate (keel-dev ci)..."
go run ./cmd/keel-dev ci

echo ""
echo "Repo bootstrap complete. The gate is green; keel-dev is at ./bin/keel-dev and keel-demo is at ./bin/keel-demo."
