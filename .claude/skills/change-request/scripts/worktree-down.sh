#!/usr/bin/env bash
# Author-side pre-merge teardown. Refuses a checkout that still holds work.
# Does NOT delete the branch.
#
# Thin wrapper. Every lifecycle decision belongs to `keel-dev worktree down`,
# which is backed by the keel/worktree package; this script only validates its
# arguments, composes the work-item name, and delegates. It runs no git
# lifecycle command of its own — locating the repository is the one read-only
# probe it keeps, because the not-in-repo exit status is part of its contract.
#
# Usage: worktree-down.sh <kind> <seq> <slug>
# Output (success): down <kind>-<seq>-<slug> <absolute-path>
# Output (no-op):   down-noop <kind>-<seq>-<slug> <absolute-path>
# Exit codes: 0 success/no-op; 2 not-in-repo; 64 bad args; 66 dirty worktree or path not registered; 1 git error
#
# KEEL_DEV_BIN overrides the delegate command (default: cached ./cmd/keel-dev build).
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

# --- Repository discovery (read-only) ---
TOPLEVEL="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "not in a git repo" >&2
  exit 2
}

cd "$TOPLEVEL"

# --- Delegate ---
if [[ -n "${KEEL_DEV_BIN:-}" ]]; then
  read -r -a KEEL_DEV <<<"$KEEL_DEV_BIN"
else
  # DHF-REQ: keel/requirement-114 (keel/ac-409, keel/ac-410)
  CACHE_ROOT="${XDG_CACHE_HOME:-${HOME:-${TMPDIR:-/tmp}}/.cache}"
  CACHE_DIR="$CACHE_ROOT/keel/change-request"
  mkdir -p "$CACHE_DIR"
  KEEL_DEV_PATH="$CACHE_DIR/keel-dev"
  KEEL_DEV_TMP="$(mktemp "$CACHE_DIR/keel-dev.XXXXXX")"
  go build -o "$KEEL_DEV_TMP" ./cmd/keel-dev || {
    rm -f "$KEEL_DEV_TMP"
    exit 1
  }
  mv "$KEEL_DEV_TMP" "$KEEL_DEV_PATH"
  KEEL_DEV=("$KEEL_DEV_PATH")
fi

exec "${KEEL_DEV[@]}" --no-header worktree down "${KIND}-${SEQ}-${SLUG}"
