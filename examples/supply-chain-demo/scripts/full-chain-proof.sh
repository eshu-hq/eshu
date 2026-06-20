#!/usr/bin/env bash
#
# full-chain-proof.sh orchestrates the supply-chain proof scripts and records a
# deterministic JSON proof artifact. The default mode runs the live proof steps;
# --verify-fixture-only only validates the committed fixture for quick CI checks.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${DEMO_DIR}/../.." && pwd)"
TEMPLATE="${DEMO_DIR}/fixtures/full-chain-proof-output.json"
OUTPUT="${OUTPUT:-${TEMPLATE}}"
VERIFY_ONLY=0
SKIP_LOCALTLS=0

usage() {
	cat <<'EOF'
Usage: examples/supply-chain-demo/scripts/full-chain-proof.sh [options]

Runs the supply-chain proof ladder:
  1. live Compose repo -> advisory -> impact -> workload/SBOM proof
  2. live reducer proof for seeded image identity -> image_ref
  3. live localhost TLS OCI collector transport proof

Options:
  --output PATH           Write the proof artifact to PATH (default: committed fixture).
  --skip-localtls         Skip the localhost TLS registry proof and mark it skipped.
  --verify-fixture-only   Do not start Compose; validate the current fixture only.
  -h, --help              Show this help.
EOF
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--output)
			[[ $# -ge 2 ]] || { printf '%s\n' "--output requires a path" >&2; exit 2; }
			OUTPUT="$2"
			shift 2
			;;
		--skip-localtls)
			SKIP_LOCALTLS=1
			shift
			;;
		--verify-fixture-only)
			VERIFY_ONLY=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			printf 'unknown argument: %s\n' "$1" >&2
			usage >&2
			exit 2
			;;
	esac
done

require_tool() {
	local tool="$1"
	command -v "${tool}" >/dev/null 2>&1 || { printf '%s is required\n' "${tool}" >&2; exit 1; }
}

require_tool jq

if [[ "${VERIFY_ONLY}" -eq 1 ]]; then
	exec "${DEMO_DIR}/test/verify-full-chain-proof-output.sh" "${OUTPUT}"
fi

WORKDIR="$(mktemp -d)"
ROW_DIR="${WORKDIR}/rows"
mkdir -p "${ROW_DIR}"
cleanup() {
	rm -rf "${WORKDIR}"
}
trap cleanup EXIT

write_row() {
	local row_path="$1"
	local operation="$2"
	local state="$3"
	local elapsed_ms="$4"
	local sample_count="$5"
	local boundary="$6"
	jq -n \
		--arg surface "script" \
		--arg operation "${operation}" \
		--arg state "${state}" \
		--arg boundary "${boundary}" \
		--argjson p95_ms "${elapsed_ms}" \
		--argjson sample_count "${sample_count}" \
		'{
			surface: $surface,
			operation: $operation,
			p95_ms: $p95_ms,
			sample_count: $sample_count,
			state: $state,
			boundary: $boundary
		}' >"${row_path}"
}

run_step() {
	local id="$1"
	local boundary="$2"
	shift 2
	local start_s
	local end_s
	local elapsed_ms
	start_s="$(date +%s)"
	if "$@"; then
		end_s="$(date +%s)"
		elapsed_ms=$(( (end_s - start_s) * 1000 ))
		write_row "${ROW_DIR}/${id}.json" "${id}" "passed" "${elapsed_ms}" 1 "${boundary}"
		return 0
	fi
	end_s="$(date +%s)"
	elapsed_ms=$(( (end_s - start_s) * 1000 ))
	write_row "${ROW_DIR}/${id}.json" "${id}" "failed" "${elapsed_ms}" 1 "${boundary}"
	return 1
}

run_step "compose_repo_to_workload" \
	"Runs the production-shape Compose proof for repo, advisory, impact finding, workload anchor, and SBOM attachment." \
	"${SCRIPT_DIR}/run-full-chain-proof.sh"

run_step "seeded_image_identity" \
	"Runs the live reducer proof for seeded raw collector facts through image_ref and subject_digest materialization." \
	"${SCRIPT_DIR}/run-image-identity-proof.sh"

if [[ "${SKIP_LOCALTLS}" -eq 1 ]]; then
	write_row "${ROW_DIR}/localtls_oci_transport.json" "localtls_oci_transport" "skipped" 0 0 \
		"Skipped by --skip-localtls; run without that flag before using the artifact as closure evidence."
else
	run_step "localtls_oci_transport" \
		"Runs the real OCI collector against a sandbox localhost TLS registry with an ephemeral CA." \
		"${SCRIPT_DIR}/run-oci-localtls-identity-proof.sh"
fi

ROWS_JSON="${WORKDIR}/rows.json"
jq -s '.' "${ROW_DIR}"/*.json >"${ROWS_JSON}"
observed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
mkdir -p "$(dirname "${OUTPUT}")"
jq \
	--arg observed_at "${observed_at}" \
	--arg commit "$(git -C "${REPO_ROOT}" rev-parse HEAD)" \
	--slurpfile rows "${ROWS_JSON}" \
	'.artifact_status = "live_orchestrator_output"
	| .proof_run = {
		observed_at: $observed_at,
		commit: $commit,
		orchestrator: "examples/supply-chain-demo/scripts/full-chain-proof.sh"
	}
	| .latency_matrix.rows = $rows[0]' \
	"${TEMPLATE}" >"${OUTPUT}"

"${DEMO_DIR}/test/verify-full-chain-proof-output.sh" "${OUTPUT}"
printf 'full-chain proof artifact written: %s\n' "${OUTPUT}"
