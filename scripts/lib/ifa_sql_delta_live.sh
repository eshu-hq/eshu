#!/usr/bin/env bash
# SQL relationship live-proof helpers for verify-ifa-determinism.sh (#5554).
# This sourced library keeps the matrix driver below the repository's hard
# 500-line file cap. The caller owns strict mode, logging, cleanup, and the
# shared ifa_det_* helpers from ifa_determinism_common.sh.

# ifa_det_drive_sql_baseline drives the committed SQL relationship family into
# the current matrix cell.
ifa_det_drive_sql_baseline() {
	local n="$1" bin_dir="$2" sql_cassette="$3" log_dir="$4"

	printf '\n=== N=%s: drive SQL relationship family cassette through eshu-ifa drive -workers %s ===\n' "${n}" "${n}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${sql_cassette}" -workers "${n}" \
		>"${log_dir}/ifa-drive-sql-n${n}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-sql-n${n}.log" >&2 || true
		echo "N=${n}: eshu-ifa drive (SQL relationship family) failed" >&2
		return 1
	fi
	cat "${log_dir}/ifa-drive-sql-n${n}.log"
}

# ifa_det_assert_sql_baseline rejects an empty, incomplete, or spurious SQL
# materialization even when every worker-count cell has the same graph digest.
ifa_det_assert_sql_baseline() {
	local n="$1" bin_dir="$2" sql_expected_edges="$3"

	printf '\n=== N=%s: assert SQL relationship family materialized edges (absolute set, non-vacuity) ===\n' "${n}"
	if ! "${bin_dir}/eshu-ifa" assert-edges \
		-domain sql_relationships \
		-expected "${sql_expected_edges}"; then
		echo "N=${n}: SQL relationship family materialized edge set did not match the expected set — a family silently not materializing (or spurious edges); do NOT normalize this away" >&2
		return 1
	fi
}

# ifa_det_run_sql_delta_live drives generation 2 into the current durable cell,
# proves the drive added work, drains it through projector + reducer, and
# asserts the accumulated SQL edge set exactly.
ifa_det_run_sql_delta_live() {
	local n="$1" bin_dir="$2" sql_delta_cassette="$3"
	local sql_delta_expected_edges="$4" log_dir="$5"
	local compose_project="$6" use_compose="$7" postgres_dsn="$8"
	local compose_file="$9" drain_timeout="${10}"
	local work_items_before_delta work_items_after_delta
	local projector_pid reducer_pid

	work_items_before_delta="$(ifa_det_pg "${compose_project}" "${use_compose}" "${postgres_dsn}" \
		'SELECT count(*) FROM fact_work_items;' "${compose_file}" | tr -d '[:space:]')"
	printf '\n=== N=%s: drive SQL relationship gen-2 delta cassette through eshu-ifa drive -workers %s ===\n' "${n}" "${n}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${sql_delta_cassette}" -workers "${n}" \
		>"${log_dir}/ifa-drive-sql-delta-n${n}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-sql-delta-n${n}.log" >&2 || true
		echo "N=${n}: eshu-ifa drive (SQL relationship delta) failed" >&2
		return 1
	fi
	cat "${log_dir}/ifa-drive-sql-delta-n${n}.log"
	work_items_after_delta="$(ifa_det_pg "${compose_project}" "${use_compose}" "${postgres_dsn}" \
		'SELECT count(*) FROM fact_work_items;' "${compose_file}" | tr -d '[:space:]')"
	if [[ -z "${work_items_before_delta}" || -z "${work_items_after_delta}" || \
		"${work_items_after_delta}" -le "${work_items_before_delta}" ]]; then
		echo "N=${n}: SQL delta drive enqueued 0 new fact_work_items rows (vacuous delta proof)" >&2
		return 1
	fi

	printf '\n=== N=%s: drain SQL gen-2 delta through projector + reducer ===\n' "${n}"
	ifa_det_start_bg "${log_dir}" "projector-delta-n${n}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-delta-n${n}" reducer_pid "${bin_dir}/eshu-reducer"
	if ! "${bin_dir}/eshu-golden-corpus-gate" \
		-phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-drain-timeout="${drain_timeout}"; then
		tail -30 "${log_dir}/reducer-delta-n${n}.log" || true
		tail -30 "${log_dir}/projector-delta-n${n}.log" || true
		echo "N=${n}: SQL delta drain did not reach the snapshot residual bound within ${drain_timeout}" >&2
		return 1
	fi
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true

	printf '\n=== N=%s: assert SQL delta-live accumulated materialized edges (exact set) ===\n' "${n}"
	if ! "${bin_dir}/eshu-ifa" assert-edges \
		-domain sql_relationships \
		-expected "${sql_delta_expected_edges}"; then
		echo "N=${n}: SQL delta-live materialized edge set did not match the expected accumulated set — stale, missing, or spurious SQL truth after gen 2" >&2
		return 1
	fi
}
