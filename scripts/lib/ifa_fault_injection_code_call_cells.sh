#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# code_calls-targeted live fault cells (#5991). This function library is sourced
# by verify-ifa-fault-injection.sh; the driver owns strict mode and globals.

# ifa_code_call_require_fresh_intents fails closed unless a fresh compose stack
# has a numeric zero count for the code_calls intent domain.
ifa_code_call_require_fresh_intents() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" postgres_dsn="$4" compose_file_arg="$5"
	local pre_intents pre_intents_rc
	if pre_intents="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${postgres_dsn}" \
		"SELECT count(*) FROM shared_projection_intents WHERE projection_domain = 'code_calls';" \
		"${compose_file_arg}")"; then
		pre_intents_rc=0
	else
		pre_intents_rc=$?
	fi
	if [[ "${pre_intents_rc}" -ne 0 ]]; then
		printf '%s: fresh-stack precondition query FAILED (exit %s)\n' "${cell}" "${pre_intents_rc}" >&2
		return "${pre_intents_rc}"
	fi
	pre_intents="$(printf '%s' "${pre_intents}" | tr -d '[:space:]')"
	if [[ -z "${pre_intents}" ]]; then
		printf '%s: fresh-stack precondition query returned empty output; treat that as unknown, not as zero\n' "${cell}" >&2
		return 1
	fi
	if [[ ! "${pre_intents}" =~ ^[0-9]+$ ]]; then
		printf '%s: fresh-stack precondition query returned non-numeric output %q; treat that as unknown, not as zero\n' \
			"${cell}" "${pre_intents}" >&2
		return 1
	fi
	if [[ "${pre_intents}" != "0" ]]; then
		printf '%s: %s code_calls intent row(s) survived fresh_stack\n' "${cell}" "${pre_intents}" >&2
		return 1
	fi
}

# ifa_code_call_start_intent_lock holds the first durable write used by
# code_call_materialization. This makes the claimed-row observation and kill
# deterministic: the handler cannot acknowledge between the observation and
# kill, and the post-restart attempt_count proof identifies the same domain.
ifa_code_call_start_intent_lock() {
	local cell="$1" pid_var="$2"
	local app_name="ifa_code_call_lock_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE shared_projection_intents IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/code-call-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/code-call-lock-${cell}.log" 2>&1 &
	fi
	local holder_pid=$!
	bg_pids+=("${holder_pid}")
	printf -v "${pid_var}" '%s' "${holder_pid}"

	local i lock_count
	for i in $(seq 1 60); do
		lock_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE a.application_name = '${app_name}' AND l.relation = 'shared_projection_intents'::regclass AND l.mode = 'AccessExclusiveLock' AND l.granted;" \
			"${compose_file}" | tr -d '[:space:]')"
		if [[ "${lock_count}" == "1" ]]; then
			return 0
		fi
		sleep 0.25
	done
	return 1
}

# ifa_code_call_release_intent_lock terminates the named lock-holder backend,
# then joins its local psql/docker process before the replacement reducer starts.
ifa_code_call_release_intent_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_code_call_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_killworker_code_calls proves a genuinely in-flight code-call handler is
# reclaimed after process death. The table lock prevents the short handler from
# acknowledging before kill; attempt_count > the clean baseline proves the
# replacement reducer re-executed that domain, not merely another queued row.
cell_killworker_code_calls() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-code-calls: fresh stack"
	fresh_stack killworkercodecalls
	drive_all_cassettes killworkercodecalls
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkercodecalls" projector_pid "${bin_dir}/eshu-projector"
	ifa_code_call_start_intent_lock "killworkercodecalls" lock_holder_pid \
		|| die "kill-worker-after-claim-code-calls: could not acquire the deterministic shared_projection_intents blocker"
	ifa_det_start_bg "${log_dir}" "reducer-killworkercodecalls-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "code_call_materialization")" \
		|| die "kill-worker-after-claim-code-calls: no code_call_materialization row was claimed while its durable write was blocked"
	printf 'kill-worker-after-claim-code-calls: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_code_call_release_intent_lock "killworkercodecalls" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkercodecalls-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkercodecalls
	assert_no_dead_letters killworkercodecalls
	ifa_code_call_assert "killworkercodecalls" "${bin_dir}" "${code_call_expected_edges}" \
		|| die "kill-worker-after-claim-code-calls: recovered graph does not match the five-edge exact set"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_code_call_retried}" 15 "code_call_materialization" \
		|| die "kill-worker-after-claim-code-calls: code_call_materialization did not re-execute above its fault-free retry baseline"
	capture_digest killworkercodecalls
	assert_matches_baseline killworkercodecalls
	teardown_cell killworkercodecalls
	wall_times[killworkercodecalls]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-code-calls: cell wall time: %ss\n' "${wall_times[killworkercodecalls]}"
}

# cell_failgraphwrite_code_calls fails the live CALLS MERGE exactly once, proves
# the fault decorator's durable marker names that write, and requires the full
# code_calls intent/edge set to converge without dead letters.
cell_failgraphwrite_code_calls() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-code-calls: fresh stack"
	fresh_stack failgraphwritecodecalls
	if [[ "${use_compose}" -eq 1 ]]; then
		ifa_code_call_require_fresh_intents \
			"fail-graph-write-once-then-succeed-code-calls" "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
			"${ESHU_POSTGRES_DSN}" "${compose_file}" \
			|| die "fail-graph-write-once-then-succeed-code-calls: fresh-stack precondition failed"
	fi
	drive_all_cassettes failgraphwritecodecalls
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-code-calls.json"
	ifa_fault_write_once_script "${fault_once_script}" "${code_call_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritecodecalls" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritecodecalls" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritecodecalls
	assert_no_dead_letters failgraphwritecodecalls
	ifa_code_call_assert "failgraphwritecodecalls" "${bin_dir}" "${code_call_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-code-calls: recovered graph does not match the five-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE completed_at IS NULL) AS pending FROM shared_projection_intents WHERE projection_domain = 'code_calls';" \
		"${compose_file}" | sed 's/^/  code_calls intent window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${code_call_edge_operation_match}" || marker_rc=$?
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-code-calls: once-fired marker did not name the targeted CALLS MERGE (marker status ${marker_rc})"
	capture_digest failgraphwritecodecalls
	assert_matches_baseline failgraphwritecodecalls
	teardown_cell failgraphwritecodecalls
	wall_times[failgraphwritecodecalls]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-code-calls: cell wall time: %ss\n' "${wall_times[failgraphwritecodecalls]}"
}
