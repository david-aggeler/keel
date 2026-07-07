#!/usr/bin/env bash
# Create a new git worktree on a fresh branch off main.
# Usage: worktree-up.sh <kind> <seq> <slug>
# Output (success): up <kind>-<seq>-<slug> <absolute-path>
# Output (no-op):   up-noop <kind>-<seq>-<slug> <absolute-path>
# Exit codes: 0 success/no-op; 2 not-in-repo; 64 bad args; 65 path or branch conflict; 1 git error
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
[[ -n "$PRIMARY" && -d "$PRIMARY" ]] || {
  echo "could not locate primary worktree" >&2
  exit 2
}

# --- Resolve worktree_base from openbrain-client.local.yaml (nested YAML) ---
MARKER_PATH="$PRIMARY/openbrain-client.local.yaml"
WORKTREE_BASE_REL="worktrees/"
if [[ -f "$MARKER_PATH" ]]; then
  _val="$(awk '
    /^placeholders:[[:space:]]*$/ { in_ph = 1; next }
    /^[^[:space:]#]/              { in_ph = 0 }
    in_ph && /^[[:space:]]+worktree_base:[[:space:]]*/ {
      sub(/^[[:space:]]+worktree_base:[[:space:]]*/, "")
      sub(/\r$/, "")
      sub(/[[:space:]]*#.*$/, "")
      gsub(/^["\x27]|["\x27]$/, "")
      print
      exit
    }' "$MARKER_PATH")"
  [[ -n "$_val" ]] && WORKTREE_BASE_REL="$_val"
fi
[[ "$WORKTREE_BASE_REL" == /* ]] && {
  echo "worktree_base must be a relative path, not absolute: $WORKTREE_BASE_REL" >&2
  exit 65
}
mkdir -p "$PRIMARY/$WORKTREE_BASE_REL"
WORKTREE_BASE_ABS="$(cd "$PRIMARY" && cd "$WORKTREE_BASE_REL" && pwd -P)"

# --- Derive names ---
BRANCH="${KIND}-${SEQ}-${SLUG}"
WORKTREE_PATH="${WORKTREE_BASE_ABS}/${BRANCH}"

# --- Idempotency check ---
if [[ -e "$WORKTREE_PATH" ]]; then
  if git -C "$PRIMARY" worktree list --porcelain | grep -Fxq -- "worktree $WORKTREE_PATH"; then
    echo "worktree already exists; no-op" >&2
    echo "up-noop ${BRANCH} ${WORKTREE_PATH}"
    exit 0
  else
    echo "path $WORKTREE_PATH exists but is not a registered worktree; remove it or run worktree-resume.sh" >&2
    exit 65
  fi
fi

# --- Detect default branch ---
DEFAULT_BRANCH=$(git -C "$PRIMARY" symbolic-ref --short -q refs/remotes/origin/HEAD 2>/dev/null | sed 's|^origin/||' || true)
if [[ -z "$DEFAULT_BRANCH" ]]; then
  for cand in main master trunk; do
    if git -C "$PRIMARY" show-ref --verify --quiet "refs/heads/$cand"; then
      DEFAULT_BRANCH="$cand"
      break
    fi
  done
fi
[[ -n "$DEFAULT_BRANCH" ]] || {
  echo "no default branch found (tried origin/HEAD, main, master, trunk)" >&2
  exit 65
}

# --- Branch-exists guard ---
# If we reach here, no path conflict. If the branch already exists, the
# operator likely deleted the worktree directory without 'git worktree
# remove' — direct them at worktree-resume.sh instead of letting git emit
# an opaque "branch already exists" error.
#
# Skip the guard when $BRANCH happens to equal $DEFAULT_BRANCH (an operator
# whose CR slug collides with main/master would hit a misleading "use
# worktree-resume.sh" message that wouldn't help). Let git handle that case
# with its native error — collision with the default branch is its own kind
# of operator mistake and not the one this guard is for.
if [[ "$BRANCH" != "$DEFAULT_BRANCH" ]] &&
  git -C "$PRIMARY" show-ref --verify --quiet "refs/heads/$BRANCH"; then
  echo "branch $BRANCH already exists but no worktree is registered at $WORKTREE_PATH;" >&2
  echo "  run worktree-resume.sh $KIND $SEQ $SLUG to recreate the worktree," >&2
  echo "  or 'git branch -D $BRANCH' to start over" >&2
  exit 65
fi

# --- Create ---
git -C "$PRIMARY" worktree add -b "$BRANCH" "$WORKTREE_PATH" "$DEFAULT_BRANCH"
echo "up ${BRANCH} ${WORKTREE_PATH}"
