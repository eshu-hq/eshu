#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# deployable_unit_edges-targeted live fault cells (#5993). Sourced by
# verify-ifa-fault-injection.sh; the driver owns strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, GATE_DRAIN_TIMEOUT, digests,
# wall_times, bg_pids, sql_expected_edges, code_call_expected_edges, log,
# die), exactly like ifa_fault_injection_code_call_cells.sh. Also sources
# ifa_deployable_unit_live.sh's helpers
# (ifa_deployable_unit_live_assert_empty_before_maintenance,
# ifa_deployable_unit_live_run_maintenance_pass,
# ifa_deployable_unit_live_assert_readiness_opened,
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
# RULING (superseding an earlier draft that omitted baseline-digest comparison
# entirely): the shared digests[baseline] (cell_baseline,
# ifa_fault_injection_cells.sh) never runs a maintenance pass, so it has ZERO
# deployable_unit_edges materialization by construction -- comparing against
# it would never pass for a cell that DOES run the pass, fault or not. But
# ifa_deployable_unit_live_assert's exact-edge-set check and a whole-graph
# digest comparison prove DIFFERENT things: the exact-set check covers only
# this family's own relationship types, while the digest catches CROSS-family
# collateral damage from fault recovery (assert_matches_baseline's own die
# message calls a divergence "a real recovery/concurrency defect"). This
# family's fault path uniquely runs a bootstrap-index maintenance pass, which
# reopens every crossScopeCorrelationReopenDomains domain and republishes
# readiness -- collateral from a faulted-then-recovered maintenance leg is a
# plausible defect class nothing else in the suite would see. Dropping the
# digest check would have silently lost exactly that coverage.
#
# The fix is a family-scoped baseline, not an omission: cell_baseline_deployable_unit
# below runs the SAME fresh_stack -> drive -> pre-maintenance drain -> ONE
# maintenance pass -> post-maintenance drain shape as the two fault cells,
# fault-free, and captures digests[baseline_deployable_unit] +
# baseline_deployable_unit_retried. Both fault cells then call
# assert_matches_baseline with that key (assert_matches_baseline,
# ifa_fault_injection_driver.sh, now takes an optional baseline_key parameter
# defaulting to "baseline" so every other family's call sites are
# byte-identical). This restores the true invariant: every fault cell compares
# against a fault-free baseline with the SAME terminal state -- this family
# does not prove recovery differently from its siblings, it proves it
# identically, against its own correctly-shaped baseline.

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

