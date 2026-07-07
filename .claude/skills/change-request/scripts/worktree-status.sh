#!/usr/bin/env bash
# Print branch/worktree existence. Read-only. Used by /story dev to detect a live
# parent-epic worktree.
#
# Three-arg form:
#   Usage: worktree-status.sh <kind> <seq> <slug>
#   Output: status <kind>-<seq>-<slug> <absolute-path> branch=<true|false> worktree=<true|false>
#
# Glob form (mutually exclusive with three-arg form):
#   Usage: worktree-status.sh --glob <pattern>
#   Output: zero or more status lines for registered worktrees matching the pattern
#
# Exit codes: 0 always (informational); 2 not-in-repo; 64 bad args
set -euo pipefail
export LC_ALL=C

# --- Project root discovery (needed for both forms) ---
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
# Resolve base dir; if it does not exist yet we note it but do not fail here —
# the --glob form treats a missing base as zero matches (exit 0); the three-arg
# form's WORKTREE_PATH simply won't exist (branch=false, worktree=false).
if [[ -d "$PRIMARY/$WORKTREE_BASE_REL" ]]; then
  WORKTREE_BASE_ABS="$(cd "$PRIMARY" && cd "$WORKTREE_BASE_REL" && pwd -P)"
else
  WORKTREE_BASE_ABS=""
fi

# --- Dispatch on form ---
if [[ "${1:-}" == "--glob" ]]; then
  # Strict arg-count guard BEFORE charset regex — a missing or extra positional
  # gets a clearer 'usage:' diagnostic than the generic charset rejection.
  [[ $# -eq 2 ]] || {
    echo "usage: worktree-status.sh --glob <pattern>" >&2
    exit 64
  }
  PATTERN="$2"
  # Defense-in-depth: validate pattern charset (^[a-z][a-z0-9*_-]*$)
  [[ "$PATTERN" =~ ^[a-z][a-z0-9*_-]*$ ]] || {
    echo "invalid glob pattern" >&2
    exit 64
  }

  # Missing base dir → zero matches; not an error
  if [[ -z "$WORKTREE_BASE_ABS" || ! -d "$WORKTREE_BASE_ABS" ]]; then
    exit 0
  fi

  # Read all registered worktrees once
  WT_LIST="$(git -C "$PRIMARY" worktree list --porcelain)"

  # Capture find output; surface errors instead of silently treating them as empty
  FIND_ERR="$(mktemp)"
  mapfile -t CANDS < <(find "$WORKTREE_BASE_ABS" -maxdepth 1 -mindepth 1 -type d -name "$PATTERN" 2>"$FIND_ERR")
  if [[ -s "$FIND_ERR" ]]; then
    cat "$FIND_ERR" >&2
    rm -f "$FIND_ERR"
    exit 67
  fi
  rm -f "$FIND_ERR"

  for candidate in "${CANDS[@]+"${CANDS[@]}"}"; do
    dir_name="$(basename "$candidate")"
    BRANCH="$dir_name"
    BRANCH_EXISTS=false
    WT_EXISTS=false
    if git -C "$PRIMARY" show-ref --verify --quiet "refs/heads/$BRANCH" 2>/dev/null; then
      BRANCH_EXISTS=true
    fi
    if printf '%s\n' "$WT_LIST" | grep -Fxq -- "worktree $candidate"; then
      WT_EXISTS=true
    fi
    echo "status ${BRANCH} ${candidate} branch=${BRANCH_EXISTS} worktree=${WT_EXISTS}"
  done
  exit 0
fi

# --- Three-arg form ---
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

# --- Derive names ---
BRANCH="${KIND}-${SEQ}-${SLUG}"
# If base dir was missing, path cannot exist — report false for both flags.
# Both branches resolve to a physical path so the porcelain grep below compares
# against the same realpaths git records.
if [[ -n "$WORKTREE_BASE_ABS" ]]; then
  WORKTREE_PATH="${WORKTREE_BASE_ABS}/${BRANCH}"
else
  WORKTREE_PATH="$(cd "$PRIMARY" && cd .. && pwd -P)/${BRANCH}"
fi

# --- Check branch existence ---
BRANCH_EXISTS=false
if git -C "$PRIMARY" show-ref --verify --quiet "refs/heads/$BRANCH" 2>/dev/null; then
  BRANCH_EXISTS=true
fi

# --- Check worktree registration ---
WT_EXISTS=false
if git -C "$PRIMARY" worktree list --porcelain | grep -Fxq -- "worktree ${WORKTREE_PATH}"; then
  WT_EXISTS=true
fi

echo "status ${BRANCH} ${WORKTREE_PATH} branch=${BRANCH_EXISTS} worktree=${WT_EXISTS}"
