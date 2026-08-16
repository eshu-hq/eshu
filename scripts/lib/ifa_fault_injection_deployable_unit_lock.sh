#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# deployable_unit_edges fault-cell lock target (#6149), split out of
# ifa_fault_injection_deployable_unit_cells.sh to keep that file under the
# repository's 500-line cap -- same reason ifa_deployable_unit_live_diagnostics.sh
# was split out of ifa_deployable_unit_live.sh. Sourced by
# verify-ifa-fault-injection.sh alongside that cells file; the driver owns
# strict mode and globals (use_compose, compose_file, FAULT_COMPOSE_PROJECT,
# ESHU_POSTGRES_DSN, log_dir, bg_pids, ifa_det_pg, ifa_det_untrack_bg_pid).
#
# cell_killworker_deployable_unit (ifa_fault_injection_deployable_unit_cells.sh)
# is the only caller of the lock/release pair below; cell_baseline_deployable_unit
# in that same file calls ifa_deployable_unit_require_admission_decisions_written.

# ifa_deployable_unit_require_admission_decisions_written fails closed unless
# admission_decisions carries at least one row for domain =
# 'deployable_unit_correlation'. This is the permanent version of a check that
# was, for one live run, done by hand instead of wired in -- and that gap is
# exactly the defect class this file exists to remove (a helper's assumption
# about what a handler writes, unverified against the handler's actual code).
#
# Both writeDeployableUnitAdmissionDecisions (go/internal/reducer/
# deployable_unit_admission_decisions.go) and the shared writeAdmissionDecisions
# it calls (admission_decisions.go) return nil without writing anything when
# their input is empty or their writer is nil -- the same "returns early, the
# check downstream never sees anything happen" shape as
# publishIntentGraphPhase's two nil-exits. If this family's cassette ever
# stopped producing an admitted candidate, admission_decisions would carry
# zero rows for this domain, ifa_deployable_unit_start_admission_decisions_lock
# below would engage on a table that gets no traffic from this handler, and
# the kill cell would fail a third way for a third reason -- indistinguishable
# from the first two without this assertion naming it. Called from
# cell_baseline_deployable_unit, once, after that cell's maintenance pass and
# post-maintenance drain converge Handle to completion unblocked: this is a
# property of the fixture and the handler, identical across all three cells
# that drive the same cassette, so proving it once where Handle runs
# uncontested covers the fault cells too.
#
# Same fail-closed shape as ifa_deployable_unit_require_fresh_intents
# (ifa_fault_injection_deployable_unit_cells.sh), with the final comparison
# inverted (this asserts NON-zero, not zero): the query's own exit code, then
# empty output, then non-numeric output, each rejected as unknown rather than
# read as a pass or a fail in either direction -- only once all three are
# ruled out does a literal "0" fail the precondition.
ifa_deployable_unit_require_admission_decisions_written() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" postgres_dsn="$4" compose_file_arg="$5"
	local admission_count admission_count_rc
	if admission_count="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${postgres_dsn}" \
		"SELECT count(*) FROM admission_decisions WHERE domain = 'deployable_unit_correlation';" \
		"${compose_file_arg}")"; then
		admission_count_rc=0
	else
		admission_count_rc=$?
	fi
	if [[ "${admission_count_rc}" -ne 0 ]]; then
		printf '%s: admission_decisions precondition query FAILED (exit %s); treat this as unknown, not as a verdict\n' "${cell}" "${admission_count_rc}" >&2
		return "${admission_count_rc}"
	fi
	admission_count="$(printf '%s' "${admission_count}" | tr -d '[:space:]')"
	if [[ -z "${admission_count}" ]]; then
		printf '%s: admission_decisions precondition query returned empty output; treat that as unknown, not as zero\n' "${cell}" >&2
		return 1
	fi
	if [[ ! "${admission_count}" =~ ^[0-9]+$ ]]; then
		printf '%s: admission_decisions precondition query returned non-numeric output %q; treat that as unknown, not as zero\n' \
			"${cell}" "${admission_count}" >&2
		return 1
	fi
	if [[ "${admission_count}" == "0" ]]; then
		printf '%s: expected at least one admission_decisions row for domain=deployable_unit_correlation after the maintenance pass, got 0 -- the lock target below would engage on a table this handler never actually wrote for this fixture\n' "${cell}" >&2
		return 1
	fi
	printf '%s: confirmed %s admission_decisions row(s) for domain=deployable_unit_correlation -- the lock target below is a real write, not a race\n' "${cell}" "${admission_count}"
}

