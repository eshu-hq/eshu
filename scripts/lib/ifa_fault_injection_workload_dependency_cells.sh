#!/usr/bin/env bash
# shellcheck disable=SC2004,SC2034,SC2154
# workload_dependency fault trio. Every cell first proves repo_dependency's
# automatic replay converged the exact workload edge set, then deliberately
# reopens the source workload_materialization row for the fault probe.

workload_dependency_edge_operation_match='MERGE (source)-[rel:DEPENDS_ON]->(target)'

ifa_workload_dependency_fault_prepare() {
	local cell="$1" projector_pid reducer_pid
	fresh_stack "${cell}"
	drive_all_cassettes "${cell}"
	ifa_workload_dependency_live_drive "${bin_dir}" "${workload_dependency_cassette}" \
		|| die "${cell}: workload_dependency cassette drive failed"
	ifa_det_start_bg "${log_dir}" "projector-${cell}-pre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-pre" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}-pre"
	ifa_det_stop_join_untrack_bg_pid "${projector_pid}" TERM || die "${cell}: could not stop pre-maintenance projector"
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid}" TERM || die "${cell}: could not stop pre-maintenance reducer"
	ifa_workload_dependency_live_assert_owned_absent "${bin_dir}" "${cell}-pre-maintenance" \
		|| die "${cell}: workload-owned edge existed before repo_dependency maintenance"
	ifa_repo_dependency_live_run_maintenance_pass "workload-dependency-${cell}" "${bin_dir}" "${log_dir}" \
		|| die "${cell}: bootstrap-index maintenance failed"
	ifa_det_start_bg "${log_dir}" "projector-${cell}-repo" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-repo" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}-repo"
	ifa_det_stop_join_untrack_bg_pid "${projector_pid}" TERM || die "${cell}: could not stop repo prerequisite projector"
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid}" TERM || die "${cell}: could not stop repo prerequisite reducer"
	ifa_workload_dependency_live_assert_repo_prerequisite "${bin_dir}" "${workload_dependency_repo_expected_edges}" \
		|| die "${cell}: repo_dependency prerequisite did not converge to its exact three-edge set"
	ifa_workload_dependency_live_assert "${bin_dir}" "${workload_dependency_expected_edges}" \
		|| die "${cell}: automatic repo_dependency replay did not converge workload_dependency before the fault probe"
}

ifa_workload_dependency_fault_reopen() {
	local cell="$1" reopened
	reopened="$(ifa_workload_dependency_live_reopen_materialization \
		"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}")" \
		|| die "${cell}: workload_materialization reopen failed"
	[[ "${reopened}" == "1" ]] \
		|| die "${cell}: reopened ${reopened@Q} workload_materialization rows, want exactly 1 (non-vacuity guard)"
	printf '%s: explicitly reopened exactly one workload_materialization row after automatic exact-set convergence\n' "${cell}"
}

ifa_workload_dependency_fault_assert_terminal() {
	local cell="$1"
	assert_no_dead_letters "${cell}"
	ifa_workload_dependency_live_assert "${bin_dir}" "${workload_dependency_expected_edges}" \
		|| die "${cell}: workload_dependency did not converge to its exact two-edge owned set"
}

cell_baseline_workload_dependency() {
	local cell="baseline_workload_dependency" cell_start reducer_pid
	cell_start=$(date +%s)
	ifa_workload_dependency_fault_prepare "${cell}"
	ifa_workload_dependency_fault_reopen "${cell}"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}" reducer_pid \
		env "ESHU_REDUCER_CLAIM_DOMAIN=workload_materialization" "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	ifa_workload_dependency_fault_assert_terminal "${cell}"
	baseline_workload_dependency_retried="$(ifa_fault_count_retried \
		"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" workload_materialization)" \
		|| die "${cell}: could not capture workload_materialization retry baseline"
	capture_digest "${cell}"
	teardown_cell "${cell}"
	wall_times[${cell}]=$(( $(date +%s) - cell_start ))
}

cell_killworker_workload_dependency() {
	local cell="killworker_workload_dependency" cell_start lock_pid reducer_before reducer_after claimed
	cell_start=$(date +%s)
	ifa_workload_dependency_fault_prepare "${cell}"
	_ifa_generic_table_lock_start "${cell}" fact_records lock_pid \
		|| die "${cell}: could not acquire the deterministic fact_records read blocker"
	ifa_workload_dependency_fault_reopen "${cell}"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-before" reducer_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=workload_materialization" "${bin_dir}/eshu-reducer"
	claimed="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
		"${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" workload_materialization)" \
		|| die "${cell}: no workload_materialization row was claimed while fact_records was blocked"
	printf '%s: non-vacuous: %s blocked workload_materialization row(s) observed\n' "${cell}" "${claimed}"
	ifa_det_stop_join_untrack_bg_pid "${reducer_before}" KILL || die "${cell}: could not kill and join reducer"
	_ifa_generic_table_lock_release "${cell}" fact_records "${lock_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-after" reducer_after \
		env "ESHU_REDUCER_CLAIM_DOMAIN=workload_materialization" "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	ifa_workload_dependency_fault_assert_terminal "${cell}"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"${compose_file}" "${baseline_workload_dependency_retried}" 15 workload_materialization \
		|| die "${cell}: interrupted workload_materialization did not re-execute above baseline"
	capture_digest "${cell}"
	assert_matches_baseline "${cell}" baseline_workload_dependency
	teardown_cell "${cell}"
	wall_times[${cell}]=$(( $(date +%s) - cell_start ))
}

cell_failgraphwrite_workload_dependency() {
	local cell="failgraphwrite_workload_dependency" cell_start fault_script reducer_pid marker_rc=0
	cell_start=$(date +%s)
	ifa_workload_dependency_fault_prepare "${cell}"
	ifa_workload_dependency_fault_reopen "${cell}"
	fault_script="${work_dir}/fault-once-then-succeed-workload-dependency.json"
	ifa_fault_write_once_script "${fault_script}" "${workload_dependency_edge_operation_match}" queue-retry
	ifa_det_start_bg "${log_dir}" "reducer-${cell}" reducer_pid \
		env "ESHU_REDUCER_CLAIM_DOMAIN=workload_materialization" \
		"ESHU_IFA_FAULT_SCRIPT=${fault_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	ifa_workload_dependency_fault_assert_terminal "${cell}"
	ifa_fault_assert_once_fault_marker "${fault_script}" "${workload_dependency_edge_operation_match}" || marker_rc=$?
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "${cell}: once-fired marker did not name the workload DEPENDS_ON MERGE (status ${marker_rc})"
	printf '%s: non-vacuous: once-fired marker names the workload DEPENDS_ON MERGE\n' "${cell}"
	capture_digest "${cell}"
	assert_matches_baseline "${cell}" baseline_workload_dependency
	teardown_cell "${cell}"
	wall_times[${cell}]=$(( $(date +%s) - cell_start ))
}
