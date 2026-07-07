#!/bin/bash
# Install Go to /usr/local/go and configure PATH for the current user.
#
# Idempotent: if the installed version already matches $GO_VERSION the
# script exits early. If the installed version differs (older or newer),
# /usr/local/go is wiped and the pinned version is installed in its place
# — there is only ever one Go on the system at /usr/local/go.
#
# When upgrading, the auto-downloaded toolchains under
# ~/.gopath/pkg/mod/golang.org/toolchain@* are also pruned so stale
# minor versions don't accumulate. GOTOOLCHAIN=auto (the Go default) will
# repopulate the cache on demand if a future project's go.work pins a
# newer release than what's installed here — bump $GO_VERSION below to
# absorb such a project rather than letting the cache fill up.

set -euo pipefail

if [[ "${EUID}" -eq 0 ]]; then
	SUDO=""
else
	SUDO="sudo"
fi

# Pinned latest stable. Bump in lockstep with go.mod's `go <version>` line.
GO_VERSION="1.25.11"
ARCH="amd64"
TARBALL="go${GO_VERSION}.linux-${ARCH}.tar.gz"
DOWNLOAD_URL="https://dl.google.com/go/${TARBALL}"
INSTALL_DIR="/usr/local"
GO_BIN="${INSTALL_DIR}/go/bin/go"
GO_VERSION_FILE="${INSTALL_DIR}/go/VERSION"

# Detect the on-disk Go version. /usr/local/go/VERSION carries the literal
# "go1.X.Y" string written by the upstream tarball; cheaper and more
# reliable than parsing `go version` (which can be hijacked by
# GOTOOLCHAIN auto-download into reporting a downloaded toolchain).
installed_version() {
	if [[ -r "$GO_VERSION_FILE" ]]; then
		head -n1 "$GO_VERSION_FILE" | sed 's/^go//'
	else
		echo ""
	fi
}

CURRENT="$(installed_version)"
if [[ "$CURRENT" == "$GO_VERSION" ]]; then
	echo "Go ${GO_VERSION} already installed at ${INSTALL_DIR}/go — nothing to do."
	exit 0
fi

if [[ -n "$CURRENT" ]]; then
	echo "Replacing Go ${CURRENT} → ${GO_VERSION} at ${INSTALL_DIR}/go..."
else
	echo "Installing Go ${GO_VERSION} to ${INSTALL_DIR}/go..."
fi

echo "Downloading ${TARBALL}..."
curl -fsSL "${DOWNLOAD_URL}" -o "/tmp/${TARBALL}"

${SUDO} rm -rf "${INSTALL_DIR}/go"
${SUDO} tar -C "${INSTALL_DIR}" -xzf "/tmp/${TARBALL}"
rm "/tmp/${TARBALL}"

# Prune any auto-downloaded toolchains from previous installs. The Go
# toolchain mechanism caches each downloaded minor version under each
# user's GOPATH; wiping them forces GOTOOLCHAIN=auto to fall back to
# the newly installed binary first. New downloads only happen if a
# project actually demands a newer release.
#
# Per-user, not system-wide: GOPATH defaults to $HOME/go (or $HOME/.gopath
# on this host). When this script runs as root (because the invoking user
# lacks sudo), $HOME is /root and the real user's cache would be missed.
# Resolve the set of GOPATH roots to scrub by, in order:
#   1. $TARGET_GOPATH env var (explicit override; full path to GOPATH dir)
#   2. $SUDO_USER's home (set automatically when invoked via sudo)
#   3. $DEV_USER's home (the convention setup_as_root.sh uses; default
#      `homelab-devops`)
#   4. The current $HOME (works when running as normal user or as root
#      after `su - <user>`)
#   5. Every /home/*/.gopath and /home/*/go and /root/.gopath / /root/go
#      directory present on the box — broad sweep when no signal narrows
#      things down.
prune_toolchain_cache() {
	local gopath_dir="$1"
	local cache_glob="${gopath_dir}/pkg/mod/golang.org/toolchain@*"
	# shellcheck disable=SC2086
	if compgen -G "${cache_glob}" >/dev/null; then
		echo "  pruning ${gopath_dir}/pkg/mod/golang.org/toolchain@*"
		# toolchain cache dirs are mode 0555; chmod first so rm can proceed.
		chmod -R u+w ${cache_glob} 2>/dev/null || true
		rm -rf ${cache_glob}
	fi
}

declare -A SEEN_GOPATHS=()
add_gopath() {
	local g="$1"
	[[ -z "$g" || -n "${SEEN_GOPATHS[$g]:-}" ]] && return
	SEEN_GOPATHS[$g]=1
	[[ -d "$g" ]] && prune_toolchain_cache "$g"
}

echo "Scrubbing auto-downloaded Go toolchain caches..."
if [[ -n "${TARGET_GOPATH:-}" ]]; then
	add_gopath "${TARGET_GOPATH}"
elif [[ -n "${SUDO_USER:-}" ]]; then
	sudo_home="$(getent passwd "${SUDO_USER}" | cut -d: -f6)"
	add_gopath "${sudo_home}/.gopath"
	add_gopath "${sudo_home}/go"
elif [[ -n "${DEV_USER:-}" ]] && id "${DEV_USER}" >/dev/null 2>&1; then
	dev_home="$(getent passwd "${DEV_USER}" | cut -d: -f6)"
	add_gopath "${dev_home}/.gopath"
	add_gopath "${dev_home}/go"
else
	add_gopath "${GOPATH:-}"
	add_gopath "${HOME}/.gopath"
	add_gopath "${HOME}/go"
	# Broad sweep — pick up any other user homes on the box.
	for h in /home/* /root; do
		[[ -d "$h" ]] || continue
		add_gopath "${h}/.gopath"
		add_gopath "${h}/go"
	done
fi

# Add to system-wide PATH via /etc/profile.d so all users and non-interactive
# shells (e.g. Claude Code's Bash tool) pick it up without sourcing ~/.zshrc.
PROFILE_D="/etc/profile.d/go.sh"
if [[ ! -f "$PROFILE_D" ]]; then
	echo "Adding Go to PATH via ${PROFILE_D}..."
	${SUDO} tee "$PROFILE_D" >/dev/null <<'EOF'
export PATH=$PATH:/usr/local/go/bin
EOF
	${SUDO} chmod 644 "$PROFILE_D"
fi

export PATH=$PATH:/usr/local/go/bin

echo ""
echo "Go installed: $($GO_BIN version)"
echo ""
echo "Open a new shell (or run 'source /etc/profile.d/go.sh') to pick up PATH."
