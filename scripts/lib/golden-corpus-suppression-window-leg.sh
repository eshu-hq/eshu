#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #7740 hidden-leg window management for the B-7 suppression producer proof.
# Companion to scripts/lib/golden-corpus-vulnerability-suppression.sh, which
# holds the globals, payload writers, drains, and orchestration; sourcers load
# that lib first, then this one. Split to keep both files under the repo's
# 500-line cap. The caller provides start_bg, pg, die, bin_dir, log_dir,
# GATE_API_PORT, GATE_API_KEY, and GATE_DRAIN_TIMEOUT.
# shellcheck disable=SC2154,SC2016

# #7740: arms A's window immediately before the snapshot block and records
# the armed body path and expiry epoch for the hidden leg.
golden_suppression_arm_active_payload() {
	golden_suppression_active_body="${log_dir}/suppression-active-request.json"
	golden_suppression_active_epoch=$(( $(date -u '+%s') + golden_suppression_active_window ))
	local authored_at expires_at
	authored_at="$(golden_suppression_rfc3339_from_epoch "$((golden_suppression_active_epoch - golden_suppression_active_window))")"
	expires_at="$(golden_suppression_rfc3339_from_epoch "${golden_suppression_active_epoch}")"
	golden_suppression_write_ignored_body \
		"${golden_suppression_id}" "${golden_suppression_active_body}" \
		"${authored_at}" "${expires_at}"
}

golden_suppression_assert_hidden() {
	golden_suppression_measure_get \
		"ignored_hidden" \
		"/api/v0/supply-chain/impact/findings?limit=10&cve_id=${golden_suppression_cve}&profile=comprehensive" \
		'.count == 0 and (.findings | length) == 0'
	golden_suppression_measure_get \
		"ignored_audit" \
		"/api/v0/supply-chain/impact/findings?limit=10&cve_id=${golden_suppression_cve}&profile=comprehensive&include_suppressed=true" \
		'.count >= 1 and any(.findings[]?; .cve_id == $cve and .suppression.state == "ignored" and .suppression.suppression_id == $id)'
}

golden_suppression_assert_retry_noop() {
	local before_generations before_work after_generations after_work
	local response_file status duration timings_file
	read -r before_generations before_work < <(golden_suppression_counts)
	timings_file="${log_dir}/suppression-retry-timings.txt"
	: >"${timings_file}"
	for attempt in 1 2 3 4 5 6 7; do
		response_file="${log_dir}/suppression-retry-${attempt}.json"
		read -r status duration < <(
			golden_suppression_request "${golden_suppression_active_body}" "${response_file}"
		)
		[[ "${status}" == "200" ]] ||
			die "suppression retry returned HTTP ${status}: $(jq -c . "${response_file}")"
		jq -e \
			--arg id "${golden_suppression_id}" \
			'.suppression_id == $id and .status == "unchanged" and ((.generation_id // "") | length > 0)' \
			"${response_file}" >/dev/null ||
			die "suppression retry response failed contract: $(jq -c . "${response_file}")"
		printf '%s\n' "${duration}" >>"${timings_file}"
	done
	read -r after_generations after_work < <(golden_suppression_counts)
	[[ "${after_generations}" == "${before_generations}" && "${after_work}" == "${before_work}" ]] ||
		die "identical suppression retry changed generation/work counts: ${before_generations}/${before_work} -> ${after_generations}/${after_work}"
	local p50
	p50="$(sort -n "${timings_file}" | awk 'NR == 4 { print; exit }')"
	printf 'suppression_perf mutation=identical_retry handler_p50_seconds=%s samples=7 generations=%s work_items=%s\n' \
		"${p50}" "${after_generations}" "${after_work}"
}
