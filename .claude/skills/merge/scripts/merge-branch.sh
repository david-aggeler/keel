#!/usr/bin/env bash
# merge-branch.sh — merge a unit branch onto main for the
# automated-change-request `merge` verb.
#
# It merges <branch> into main with a --no-ff merge commit and prints the merge
# commit SHA as `MERGE_SHA=<sha>` for the caller to capture. Gate verification
# belongs to the invoker's transition_gates runner-owned stages at the merged
# boundary; this command performs the git merge only.
#
# Usage: merge-branch.sh <branch>
#
# Run from the primary checkout with main checked out. Fail-closed: exits
# non-zero WITHOUT advancing main on wrong directory, not-on-main, a dirty
# tracked tree, a missing branch, a branch with no commits ahead and no prior
# merge commit, or a merge conflict.
set -euo pipefail

BRANCH="${1:?usage: merge-branch.sh <branch>}"

existing_merge_for_branch() {
	local branch="$1"
	local branch_tip merge parent
	local -a parents

	branch_tip="$(git rev-parse "$branch")"
	while read -r merge; do
		read -r -a parents < <(git rev-list --parents -n 1 "$merge")
		for parent in "${parents[@]:2}"; do
			if [[ "$parent" == "$branch_tip" ]]; then
				printf '%s\n' "$merge"
				return 0
			fi
		done
	done < <(git rev-list --first-parent --merges HEAD)

	return 1
}

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

# DHF-REQ: keel/requirement-89
if git merge-base --is-ancestor "$BRANCH" HEAD; then
	if merged="$(existing_merge_for_branch "$BRANCH")"; then
		echo "merge-branch: branch '${BRANCH}' is already merged into main at ${merged}"
		echo "MERGE_SHA=${merged}"
		exit 0
	fi
	echo "merge-branch: branch '${BRANCH}' has no commits ahead of main; refusing to report a merge" >&2
	exit 1
fi

if ! git merge --no-ff --no-edit "$BRANCH"; then
	git merge --abort 2>/dev/null || true
	echo "merge-branch: merge conflict merging '${BRANCH}' into main; aborted, main unchanged" >&2
	exit 1
fi

merged="$(git rev-parse HEAD)"

echo "merge-branch: merged '${BRANCH}' into main at ${merged}"
echo "MERGE_SHA=${merged}"
