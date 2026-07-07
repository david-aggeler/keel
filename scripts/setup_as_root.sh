#!/usr/bin/env bash
# setup_as_root.sh — machine bootstrap for keel when already running as root.
# Installs Go, the `just` task runner, shellcheck, and shared PATH wiring.
#
# keel is a pure-Go, zero-dependency module with no Docker/DB stack, so this
# is deliberately lean: no Docker, no BuildKit GC, no container tooling. See
# scripts/setup_user.sh for the user-scoped Go lint/security tools and
# scripts/setup_repo.sh for the repo-level gate check.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

if [[ "${EUID}" -ne 0 ]]; then
	echo "Run this script as root."
	exit 1
fi

# Developer login that owns user-scoped state (~/.gopath toolchain cache, etc.).
# Exported so install_go.sh can scrub the right home directory without guessing.
export DEV_USER="${DEV_USER:-homelab-devops}"

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

echo "Installing just + shellcheck + Node toolchain via apt-get..."
# `just` runs keel's Justfile; `shellcheck` lints these bootstrap scripts;
# `nodejs`/`npm` provide the Node runtime that scripts/setup_user.sh needs to
# install cspell (the keel-dev ci spell-check tool).
# Pin to the distro package; the version assertion below guards drift.
EXPECTED_SHELLCHECK_VERSION="0.10.0"
apt-get update -qq
apt-get install -y just shellcheck nodejs npm

installed_sc_ver="$(shellcheck --version | awk '/^version:/{print $2}')"
if [[ "$installed_sc_ver" != "$EXPECTED_SHELLCHECK_VERSION" ]]; then
	echo "WARN: shellcheck version mismatch: installed=${installed_sc_ver} expected=${EXPECTED_SHELLCHECK_VERSION}" >&2
	echo "      Update EXPECTED_SHELLCHECK_VERSION in setup_as_root.sh if the new version is intentional." >&2
fi

echo ""
echo "Machine bootstrap complete. Next: run scripts/setup_user.sh as ${DEV_USER}."
