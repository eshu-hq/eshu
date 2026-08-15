#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# deployable_unit_edges-targeted live fault cells (#5993). Sourced by
# verify-ifa-fault-injection.sh; the driver owns strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, GATE_DRAIN_TIMEOUT, digests,
# wall_times, bg_pids, log, die), exactly like
# ifa_fault_injection_code_call_cells.sh. Also sources
# ifa_deployable_unit_live.sh's helpers (ifa_deployable_unit_live_drain,
# ifa_deployable_unit_live_run_maintenance_pass,
# ifa_deployable_unit_live_assert).
#
# BOTH cells below need one thing no sibling family's cells do: a bootstrap-
# index maintenance pass between drive_all_cassettes and the point where a
# fault can usefully target this family's work, because
# deployable_unit_correlation is gated shut on the FIRST pass (see
# ifa_deployable_unit_live.sh's header for the full traced rationale: the
# readiness gate CrossRepoRelationshipHandler.Resolve checks is never
# published without a maintenance pass in this gate's runtime). So the shape
# here is: fresh_stack -> drive_all_cassettes -> a PRE-maintenance drain (so
# every other family reaches a terminal state cleanly) -> ONE bootstrap-index
# maintenance pass -> the fault is injected on the POST-maintenance drain,
# which is the first point deployable_unit_correlation has real work to do.
#
# DELIBERATE DEVIATION from the sibling cells' shape, flagged for review, not
# silently done: neither cell below calls capture_digest/assert_matches_baseline.
# cell_baseline (ifa_fault_injection_cells.sh) never runs a maintenance pass,
# so digests[baseline] reflects a graph with ZERO deployable_unit_edges
# materialization by construction -- any cell here that DOES run the
# maintenance pass to get a real edge to fault-test would then differ from
# baseline by exactly that one new edge, on every run, fault or not. Comparing
# against baseline would therefore never pass and would not be testing
# anything about THIS family's fault recovery. ifa_deployable_unit_live_assert's
# exact-edge-set comparison is the correct, family-specific non-vacuity proof
# here, exactly as ifa_code_call_assert/ifa_det_assert_sql_baseline are for
# their families -- baseline-digest comparison is an ADDITIONAL check those
# families' cells can afford because their edges already exist in baseline,
# which is not true here. If a shared baseline that also runs one maintenance
# pass is added later, this decision should be revisited.

# ifa_deployable_unit_require_fresh_intents fails closed unless a fresh
# compose stack has a numeric zero count for the deployable_unit_edges
# shared-projection domain, mirroring
# ifa_code_call_require_fresh_intents exactly.
ifa_deployable_unit_require_fresh_intents() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" postgres_dsn="$4" compose_file_arg="$5"
	local pre_intents pre_intents_rc
	if pre_intents="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${postgres_dsn}" \
		"SELECT count(*) FROM shared_projection_intents WHERE projection_domain = 'deployable_unit_edges';" \
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
		printf '%s: %s deployable_unit_edges intent row(s) survived fresh_stack\n' "${cell}" "${pre_intents}" >&2
		return 1
	fi
}

