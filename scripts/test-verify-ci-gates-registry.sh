#!/usr/bin/env bash
# Static structural test for the CI gate registry (#4213): the verify script,
# the registry YAML, and the committed specs/ci-gates.v1.yaml. Fast,
# credential-free, Docker-free, network-free.
#
# This test runs:
#   1. Structural checks on the verify script itself.
#   2. Existence and syntax checks on the registry YAML.
#   3. The real validate command against the committed registry, so every gate
#      entry's script and workflow references are proven live.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ci-gates-registry.sh"
registry="${repo_root}/specs/ci-gates.v1.yaml"

fail() {
	printf 'test-verify-ci-gates-registry: %s\n' "$*" >&2
	exit 1
}

require() {
	local label="$1" needle="$2" file="$3"
	rg --fixed-strings --quiet -- "${needle}" "${file}" || \
		fail "missing ${label} (${needle}) in ${file}"
}

# ── 1. Verify script structural checks ─────────────────────────────────────

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-ci-gates-registry.sh must be executable"
bash -n "${script}" || fail "verify-ci-gates-registry.sh has a syntax error"

require "strict mode"    "set -euo pipefail"          "${script}"
require "validate call"  "go run ./cmd/ci-gates validate" "${script}"
require "registry arg"   "--registry"                  "${script}"
require "repo-root arg"  "--repo-root"                 "${script}"
require "drift flag"     "--drift"                     "${script}"

# ── 2. Registry YAML structural checks ─────────────────────────────────────

[[ -f "${registry}" ]] || fail "missing ${registry}"
require "schema version"    "version: v1"     "${registry}"
require "gates section"     "gates:"          "${registry}"
require "id field present"  "  - id:"         "${registry}"
require "triggers present"  "    triggers:"   "${registry}"
require "ci_only_reason"    "ci_only_reason:" "${registry}"

# Every gate must declare a tier. Spot-check the enumerated tiers.
require "pre-commit tier"  "tier: pre-commit" "${registry}"
require "pre-push tier"    "tier: pre-push"   "${registry}"
require "pre-pr tier"      "tier: pre-pr"     "${registry}"
require "ci-heavy tier"    "tier: ci-heavy"   "${registry}"

# #4220 drift surfaces: hook mappings + the two reconciliation allowlists.
require "hook_id mapping"     "hook_id:"            "${registry}"
require "hygiene_hooks list"  "hygiene_hooks:"      "${registry}"
require "non_gate_workflows"  "non_gate_workflows:" "${registry}"

# ── 3. Live validate + drift — proves every script + workflow ref exists AND
#       that .pre-commit-config.yaml / .github/workflows stay in lockstep ─────

printf 'test-verify-ci-gates-registry: running live validate --drift on committed registry...\n'
(cd "${repo_root}/go" && go run ./cmd/ci-gates validate \
	--registry "${registry}" \
	--repo-root "${repo_root}" \
	--drift) || fail "live validate --drift failed — see errors above"

printf 'PASS: ci-gates registry static contract + drift\n'