# ifa_deployable_unit_start_admission_decisions_lock holds admission_decisions
# in ACCESS EXCLUSIVE MODE. Getting here took two wrong lock targets, both
# instructive:
#
#   1. shared_projection_intents (the code_calls-style default): this handler
#      has no IntentWriter and never touches that table at all
#      (DomainDeployableUnitEdges is absent from sharedProjectionDomains,
#      go/internal/reducer/shared_projection_runner.go), so the lock never
#      engaged -- Handle ran to completion and marked the claimed
#      fact_work_items row 'succeeded' before kill -9 landed. Failed in CI:
#      "drain did not reach the snapshot's residual bound".
#   2. graph_projection_phase_state (Handle's LAST write, step 3 of 3 --
#      materializeDeployableUnitEdges, then writeDeployableUnitAdmissionDecisions,
#      then publishIntentGraphPhase): the lock DID engage, but that table has
#      FOURTEEN writing handlers against this gate's four reducer workers
#      (drive_workers, verify-ifa-fault-injection.sh). The workers filled with
#      other domains' items, all of them stalled on the same lock, and this
#      family's row was never claimed at all within the wait budget -- a
#      starvation failure, not a handler defect: "no claimed/running
#      fact_work_items row for domain=deployable_unit_correlation appeared
#      within 60s", with items demonstrably enqueued. A later write buys a
#      wider blocking window in TIME at the cost of a wider blast radius in
#      DOMAINS, and domain breadth is what starves a shared worker pool.
#
# admission_decisions (step 2 of 3, still after the graph write) has only
# THREE writing handlers in the whole codebase -- cloud_inventory_admission,
# package_source_correlation_handler, and this one -- and this gate drives
# neither of the other two (`rg -c 'cloud_inventory|package_source'
# scripts/lib/ifa_fault_injection_driver.sh` is 0). Within this harness this
# handler is the ONLY writer of this table, so starvation is impossible by
# construction, not by probability: nothing else competing for the workers
# ever queues a row that blocks on this lock.
#
# DO NOT "correct" this back to shared_projection_intents to match every
# sibling family's lock target. Every other family in this suite genuinely
# has an IntentWriter; this one does not, and admission_decisions is the only
# table this handler commits to that no other domain in this gate also
# writes. Record both reasons in one place so the next maintainer sees why
# this is the odd one out rather than rediscovering it by CI failure a third
# time. ifa_deployable_unit_require_admission_decisions_written above is the
# permanent proof that this table is genuinely written by this fixture, not
# a claim taken on trust.
#
# Consequence worth stating plainly, unlike code_calls' equivalent lock
# (which sits BEFORE that family's one durable write): because this lock
# lands AFTER the graph write (step 1), a kill -9 taken while blocked here
# kills a handler that has ALREADY written its CORRELATES_DEPLOYABLE_UNIT
# edge. The retry that follows re-executes Handle from scratch, so the graph
# write repeats as an idempotent MERGE and the admission-decision write that
# was blocked repeats as a fresh, successful one -- this proves recovery from
# a POST-write death, not a PRE-write death. It is still a real
# reclaim-and-re-execute proof: the kill cell (ifa_fault_injection_
# deployable_unit_cells.sh) snapshots attempt_count immediately after the
# post-kill drain reaches its residual bound and compares that SNAPSHOT
# against the fault-free baseline, not a live re-query at assertion time.
# CI caught why the live re-query is not equivalent: this family's own
# convergence loop (ifa_deployable_unit_live_converge_edges) can run another
# bootstrap-index maintenance pass after the drain, which reopens the
# recovered row and resets attempt_count back to 0 -- a live re-query taken
# after that point reads the repair's reset, not the recovery's evidence,
# and this family converges on maintenance pass 2 as its NORMAL path, so
# that race is the common case here, not a rare one.
ifa_deployable_unit_start_admission_decisions_lock() {
	local cell="$1" pid_var="$2"
	local app_name="ifa_deployable_unit_lock_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE admission_decisions IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/deployable-unit-admission-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/deployable-unit-admission-lock-${cell}.log" 2>&1 &
	fi
	local holder_pid=$!
	bg_pids+=("${holder_pid}")
	printf -v "${pid_var}" '%s' "${holder_pid}"

	local i lock_count
	for i in $(seq 1 60); do
		lock_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE a.application_name = '${app_name}' AND l.relation = 'admission_decisions'::regclass AND l.mode = 'AccessExclusiveLock' AND l.granted;" \
			"${compose_file}" | tr -d '[:space:]')"
		if [[ "${lock_count}" == "1" ]]; then
			return 0
		fi
		sleep 0.25
	done
	return 1
}

# ifa_deployable_unit_release_admission_decisions_lock terminates the named
# lock-holder backend and joins its local psql/docker process, mirroring
# ifa_code_call_release_intent_lock.
ifa_deployable_unit_release_admission_decisions_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_deployable_unit_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}
