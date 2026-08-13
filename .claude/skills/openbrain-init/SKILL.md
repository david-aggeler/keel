---
name: openbrain-init
description: "Bootstrap project-level HELIX01 skills via the openbrain-client binary. Fetches the skill catalog over HTTP, moves aside any existing skills into a legacy/ dir, materialises fresh content with placeholder substitution and content-hash stamping, restores local-only skills, and places agent files into .claude/agents/. Use when the user says: '/openbrain-init', 'bootstrap skills', 'fetch skills from openbrain', 'init openbrain skills', 'sync skills'"
allowed-tools: Bash, Read, Write
x-openbrain-source: openbrain-init/v7
x-openbrain-content-source-hash: sha256:1be2f679b99610d12fd6231e9fefa2196a738aaf64eb60b971cbf94f154dae27
x-openbrain-content-hash: sha256:c133ab53237d514caff7dc52d1cb21f6305fe7fb0d2e6bc8b28118ddf6934b57
---

# /openbrain-init — Bootstrap HELIX01 Skills

Drives the `openbrain-client init-skills` subcommand to fetch the HELIX01 skill catalog
from an OpenBrain MCP instance and reconcile it into the consuming project's
`.claude/skills/` and `.claude/agents/` trees. OpenBrain content wins over stale
local copies of managed skills (precedence inversion from v1). Local-only skills
and agents are preserved.

## Prerequisites

The following three steps install the `openbrain-client` binary from GitHub
Releases and verify the download is uncorrupted before executing it. They are
the **first-acquisition** path only: once a client is installed, every
subsequent upgrade goes through `openbrain-client self-update` (see
*Upgrading an installed client* below). Note that
sha256 verifies download integrity — it does not prove the release was produced
by a trusted party, since both the binary and the checksums file are fetched from
the same Release.

**Step 1: Download the binary**

Find the latest release version at `https://github.com/david-aggeler/openbrain/releases`
and set `VERSION` to the release tag (e.g. `v1.3.0`). Detect your platform:

```bash
VERSION=v1.3.0    # replace with the actual release tag
OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # linux or darwin
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
ASSET="openbrain-client_${VERSION}_${OS}_${ARCH}"

curl -fsSL \
  "https://github.com/david-aggeler/openbrain/releases/download/${VERSION}/${ASSET}" \
  -o "/tmp/${ASSET}"
```

**Step 2: Download sha256sums.txt and verify the download is uncorrupted**

```bash
curl -fsSL \
  "https://github.com/david-aggeler/openbrain/releases/download/${VERSION}/sha256sums.txt" \
  -o "/tmp/sha256sums.txt"

# Assert the asset's line is present in the sums file before checking.
# Without this, sha256sum --ignore-missing exits 0 vacuously when the asset
# is not listed at all, passing an unverified binary through.
grep -F " ${ASSET}" /tmp/sha256sums.txt > /tmp/sha256check.txt

# Verify integrity against the asset's exact line (no --ignore-missing):
(cd /tmp && sha256sum --check sha256check.txt)
```

The check must print `OK` for the asset. If it fails, do not proceed — re-download
and re-verify. Only continue to Step 3 after a passing check.

**Step 3: Install to PATH (only after Step 2 passes)**

```bash
INSTALL_DIR="$HOME/.local/bin"
mkdir -p "$INSTALL_DIR"
cp "/tmp/${ASSET}" "${INSTALL_DIR}/openbrain-client"
chmod +x "${INSTALL_DIR}/openbrain-client"
# Verify:
openbrain-client --version
```

Ensure `$INSTALL_DIR` is in your `PATH`. If not, add it or use the full path.

**Upgrading an installed client**

The shell installer above owns first acquisition; the client owns every
subsequent upgrade. An installed client upgrades itself from its configured
instance's client-distribution route:

```bash
openbrain-client self-update --check   # report-only: current vs available, no change
openbrain-client self-update           # digest-verified, atomic in-place upgrade
```

Notes on the contract:

- The published digest is the sole update authority — a payload that does not
  match it is never installed.
- The replaced binary is retained beside the new one as `<exe>.prev`; restore it
  by copying it back over the executable to roll back.
- Guards are fail-closed: a downgrade, a platform mismatch, or an unwritable
  install location aborts the update with the binary untouched.
- There is no ambient or scheduled checking — `self-update` runs only when
  invoked explicitly.

You also need `openbrain-client.local.yaml` in the project root and at least one
skill pinned and released on the target HELIX01 process (see Troubleshooting —
empty manifest).

## Step 1: Check local config

Read `openbrain-client.local.yaml`. It has this shape:

```yaml
product: <product-slug>
process: HELIX01
mcp_instance: coal                       # target profile and tool prefix
placeholders:                            # optional — operator-filled substitution values
  primary_language: Go                   # example: fill Go in skill bodies

targets:
  coal:
    mcp_base_url: http://localhost:4010/mcp
    mcp_auth_token: <bearer-token>
    advanced_ingestion_token: <advanced-token>
```

If the file is missing, create it from `openbrain-client.local.yaml.example`,
fill in the target profile, and re-run.

The `placeholders:` map is the operator-input surface for `{token}` substitutions
baked into skill bodies at `init-skills` time. After editing the map, re-run `init-skills` for
the changes to take effect — edits do not apply retroactively to already-materialized
files without a re-run.

## Step 2: Run the binary

```bash
openbrain-client init-skills --report-json 2>init.log
```

Human log goes to stderr (redirected to `init.log` above). The JSON `Report`
object goes to stdout. Capture it:

