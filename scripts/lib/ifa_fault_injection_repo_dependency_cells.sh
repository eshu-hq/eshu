#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Maintenance-backed repo_dependency fault trio. The Platform is created only
# after the first drain has stopped its reducer, so neither recovery cell can
# accidentally pass by consuming a pre-existing RUNS_ON edge.

ifa_repo_dependency_fault_validate_partition_leases() {
	local reducer_pid="$1" snapshot="$2" row partition_id partition_count worker_id worker_count seen=$'\n'
	local row_re
	[[ "${reducer_pid}" =~ ^[1-9][0-9]*$ && -n "${snapshot}" ]] || return 1
	row_re="^repo_dependency\\|([0-9]+)\\|([1-9][0-9]*)\\|repo-dependency-projection-runner:[A-Za-z0-9._-]+:${reducer_pid}:[0-9a-f]{32}/worker-([0-9]+)-of-([1-9][0-9]*)$"
	while IFS= read -r row; do
		[[ "${row}" =~ ${row_re} ]] || return 1
		partition_id="${BASH_REMATCH[1]}"
		partition_count="${BASH_REMATCH[2]}"
		worker_id="${BASH_REMATCH[3]}"
		worker_count="${BASH_REMATCH[4]}"
		(( 10#${partition_id} < 10#${partition_count} )) || return 1
		(( 10#${worker_id} < 10#${worker_count} )) || return 1
		[[ "${seen}" != *$'\n'"${row}"$'\n'* ]] || return 1
		seen+="${row}"$'\n'
	done <<<"${snapshot}"
}

ifa_repo_dependency_fault_capture_partition_leases() {
	local cell="$1" reducer_pid="$2" output_var="$3" snapshot query_rc
	[[ "${cell}" =~ ^[a-z0-9_]+$ && "${reducer_pid}" =~ ^[1-9][0-9]*$ && "${output_var}" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]] || return 2
	if snapshot="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT COALESCE(string_agg(projection_domain || '|' || partition_id::text || '|' || partition_count::text || '|' || lease_owner, E'\\n' ORDER BY partition_id, partition_count, lease_owner), '') FROM shared_projection_partition_leases WHERE projection_domain = 'repo_dependency' AND lease_expires_at > CURRENT_TIMESTAMP AND lease_owner ~ '^repo-dependency-projection-runner:[A-Za-z0-9._-]+:${reducer_pid}:[0-9a-f]{32}/worker-[0-9]+-of-[1-9][0-9]*$';" \
		"${compose_file}")"; then
		:
	else
		query_rc=$?
		printf '%s: repo_dependency partition-lease capture query failed (exit %s)\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	ifa_repo_dependency_fault_validate_partition_leases "${reducer_pid}" "${snapshot}" || {
		printf '%s: expected nonzero active repo_dependency partition leases owned by reducer PID %s\n' "${cell}" "${reducer_pid}" >&2
		return 1
	}
	printf -v "${output_var}" '%s' "${snapshot}"
}

ifa_repo_dependency_fault_require_partition_lease_subset() {
	local reducer_pid="$1" before="$2" after="$3" row framed_after
	ifa_repo_dependency_fault_validate_partition_leases "${reducer_pid}" "${before}" || return 2
	ifa_repo_dependency_fault_validate_partition_leases "${reducer_pid}" "${after}" || return 2
	framed_after=$'\n'"${after}"$'\n'
	while IFS= read -r row; do
		[[ "${framed_after}" == *$'\n'"${row}"$'\n'* ]] || {
			printf 'repo_dependency post-death lease census omitted a pre-kill dead-owner lease\n' >&2
			return 1
		}
	done <<<"${before}"
}

ifa_repo_dependency_fault_assert_no_active_partition_leases() {
	local cell="$1" reducer_pid="$2" remaining query_rc
	[[ "${cell}" =~ ^[a-z0-9_]+$ && "${reducer_pid}" =~ ^[1-9][0-9]*$ ]] || return 2
	if kill -0 "${reducer_pid}" >/dev/null 2>&1; then
		printf '%s: refusing dead-owner lease verification while reducer PID %s is live\n' "${cell}" "${reducer_pid}" >&2
		return 1
	fi
	if remaining="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) FROM shared_projection_partition_leases WHERE projection_domain = 'repo_dependency' AND lease_expires_at > CURRENT_TIMESTAMP AND lease_owner ~ '^repo-dependency-projection-runner:[A-Za-z0-9._-]+:${reducer_pid}:[0-9a-f]{32}/worker-[0-9]+-of-[1-9][0-9]*$';" \
		"${compose_file}")"; then
		:
	else
		query_rc=$?
		printf '%s: repo_dependency post-expiry lease census failed (exit %s)\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	[[ "${remaining}" =~ ^[0-9]+$ ]] || {
		printf '%s: repo_dependency post-expiry lease census returned malformed count\n' "${cell}" >&2
		return 1
	}
	[[ "${remaining}" -eq 0 ]] || {
		printf '%s: %s active repo_dependency leases remain for the dead reducer owner\n' "${cell}" "${remaining}" >&2
		return 1
	}
}

