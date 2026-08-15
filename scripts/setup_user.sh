#!/usr/bin/env bash
# setup_user.sh — user-scoped bootstrap for keel development.
# Installs and verifies user-owned CLI tools without requiring sudo.
#
# keel's toolchain (owner-confirmed 2026-07-07, aligned to keel/formal_review-1):
#   Go gate baseline — gopls, golangci-lint, govulncheck, gofumpt, shfmt, deadcode
#   Secret + spell    — gitleaks, cspell
# All Go tools land in $GOBIN (or $GOPATH/bin); cspell via npm install -g.
# Pinned versions — bumps are CR-sized decisions. Run scripts/setup_as_root.sh
# first so `go` is on PATH.
#
# WHERE THE GATE LOOKS (keel/ac-465, keel/issue-142): `keel-dev ci` does NOT run
# these $GOBIN binaries for its go-installed pins. It resolves each pinned tool
# from a version-keyed cache —
#   ${KEEL_DEV_TOOL_CACHE:-$HOME/.cache/keel-dev/tools}/<tool>/<version>/<tool>
# — installing it on demand from the pin declared in that branch's keel-dev.yaml,
# so two worktrees pinning different versions are both gateable on one host. The
# installs below stay for interactive use (editors, direct shell invocation) and
# for the pins keel cannot install itself (cspell, shellcheck), which the gate
# does still resolve from PATH. Bumping a pin therefore does NOT require
# re-running this script.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

TARGET_USER="${TARGET_USER:-homelab-devops}"
CURRENT_USER="$(whoami)"
HOME_DIR="${HOME:-/home/${CURRENT_USER}}"
PROFILE_FILE="${HOME_DIR}/.profile"
ZSHRC_FILE="${HOME_DIR}/.zshrc"

if [[ "$CURRENT_USER" != "$TARGET_USER" ]]; then
	echo "Run this script as ${TARGET_USER}. Current user: ${CURRENT_USER}"
	exit 1
fi

ensure_local_bin_path_block() {
	local file="$1"
	touch "$file"
	if grep -Fq '# local bin' "$file"; then
		return 0
	fi
	cat >>"$file" <<'EOF'

# local bin
case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) export PATH="$HOME/.local/bin:$PATH" ;;
esac
# local bin end
EOF
}

echo "Ensuring user-local bin directory is on PATH..."
ensure_local_bin_path_block "$PROFILE_FILE"
ensure_local_bin_path_block "$ZSHRC_FILE"
mkdir -p "${HOME_DIR}/.local/bin"
case ":$PATH:" in
*":${HOME_DIR}/.local/bin:"*) ;;
*) export PATH="${HOME_DIR}/.local/bin:$PATH" ;;
esac

ensure_versioned_go_tool() {
	local name="$1" version="$2" bin="$3" package="$4" want="$5"
	shift 5
	local version_args=("$@")
	local current=""
	if [[ -x "$bin" ]]; then
		current="$("$bin" "${version_args[@]}" 2>&1 || true)"
		if grep -qF "$want" <<<"$current"; then
			echo "${name} already installed: $(head -n1 <<<"$current") (${bin})"
			return 0
		fi
		echo "Updating ${name} to ${version}; current version output:"
		printf '%s\n' "$current"
	else
		echo "Installing ${name} ${version} into $(dirname "$bin")..."
	fi
	go install "${package}@${version}"
	current="$("$bin" "${version_args[@]}" 2>&1 || true)"
	if ! grep -qF "$want" <<<"$current"; then
		echo "ERROR: ${name} install did not report expected version substring ${want}." >&2
		printf '%s\n' "$current" >&2
		exit 1
	fi
	echo "${name} installed: $(head -n1 <<<"$current") (${bin})"
}

ensure_local_bin_link() {
	local name="$1" target="$2"
	local link="${HOME_DIR}/.local/bin/${name}"
	if [[ "$link" == "$target" ]]; then
		return 0
	fi
	if [[ -e "$link" || -L "$link" ]]; then
		local resolved
		resolved="$(readlink -f "$link" 2>/dev/null || true)"
		if [[ "$resolved" == "$target" ]]; then
			return 0
		fi
		echo "Linking ${link} -> ${target} so PATH does not resolve a stale ${name} first."
	else
		echo "Linking ${link} -> ${target}."
	fi
	ln -sfn "$target" "$link"
}

