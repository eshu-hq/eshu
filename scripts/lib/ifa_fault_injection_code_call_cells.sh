#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# code_calls-targeted live fault cells (#5991). This function library is sourced
# by verify-ifa-fault-injection.sh; the driver owns strict mode and globals.

# The live driver sources the common library first. Focused helper tests source
# this file alone, so load the shared implementation only in that case.
if ! declare -F ifa_fault_require_fresh_domain_intents >/dev/null; then
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ifa_fault_injection_common.sh"
fi

# Compatibility wrappers keep the focused helper regressions stable while the
# live cells use the shared helpers directly. New domains must call the shared
# functions rather than copy this SQL/lock lifecycle again.
ifa_code_call_require_fresh_intents() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" postgres_dsn="$4" compose_file_arg="$5"
	ifa_fault_require_fresh_domain_intents \
		"${cell}" code_calls "${compose_project}" "${use_compose_arg}" "${postgres_dsn}" "${compose_file_arg}"
}

# ifa_code_call_start_intent_lock holds the first durable write used by
# code_call_materialization. This makes the claimed-row observation and kill
# deterministic: the handler cannot acknowledge between the observation and
# kill, and the post-restart attempt_count proof identifies the same domain.
ifa_code_call_start_intent_lock() {
	local cell="$1" pid_var="$2"
	ifa_fault_start_shared_intent_lock "${cell}" code_call "${pid_var}"
}

# ifa_code_call_release_intent_lock terminates the named lock-holder backend,
# then joins its local psql/docker process before the replacement reducer starts.
ifa_code_call_release_intent_lock() {
	local cell="$1" holder_pid="$2"
	ifa_fault_release_shared_intent_lock "${cell}" code_call "${holder_pid}"
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
	ifa_fault_start_shared_intent_lock "killworkercodecalls" code_call lock_holder_pid \
		|| die "kill-worker-after-claim-code-calls: could not acquire the deterministic shared_projection_intents blocker"
	ifa_det_start_bg "${log_dir}" "reducer-killworkercodecalls-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "code_call_materialization")" \
		|| die "kill-worker-after-claim-code-calls: no code_call_materialization row was claimed while its durable write was blocked"
	printf 'kill-worker-after-claim-code-calls: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid_before}" KILL \
		|| die "kill-worker-after-claim-code-calls: could not stop, join, and untrack the killed reducer"
	ifa_fault_release_shared_intent_lock "killworkercodecalls" code_call "${lock_holder_pid}"
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
		ifa_fault_require_fresh_domain_intents \
			"fail-graph-write-once-then-succeed-code-calls" code_calls "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
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
