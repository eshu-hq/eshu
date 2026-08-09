#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/golden-corpus-aggregate-counts.sh
source "${repo_root}/scripts/lib/golden-corpus-aggregate-counts.sh"

fail() { printf 'test-golden-corpus-aggregate-counts: %s\n' "$*" >&2; exit 1; }
die() { fail "$@"; }

case_dir="$(mktemp -d -t golden-aggregate-counts.XXXXXX)"
trap 'rm -rf "${case_dir}"' EXIT
log_dir="${case_dir}/logs"
mkdir -p "${log_dir}"
GATE_API_PORT=39091
GATE_API_KEY="golden-public-test-key"
mock_mode="success"

curl() {
	local output_file="" header="" url=""
	while (($# > 0)); do
		case "$1" in
			-o)
				output_file="$2"
				shift 2
				;;
			-H)
				header="$2"
				shift 2
				;;
			-w | --connect-timeout | --max-time)
				shift 2
				;;
			-sS)
				shift
				;;
			*)
				url="$1"
				shift
				;;
		esac
	done
	[[ "${header}" == "Authorization: Bearer ${GATE_API_KEY}" ]] || fail "gate auth header missing"
	[[ -n "${output_file}" ]] || fail "curl output path missing"
	case "${mock_mode}:${url}" in
		success:*'/api/v0/infra/resources/count')
			printf '%s\n' '{"total_resources":120,"by_provider":{"aws":8,"gcp":110}}' >"${output_file}"
			printf '200'
			;;
		ecosystem-zero:*'/api/v0/infra/resources/count' | ecosystem-missing:*'/api/v0/infra/resources/count' | ecosystem-noninteger:*'/api/v0/infra/resources/count')
			printf '%s\n' '{"total_resources":120,"by_provider":{"aws":8,"gcp":110}}' >"${output_file}"
			printf '200'
			;;
		success:*'/api/v0/ecosystem/overview')
			printf '%s\n' '{"repo_count":30,"workload_count":2,"platform_count":1,"instance_count":4}' >"${output_file}"
			printf '200'
			;;
		ecosystem-zero:*'/api/v0/ecosystem/overview')
			printf '%s\n' '{"repo_count":30,"workload_count":0}' >"${output_file}"
			printf '200'
			;;
		ecosystem-missing:*'/api/v0/ecosystem/overview')
			printf '%s\n' '{"workload_count":2}' >"${output_file}"
			printf '200'
			;;
		ecosystem-noninteger:*'/api/v0/ecosystem/overview')
			printf '%s\n' '{"repo_count":30,"workload_count":"2"}' >"${output_file}"
			printf '200'
			;;
		zero:*'/api/v0/infra/resources/count')
			printf '%s\n' '{"by_provider":{"aws":0,"gcp":110}}' >"${output_file}"
			printf '200'
			;;
		missing:*'/api/v0/infra/resources/count')
			printf '%s\n' '{"by_provider":{"aws":8}}' >"${output_file}"
			printf '200'
			;;
		noninteger:*'/api/v0/infra/resources/count')
			printf '%s\n' '{"by_provider":{"aws":8.5,"gcp":110}}' >"${output_file}"
			printf '200'
			;;
		malformed:*'/api/v0/infra/resources/count')
			printf '%s\n' '{not-json' >"${output_file}"
			printf '200'
			;;
		auth:*'/api/v0/infra/resources/count')
			printf '%s\n' '{"error":"Unauthorized"}' >"${output_file}"
			printf '401'
			;;
		transport:*'/api/v0/infra/resources/count')
			return 7
			;;
		*)
			fail "unexpected mock request: ${mock_mode}:${url}"
			;;
	esac
}

expect_capture_failure() {
	local mode="$1"
	if (
		die() { exit 91; }
		mock_mode="${mode}"
		golden_aggregate_counts_capture
	); then
		fail "${mode} API response was accepted"
	fi
}

expect_compose_failure() {
	local input="$1" label="$2"
	if (
		die() { exit 92; }
		golden_aggregate_counts_compose_snapshot "${input}" "${case_dir}/${label}.out.json"
	); then
		fail "${label} snapshot was accepted"
	fi
}

golden_aggregate_counts_capture
[[ "${golden_aggregate_infra_aws_count}" == "8" ]] || fail "AWS count was not captured"
[[ "${golden_aggregate_infra_gcp_count}" == "110" ]] || fail "GCP count was not captured"
[[ "${golden_aggregate_ecosystem_repo_count}" == "30" ]] || fail "repository count was not captured"
[[ "${golden_aggregate_ecosystem_workload_count}" == "2" ]] || fail "workload count was not captured"

input_snapshot="${case_dir}/input.json"
output_snapshot="${case_dir}/output.json"
jq -n \
	--arg aws "${golden_aggregate_infra_aws_sentinel}" \
	--arg gcp "${golden_aggregate_infra_gcp_sentinel}" \
	--arg repos "${golden_aggregate_ecosystem_repo_sentinel}" \
	--arg workloads "${golden_aggregate_ecosystem_workload_sentinel}" \
	'{prior_transform: "preserved", counts: {aws: $aws, gcp: $gcp, repos: $repos, workloads: $workloads}}' \
	>"${input_snapshot}"
golden_aggregate_counts_compose_snapshot "${input_snapshot}" "${output_snapshot}"
jq -e '
	.prior_transform == "preserved"
	and .counts == {aws: 8, gcp: 110, repos: 30, workloads: 2}
	and ([.counts[] | type] | all(. == "number"))
' "${output_snapshot}" >/dev/null || fail "runtime counts were not composed as JSON integers"

in_place_snapshot="${case_dir}/in-place.json"
cp "${input_snapshot}" "${in_place_snapshot}"
golden_aggregate_counts_compose_snapshot "${in_place_snapshot}" "${in_place_snapshot}"
cmp -s "${in_place_snapshot}" "${output_snapshot}" || fail "input=output composition changed bytes"

duplicate_snapshot="${case_dir}/duplicate.json"
jq --arg duplicate "${golden_aggregate_infra_aws_sentinel}" '.duplicate = $duplicate' \
	"${input_snapshot}" >"${duplicate_snapshot}"
expect_compose_failure "${duplicate_snapshot}" "duplicate"

missing_snapshot="${case_dir}/missing.json"
jq 'del(.counts.gcp)' "${input_snapshot}" >"${missing_snapshot}"
expect_compose_failure "${missing_snapshot}" "missing"

malformed_snapshot="${case_dir}/malformed.json"
printf '%s\n' '{not-json' >"${malformed_snapshot}"
expect_compose_failure "${malformed_snapshot}" "malformed"

for mode in zero missing noninteger ecosystem-zero ecosystem-missing ecosystem-noninteger malformed auth transport; do
	expect_capture_failure "${mode}"
done

printf 'PASS: golden aggregate count leaf helper\n'
