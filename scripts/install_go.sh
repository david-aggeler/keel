#!/bin/bash
# Install Go to $HOME/go (this host's GOROOT) and let ~/.profile own PATH.
#
# Idempotent: if $HOME/go already matches $GO_VERSION the script exits early.
# If the installed version differs (older or newer), $HOME/go is wiped and the
# pinned version is installed in its place — there is only ever one Go per user
# at $HOME/go.
#
# GOROOT is $HOME/go and GOPATH is separate at $HOME/.gopath (per the operator's
# ~/.claude/CLAUDE.md convention), so no sudo is needed: everything written here
# is under the user's own $HOME. PATH already includes $HOME/go/bin via
# ~/.profile; this script does not touch any system-wide profile.
#
# When upgrading, the auto-downloaded toolchains under
# $GOPATH/pkg/mod/golang.org/toolchain@* are also pruned so stale minor
# versions don't accumulate. GOTOOLCHAIN=auto (the Go default) will repopulate
# the cache on demand if a future project pins a newer release than what's
# installed here — bump $GO_VERSION below to absorb such a project rather than
# letting the cache fill up.

set -euo pipefail

# Pinned latest stable. Bump in lockstep with go.mod's `go <version>` line.
GO_VERSION="1.26.5"
ARCH="amd64"
TARBALL="go${GO_VERSION}.linux-${ARCH}.tar.gz"
DOWNLOAD_URL="https://dl.google.com/go/${TARBALL}"
INSTALL_DIR="${HOME}"
GOROOT_DIR="${INSTALL_DIR}/go"
GO_BIN="${GOROOT_DIR}/bin/go"
GO_VERSION_FILE="${GOROOT_DIR}/VERSION"

# Detect the on-disk Go version. $HOME/go/VERSION carries the literal "go1.X.Y"
# string written by the upstream tarball; cheaper and more reliable than parsing
# `go version` (which can be hijacked by GOTOOLCHAIN auto-download into reporting
# a downloaded toolchain).
installed_version() {
	if [[ -r "$GO_VERSION_FILE" ]]; then
		head -n1 "$GO_VERSION_FILE" | sed 's/^go//'
	else
		echo ""
	fi
}

CURRENT="$(installed_version)"
if [[ "$CURRENT" == "$GO_VERSION" ]]; then
	echo "Go ${GO_VERSION} already installed at ${GOROOT_DIR} — nothing to do."
	exit 0
fi

if [[ -n "$CURRENT" ]]; then
	echo "Replacing Go ${CURRENT} → ${GO_VERSION} at ${GOROOT_DIR}..."
else
	echo "Installing Go ${GO_VERSION} to ${GOROOT_DIR}..."
fi

echo "Downloading ${TARBALL}..."
curl -fsSL "${DOWNLOAD_URL}" -o "/tmp/${TARBALL}"

# Go's module cache writes files mode 0444 (read-only) by design. If a stray
# GOPATH-default cache ever landed under $HOME/go, those bits make a plain
# `rm -rf` fail — chmod the tree writable first so the replace is self-healing.
if [[ -d "${GOROOT_DIR}" ]]; then
	chmod -R u+w "${GOROOT_DIR}" 2>/dev/null || true
fi
rm -rf "${GOROOT_DIR}"
tar -C "${INSTALL_DIR}" -xzf "/tmp/${TARBALL}"
rm "/tmp/${TARBALL}"

# Prune any auto-downloaded toolchains from previous installs so GOTOOLCHAIN=auto
# falls back to the newly installed binary first. GOPATH here is $HOME/.gopath
# (NOT $HOME/go, which is the GOROOT replaced above).
GOPATH_DIR="${GOPATH:-${HOME}/.gopath}"
CACHE_GLOB="${GOPATH_DIR}/pkg/mod/golang.org/toolchain@*"
# shellcheck disable=SC2086
if compgen -G "${CACHE_GLOB}" >/dev/null; then
	echo "Pruning auto-downloaded Go toolchains under ${GOPATH_DIR}..."
	# toolchain cache dirs are mode 0555; chmod first so rm can proceed.
	chmod -R u+w ${CACHE_GLOB} 2>/dev/null || true
	rm -rf ${CACHE_GLOB}
fi

echo ""
echo "Go installed: $("${GO_BIN}" version)"
echo ""
echo "PATH: \$HOME/go/bin is added by ~/.profile on this host — open a new shell"
echo "if 'go' does not yet resolve to ${GO_BIN}."
