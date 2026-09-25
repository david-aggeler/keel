#!/usr/bin/env bash
# setup_as_root.sh — machine bootstrap for keel when already running as root.
# Installs Go, the host tools and shared libraries required by both gates, and
# shared PATH wiring. Version values come from setup_user.sh's pin block.
#
# keel is a pure-Go, zero-dependency module with no Docker/DB stack, so this
# is deliberately lean: no Docker, no BuildKit GC, no container tooling. See
# scripts/setup_user.sh for the user-scoped Go lint/security tools and
# scripts/setup_repo.sh for the repo-level gate check.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."
# shellcheck source=scripts/bootstrap_versions.sh
source "$SCRIPT_DIR/bootstrap_versions.sh"

if [[ "${EUID}" -ne 0 ]]; then
	echo "Run this script as root."
	exit 1
fi

# Developer login that owns user-scoped state (~/.gopath toolchain cache, etc.).
# Exported so install_go.sh can scrub the right home directory without guessing.
export DEV_USER="${DEV_USER:-homelab-devops}"

pin_value() {
	local variable="$1"
	awk -F= -v variable="$variable" '$1 == variable { value=$2; sub(/[[:space:]]+#.*/, "", value); gsub(/^[[:space:]\"]+|[[:space:]\"]+$/, "", value); print value; exit }' ./scripts/setup_user.sh
}

NODE_MAJOR="$(pin_value NODE_MAJOR)"
EXPECTED_SHELLCHECK_VERSION="$(pin_value SHELLCHECK_VERSION)"
if [[ -z "$NODE_MAJOR" || -z "$EXPECTED_SHELLCHECK_VERSION" ]]; then
	echo "ERROR: node or shellcheck pin missing from scripts/setup_user.sh" >&2
	exit 1
fi

echo "Installing Go via scripts/install_go.sh..."
bash ./scripts/install_go.sh

echo "Ensuring Go is on the zsh system PATH..."
# shellcheck disable=SC2043 # single path intentional; loop kept for future expansion
for SYSFILE in /etc/zsh/zshenv; do
	touch "$SYSFILE"
	if ! grep -q 'usr/local/go/bin' "$SYSFILE" 2>/dev/null; then
		# shellcheck disable=SC2016 # intentional: write the literal $PATH into the rc, do not expand it now
		echo 'export PATH=$PATH:/usr/local/go/bin' >>"$SYSFILE"
	fi
done

echo "Installing base host packages via apt-get..."
# `just` runs keel's Justfile; `shellcheck` lints these bootstrap scripts;
# `nodejs`/`npm` provide the Node runtime that scripts/setup_user.sh needs to
# install cspell (the keel-dev ci spell-check tool).
apt-get update -qq
apt-get install -y ca-certificates curl just xz-utils \
	xvfb \
	libasound2t64 libatk1.0-0t64 libatk-bridge2.0-0t64 libatspi2.0-0t64 \
	libc6 libcairo2 libcups2t64 libdbus-1-3 libexpat1 libgbm1 libgcc-s1 \
	libglib2.0-0t64 libgtk-3-0t64 libnspr4 libnss3 libpango-1.0-0 \
	libudev1 libx11-6 libxcb1 libxcomposite1 libxdamage1 libxext6 \
	libxfixes3 libxkbcommon0 libxrandr2

shellcheck_archive="/tmp/shellcheck-v${EXPECTED_SHELLCHECK_VERSION}.linux.x86_64.tar.xz"
shellcheck_dir="/tmp/shellcheck-v${EXPECTED_SHELLCHECK_VERSION}"
curl -fsSL "https://github.com/koalaman/shellcheck/releases/download/v${EXPECTED_SHELLCHECK_VERSION}/shellcheck-v${EXPECTED_SHELLCHECK_VERSION}.linux.x86_64.tar.xz" -o "$shellcheck_archive"
tar -xJf "$shellcheck_archive" -C /tmp
install -m 0755 "${shellcheck_dir}/shellcheck" /usr/local/bin/shellcheck
rm -f "$shellcheck_archive"

echo "Installing Node.js major ${NODE_MAJOR} from NodeSource..."
curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash -
apt-get install -y nodejs

installed_sc_ver="$(shellcheck --version | awk '/^version:/{print $2}')"
if [[ "$installed_sc_ver" != "$EXPECTED_SHELLCHECK_VERSION" ]]; then
	echo "ERROR: shellcheck version mismatch: installed=${installed_sc_ver} expected=${EXPECTED_SHELLCHECK_VERSION}" >&2
	exit 1
fi

require_node_major "$NODE_MAJOR"

echo ""
echo "Machine bootstrap complete. Next: run scripts/setup_user.sh as ${DEV_USER}."
