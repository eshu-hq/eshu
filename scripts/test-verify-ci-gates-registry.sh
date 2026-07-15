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
static_contract_workflow="${repo_root}/.github/workflows/static-contract-gates.yml"
build_test_workflow="${repo_root}/.github/workflows/test.yml"
frontend_workflow="${repo_root}/.github/workflows/frontend.yml"

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

# Retained-console SQL fixtures are executable proof inputs. A fixture-only
# change must select the same frontend gate in both GitHub and local parity.
[[ -f "${frontend_workflow}" ]] || fail "missing ${frontend_workflow}"
for sql_fixture in \
	'scripts/lib/console-retained-create-proof-schema.sql' \
	'scripts/lib/console-retained-verify-public-identity.sql'; do
	require "frontend workflow retained SQL trigger" "\"${sql_fixture}\"" "${frontend_workflow}"
	require "frontend registry retained SQL trigger" "\"${sql_fixture}\"" "${registry}"
done

# Every gate must declare a tier. Spot-check the enumerated tiers.
require "pre-commit tier"  "tier: pre-commit" "${registry}"
require "pre-push tier"    "tier: pre-push"   "${registry}"
require "pre-pr tier"      "tier: pre-pr"     "${registry}"
require "ci-heavy tier"    "tier: ci-heavy"   "${registry}"

# #4220 drift surfaces: hook mappings + the two reconciliation allowlists.
require "hook_id mapping"     "hook_id:"            "${registry}"
require "hygiene_hooks list"  "hygiene_hooks:"      "${registry}"
require "non_gate_workflows"  "non_gate_workflows:" "${registry}"

# #4218 workflow contract: dorny/paths-filter needs pull-request read
# permission, matrix context cannot be used at jobs.<job_id>.if, and main
# pushes must keep the old all-gates backstop instead of path-filtering only
# the changed files.
[[ -f "${static_contract_workflow}" ]] || fail "missing ${static_contract_workflow}"
require "paths-filter PR permission" "pull-requests: read" "${static_contract_workflow}"
if rg --quiet '^    if:.*matrix\.' "${static_contract_workflow}"; then
	fail "static-contract-gates.yml must not use matrix context in jobs.<job_id>.if"
fi
require "main-push all-gates selector" \
	'[[ "${{ github.event_name }}" == "push" || "${selected}" == "true" ]]' \
	"${static_contract_workflow}"
require "selected gate matrix" \
	"fromJSON(needs.changes.outputs.matrix)" \
	"${static_contract_workflow}"
require "empty-selection job guard" \
	"needs.changes.outputs.any == 'true'" \
	"${static_contract_workflow}"
require "Ifa workflow filter" \
	"ifa:" \
	"${static_contract_workflow}"
require "Ifa workflow path filter" \
	"go/internal/ifa/**" \
	"${static_contract_workflow}"
require "Ifa workflow matrix entry" \
	'append_gate "${{ steps.filter.outputs.ifa }}" "ifa" "Verify Ifa contract-layer gate" "cd go && go test ./internal/ifa ./cmd/ifa -count=1" "cd go && go test ./internal/ifa ./cmd/ifa -count=1"' \
	"${static_contract_workflow}"

# #4263 workflow shape: Build Test must expose separately timed verdict
# surfaces for static contract verifiers, Go lint/build, race tests, and the
# Helm/docs/whitespace tail. A monolithic build job hides which surface hit the
# timeout.
[[ -f "${build_test_workflow}" ]] || fail "missing ${build_test_workflow}"
require "Build Test read-only token permissions" "permissions:" "${build_test_workflow}"
require "Build Test contents read permission" "  contents: read" "${build_test_workflow}"
require "Build Test contract verifier job" "  verify-contracts:" "${build_test_workflow}"
require "Build Test Go core job" "  go-core:" "${build_test_workflow}"
require "Build Test Go race job" "  go-race:" "${build_test_workflow}"
require "Build Test docs/Helm hygiene job" "  docs-helm-hygiene:" "${build_test_workflow}"
require "Build Test go-core cancellation guards" 'if: ${{ !cancelled() }}' "${build_test_workflow}"
require "Build Test race Helm setup" "Set up Helm for race tests" "${build_test_workflow}"
if rg --quiet '^  build:' "${build_test_workflow}"; then
	fail "test.yml must not keep the monolithic build job after #4263 split"
fi

# ── 3. Live validate + drift — proves every script + workflow ref exists AND
#       that .pre-commit-config.yaml / .github/workflows stay in lockstep ─────

printf 'test-verify-ci-gates-registry: running live validate --drift on committed registry...\n'
(cd "${repo_root}/go" && go run ./cmd/ci-gates validate \
	--registry "${registry}" \
	--repo-root "${repo_root}" \
	--drift) || fail "live validate --drift failed — see errors above"

printf 'PASS: ci-gates registry static contract + drift\n'
