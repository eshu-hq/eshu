#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Maintenance-backed repo_dependency fault trio. The Platform is created only
# after the first drain has stopped its reducer, so neither recovery cell can
# accidentally pass by consuming a pre-existing RUNS_ON edge.

ifa_repo_dependency_fault_prepare() {
	local cell="$1" projector_pid reducer_pid
	fresh_stack "${cell}"
	drive_all_cassettes "${cell}"
	ifa_repo_dependency_live_drive "${bin_dir}" "${repo_dependency_cassette}" || die "${cell}: cassette drive failed"
	ifa_det_start_bg "${log_dir}" "projector-${cell}-pre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-pre" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}-pre"
	ifa_det_stop_join_untrack_bg_pid "${projector_pid}" TERM || die "${cell}: could not stop pre-maintenance projector"
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid}" TERM || die "${cell}: could not stop pre-maintenance reducer"
	ifa_repo_dependency_live_assert_readiness_state "${log_dir}" "reducer-${cell}-pre" 1 || die "${cell}: pre-maintenance reducer log did not prove the resolver gate was closed"
	ifa_repo_dependency_live_assert_gated "${bin_dir}" || die "${cell}: resolver emitted an edge before its Platform prerequisite"
	ifa_repo_dependency_live_materialize_platform_prerequisite "${bin_dir}" || die "${cell}: exact Platform prerequisite was not materialized"
	ifa_repo_dependency_live_run_maintenance_pass "${cell}" "${bin_dir}" "${log_dir}" || die "${cell}: bootstrap-index maintenance failed"
}

ifa_repo_dependency_fault_assert_terminal() {
	local cell="$1" reducer_label="$2"
	assert_no_dead_letters "${cell}"
	ifa_repo_dependency_live_assert_readiness_state "${log_dir}" "${reducer_label}" 0 || die "${cell}: post-maintenance reducer log did not prove the resolver gate opened"
	ifa_repo_dependency_live_assert "${bin_dir}" "${repo_dependency_expected_edges}" \
		|| die "${cell}: repo_dependency did not converge to its seven-edge exact set"
}

cell_baseline_repo_dependency() {
	local cell="baseline_repo_dependency" cell_start projector_pid reducer_pid
	cell_start=$(date +%s)
	ifa_repo_dependency_fault_prepare "${cell}"
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	ifa_repo_dependency_fault_assert_terminal "${cell}" "reducer-${cell}"
	baseline_deployment_mapping_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" deployment_mapping)"
	baseline_deployment_mapping_retried="${baseline_deployment_mapping_retried:-0}"
	capture_digest "${cell}"
	teardown_cell "${cell}"
	wall_times[${cell}]=$(( $(date +%s) - cell_start ))
}

cell_killworker_repo_dependency() {
	local cell="killworker_repo_dependency" cell_start projector_pid reducer_before reducer_after lock_pid=""
	cell_start=$(date +%s)
	ifa_repo_dependency_fault_prepare "${cell}"
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_fault_start_shared_intent_lock "${cell}" repo_dependency lock_pid || die "${cell}: shared-intent lock did not start"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-before" reducer_before "${bin_dir}/eshu-reducer"
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" deployment_mapping \
		|| die "${cell}: deployment_mapping was not claimed while the lock was held"
	ifa_det_stop_join_untrack_bg_pid "${reducer_before}" KILL || die "${cell}: reducer kill failed"
	ifa_fault_release_shared_intent_lock "${cell}" repo_dependency "${lock_pid}" || die "${cell}: shared-intent lock release failed"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-after" reducer_after "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	ifa_repo_dependency_fault_assert_terminal "${cell}" "reducer-${cell}-after"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_deployment_mapping_retried}" 15 deployment_mapping || die "${cell}: deployment_mapping retry did not exceed baseline"
	capture_digest "${cell}"
	assert_matches_baseline "${cell}" baseline_repo_dependency
	teardown_cell "${cell}"
	wall_times[${cell}]=$(( $(date +%s) - cell_start ))
}

cell_failgraphwrite_repo_dependency() {
	local cell="failgraphwrite_repo_dependency" cell_start projector_pid reducer_pid fault_script anchor
	cell_start=$(date +%s)
	ifa_repo_dependency_fault_prepare "${cell}"
	anchor="MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)"
	fault_script="${work_dir}/fault-once-then-succeed-repo-dependency.json"
	ifa_fault_write_once_script "${fault_script}" "${anchor}" queue-retry
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}" reducer_pid env "ESHU_IFA_FAULT_SCRIPT=${fault_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	ifa_fault_assert_once_fault_marker "${fault_script}" "${anchor}" || die "${cell}: DEPENDS_ON fault marker did not fire"
	ifa_repo_dependency_fault_assert_terminal "${cell}" "reducer-${cell}"
	capture_digest "${cell}"
	assert_matches_baseline "${cell}" baseline_repo_dependency
	teardown_cell "${cell}"
	wall_times[${cell}]=$(( $(date +%s) - cell_start ))
}
