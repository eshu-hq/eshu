#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
FIXTURE="${1:-${DEMO_DIR}/fixtures/full-chain-proof-output.json}"

die() {
	printf '%s\n' "$*" >&2
	exit 1
}

require_tool() {
	local tool="$1"
	command -v "${tool}" >/dev/null 2>&1 || die "${tool} is required"
}

require_tool jq
require_tool rg

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

[[ -s "${FIXTURE}" ]] || die "missing full-chain proof fixture: ${FIXTURE}"
jq -e . "${FIXTURE}" >/dev/null || die "full-chain proof fixture is not valid JSON"

node_count="$(jq '(.chain.evidence_nodes // []) | length' "${FIXTURE}")"
[[ "${node_count}" -ge 7 ]] || die "expected at least 7 evidence nodes, got ${node_count}"

for kind in advisory package_manifest lockfile package_registry impact_finding workload sbom_attachment image_identity; do
	jq -e --arg kind "${kind}" \
		'any(.chain.evidence_nodes[]?; .kind == $kind)' \
		"${FIXTURE}" >/dev/null \
		|| die "missing required evidence node kind: ${kind}"
done

jq -e '.chain.assertions.minimum_evidence_nodes >= 7' "${FIXTURE}" >/dev/null \
	|| die "fixture must declare a minimum evidence-node assertion of at least 7"
jq -e '.chain.assertions.api_mcp_cli_truth_labels_agree == true' "${FIXTURE}" >/dev/null \
	|| die "fixture must assert API/MCP/CLI truth-label agreement"
jq -e '.chain.assertions.missing_evidence_labels_agree == true' "${FIXTURE}" >/dev/null \
	|| die "fixture must assert missing-evidence label agreement"
jq -e '.refusal_variant.readiness_state == "evidence_incomplete"' "${FIXTURE}" >/dev/null \
	|| die "refusal variant must remain evidence_incomplete"
jq -e '(.refusal_variant.missing_evidence // []) | index("advisory_sources") != null' \
	"${FIXTURE}" >/dev/null \
	|| die "refusal variant must require advisory_sources"
jq -e '(.latency_matrix.rows // []) | length >= 1' "${FIXTURE}" >/dev/null \
	|| die "fixture must include at least one latency matrix row"
jq -e 'all(.latency_matrix.rows[]?; (.p95_ms | type == "number") and .p95_ms >= 0)' \
	"${FIXTURE}" >/dev/null \
	|| die "all latency matrix rows must include non-negative numeric p95_ms"
jq -e '(.truth_boundaries // []) | length >= 3' "${FIXTURE}" >/dev/null \
	|| die "fixture must document proof boundaries"
jq -e 'any(.scripts[]?; .path == "scripts/full-chain-proof.sh")' "${FIXTURE}" >/dev/null \
	|| die "fixture must point to scripts/full-chain-proof.sh as the unified proof entrypoint"

while IFS= read -r script_path; do
	[[ -x "${DEMO_DIR}/${script_path}" ]] || die "missing executable proof script: ${script_path}"
done < <(jq -r '.scripts[]?.path' "${FIXTURE}")

fixture_strings="${TMP_DIR}/fixture-strings.txt"
jq -r '.. | strings' "${FIXTURE}" >"${fixture_strings}"
if rg --quiet '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' "${fixture_strings}"; then
	die "full-chain proof fixture contains private-shaped or credential-shaped values"
fi

printf 'full-chain proof fixture verified\n'
