#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
specs="${repo_root}/specs"
contract="${repo_root}/specs/capability-budget-proof.v1.yaml"
artifact=""

usage() {
	cat >&2 <<'USAGE'
Usage: scripts/verify-capability-budget-proof.sh [--artifact PATH] [--specs DIR] [--contract PATH]

Validates the capability-matrix performance-budget proof contract for issue
#4062. Without --artifact, validates the repository contract spec. With
--artifact, verifies the public-safe proof artifact against the capability
matrix.
USAGE
}

die() {
	printf 'verify-capability-budget-proof: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

while (($# > 0)); do
	case "$1" in
		--artifact) artifact="${2:-}"; shift 2 ;;
		--specs) specs="${2:-}"; shift 2 ;;
		--contract) contract="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown argument: $1" ;;
	esac
done

require_tool rg
require_tool go
export GOCACHE="${GOCACHE:-${repo_root}/.gocache}"

if [[ -n "${artifact}" ]]; then
	[[ -f "${artifact}" ]] || die "artifact file not found: ${artifact}"
	(
		cd "${repo_root}/go"
		go run ./cmd/capability-inventory \
			-mode budget-proof \
			-specs "${specs}" \
			-budget-artifact "${artifact}"
	)
	exit 0
fi

[[ -f "${contract}" ]] || die "contract spec not found: ${contract}"

required_text=(
	"version: capability-budget-proof/v1"
	"issue: 4062"
	"schema_version: capability-budget-proof/v1"
	"- id: capability"
	"- id: profile"
	"- id: p95_ms"
	"- id: declared_max_scope_size"
	"- id: surface_parity"
	"- id: surface_parity.api_p95_ms"
	"- id: surface_parity.mcp_p95_ms"
	"- id: surface_parity.max_delta_ms"
	"rule: required_when_matrix_declares_p95"
	"rule: required_when_matrix_declares_max_scope_size"
	"rule: required_for_api_mcp_proxy"
	"privacy_status: public_safe"
	"raw_evidence_location: operator_local_only"
)

for text in "${required_text[@]}"; do
	rg --fixed-strings --quiet -- "${text}" "${contract}" \
		|| die "missing contract text: ${text}"
done

printf 'verify-capability-budget-proof: pass contract=%s\n' "${contract}"
