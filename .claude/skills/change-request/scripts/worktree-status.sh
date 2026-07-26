#!/usr/bin/env bash
# Print branch/worktree existence. Read-only. Used by /story dev to detect a live
# parent-epic worktree.
#
# Thin wrapper. The report belongs to `keel-dev worktree status`, which is
# backed by the keel/worktree package; this script only validates its arguments,
# composes the work-item name, and delegates. It runs no git lifecycle command
# of its own — locating the repository is the one read-only probe it keeps,
# because the not-in-repo exit status is part of its contract.
#
# Three-arg form:
#   Usage: worktree-status.sh <kind> <seq> <slug>
#   Output: status <kind>-<seq>-<slug> <absolute-path> branch=<true|false> worktree=<true|false>
#
# Glob form (mutually exclusive with three-arg form):
#   Usage: worktree-status.sh --glob <pattern>
#   Output: zero or more status lines for registered worktrees matching the pattern
#
# Exit codes: 0 always (informational); 2 not-in-repo; 64 bad args; 65 unusable worktree_base
#
# KEEL_DEV_BIN overrides the delegate command (default: go run ./cmd/keel-dev).
set -euo pipefail
export LC_ALL=C

# --- Repository discovery (read-only; needed for both forms) ---
TOPLEVEL="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "not in a git repo" >&2
  exit 2
}

if [[ -n "${KEEL_DEV_BIN:-}" ]]; then
  read -r -a KEEL_DEV <<<"$KEEL_DEV_BIN"
else
  KEEL_DEV=(go run ./cmd/keel-dev)
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
  [[ "$PATTERN" =~ ^[a-z][a-z0-9*_-]*$ ]] || {
    echo "invalid glob pattern" >&2
    exit 64
  }
  cd "$TOPLEVEL"
  exec "${KEEL_DEV[@]}" --no-header worktree status --glob "$PATTERN"
fi

# --- Three-arg form ---
KIND="${1:-}"
SEQ="${2:-}"
SLUG="${3:-}"

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

cd "$TOPLEVEL"
exec "${KEEL_DEV[@]}" --no-header worktree status "${KIND}-${SEQ}-${SLUG}"
