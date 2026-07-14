#!/usr/bin/env bash
# path-diagnostic.sh — portable PATH / node / codex resolver diagnostic.
#
# Run it in every context you care about and COMPARE the VERDICT line:
#   zsh  scripts/path-diagnostic.sh zsh
#   bash scripts/path-diagnostic.sh bashinzsh
#   !bash scripts/path-diagnostic.sh claude     # via Claude's `!` prefix = Claude's Bash-tool shell
#
# Works under bash and zsh (POSIX-ish; no arrays, no [[ ]], no process substitution).
# The important comparison is INHERITED (what the caller handed us) vs FRESH
# (env -i reference shell = what a *restarted* session would capture).
#
# Every run writes a timestamped file to /tmp/path-diag/ (override: PATHDIAG_OUT=<dir>).

# ---- which interpreter is actually running this file -------------------------
if [ -n "${ZSH_VERSION:-}" ]; then
	THIS="zsh ${ZSH_VERSION}"
elif [ -n "${BASH_VERSION:-}" ]; then
	THIS="bash ${BASH_VERSION}"
else
	THIS="sh (unknown)"
fi
case "$-" in *i*) INTER=interactive ;; *) INTER=non-interactive ;; esac

LABEL="${1:-${THIS%% *}-$INTER}"
LABEL=$(printf '%s' "$LABEL" | tr ' /' '__')
STAMP=$(date +%Y%m%d-%H%M%S 2>/dev/null)
HOST=$(hostname -s 2>/dev/null || hostname 2>/dev/null)
OUTDIR="${PATHDIAG_OUT:-/tmp/path-diag}"
mkdir -p "$OUTDIR" 2>/dev/null
OUTFILE="$OUTDIR/pathdiag_${HOST}_${LABEL}_${STAMP}.txt"

# Run `codex --version` with a timeout when available. A helper (not a $VAR of
# "timeout 10") because zsh does NOT word-split unquoted parameters like bash does,
# so `$TO codex` would look for a command literally named "timeout 10" and 127.
codex_ver() {
	if command -v timeout >/dev/null 2>&1; then
		timeout 10 codex --version 2>&1
	else codex --version 2>&1; fi
}

on_path() { case ":$PATH:" in *":$1:"*) echo yes ;; *) echo no ;; esac }
hr() { printf '%s\n' "----------------------------------------------------------------"; }