ifa_repo_dependency_fault_expire_partition_leases() {
	local cell="$1" reducer_pid="$2" captured="$3" row domain partition_id partition_count owner
	local values_sql="" expected_count=0 result query_rc affected updated
	[[ "${cell}" =~ ^[a-z0-9_]+$ && "${reducer_pid}" =~ ^[1-9][0-9]*$ ]] || return 2
	ifa_repo_dependency_fault_validate_partition_leases "${reducer_pid}" "${captured}" || return 2
	if kill -0 "${reducer_pid}" >/dev/null 2>&1; then
		printf '%s: refusing to expire partition leases while reducer PID %s is live\n' "${cell}" "${reducer_pid}" >&2
		return 1
	fi
	while IFS='|' read -r domain partition_id partition_count owner; do
		[[ -z "${values_sql}" ]] || values_sql+=","
		values_sql+="('${domain}',${partition_id},${partition_count},'${owner}')"
		expected_count=$((expected_count + 1))
	done <<<"${captured}"
	if result="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"WITH captured(projection_domain, partition_id, partition_count, lease_owner) AS (VALUES ${values_sql}), updated AS (UPDATE shared_projection_partition_leases AS lease SET lease_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 second', updated_at = CURRENT_TIMESTAMP FROM captured WHERE lease.projection_domain = captured.projection_domain AND lease.partition_id = captured.partition_id AND lease.partition_count = captured.partition_count AND lease.lease_owner = captured.lease_owner AND lease.lease_expires_at > CURRENT_TIMESTAMP RETURNING lease.projection_domain, lease.partition_id, lease.partition_count, lease.lease_owner) SELECT count(*)::text || CASE WHEN count(*) = 0 THEN '' ELSE E'\\n' || string_agg(projection_domain || '|' || partition_id::text || '|' || partition_count::text || '|' || lease_owner, E'\\n' ORDER BY partition_id, partition_count, lease_owner) END FROM updated;" \
		"${compose_file}")"; then
		:
	else
		query_rc=$?
		printf '%s: repo_dependency dead-owner partition-lease expiry query failed (exit %s)\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	affected="${result%%$'\n'*}"
	updated=""
	[[ "${result}" != *$'\n'* ]] || updated="${result#*$'\n'}"
	[[ "${affected}" =~ ^[1-9][0-9]*$ && "${affected}" -eq "${expected_count}" ]] || {
		printf '%s: expired %q dead-owner repo_dependency leases, want exactly %s\n' "${cell}" "${affected}" "${expected_count}" >&2
		return 1
	}
	ifa_repo_dependency_fault_validate_partition_leases "${reducer_pid}" "${updated}" || return 1
	[[ "${updated}" == "${captured}" ]] || {
		printf '%s: expired lease identities differ from the exact captured dead-owner set\n' "${cell}" >&2
		return 1
	}
	ifa_repo_dependency_fault_assert_no_active_partition_leases "${cell}" "${reducer_pid}"
}

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
	local pre_kill_partition_leases="" post_kill_partition_leases=""
	cell_start=$(date +%s)
	ifa_repo_dependency_fault_prepare "${cell}"
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_fault_start_shared_intent_lock "${cell}" repo_dependency lock_pid || die "${cell}: shared-intent lock did not start"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-before" reducer_before "${bin_dir}/eshu-reducer"
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" deployment_mapping \
		|| die "${cell}: deployment_mapping was not claimed while the lock was held"
	ifa_repo_dependency_fault_capture_partition_leases "${cell}" "${reducer_before}" pre_kill_partition_leases \
		|| die "${cell}: could not capture the reducer-before repo_dependency partition leases"
	ifa_det_stop_join_untrack_bg_pid "${reducer_before}" KILL || die "${cell}: reducer kill failed"
	ifa_repo_dependency_fault_capture_partition_leases "${cell}" "${reducer_before}" post_kill_partition_leases \
		|| die "${cell}: could not recapture the stable dead-owner repo_dependency partition leases"
	ifa_repo_dependency_fault_require_partition_lease_subset \
		"${reducer_before}" "${pre_kill_partition_leases}" "${post_kill_partition_leases}" \
		|| die "${cell}: post-death lease census lost pre-kill ownership evidence"
	ifa_repo_dependency_fault_expire_partition_leases "${cell}" "${reducer_before}" "${post_kill_partition_leases}" \
		|| die "${cell}: exact dead-owner repo_dependency partition leases did not expire"
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
