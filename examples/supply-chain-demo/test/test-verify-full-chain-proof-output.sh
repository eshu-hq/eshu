#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERIFIER="${SCRIPT_DIR}/verify-full-chain-proof-output.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

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
REAL_RG="$(command -v rg)"

[[ -x "${VERIFIER}" ]] || die "missing executable verifier: ${VERIFIER}"
bash -n "${VERIFIER}"

write_valid_fixture() {
	local output="$1"
	jq -n '
		{
			chain: {
				evidence_nodes: [
					{kind: "advisory"},
					{kind: "package_manifest"},
					{kind: "lockfile"},
					{kind: "package_registry"},
					{kind: "impact_finding"},
					{kind: "workload"},
					{kind: "sbom_attachment"},
					{kind: "image_identity"}
				],
				assertions: {
					minimum_evidence_nodes: 8,
					api_mcp_cli_truth_labels_agree: true,
					missing_evidence_labels_agree: true
				}
			},
			refusal_variant: {
				readiness_state: "evidence_incomplete",
				missing_evidence: ["advisory_sources"]
			},
			latency_matrix: {
				rows: [{step: "fixture", p95_ms: 0}]
			},
			truth_boundaries: ["live-compose", "image-identity", "oci-localtls"],
			scripts: [{path: "scripts/full-chain-proof.sh"}]
		}
	' >"${output}"
}

expect_pass() {
	local label="$1"
	local input="$2"
	if ! "${VERIFIER}" "${input}" >"${TMP_DIR}/${label}.out" 2>"${TMP_DIR}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${TMP_DIR}/${label}.err" >&2
		exit 1
	fi
}

expect_fail() {
	local label="$1"
	local input="$2"
	local expected="$3"
	if "${VERIFIER}" "${input}" >"${TMP_DIR}/${label}.out" 2>"${TMP_DIR}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	"${REAL_RG}" --fixed-strings --quiet -- "${expected}" "${TMP_DIR}/${label}.err" \
		|| { printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2; sed -n '1,120p' "${TMP_DIR}/${label}.err" >&2; exit 1; }
}

write_early_exit_rg() {
	local output="$1"
	{
		printf '#!/usr/bin/env bash\n'
		printf 'set -euo pipefail\n'
		printf 'real_rg=%q\n' "${REAL_RG}"
		printf 'args=("$@")\n'
		printf 'pattern=""\n'
		printf 'files=()\n'
		printf 'for arg in "${args[@]}"; do\n'
		printf '  case "${arg}" in\n'
		printf '    --quiet|--fixed-strings) ;;\n'
		printf '    --) ;;\n'
		printf '    -*) ;;\n'
		printf '    *)\n'
		printf '      if [[ -z "${pattern}" ]]; then pattern="${arg}"; else files+=("${arg}"); fi\n'
		printf '      ;;\n'
		printf '  esac\n'
		printf 'done\n'
		printf 'if [[ "${#files[@]}" -gt 0 ]]; then\n'
		printf '  exec "${real_rg}" --quiet -- "${pattern}" "${files[@]}"\n'
		printf 'fi\n'
		printf 'while IFS= read -r line; do\n'
		printf '  if printf "%%s\\n" "${line}" | "${real_rg}" --quiet -- "${pattern}"; then exit 0; fi\n'
		printf 'done\n'
		printf 'exit 1\n'
	} >"${output}"
	chmod +x "${output}"
}

valid_fixture="${TMP_DIR}/valid.json"
write_valid_fixture "${valid_fixture}"
expect_pass valid "${valid_fixture}"

private_shaped_fixture="${TMP_DIR}/private-shaped.json"
jq '
	{
		leak_probe: "person@example.invalid",
		chain,
		refusal_variant,
		latency_matrix,
		truth_boundaries,
		scripts,
		padding: [range(0; 200000) | "public-padding-\(.)"]
	}
' "${valid_fixture}" >"${private_shaped_fixture}"

fake_bin="${TMP_DIR}/bin"
mkdir -p "${fake_bin}"
write_early_exit_rg "${fake_bin}/rg"
PATH="${fake_bin}:${PATH}"
expect_fail private_shaped "${private_shaped_fixture}" "private-shaped or credential-shaped values"

printf 'full-chain proof fixture verifier tests passed\n'
