#!/usr/bin/env bash
# Re-attach a worktree to an existing branch when the directory was removed.
# Usage: worktree-resume.sh <kind> <seq> <slug>
# Output (success): resume <kind>-<seq>-<slug> <absolute-path>
# Output (no-op):   resume-noop <kind>-<seq>-<slug> <absolute-path>
# Exit codes: 0 success/no-op; 2 not-in-repo; 64 bad args; 67 branch missing; 1 git error
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

# --- Already present ---
if git -C "$PRIMARY" worktree list --porcelain | grep -Fxq -- "worktree $WORKTREE_PATH"; then
  echo "resume-noop ${BRANCH} ${WORKTREE_PATH}"
  exit 0
fi

# --- Branch must exist ---
if ! git -C "$PRIMARY" show-ref --verify --quiet "refs/heads/$BRANCH"; then
  echo "branch $BRANCH does not exist; use worktree-up.sh to create" >&2
  exit 67
fi

# --- Ensure base dir exists before attach ---
mkdir -p "$WORKTREE_BASE_ABS"

# --- Attach ---
git -C "$PRIMARY" worktree add "$WORKTREE_PATH" "$BRANCH"
echo "resume ${BRANCH} ${WORKTREE_PATH}"
