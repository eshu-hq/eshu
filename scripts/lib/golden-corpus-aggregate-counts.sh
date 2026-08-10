#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Runtime aggregate-count capture and snapshot composition for the B-7 gate.
# The caller supplies strict JSON printed by golden-corpus-gate's independent
# persisted-graph oracle. API/MCP values are never used as their own expected
# values, so a shared wrong positive cannot self-validate.
# shellcheck disable=SC2154

golden_aggregate_infra_aws_sentinel="__runtime_infra_aws_count__"
golden_aggregate_infra_gcp_sentinel="__runtime_infra_gcp_count__"
golden_aggregate_ecosystem_repo_sentinel="__runtime_ecosystem_repo_count__"
golden_aggregate_ecosystem_workload_sentinel="__runtime_ecosystem_workload_count__"

golden_aggregate_counts_read_positive_integer() {
	local oracle_file="$1" expression="$2" label="$3" value
	value="$(
		jq -er \
			"${expression} | if type == \"number\" and . > 0 and . == floor then tostring else error(\"expected positive integer\") end" \
			"${oracle_file}"
	)" || die "persisted aggregate oracle has no positive integer ${label}"
	printf '%s\n' "${value}"
}

golden_aggregate_counts_capture() {
	local oracle_file="$1"
	command -v jq >/dev/null 2>&1 || die "jq is required for aggregate count capture"
	[[ -f "${oracle_file}" ]] || die "persisted aggregate oracle file is missing"
	jq -es 'length == 1 and (.[0] | type == "object")' "${oracle_file}" >/dev/null 2>&1 ||
		die "persisted aggregate oracle must contain exactly one JSON object"
	golden_aggregate_infra_aws_count="$(
		golden_aggregate_counts_read_positive_integer "${oracle_file}" '.infra_aws_count' 'infra_aws_count'
	)" || return $?
	golden_aggregate_infra_gcp_count="$(
		golden_aggregate_counts_read_positive_integer "${oracle_file}" '.infra_gcp_count' 'infra_gcp_count'
	)" || return $?
	golden_aggregate_ecosystem_repo_count="$(
		golden_aggregate_counts_read_positive_integer "${oracle_file}" '.ecosystem_repo_count' 'ecosystem_repo_count'
	)" || return $?
	golden_aggregate_ecosystem_workload_count="$(
		golden_aggregate_counts_read_positive_integer "${oracle_file}" '.ecosystem_workload_count' 'ecosystem_workload_count'
	)" || return $?
}

golden_aggregate_counts_assert_runtime_value() {
	local value="$1" label="$2"
	[[ "${value}" =~ ^[1-9][0-9]*$ ]] || die "${label} is not a captured positive integer"
}

golden_aggregate_counts_assert_single_sentinel() {
	local input_snapshot="$1" sentinel="$2" label="$3" count
	count="$(
		jq -er --arg sentinel "${sentinel}" \
			'[paths(strings) as $path | select(getpath($path) == $sentinel)] | length' \
			"${input_snapshot}"
	)" || die "failed to inspect aggregate-count sentinel ${label}"
	[[ "${count}" == "1" ]] ||
		die "aggregate-count sentinel ${label} must occur exactly once, found ${count}"
}

golden_aggregate_counts_compose_snapshot() {
	local input_snapshot="$1" output_snapshot="$2" temporary
	command -v jq >/dev/null 2>&1 || die "jq is required for aggregate count composition"
	[[ -f "${input_snapshot}" ]] || die "aggregate-count input snapshot is missing"
	jq -e . "${input_snapshot}" >/dev/null 2>&1 || die "aggregate-count input snapshot is malformed JSON"

	golden_aggregate_counts_assert_runtime_value "${golden_aggregate_infra_aws_count:-}" "AWS infrastructure count"
	golden_aggregate_counts_assert_runtime_value "${golden_aggregate_infra_gcp_count:-}" "GCP infrastructure count"
	golden_aggregate_counts_assert_runtime_value "${golden_aggregate_ecosystem_repo_count:-}" "ecosystem repository count"
	golden_aggregate_counts_assert_runtime_value "${golden_aggregate_ecosystem_workload_count:-}" "ecosystem workload count"
	golden_aggregate_counts_assert_single_sentinel "${input_snapshot}" "${golden_aggregate_infra_aws_sentinel}" "infra AWS"
	golden_aggregate_counts_assert_single_sentinel "${input_snapshot}" "${golden_aggregate_infra_gcp_sentinel}" "infra GCP"
	golden_aggregate_counts_assert_single_sentinel "${input_snapshot}" "${golden_aggregate_ecosystem_repo_sentinel}" "ecosystem repository"
	golden_aggregate_counts_assert_single_sentinel "${input_snapshot}" "${golden_aggregate_ecosystem_workload_sentinel}" "ecosystem workload"

	temporary="$(mktemp "${output_snapshot}.tmp.XXXXXX")" || die "failed to create aggregate-count snapshot temporary file"
	jq \
		--arg aws_sentinel "${golden_aggregate_infra_aws_sentinel}" \
		--arg gcp_sentinel "${golden_aggregate_infra_gcp_sentinel}" \
		--arg repo_sentinel "${golden_aggregate_ecosystem_repo_sentinel}" \
		--arg workload_sentinel "${golden_aggregate_ecosystem_workload_sentinel}" \
		--argjson aws_count "${golden_aggregate_infra_aws_count}" \
		--argjson gcp_count "${golden_aggregate_infra_gcp_count}" \
		--argjson repo_count "${golden_aggregate_ecosystem_repo_count}" \
		--argjson workload_count "${golden_aggregate_ecosystem_workload_count}" \
		'walk(
			if type == "string" and . == $aws_sentinel then $aws_count
			elif type == "string" and . == $gcp_sentinel then $gcp_count
			elif type == "string" and . == $repo_sentinel then $repo_count
			elif type == "string" and . == $workload_sentinel then $workload_count
			else . end
		)' \
		"${input_snapshot}" >"${temporary}" || {
		rm -f "${temporary}"
		die "failed to compose aggregate-count runtime snapshot"
	}
	mv "${temporary}" "${output_snapshot}" || die "failed to install aggregate-count runtime snapshot"
}