# cell_baseline_deployable_unit is this family's fault-free reference: fresh
# stack -> drive -> pre-maintenance drain (asserts zero edges, proving the
# readiness gate is genuinely shut, not racy) -> ONE bootstrap-index
# maintenance pass -> post-maintenance drain -> exact-set assert -> capture
# digests[baseline_deployable_unit] and baseline_deployable_unit_retried.
# Both fault cells below compare against this, not the shared cell_baseline,
# for the reason in this file's header.
cell_baseline_deployable_unit() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-deployable-unit: fresh stack"
	fresh_stack baseline_deployable_unit
	if [[ "${use_compose}" -eq 1 ]]; then
		ifa_deployable_unit_require_fresh_intents \
			"baseline-deployable-unit" "${FAULT_COMPOSE_PROJECT}" "${use_compose}" \
			"${ESHU_POSTGRES_DSN}" "${compose_file}" \
			|| die "baseline-deployable-unit: fresh-stack precondition failed"
	fi
	drive_all_cassettes baseline_deployable_unit

	log "baseline-deployable-unit: pre-maintenance drain (deployable_unit_correlation is gated shut here by design)"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_deployable_unitpre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_deployable_unitpre" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_deployable_unitpre
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
	ifa_deployable_unit_live_assert_empty_before_maintenance \
		"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		|| die "baseline-deployable-unit: expected zero deployable_unit_edges rows before the maintenance pass"
	# The other families this cell drives (sql_relationships, code_calls) are
	# NOT gated: they already converged in the drain above. Asserting them
	# here, then again after the maintenance pass, gives an explicit
	# attribution point for the maintenance pass's one unproven risk (a
	# reconcile path misreading the zero-repo filesystem collection as a
	# removal and retracting facts) -- a divergence here names the exact
	# family the pass corrupted, rather than only a whole-graph digest
	# mismatch against this cell's own baseline.
	"${bin_dir}/eshu-ifa" assert-edges -domain sql_relationships -expected "${sql_expected_edges}" \
		|| die "baseline-deployable-unit: sql_relationships does not match its exact set BEFORE the maintenance pass"
	"${bin_dir}/eshu-ifa" assert-edges -domain code_calls -expected "${code_call_expected_edges}" \
		|| die "baseline-deployable-unit: code_calls does not match its exact set BEFORE the maintenance pass"

	ifa_deployable_unit_live_run_maintenance_pass "baseline_deployable_unit" "${bin_dir}" "${log_dir}" \
		|| die "baseline-deployable-unit: bootstrap-index maintenance pass failed"

	ifa_det_start_bg "${log_dir}" "projector-baseline_deployable_unit" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_deployable_unit" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_deployable_unit
	assert_no_dead_letters baseline_deployable_unit
	ifa_deployable_unit_live_assert_readiness_opened "${log_dir}" "reducer-baseline_deployable_unit" \
		|| die "baseline-deployable-unit: post-maintenance reducer log does not prove the readiness gate opened"
	"${bin_dir}/eshu-ifa" assert-edges -domain sql_relationships -expected "${sql_expected_edges}" \
		|| die "baseline-deployable-unit: sql_relationships no longer matches its exact set AFTER the maintenance pass -- the maintenance pass corrupted an unrelated family"
	"${bin_dir}/eshu-ifa" assert-edges -domain code_calls -expected "${code_call_expected_edges}" \
		|| die "baseline-deployable-unit: code_calls no longer matches its exact set AFTER the maintenance pass -- the maintenance pass corrupted an unrelated family"
	ifa_deployable_unit_live_assert "${bin_dir}" "${deployable_unit_expected_edges}" \
		|| die "baseline-deployable-unit: fault-free graph does not match the one-edge exact set (fault-free baseline must materialize this family's edge before the recovery cells compare against it)"
	capture_digest baseline_deployable_unit

	# Snapshot the fault-free retry count so the fault cells can prove their
	# injected fault ADDED a retry this identical drive did not produce on its
	# own, mirroring cell_baseline's baseline_retried/baseline_code_call_retried
	# capture exactly -- deployable_unit_correlation only does real work AFTER
	# the maintenance pass, so this is the reference flow that establishes what
	# "zero natural retries" means for this domain, not an assumed literal.
	baseline_deployable_unit_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "deployable_unit_correlation")"
	baseline_deployable_unit_retried="${baseline_deployable_unit_retried:-0}"
	printf 'baseline-deployable-unit: fault-free deployable_unit_correlation retried rows (attempt_count>1): %s\n' "${baseline_deployable_unit_retried}"

	teardown_cell baseline_deployable_unit
	wall_times[baseline_deployable_unit]=$(( $(date +%s) - cell_start ))
	printf 'baseline-deployable-unit: cell wall time: %ss\n' "${wall_times[baseline_deployable_unit]}"
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
	ifa_det_start_bg "${log_dir}" "projector-killworkerdeployableunitpre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdeployableunitpre" reducer_pid "${bin_dir}/eshu-reducer"
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
		"${baseline_deployable_unit_retried}" 15 "deployable_unit_correlation" \
		|| die "kill-worker-after-claim-deployable-unit: deployable_unit_correlation did not re-execute above its fault-free retry baseline"
	capture_digest killworkerdeployableunit
	assert_matches_baseline killworkerdeployableunit baseline_deployable_unit
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
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritedeployableunitpre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritedeployableunitpre" reducer_pid "${bin_dir}/eshu-reducer"
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
	capture_digest failgraphwritedeployableunit
	assert_matches_baseline failgraphwritedeployableunit baseline_deployable_unit
	teardown_cell failgraphwritedeployableunit
	wall_times[failgraphwritedeployableunit]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-deployable-unit: cell wall time: %ss\n' "${wall_times[failgraphwritedeployableunit]}"
}
