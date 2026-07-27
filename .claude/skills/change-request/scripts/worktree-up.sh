#!/usr/bin/env bash
# Create a new git worktree on a fresh branch off the local default branch.
#
# Thin wrapper. Every lifecycle decision belongs to `keel-dev worktree up`,
# which is backed by the keel/worktree package; this script only composes the
# work-item name and delegates. What a well-formed <kind>-<seq>-<slug> is, is
# the delegate's judgement alone — a violation comes back from it as exit 64.
# The script runs no git lifecycle command of its own — locating the repository
# is the one read-only probe it keeps, because the not-in-repo exit status is
# part of its contract.
#
# Usage: worktree-up.sh <kind> <seq> <slug>
# Output (success): up <kind>-<seq>-<slug> <absolute-path>
# Output (no-op):   up-noop <kind>-<seq>-<slug> <absolute-path>
# Exit-code taxonomy: see `keel-dev help worktree`.
#
# KEEL_DEV_BIN overrides the delegate command (default: per-invocation ./cmd/keel-dev build).
set -euo pipefail
export LC_ALL=C

KIND="${1:-}"
SEQ="${2:-}"
SLUG="${3:-}"

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
	KEEL_DEV_TMP="$(mktemp "$CACHE_DIR/keel-dev.XXXXXX")"
	go build -o "$KEEL_DEV_TMP" ./cmd/keel-dev || {
		rm -f "$KEEL_DEV_TMP"
		exit 1
	}
	KEEL_DEV=("$KEEL_DEV_TMP")
fi

set +e
"${KEEL_DEV[@]}" --no-header worktree up "${KIND}-${SEQ}-${SLUG}"
STATUS=$?
set -e
if [[ -n "${KEEL_DEV_TMP:-}" ]]; then
	rm -f "$KEEL_DEV_TMP"
fi
exit "$STATUS"
