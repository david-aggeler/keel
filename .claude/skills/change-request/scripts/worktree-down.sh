#!/usr/bin/env bash
# Author-side pre-merge teardown. Refuses dirty worktrees. Does NOT delete the branch.
# Usage: worktree-down.sh <kind> <seq> <slug>
# Output (success): down <kind>-<seq>-<slug> <absolute-path>
# Output (no-op):   down-noop <kind>-<seq>-<slug> <absolute-path>
# Exit codes: 0 success/no-op; 2 not-in-repo; 64 bad args; 66 dirty worktree or path not registered; 1 git error
set -euo pipefail
export LC_ALL=C

KIND="${1:-}"
SEQ="${2:-}"
SLUG="${3:-}"

# --- Common pre-flight ---
[[ "$KIND" =~ ^(cr|epic|story)$ ]] || {
  echo "invalid kind" >&2
  exit 64
}
[[ "$SLUG" =~ ^[a-z0-9][a-z0-9-]*$ ]] || {
  echo "invalid slug" >&2
  exit 64
}
[[ ${#SLUG} -le 100 ]] || {
  echo "slug too long" >&2
  exit 64
}
[[ "$SEQ" =~ ^[0-9]+$ ]] || {
  echo "invalid seq" >&2
  exit 64
}

# --- Project root discovery ---
git rev-parse --show-toplevel >/dev/null 2>&1 || {
  echo "not in a git repo" >&2
  exit 2
}
PRIMARY="$(cd "$(git rev-parse --path-format=absolute --git-common-dir)/.." && pwd -P)"

# --- Resolve worktree_base from openbrain-client.local.yaml (nested YAML) ---
MARKER_PATH="$PRIMARY/openbrain-client.local.yaml"
WORKTREE_BASE_REL="worktrees/"
if [[ -f "$MARKER_PATH" ]]; then
  _val="$(awk '
    /^placeholders:[[:space:]]*$/ { in_ph = 1; next }
    /^[^[:space:]#]/              { in_ph = 0 }
    in_ph && /^[[:space:]]+worktree_base:[[:space:]]*/ {
      sub(/^[[:space:]]+worktree_base:[[:space:]]*/, "")
      sub(/[[:space:]]*#.*$/, "")
      gsub(/^["\x27]|["\x27]$/, "")
      print
      exit
    }' "$MARKER_PATH")"
  [[ -n "$_val" ]] && WORKTREE_BASE_REL="$_val"
fi
WORKTREE_BASE_ABS="$(cd "$PRIMARY" && cd "$WORKTREE_BASE_REL" && pwd -P)"

# --- Derive names ---
BRANCH="${KIND}-${SEQ}-${SLUG}"
WORKTREE_PATH="${WORKTREE_BASE_ABS}/${BRANCH}"

# --- Idempotency: already gone ---
if [[ ! -e "$WORKTREE_PATH" ]]; then
  echo "down-noop ${BRANCH} ${WORKTREE_PATH}"
  exit 0
fi

# --- Assert target is a git worktree ---
git -C "$WORKTREE_PATH" rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  echo "$WORKTREE_PATH is not a git worktree" >&2
  exit 66
}

# --- Assert target is a REGISTERED worktree ---
# Catches stale-metadata state: directory + .git file present but porcelain
# has lost the registration (e.g. operator deleted .git/worktrees/<name>).
if ! git -C "$PRIMARY" worktree list --porcelain | grep -Fxq -- "worktree $WORKTREE_PATH"; then
  echo "$WORKTREE_PATH is not a registered worktree; run 'git worktree prune' to clean up stale entries" >&2
  exit 66
fi

# --- Refuse dirty worktree ---
if [[ -n "$(git -C "$WORKTREE_PATH" status --porcelain 2>/dev/null)" ]]; then
  echo "worktree $WORKTREE_PATH has uncommitted changes; commit or stash before teardown" >&2
  exit 66
fi

# --- Remove ---
git -C "$PRIMARY" worktree remove "$WORKTREE_PATH"
echo "down ${BRANCH} ${WORKTREE_PATH}"