# ifa_deployable_unit_start_intent_lock holds shared_projection_intents in
# ACCESS EXCLUSIVE MODE, exactly like ifa_code_call_start_intent_lock (same
# table, different app_name so a same-process sequential run never collides
# with a concurrent code_calls cell's lock holder). This blocks
# DeployableUnitCorrelationHandler.materializeDeployableUnitEdges's
# EdgeWriter.WriteEdges call -- called synchronously inside Handle(), per
# go/internal/reducer/deployable_unit_correlation.go -- so the claimed
# fact_work_items row cannot reach 'succeeded' until the lock releases,
# making the claimed-row observation below deterministic.
ifa_deployable_unit_start_intent_lock() {
	local cell="$1" pid_var="$2"
	local app_name="ifa_deployable_unit_lock_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE shared_projection_intents IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/deployable-unit-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/deployable-unit-lock-${cell}.log" 2>&1 &
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

# ifa_deployable_unit_release_intent_lock terminates the named lock-holder
# backend and joins its local psql/docker process, mirroring
# ifa_code_call_release_intent_lock.
ifa_deployable_unit_release_intent_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_deployable_unit_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_killworker_deployable_unit proves a genuinely in-flight
# deployable_unit_correlation handler is reclaimed after process death,
# AFTER the maintenance pass has opened the readiness gate. The
# shared_projection_intents table lock prevents the handler from
# acknowledging before kill; attempt_count > 0 (this domain has no natural
# retries in a clean maintenance-pass run, so any positive count is the
# fault's fingerprint) proves the replacement reducer re-executed
# deployable_unit_correlation, not merely another queued row.
cell_killworker_deployable_unit() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-deployable-unit: fresh stack"
	fresh_stack killworkerdeployableunit
	if [[ "${use_compose}" -eq 1 ]]; then
		ifa_deployable_unit_require_fresh_intents \
			"kill-worker-after-claim-deployable-unit" "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
			"${ESHU_POSTGRES_DSN}" "${compose_file}" \
			|| die "kill-worker-after-claim-deployable-unit: fresh-stack precondition failed"
	fi
	drive_all_cassettes killworkerdeployableunit

	log "kill-worker-after-claim-deployable-unit: pre-maintenance drain (deployable_unit_correlation is gated shut here by design)"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-killworkerdeployableunit-pre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdeployableunit-pre" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerdeployableunitpre
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
	ifa_deployable_unit_live_assert_empty_before_maintenance \
		"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		|| die "kill-worker-after-claim-deployable-unit: expected zero deployable_unit_edges rows before the maintenance pass"

	ifa_deployable_unit_live_run_maintenance_pass "killworkerdeployableunit" "${bin_dir}" "${log_dir}" \
		|| die "kill-worker-after-claim-deployable-unit: bootstrap-index maintenance pass failed"

	local lock_holder_pid claimed_before reducer_pid_before reducer_pid_after
	ifa_deployable_unit_start_intent_lock "killworkerdeployableunit" lock_holder_pid \
		|| die "kill-worker-after-claim-deployable-unit: could not acquire the deterministic shared_projection_intents blocker"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdeployableunit-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "deployable_unit_correlation")" \
		|| die "kill-worker-after-claim-deployable-unit: no deployable_unit_correlation row was claimed while its durable write was blocked"
	printf 'kill-worker-after-claim-deployable-unit: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_deployable_unit_release_intent_lock "killworkerdeployableunit" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdeployableunit-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerdeployableunit
	assert_no_dead_letters killworkerdeployableunit
	ifa_deployable_unit_live_assert "${bin_dir}" "${deployable_unit_expected_edges}" \
		|| die "kill-worker-after-claim-deployable-unit: recovered graph does not match the one-edge exact set"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		0 15 "deployable_unit_correlation" \
		|| die "kill-worker-after-claim-deployable-unit: deployable_unit_correlation did not re-execute (attempt_count>1 never appeared)"
	teardown_cell killworkerdeployableunit
	wall_times[killworkerdeployableunit]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-deployable-unit: cell wall time: %ss\n' "${wall_times[killworkerdeployableunit]}"
}

# cell_failgraphwrite_deployable_unit fails the live CORRELATES_DEPLOYABLE_UNIT
# MERGE exactly once, proves the fault decorator's durable marker names that
# write, and requires the one-edge exact set to converge without dead
# letters. Same pre-maintenance-drain / maintenance-pass / post-maintenance
# fault shape as cell_killworker_deployable_unit above.
cell_failgraphwrite_deployable_unit() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-deployable-unit: fresh stack"
	fresh_stack failgraphwritedeployableunit
	if [[ "${use_compose}" -eq 1 ]]; then
		ifa_deployable_unit_require_fresh_intents \
			"fail-graph-write-once-then-succeed-deployable-unit" "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
			"${ESHU_POSTGRES_DSN}" "${compose_file}" \
			|| die "fail-graph-write-once-then-succeed-deployable-unit: fresh-stack precondition failed"
	fi
	drive_all_cassettes failgraphwritedeployableunit

	log "fail-graph-write-once-then-succeed-deployable-unit: pre-maintenance drain (deployable_unit_correlation is gated shut here by design)"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritedeployableunit-pre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritedeployableunit-pre" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritedeployableunitpre
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
	ifa_deployable_unit_live_assert_empty_before_maintenance \
		"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		|| die "fail-graph-write-once-then-succeed-deployable-unit: expected zero deployable_unit_edges rows before the maintenance pass"

	ifa_deployable_unit_live_run_maintenance_pass "failgraphwritedeployableunit" "${bin_dir}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-deployable-unit: bootstrap-index maintenance pass failed"

	local fault_once_script marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-deployable-unit.json"
	ifa_fault_write_once_script "${fault_once_script}" "${deployable_unit_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritedeployableunit" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritedeployableunit" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritedeployableunit
	assert_no_dead_letters failgraphwritedeployableunit
	ifa_deployable_unit_live_assert "${bin_dir}" "${deployable_unit_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-deployable-unit: recovered graph does not match the one-edge exact set"
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${deployable_unit_edge_operation_match}" || marker_rc=$?
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-deployable-unit: once-fired marker did not name the targeted CORRELATES_DEPLOYABLE_UNIT MERGE (marker status ${marker_rc})"
	teardown_cell failgraphwritedeployableunit
	wall_times[failgraphwritedeployableunit]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-deployable-unit: cell wall time: %ss\n' "${wall_times[failgraphwritedeployableunit]}"
}
