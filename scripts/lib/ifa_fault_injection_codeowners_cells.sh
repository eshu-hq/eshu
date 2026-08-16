#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# codeowners_ownership_edges-targeted live fault cells (#5992), mirroring
# scripts/lib/ifa_fault_injection_code_call_cells.sh's shape exactly. The
# sibling this mirrors says "sourced by verify-ifa-fault-injection.sh", which
# is true there and NOT here -- that driver does not source this file (see
# NOT YET WIRED below). The cells are written against the contract that driver
# WILL own once wired: strict mode and the globals (bin_dir, tagged_bin_dir,
# log_dir, work_dir, use_compose, compose_file, FAULT_COMPOSE_PROJECT,
# ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT, wall_times, bg_pids, log, die,
# plus the
# fresh_stack / drive_all_cassettes / run_drain_gate / assert_no_dead_letters
# / capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh).
#
# PARTIALLY EXERCISED: no cell function here runs against a live stack yet
# (see NOT YET WIRED below), but this file is no longer unread.
# scripts/lib/test-ifa-fault-injection-codeowners-cases.sh sources it and
# drives ifa_codeowners_start_intent_lock and
# ifa_codeowners_release_intent_lock under stubs, and
# scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will fail
# `make pre-pr`.
#
# NOT YET WIRED (documented for the #5543 coordinator, out of #5992's scope
# this phase): a working baseline_codeowners_retried global and a
# codeowners_edge_operation_match anchor both need to be declared where
# code_call_materialization's siblings are declared today
# (cell_baseline in ifa_fault_injection_cells.sh; codeowners_expected_edges
# and codeowners_edge_operation_match near line 212/234 of
# verify-ifa-fault-injection.sh; codeowners_expected_edges also near line
# 175 of verify-ifa-determinism.sh) -- these two cell functions assume they
# exist. See the #5992 handoff report for the exact line-by-line list.
#
# codeowners_ownership_edges is a TWO-STAGE pipeline unlike a plain
# shared-projection domain: CodeownersOwnershipEdgeMaterializationHandler
# claims fact_work_items rows under the FIRST-STAGE domain
# "codeowners_ownership" (reducer.DomainCodeownersOwnership,
# go/internal/reducer/intent.go:75), decodes codeowners.ownership facts, and
# writes SharedProjectionIntentRow rows into the shared_projection_intents
# queue under the SECOND-STAGE domain "codeowners_ownership_edges"
# (reducer.DomainCodeownersOwnershipEdges) -- a separate shared-projection
# runner later claims THOSE rows and performs the actual DECLARES_CODEOWNER
# graph write. This exactly mirrors code_call_materialization's own
# first-stage/DomainCodeCalls second-stage split
# (go/internal/reducer/intent.go:42), so ifa_code_call_start_intent_lock's
# "lock shared_projection_intents so the first-stage handler cannot commit
# its own transaction" technique applies unchanged: the lock blocks the
# SECOND-stage insert, which keeps the first-stage fact_work_items row
# claimed under "codeowners_ownership" long enough to observe and kill.
#
# UNVERIFIED (#5992): this two-stage read of the pipeline is derived from
# reading codeowners_ownership_materialization.go and intent.go, not proven
# against a live stack -- the author of this file cannot run the live gate
# (DO NOT run any live gate is a hard rule for this family's implementation
# phase). Prove cell_killworker_codeowners actually observes a claimed
# "codeowners_ownership" row (not zero, not immediately acked) before
# trusting it in CI.

# ifa_codeowners_start_intent_lock holds the shared_projection_intents table
# lock so the first-stage codeowners_ownership handler cannot commit its
# intent-row insert (and therefore cannot ack its fact_work_items claim)
# until the lock is released. Mirrors ifa_code_call_start_intent_lock
# exactly; the locked table is the same for every shared-projection domain.
ifa_codeowners_start_intent_lock() {
	local cell="$1" pid_var="$2"
	local app_name="ifa_codeowners_lock_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE shared_projection_intents IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/codeowners-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/codeowners-lock-${cell}.log" 2>&1 &
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

# ifa_codeowners_release_intent_lock terminates the named lock-holder
# backend, then joins its local psql/docker process before the replacement
# reducer starts.
ifa_codeowners_release_intent_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_codeowners_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_killworker_codeowners proves a genuinely in-flight codeowners-ownership
# handler is reclaimed after process death. The table lock prevents the short
# handler from acknowledging before kill; attempt_count > the clean baseline
# proves the replacement reducer re-executed that domain, not merely another
# queued row.
cell_killworker_codeowners() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-codeowners: fresh stack"
	fresh_stack killworkercodeowners
	drive_all_cassettes killworkercodeowners
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkercodeowners" projector_pid "${bin_dir}/eshu-projector"
	ifa_codeowners_start_intent_lock "killworkercodeowners" lock_holder_pid \
		|| die "kill-worker-after-claim-codeowners: could not acquire the deterministic shared_projection_intents blocker"
	ifa_det_start_bg "${log_dir}" "reducer-killworkercodeowners-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "codeowners_ownership")" \
		|| die "kill-worker-after-claim-codeowners: no codeowners_ownership row was claimed while its durable write was blocked"
	printf 'kill-worker-after-claim-codeowners: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_codeowners_release_intent_lock "killworkercodeowners" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkercodeowners-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkercodeowners
	assert_no_dead_letters killworkercodeowners
	ifa_codeowners_assert "killworkercodeowners" "${bin_dir}" "${codeowners_expected_edges}" \
		|| die "kill-worker-after-claim-codeowners: recovered graph does not match the five-edge exact set"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_codeowners_retried}" 15 "codeowners_ownership" \
		|| die "kill-worker-after-claim-codeowners: codeowners_ownership did not re-execute above its fault-free retry baseline"
	capture_digest killworkercodeowners
	assert_matches_baseline killworkercodeowners
	teardown_cell killworkercodeowners
	wall_times[killworkercodeowners]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-codeowners: cell wall time: %ss\n' "${wall_times[killworkercodeowners]}"
}

# cell_failgraphwrite_codeowners fails the live DECLARES_CODEOWNER MERGE
# exactly once, proves the fault decorator's durable marker names that write,
# and requires the full codeowners_ownership_edges intent/edge set to
# converge without dead letters.
cell_failgraphwrite_codeowners() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-codeowners: fresh stack"
	fresh_stack failgraphwritecodeowners
	drive_all_cassettes failgraphwritecodeowners
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-codeowners.json"
	ifa_fault_write_once_script "${fault_once_script}" "${codeowners_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritecodeowners" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritecodeowners" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritecodeowners
	assert_no_dead_letters failgraphwritecodeowners
	ifa_codeowners_assert "failgraphwritecodeowners" "${bin_dir}" "${codeowners_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-codeowners: recovered graph does not match the five-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE completed_at IS NULL) AS pending FROM shared_projection_intents WHERE projection_domain = 'codeowners_ownership_edges';" \
		"${compose_file}" | sed 's/^/  codeowners_ownership_edges intent window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${codeowners_edge_operation_match}" || marker_rc=$?
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-codeowners: once-fired marker did not name the targeted DECLARES_CODEOWNER MERGE (marker status ${marker_rc})"
	capture_digest failgraphwritecodeowners
	assert_matches_baseline failgraphwritecodeowners
	teardown_cell failgraphwritecodeowners
	wall_times[failgraphwritecodeowners]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-codeowners: cell wall time: %ss\n' "${wall_times[failgraphwritecodeowners]}"
}
