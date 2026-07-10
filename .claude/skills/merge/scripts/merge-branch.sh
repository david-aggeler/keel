#!/usr/bin/env bash
# merge-branch.sh — guarded merge of a unit branch onto main for the
# automated-change-request `merge` verb.
#
# It merges <branch> into main with a --no-ff merge commit, then re-verifies the
# merged tree with keel's full gate (`go run ./cmd/keel-dev ci`). If the gate is
# red, it reverts the merge in place so main never lands red — the issue-166
# post-merge re-verify guard that a raw `git merge` would bypass. On success it
# prints the merge commit SHA as `MERGE_SHA=<sha>` for the caller to capture.
#
# Usage: merge-branch.sh <branch>
#
# Run from the primary checkout with main checked out. Fail-closed: exits
# non-zero WITHOUT advancing main on wrong directory, not-on-main, a dirty
# tracked tree, a missing branch, a merge conflict, or a red post-merge gate.
set -euo pipefail

BRANCH="${1:?usage: merge-branch.sh <branch>}"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

current="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$current" != "main" ]]; then
	echo "merge-branch: HEAD is '$current', expected 'main' (run from the primary checkout)" >&2
	exit 1
fi

# Tracked-file cleanliness only; untracked files (e.g. .mcp.json) are ignored.
if ! git diff-index --quiet HEAD --; then
	echo "merge-branch: working tree has uncommitted tracked changes; refusing to merge" >&2
	exit 1
fi

if ! git rev-parse --verify "refs/heads/${BRANCH}" >/dev/null 2>&1; then
	echo "merge-branch: no branch '${BRANCH}' to merge" >&2
	exit 1
fi

before="$(git rev-parse HEAD)"

if ! git merge --no-ff --no-edit "$BRANCH"; then
	git merge --abort 2>/dev/null || true
	echo "merge-branch: merge conflict merging '${BRANCH}' into main; aborted, main unchanged" >&2
	exit 1
fi

merged="$(git rev-parse HEAD)"

# Post-merge re-verify: keel's full gate must be green on the merged tree.
if ! go run ./cmd/keel-dev ci; then
	echo "merge-branch: post-merge gate red; reverting merge to keep main green" >&2
	git reset --hard "$before"
	exit 1
fi

echo "merge-branch: merged '${BRANCH}' into main at ${merged}"
echo "MERGE_SHA=${merged}"