# ---- the whole report is a function so we can `| tee` it robustly ------------
main() {
	echo "================ path-diagnostic.sh ================"
	printf 'host        : %s\n' "$HOST"
	printf 'when        : %s\n' "$(date 2>/dev/null)"
	printf 'user        : %s\n' "$(id -un 2>/dev/null)"
	printf 'interpreter : %s  (%s, flags=%s)\n' "$THIS" "$INTER" "$-"
	# shellcheck disable=SC2016 # $0 is intentionally literal label text.
	printf 'argv0 ($0)  : %s\n' "$0"

	hr
	echo "-- parent process chain (who launched this shell) --"
	ppid=$PPID
	i=0
	while [ "$i" -lt 6 ] && [ -n "$ppid" ] && [ "$ppid" != 0 ] && [ -r "/proc/$ppid/comm" ]; do
		printf '  pid=%-8s %s\n' "$ppid" "$(cat "/proc/$ppid/comm" 2>/dev/null)"
		ppid=$(awk '{print $4}' "/proc/$ppid/stat" 2>/dev/null)
		i=$((i + 1))
	done

	hr
	echo "-- key environment (as INHERITED from the caller) --"
	printf '  SHELL     = %s\n' "${SHELL:-<unset>}"
	printf '  BASH_ENV  = %s\n' "${BASH_ENV:-<unset>}"
	printf '  ZDOTDIR   = %s\n' "${ZDOTDIR:-<unset>}"
	printf '  PNPM_HOME = %s\n' "${PNPM_HOME:-<unset>}"

	hr
	echo "-- INHERITED PATH (numbered) --"
	printf '%s\n' "$PATH" | tr ':' '\n' | nl -ba

	hr
	echo "-- critical dirs: on inherited PATH? / exist on disk? --"
	for d in "$HOME/.local/share/pnpm" "$HOME/.local/share/pnpm/bin" "$HOME/.local/bin"; do
		if [ -d "$d" ]; then exists=EXISTS; else exists="MISSING-on-disk"; fi
		printf '  %-45s on-PATH=%-3s  %s\n' "$d" "$(on_path "$d")" "$exists"
	done

	hr
	echo "-- tool resolution IN THIS (inherited) SHELL --"
	if command -v node >/dev/null 2>&1; then
		printf '  node : %s  (%s)\n' "$(command -v node)" "$(node --version 2>&1 | head -1)"
	else
		printf '  node : NOT ON PATH\n'
	fi
	CUR_OK=no
	if command -v codex >/dev/null 2>&1; then
		cver=$(codex_ver)
		crc=$?
		printf '  codex: RESOLVES at %s\n' "$(command -v codex)"
		printf '         codex --version -> rc=%s : %s\n' "$crc" "$(printf '%s' "$cver" | head -1)"
		[ "$crc" = 0 ] && CUR_OK=yes
	else
		printf '  codex: NOT ON PATH (command -v failed) -> shim dir not on PATH\n'
	fi

	hr
	echo "-- FRESH reference shell (env -i zsh -c): what a RESTARTED session sees --"
	FRESH_OK=na
	if command -v zsh >/dev/null 2>&1; then
		# shellcheck disable=SC2016 # Inner script expands inside the fresh zsh.
		env -i HOME="$HOME" zsh -c '
      printf "  PATH: %s\n" "$PATH"
      if command -v codex >/dev/null 2>&1; then
        v=$(codex --version 2>&1); rc=$?
        printf "  codex RESOLVES at %s ; --version rc=%s : %s\n" "$(command -v codex)" "$rc" "$(printf "%s" "$v" | head -1)"
      else
        printf "  codex NOT ON PATH in fresh shell\n"
      fi'
		FRESH_OK=$(env -i HOME="$HOME" zsh -c 'command -v codex >/dev/null 2>&1 && codex --version >/dev/null 2>&1 && echo yes || echo no' 2>/dev/null)
	else
		echo "  (zsh not installed — skipping fresh-zsh reference)"
	fi

	hr
	echo "-- startup-file wiring (static file inspection) --"
	if [ -f "$HOME/.zshenv" ]; then
		if grep -q 'profile' "$HOME/.zshenv"; then
			echo "  ~/.zshenv       : EXISTS and references ~/.profile  (bridge present)"
		else
			echo "  ~/.zshenv       : EXISTS but does NOT source ~/.profile  (bridge MISSING)"
		fi
	else
		echo "  ~/.zshenv       : MISSING (non-interactive zsh has no bridge to ~/.profile)"
	fi
	if [ -f "$HOME/.profile" ]; then
		grep -q 'pnpm' "$HOME/.profile" && echo "  ~/.profile      : has a pnpm PATH block" || echo "  ~/.profile      : NO pnpm block"
		grep -q 'BASH_ENV' "$HOME/.profile" && echo "  ~/.profile      : exports BASH_ENV (bash bridge)" || echo "  ~/.profile      : does NOT export BASH_ENV"
	else
		echo "  ~/.profile      : MISSING"
	fi

	hr
	echo "================ VERDICT ================"
	printf 'current-shell codex works : %s\n' "$CUR_OK"
	printf 'fresh-shell   codex works : %s\n' "$FRESH_OK"
	if [ "$CUR_OK" = yes ]; then
		echo ">> HEALTHY in this context — codex resolves and runs here."
	elif [ "$CUR_OK" = no ] && [ "$FRESH_OK" = yes ]; then
		echo ">> STALE CAPTURED ENV. Wiring is correct (fresh shell works) but THIS context"
		echo "   captured its PATH before codex/node were reachable."
		echo "   FIX: restart this session / relaunch so it re-captures PATH. If it's Claude"
		echo "   launched by zx/zellij/systemd, the LAUNCHER's env is stripped -> fix the launcher"
		echo "   (start from a login shell, or set CLAUDE_ENV_FILE with the pnpm dirs)."
	elif [ "$CUR_OK" = no ] && [ "$FRESH_OK" = no ]; then
		echo ">> BROKEN WIRING. Even a fresh shell can't resolve/run codex. Restart won't help."
		echo "   Check ~/.zshenv -> ~/.profile bridge, the pnpm PATH block, and that node +"
		echo "   the codex shim actually exist on disk."
	else
		echo ">> INCONCLUSIVE (fresh-shell check unavailable). Compare sections above by hand."
	fi
	echo "========================================"
}

main 2>&1 | tee "$OUTFILE"
echo ""
echo "saved to: $OUTFILE"