```bash
report=$(openbrain-client init-skills --report-json 2>init.log)
echo "$report" | python3 -m json.tool   # pretty-print
```

For a full sandbox rehearsal without touching the real `.claude/` tree:

```bash
openbrain-client init-skills --dev --report-json 2>init.log
```

Dev mode copies the current `.claude/` into a gitignored `consumer-claude/`
sandbox and runs the complete reconcile there. Inspect the result before committing.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success — reconcile complete |
| 1 | Operational error — see stderr (empty manifest, EXDEV, fetch failure) |
| 2 | Usage error — missing local profile, missing product, empty token |

## Step 3: AI post-review

Parse the JSON report and review what the binary cannot judge mechanically:

- **`restored_agents`**: check each restored agent file's name and trigger phrase
  against `fetched_skills` and `fetched_agents` for semantic collisions (the
  binary can only compare by name, not by intent).
- **`warnings`**: inspect any warnings the binary surfaced (e.g. EXDEV on rename,
  unresolved placeholder tokens).
- **`diverged[]`**: for each entry, the operator or AI edited the previously
  materialized file locally after the last `init-skills` run. The new upstream version
  is now in place; the prior version (before the local edit) and the fresh
  upstream are both preserved in `legacy/<ts>/` (the prior version as the merge
  base). Propose a merge of the local edits onto the new upstream content, or
  surface the conflict to the operator with both versions side by side. The binary
  never auto-merges — your judgment is the resolution mechanism.
- **`legacy_dir`**: the displaced skill/agent content sits here. Review it and
  either delete it (once satisfied) or restore specific items manually.
- **`warnings` — unresolved placeholders**: any warning containing "unresolved
  placeholder" names a `{token}` in a skill body that could not be resolved
  at materialize time. An empty-valued key has been appended to the local
  `placeholders:` map in `openbrain-client.local.yaml`. Ask the operator for the
  value, fill it in, and re-run `init-skills` to bake the substitution.
- **One-time migration from `customize.toml`**: if this project previously ran
  CR-0249's `init-skills`, any operator-filled `[placeholders]` values from per-skill
  `customize.toml` files now live in `legacy/<ts>/**/customize.toml`. Those
  values are NOT auto-migrated. Locate any `legacy/<ts>/**/customize.toml` files,
  read their `[placeholders]` section, and copy the values into the local
  `placeholders:` map under `openbrain-client.local.yaml`, then re-run `init-skills` to bake
  them. This is a one-time per-project manual step.

## Step 4: Report

Print a summary table for the operator:

| Slug | Status |
|---|---|
| `amelia` | fetched |
| `winston` | fetched |
| `my-local-skill` | restored (local-only) |

End with: "Done. N skills fetched, M agents placed, K local-only skills restored."
Point the operator at `legacy_dir` for any displaced content.

---

## Local config reference

```yaml
product: <product-slug>        # required — e.g. openbrain, vela
process: HELIX01               # required — process whose skill pins to fetch
mcp_instance: coal             # target profile and tool prefix (mcp__coal__tool_name)
placeholders:                  # optional — operator-filled values baked into skill bodies at init-skills time
  primary_language: Go         # example: fill any {placeholder} token that appears in skills

targets:
  coal:
    mcp_base_url: http://localhost:4010/mcp
    mcp_auth_token: <bearer-token>
    advanced_ingestion_token: <advanced-token>
```

The `placeholders:` map is the operator-input surface for `{placeholder}` tokens
that are not resolvable from init-skills context itself (e.g. `Go`,
`.`). Values are baked into skill bodies at `init-skills` time —
editing the map takes effect on the next `init-skills` run, not on next skill activation.
Unfilled keys (empty string values) are left as literal `{token}` in the body;
the binary appends them with warnings so the operator knows what to fill in.

Values are validated at `init-skills` startup: a value containing `{{`, a YAML frontmatter
delimiter (`\n---`), ASCII control characters, or exceeding 512 bytes causes
`init-skills` to abort with exit 2 before any tree modification.

---

## Connectivity troubleshooting

**exit 2 — config file not found**
`openbrain-client.local.yaml` is missing. Create it from the example, fill in the
target profile, and re-run.

**exit 1 — "HELIX01 returned an empty skill manifest"**
No skills are released and pinned to the HELIX01 process. Remediate:

1. Release each skill version: `catalog_advance_skill_status slug=<slug> version=<n> to=released`
   (Note: the skill lifecycle is one-way — `released` cannot be reverted.)
   There is no separate pin step: the HELIX01 manifest resolves each slug to its latest
   `released` version, and `catalog_update_development_process` carries no skill pin map.
2. Re-run `openbrain-client init-skills`.

The binary guarantees the live `.claude/` tree is untouched when this error fires —
the manifest is fetched BEFORE any move is performed.

**exit 1 — EXDEV (cross-device rename)**
The `.claude/` directory is on a different filesystem from the `legacy/` target.
`os.Rename` cannot move across device boundaries. Resolution: ensure the project
repo and its `.claude/` tree are on the same filesystem, or use `--dev` mode to
test in a sandbox first.

**exit 2 — "resolved token is empty"**
No token was found in the selected local target profile. Fill `mcp_auth_token`
and re-run.

**Wrong instance name**
If `mcp_instance` is incorrect, tool calls will use the wrong prefix (baked at
init-skills time as `mcp__<instance>__<tool>`). Correct `mcp_instance` in
`openbrain-client.local.yaml` and re-run `init-skills` to re-bake the substituted skill
bodies.

**Server unreachable**
Verify the target profile's `mcp_base_url` (or the `MCP_SERVER_URL` env override)
points to a running OpenBrain instance and that the auth token is valid.