# ---------------------------------------------------------------------------
# Go tools — Go gate baseline (all via `go install`; land in $GOBIN).
# Pinned versions mirror the openbrain fleet pins where shared. See
# keel/formal_review-1 for the rationale.
# ---------------------------------------------------------------------------
if ! command -v go >/dev/null 2>&1; then
	echo "WARN: go not on PATH — skipping Go-tool installs. Run scripts/setup_as_root.sh first."
else
	GO_BIN_DIR="$(go env GOBIN)"
	[[ -z "$GO_BIN_DIR" ]] && GO_BIN_DIR="$(go env GOPATH)/bin"

	# --- gopls — Go LSP server (Go-aware refactors, e.g. `gopls rename`) ---
	# DHF-REQ: keel/requirement-12
	# deliberately floating at @latest: gopls is developer LSP tooling, not part of the gate,
	# so it cannot make gate verdicts differ across machines.
	GOPLS_BIN="${GO_BIN_DIR}/gopls"
	if [[ -x "$GOPLS_BIN" ]]; then
		echo "gopls already installed: $("$GOPLS_BIN" version | head -n1) (${GOPLS_BIN})"
	else
		echo "Installing gopls into ${GO_BIN_DIR}..."
		go install golang.org/x/tools/gopls@latest
	fi

	# --- golangci-lint (errcheck, govet, staticcheck, unused, ineffassign) ---
	# v2 line: the module path gained a /v2 suffix and gosimple folded into
	# staticcheck. Config is .golangci.yml v2 schema.
	GOLANGCI_LINT_VERSION="v2.12.2"
	GOLANGCI_LINT_BIN="${GO_BIN_DIR}/golangci-lint"
	ensure_versioned_go_tool "golangci-lint" "$GOLANGCI_LINT_VERSION" "$GOLANGCI_LINT_BIN" \
		"github.com/golangci/golangci-lint/v2/cmd/golangci-lint" "2.12.2" --version
	ensure_local_bin_link "golangci-lint" "$GOLANGCI_LINT_BIN"

	# --- govulncheck — stdlib/dependency vulnerability scan ---
	GOVULNCHECK_VERSION="v1.7.0"
	GOVULNCHECK_BIN="${GO_BIN_DIR}/govulncheck"
	ensure_versioned_go_tool "govulncheck" "$GOVULNCHECK_VERSION" "$GOVULNCHECK_BIN" \
		"golang.org/x/vuln/cmd/govulncheck" "$GOVULNCHECK_VERSION" --version
	ensure_local_bin_link "govulncheck" "$GOVULNCHECK_BIN"

	# --- gofumpt — stricter gofmt superset ---
	GOFUMPT_VERSION="v0.7.0"
	GOFUMPT_BIN="${GO_BIN_DIR}/gofumpt"
	ensure_versioned_go_tool "gofumpt" "$GOFUMPT_VERSION" "$GOFUMPT_BIN" \
		"mvdan.cc/gofumpt" "$GOFUMPT_VERSION" --version
	ensure_local_bin_link "gofumpt" "$GOFUMPT_BIN"

	# --- shfmt — shell formatter (lints/formats these bootstrap scripts) ---
	SHFMT_VERSION="v3.13.1"
	SHFMT_BIN="${GO_BIN_DIR}/shfmt"
	ensure_versioned_go_tool "shfmt" "$SHFMT_VERSION" "$SHFMT_BIN" \
		"mvdan.cc/sh/v3/cmd/shfmt" "$SHFMT_VERSION" --version
	ensure_local_bin_link "shfmt" "$SHFMT_BIN"

	# --- gitleaks — secret scanner (enforces keel/requirement-8: no secrets) ---
	GITLEAKS_VERSION="v8.30.1"
	GITLEAKS_BIN="${GO_BIN_DIR}/gitleaks"
	# Module path is github.com/zricethezav/gitleaks — the GitHub repo moved
	# to github.com/gitleaks but the Go module path never did. Go-installed
	# gitleaks does not expose a stable version probe, so reinstall the pinned
	# module every run and link it ahead of stale PATH shadows.
	echo "Installing gitleaks ${GITLEAKS_VERSION} into ${GO_BIN_DIR}..."
	go install "github.com/zricethezav/gitleaks/v8@${GITLEAKS_VERSION}"
	ensure_local_bin_link "gitleaks" "$GITLEAKS_BIN"

	# --- deadcode — advisory unreachable-function report (golang.org/x/tools) ---
	# DHF-REQ: keel/requirement-12
	DEADCODE_VERSION="v0.28.0"
	DEADCODE_BIN="${GO_BIN_DIR}/deadcode"
	# deadcode has no CLI version probe. Reinstall the pinned module every run
	# so an already-present off-pin binary still converges to the declared pin.
	echo "Installing deadcode ${DEADCODE_VERSION} into ${GO_BIN_DIR}..."
	go install "golang.org/x/tools/cmd/deadcode@${DEADCODE_VERSION}"
	ensure_local_bin_link "deadcode" "$DEADCODE_BIN"

	# PATH check — surface remediation if the bin dir is not on PATH.
	case ":${PATH}:" in
	*":${GO_BIN_DIR}:"*) ;;
	*)
		echo "WARN: ${GO_BIN_DIR} is NOT on PATH in this shell."
		echo "      Go tools are installed but won't be found by bare invocation."
		echo "      Fix: ensure your ~/.profile / ~/.zshrc adds ${GO_BIN_DIR} to PATH."
		;;
	esac
