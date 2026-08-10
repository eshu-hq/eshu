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

write_oracle() {
	local mode="$1" output="$2"
	case "${mode}" in
		success) printf '%s\n' '{"infra_aws_count":8,"infra_gcp_count":110,"ecosystem_repo_count":30,"ecosystem_workload_count":9}' >"${output}" ;;
		zero) printf '%s\n' '{"infra_aws_count":0,"infra_gcp_count":110,"ecosystem_repo_count":30,"ecosystem_workload_count":9}' >"${output}" ;;
		missing) printf '%s\n' '{"infra_aws_count":8,"ecosystem_repo_count":30,"ecosystem_workload_count":9}' >"${output}" ;;
		noninteger) printf '%s\n' '{"infra_aws_count":8.5,"infra_gcp_count":110,"ecosystem_repo_count":30,"ecosystem_workload_count":9}' >"${output}" ;;
		string) printf '%s\n' '{"infra_aws_count":8,"infra_gcp_count":110,"ecosystem_repo_count":"30","ecosystem_workload_count":9}' >"${output}" ;;
		malformed) printf '%s\n' '{not-json' >"${output}" ;;
		*) fail "unknown oracle mode ${mode}" ;;
	esac
}

expect_capture_failure() {
	local mode="$1"
	local oracle="${case_dir}/${mode}-oracle.json"
	write_oracle "${mode}" "${oracle}"
	if (
		die() { exit 91; }
		golden_aggregate_counts_capture "${oracle}"
	); then
		fail "${mode} persisted oracle was accepted"
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

oracle_file="${case_dir}/persisted-oracle.json"
write_oracle success "${oracle_file}"
curl() { fail "aggregate capture must not call API/MCP curl"; }
golden_aggregate_counts_capture "${oracle_file}"
[[ "${golden_aggregate_infra_aws_count}" == "8" ]] || fail "AWS count was not captured"
[[ "${golden_aggregate_infra_gcp_count}" == "110" ]] || fail "GCP count was not captured"
[[ "${golden_aggregate_ecosystem_repo_count}" == "30" ]] || fail "repository count was not captured"
[[ "${golden_aggregate_ecosystem_workload_count}" == "9" ]] || fail "workload count was not captured"

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
	and .counts == {aws: 8, gcp: 110, repos: 30, workloads: 9}
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

for mode in zero missing noninteger string malformed; do
	expect_capture_failure "${mode}"
done

printf 'PASS: golden aggregate count leaf helper\n'
