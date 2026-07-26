#!/usr/bin/env bash
# Create a new git worktree on a fresh branch off the default branch.
#
# Thin wrapper. Every lifecycle decision belongs to `keel-dev worktree up`,
# which is backed by the keel/worktree package; this script only validates its
# arguments, composes the work-item name, and delegates. It runs no git
# lifecycle command of its own — locating the repository is the one read-only
# probe it keeps, because the not-in-repo exit status is part of its contract.
#
# Usage: worktree-up.sh <kind> <seq> <slug>
# Output (success): up <kind>-<seq>-<slug> <absolute-path>
# Output (no-op):   up-noop <kind>-<seq>-<slug> <absolute-path>
# Exit codes: 0 success/no-op; 2 not-in-repo; 64 bad args; 65 path or branch conflict; 1 git error
#
# KEEL_DEV_BIN overrides the delegate command (default: go run ./cmd/keel-dev).
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

# --- Delegate ---
if [[ -n "${KEEL_DEV_BIN:-}" ]]; then
  read -r -a KEEL_DEV <<<"$KEEL_DEV_BIN"
else
  KEEL_DEV=(go run ./cmd/keel-dev)
fi

cd "$TOPLEVEL"
exec "${KEEL_DEV[@]}" --no-header worktree up "${KIND}-${SEQ}-${SLUG}"
