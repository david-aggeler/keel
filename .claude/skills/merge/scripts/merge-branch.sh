#!/usr/bin/env bash
# merge-branch.sh — merge a unit branch onto main for the
# automated-change-request `merge` verb.
#
# It merges <commit-ish> into main with a --no-ff merge commit and prints the
# merge commit SHA as `MERGE_SHA=<sha>` for the caller to capture. Gate
# verification belongs to the invoker's transition_gates runner-owned stages at
# the merged boundary; this command performs the git merge only.
#
# Usage: merge-branch.sh <commit-ish>
#
# The argument is any commit-ish: a branch name, a commit SHA, a tag, or any
# other revision git resolves to a commit. The invoking client resolves its
# branch-or-ref argument to a SHA before appending it, so a branch-only
# argument contract cannot be satisfied through the sanctioned merge path. Where
# the branch is determinable the merge is made by branch name, so the merge
# commit keeps its `Merge branch '<name>'` subject.
#
# Run from the primary checkout with main checked out. Fail-closed: exits
# non-zero WITHOUT advancing main on wrong directory, not-on-main, a dirty
# tracked tree, an argument that resolves to no commit, an argument with no
# commits ahead and no prior merge commit, or a merge conflict.
set -euo pipefail

BRANCH="${1:?usage: merge-branch.sh <commit-ish>}"

existing_merge_for_commit() {
	local commit_tip="$1"
	local merge parent
	local -a parents

	while read -r merge; do
		read -r -a parents < <(git rev-list --parents -n 1 "$merge")
		for parent in "${parents[@]:2}"; do
			if [[ "$parent" == "$commit_tip" ]]; then
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

# DHF-REQ: keel/requirement-162
if ! commit="$(git rev-parse --verify --quiet "${BRANCH}^{commit}")"; then
	echo "merge-branch: argument '${BRANCH}' does not resolve to a commit" >&2
	exit 1
fi

# Merge by branch name where the branch is determinable, so the merge commit on
# main keeps its `Merge branch '<name>'` subject. A branch-name argument names
# its own branch; a commit-ish argument recovers a name only when exactly one
# local branch points at the resolved commit. Zero or several fall back to the
# commit, which degrades the merge subject and nothing else.
# DHF-REQ: keel/requirement-162
merge_ref="$commit"
if git rev-parse --verify --quiet "refs/heads/${BRANCH}" >/dev/null; then
	merge_ref="$BRANCH"
else
	branches=()
	while IFS= read -r ref; do
		branches+=("$ref")
	done < <(git for-each-ref --format='%(refname:short)' --points-at "$commit" refs/heads/)
	if [[ ${#branches[@]} -eq 1 ]]; then
		merge_ref="${branches[0]}"
	fi
fi

# DHF-REQ: keel/requirement-89
if git merge-base --is-ancestor "$commit" HEAD; then
	if merged="$(existing_merge_for_commit "$commit")"; then
		echo "merge-branch: '${BRANCH}' is already merged into main at ${merged}"
		echo "MERGE_SHA=${merged}"
		exit 0
	fi
	echo "merge-branch: '${BRANCH}' has no commits ahead of main; refusing to report a merge" >&2
	exit 1
fi

if ! git merge --no-ff --no-edit "$merge_ref"; then
	git merge --abort 2>/dev/null || true
	echo "merge-branch: merge conflict merging '${merge_ref}' into main; aborted, main unchanged" >&2
	exit 1
fi

merged="$(git rev-parse HEAD)"

echo "merge-branch: merged '${merge_ref}' into main at ${merged}"
echo "MERGE_SHA=${merged}"
