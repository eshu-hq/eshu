#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# rationale_edges-targeted live recovery cells (#5998). This library is
# sourced by verify-ifa-fault-injection.sh; the driver owns strict mode,
# lifecycle, cassette paths, process tracking, and failure reporting.

# cell_killworker_rationale proves the exact rationale_materialization work
# item is reclaimed after the reducer process dies. An exclusive lock on the
# shared-intent table keeps the short handler in flight until that PID is
# killed. The replacement must add a durable retry above the clean baseline.
cell_killworker_rationale() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-rationale: fresh stack"
	fresh_stack killworkerrationale
	drive_all_cassettes killworkerrationale
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerrationale" projector_pid "${bin_dir}/eshu-projector"
	ifa_fault_start_shared_intent_lock "killworkerrationale" rationale lock_holder_pid \
		|| die "kill-worker-after-claim-rationale: could not acquire the deterministic shared_projection_intents blocker"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerrationale-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
		"${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "rationale_materialization")" \
		|| die "kill-worker-after-claim-rationale: no rationale_materialization row was claimed while its durable write was blocked"
	printf 'kill-worker-after-claim-rationale: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid_before}" KILL \
		|| die "kill-worker-after-claim-rationale: could not stop, join, and untrack the killed reducer"
	ifa_fault_release_shared_intent_lock "killworkerrationale" rationale "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerrationale-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerrationale
	assert_no_dead_letters killworkerrationale
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
		"${ESHU_POSTGRES_DSN}" "${compose_file}" "${baseline_rationale_retried}" 15 \
		"rationale_materialization" \
		|| die "kill-worker-after-claim-rationale: rationale_materialization did not re-execute above its fault-free retry baseline"
	capture_digest killworkerrationale
	assert_matches_baseline killworkerrationale
	teardown_cell killworkerrationale
	wall_times[killworkerrationale]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-rationale: cell wall time: %ss\n' "${wall_times[killworkerrationale]}"
}

# cell_failgraphwrite_rationale fails the production EXPLAINS MERGE exactly
# once on the queue-retry lane, then requires the marker to name that precise
# operation and the normal drain assertions to recover the full record set.
cell_failgraphwrite_rationale() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-rationale: fresh stack"
	fresh_stack failgraphwriterationale
	if [[ "${use_compose}" -eq 1 ]]; then
		ifa_fault_require_fresh_domain_intents \
			"fail-graph-write-once-then-succeed-rationale" rationale_edges \
			"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
			|| die "fail-graph-write-once-then-succeed-rationale: fresh-stack precondition failed"
	fi
	drive_all_cassettes failgraphwriterationale
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-rationale.json"
	ifa_fault_write_once_script "${fault_once_script}" "${rationale_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwriterationale" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwriterationale" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwriterationale
	assert_no_dead_letters failgraphwriterationale
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${rationale_edge_operation_match}" || marker_rc=$?
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-rationale: once-fired marker did not name the targeted EXPLAINS MERGE (marker status ${marker_rc})"
	capture_digest failgraphwriterationale
	assert_matches_baseline failgraphwriterationale
	teardown_cell failgraphwriterationale
	wall_times[failgraphwriterationale]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-rationale: cell wall time: %ss\n' "${wall_times[failgraphwriterationale]}"
}