fi

# ---------------------------------------------------------------------------
# cspell — spell-check over markdown + Go sources (keel-dev ci runs it pinned).
# Not a Go tool; installed as an npm global. Needs Node — scripts/setup_as_root.sh
# installs nodejs + npm.
# ---------------------------------------------------------------------------
CSPELL_VERSION="10.0.1"
CSPELL_BIN="$(command -v cspell 2>/dev/null || true)"
if [[ -n "$CSPELL_BIN" ]] && cspell --version 2>/dev/null | grep -qF "$CSPELL_VERSION"; then
	echo "cspell already installed: $(cspell --version 2>&1 | head -n1) (${CSPELL_BIN})"
elif command -v pnpm >/dev/null 2>&1; then
	echo "Installing cspell ${CSPELL_VERSION} via pnpm add -g..."
	pnpm add -g "cspell@${CSPELL_VERSION}"
elif command -v npm >/dev/null 2>&1; then
	echo "Installing cspell ${CSPELL_VERSION} via npm install -g..."
	npm install -g "cspell@${CSPELL_VERSION}"
else
	echo "WARN: pnpm/npm not found — skipping cspell. Run scripts/setup_as_root.sh (installs nodejs+npm), then re-run." >&2
fi

# ---------------------------------------------------------------------------
# gh CLI check — `keel-dev release` uses `gh release create`, which needs the
# CLI installed and authenticated with `repo` scope. This is interactive
# (browser device-code flow), so just surface remediation; don't run it here.
# ---------------------------------------------------------------------------
if ! command -v gh >/dev/null 2>&1; then
	echo "WARN: gh CLI not found — keel-dev release needs it. Install gh, then 'gh auth login'." >&2
elif ! gh auth status >/dev/null 2>&1; then
	echo "WARN: gh CLI is not authenticated — keel-dev release will fail to create the GitHub release." >&2
	echo "      Fix: gh auth login   (needs 'repo' scope to create releases)." >&2
fi

# Report by absolute path so the final lines stay truthful even when the
# tool's install dir isn't on PATH yet.
report() {
	local name="$1" candidate="$2"
	if [[ -n "$candidate" && -x "$candidate" ]]; then
		echo "${name}: ${candidate}"
	else
		local resolved
		resolved="$(command -v "$name" 2>/dev/null || true)"
		echo "${name}: ${resolved:-(not installed)}"
	fi
}

echo ""
echo "User setup complete."
echo "keel-dev ci resolves go-installed gate tools from ${KEEL_DEV_TOOL_CACHE:-${HOME_DIR}/.cache/keel-dev/tools}/<tool>/<version>/,"
echo "installing them on demand from the branch's keel-dev.yaml pins; cspell and shellcheck stay on PATH."
report gopls "${GOPLS_BIN:-}"
report golangci-lint "${GOLANGCI_LINT_BIN:-}"
report govulncheck "${GOVULNCHECK_BIN:-}"
report gofumpt "${GOFUMPT_BIN:-}"
report shfmt "${SHFMT_BIN:-}"
report gitleaks "${GITLEAKS_BIN:-}"
report deadcode "${DEADCODE_BIN:-}"
report cspell "$(command -v cspell 2>/dev/null || true)"
