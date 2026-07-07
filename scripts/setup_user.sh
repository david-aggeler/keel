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
	GOPLS_BIN="${GO_BIN_DIR}/gopls"
	if [[ -x "$GOPLS_BIN" ]]; then
		echo "gopls already installed: $("$GOPLS_BIN" version | head -n1) (${GOPLS_BIN})"
	else
		echo "Installing gopls into ${GO_BIN_DIR}..."
		go install golang.org/x/tools/gopls@latest
	fi

	# --- golangci-lint (errcheck, staticcheck, unused, ineffassign, gosimple) ---
	GOLANGCI_LINT_VERSION="v1.64.8"
	GOLANGCI_LINT_BIN="${GO_BIN_DIR}/golangci-lint"
	if [[ -x "$GOLANGCI_LINT_BIN" ]]; then
		echo "golangci-lint already installed: $("$GOLANGCI_LINT_BIN" --version | head -n1) (${GOLANGCI_LINT_BIN})"
	else
		echo "Installing golangci-lint ${GOLANGCI_LINT_VERSION} into ${GO_BIN_DIR}..."
		go install "github.com/golangci/golangci-lint/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
	fi

	# --- govulncheck — stdlib/dependency vulnerability scan ---
	GOVULNCHECK_VERSION="v1.1.4"
	GOVULNCHECK_BIN="${GO_BIN_DIR}/govulncheck"
	if [[ -x "$GOVULNCHECK_BIN" ]]; then
		echo "govulncheck already installed: $("$GOVULNCHECK_BIN" --version | head -n1) (${GOVULNCHECK_BIN})"
	else
		echo "Installing govulncheck ${GOVULNCHECK_VERSION} into ${GO_BIN_DIR}..."
		go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
	fi

	# --- gofumpt — stricter gofmt superset ---
	GOFUMPT_VERSION="v0.7.0"
	GOFUMPT_BIN="${GO_BIN_DIR}/gofumpt"
	if [[ -x "$GOFUMPT_BIN" ]]; then
		echo "gofumpt already installed: $("$GOFUMPT_BIN" --version) (${GOFUMPT_BIN})"
	else
		echo "Installing gofumpt ${GOFUMPT_VERSION} into ${GO_BIN_DIR}..."
		go install "mvdan.cc/gofumpt@${GOFUMPT_VERSION}"
	fi

	# --- shfmt — shell formatter (lints/formats these bootstrap scripts) ---
	SHFMT_VERSION="v3.10.0"
	SHFMT_BIN="${GO_BIN_DIR}/shfmt"
	if [[ -x "$SHFMT_BIN" ]]; then
		echo "shfmt already installed: $("$SHFMT_BIN" --version) (${SHFMT_BIN})"
	else
		echo "Installing shfmt ${SHFMT_VERSION} into ${GO_BIN_DIR}..."
		go install "mvdan.cc/sh/v3/cmd/shfmt@${SHFMT_VERSION}"
	fi

	# --- gitleaks — secret scanner (enforces keel/requirement-8: no secrets) ---
	GITLEAKS_VERSION="v8.18.4"
	GITLEAKS_BIN="${GO_BIN_DIR}/gitleaks"
	if [[ -x "$GITLEAKS_BIN" ]]; then
		echo "gitleaks already installed: $("$GITLEAKS_BIN" version) (${GITLEAKS_BIN})"
	else
		echo "Installing gitleaks ${GITLEAKS_VERSION} into ${GO_BIN_DIR}..."
		go install "github.com/gitleaks/gitleaks/v8@${GITLEAKS_VERSION}"
	fi

	# --- deadcode — advisory unreachable-function report (golang.org/x/tools) ---
	DEADCODE_VERSION="v0.28.0"
	DEADCODE_BIN="${GO_BIN_DIR}/deadcode"
	if [[ -x "$DEADCODE_BIN" ]]; then
		echo "deadcode already installed (${DEADCODE_BIN})"
	else
		echo "Installing deadcode ${DEADCODE_VERSION} into ${GO_BIN_DIR}..."
		go install "golang.org/x/tools/cmd/deadcode@${DEADCODE_VERSION}"
	fi

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
CSPELL_VERSION="10.0.0"
CSPELL_BIN="$(command -v cspell 2>/dev/null || true)"
if [[ -n "$CSPELL_BIN" ]]; then
	echo "cspell already installed: $(cspell --version 2>&1 | head -n1) (${CSPELL_BIN})"
elif command -v npm >/dev/null 2>&1; then
	echo "Installing cspell ${CSPELL_VERSION} via npm install -g..."
	npm install -g "cspell@${CSPELL_VERSION}"
else
	echo "WARN: npm not found — skipping cspell. Run scripts/setup_as_root.sh (installs nodejs+npm), then re-run." >&2
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
report gopls "${GOPLS_BIN:-}"
report golangci-lint "${GOLANGCI_LINT_BIN:-}"
report govulncheck "${GOVULNCHECK_BIN:-}"
report gofumpt "${GOFUMPT_BIN:-}"
report shfmt "${SHFMT_BIN:-}"
report gitleaks "${GITLEAKS_BIN:-}"
report deadcode "${DEADCODE_BIN:-}"
report cspell "$(command -v cspell 2>/dev/null || true)"
