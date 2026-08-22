#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154,SC2004
# SC2004: wall_times is driver-owned (declared in
# scripts/verify-ifa-fault-injection.sh, invisible to shellcheck when this
# file is linted alone); `wall_times[${cell}]=...` is an ASSOCIATIVE
# subscript -- word-expanded, not arithmetic -- and needs `${cell}` precisely
# because a bare `cell` would look up the literal key "cell", not this
# variable's value. Same false positive ifa_fault_generic_cells.sh's header
# documents for the identical pattern.
# Fault cells for the handles_route (#5995), runs_in (#6000), and
# invokes_cloud_action (#5997) trio. Their runner_lease_hold cells block the
# production ClaimPartitionLease advisory key for one projection domain. The
# holder starts before the reducer: a negative control proves that no runner
# is waiting, then the real reducer authors the durable intent and parks on
# that exact key. The cell kills and joins that reducer while the waiter is
# present, releases the holder, and starts a replacement only after the
# release helper drains the orphaned PostgreSQL requests.
#
# This blocker stalls all four workers in the process-wide
# SharedProjectionRunner cycle while the key is held. The collateral is not
# family-local: the other shared projection domains can stop making progress
# for the hold interval. The cells keep the production worker count and prove
# the target family by its own exact edge set after recovery.
#
# ONE shared baseline (cell_baseline_symbol_runtime) serves all three
# families: they share one cassette and one builder pass
# (reducer.ExtractSymbolRuntimeIntentRows), so driving three separate
# per-family baselines would replay the identical cassette three times for no
# additional proof. Its digest is aliased under all three families'
# digests[baseline_<family>] keys -- deliberate, so no downstream cell dies
# on an unset baseline digest: a FAULT_SHARED_DRIVE=0 family's recovery cell
# comparing against the wrong (unset or shared) baseline key would die
# reporting a graph divergence that is really a fixture difference, the
# exact standing failure mode ifa_fault_generic_baseline_cell.sh's own
# header documents for the identical shape. Each family still gets its own
# cell_failgraphwrite_<family>, anchored to its own MERGE, because the
# once-fault decorator intercepts one Cypher statement at a time and the
# three families write three different relationship types.
#
# cell_kind=custom for all three registry rows, so the gate dispatches these
# hand-written functions by name.
#
# This file is a plain function library, not a script (no `set -euo
# pipefail`; see ifa_fault_injection_driver.sh's identical note). Every
# function here reads driver-owned globals (bin_dir, tagged_bin_dir, log_dir,
# work_dir, use_compose, compose_file, FAULT_COMPOSE_PROJECT,
# ESHU_POSTGRES_DSN, wall_times, digests, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh) rather than taking them as arguments, and
# calls ifa_symbol_runtime_drive / ifa_handles_route_assert /
# ifa_runs_in_assert / ifa_invokes_cloud_action_assert from
# scripts/lib/ifa_symbol_runtime_live.sh.

# _ifa_symbol_runtime_fault_require_fresh_domains runs the fresh-stack
# non-vacuity precondition for ALL THREE domains, not only the calling cell's
# own family: the trio shares one cassette and one builder pass, so a
# surviving intent row in ANY of the three domains means this stack is
# replaying a prior cell's completed work (deterministic intent ids,
# completed rows never reopened) -- checking only the cell's nominal family
# would miss staleness that only shows up in a sibling domain.
_ifa_symbol_runtime_fault_require_fresh_domains() {
	local cell="$1" domain
	if [[ "${use_compose}" -ne 1 ]]; then
		printf '%s: fresh-stack precondition SKIPPED (--no-compose owns the stack; surviving intents are not a leak)\n' "${cell}"
		return 0
	fi
	for domain in handles_route runs_in invokes_cloud_action; do
		ifa_fault_require_fresh_domain_intents "${cell}" "${domain}" \
			"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
			|| die "${cell}: fresh-stack precondition failed for domain=${domain}"
	done
}

# _ifa_symbol_runtime_wait_for_code_calls_control proves the independent
# code_calls projection lane has completed while runner_lease_hold is armed.
# Each poll is a fresh SQL statement so PostgreSQL cannot reuse a stale
# activity snapshot across the state change.
_ifa_symbol_runtime_wait_for_code_calls_control() {
	local budget="$1" i snapshot total pending
	[[ "${budget}" =~ ^[1-9][0-9]*$ ]] || return 1
	for i in $(seq 1 "$((budget * 4))"); do
		snapshot="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) || '|' || count(*) FILTER (WHERE completed_at IS NULL) FROM shared_projection_intents WHERE projection_domain = 'code_calls';" \
			"${compose_file}" | tail -n 1 | tr -d '[:space:]')" || return 1
		IFS='|' read -r total pending <<<"${snapshot}"
		if [[ "${total}" =~ ^[1-9][0-9]*$ && "${pending}" == "0" ]]; then
			printf '%s' "${snapshot}"
			return 0
		fi
		sleep 0.25
	done
	printf 'symbol-runtime control: code_calls did not reach total>0,pending=0 within %ss (last=%s)\n' \
		"${budget}" "${snapshot:-unknown}" >&2
	return 1
}

# cell_baseline_symbol_runtime: the trio's shared fault-free baseline. Drives
# the shared cassette exactly once, exact-asserts all three expected-edge
# files (so a dropped edge in any one family fails BY NAME, not merely as a
# digest mismatch), and aliases its digest under all three
# digests[baseline_<family>] keys.
cell_baseline_symbol_runtime() {
	local cell="baseline_symbol_runtime" cell_start projector_pid reducer_pid
	cell_start=$(date +%s)
	log "cell generic-baseline (symbol-runtime trio: handles_route/runs_in/invokes_cloud_action): fresh stack"
	fresh_stack "${cell}"
	_ifa_symbol_runtime_fault_require_fresh_domains "${cell}"
	drive_all_cassettes "${cell}"
	ifa_symbol_runtime_drive "${cell}" "${bin_dir}" "${symbol_runtime_cassette}" 1 "${log_dir}" \
		|| die "${cell}: symbol-runtime cassette drive failed"
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	assert_no_dead_letters "${cell}"
	ifa_handles_route_assert "${cell}" "${bin_dir}" "${handles_route_expected_edges}" \
		|| die "${cell}: fault-free handles_route graph does not match the expected edge set -- the baseline itself is wrong"
	ifa_runs_in_assert "${cell}" "${bin_dir}" "${runs_in_expected_edges}" \
		|| die "${cell}: fault-free runs_in graph does not match the expected edge set -- the baseline itself is wrong"
	ifa_invokes_cloud_action_assert "${cell}" "${bin_dir}" "${invokes_cloud_action_expected_edges}" \
		|| die "${cell}: fault-free invokes_cloud_action graph does not match the expected edge set -- the baseline itself is wrong"
	capture_digest "${cell}"
	# Alias one digest under all three families' baseline keys. Deliberate:
	# one shared baseline serves three families, and this alias is what lets
	# each family's own cell_failgraphwrite_<family> compare against
	# digests[baseline_<family>] without a per-family baseline cell.
	digests["baseline_handles_route"]="${digests[${cell}]}"
	digests["baseline_runs_in"]="${digests[${cell}]}"
	digests["baseline_invokes_cloud_action"]="${digests[${cell}]}"
	cp "${work_dir}/graph-${cell}.dump" "${work_dir}/graph-baseline_handles_route.dump" 2>/dev/null || true
	cp "${work_dir}/graph-${cell}.dump" "${work_dir}/graph-baseline_runs_in.dump" 2>/dev/null || true
	cp "${work_dir}/graph-${cell}.dump" "${work_dir}/graph-baseline_invokes_cloud_action.dump" 2>/dev/null || true
	teardown_cell "${cell}"
	wall_times[${cell}]=$(( $(date +%s) - cell_start ))
	printf '%s: cell wall time: %ss\n' "${cell}" "${wall_times[${cell}]}"
}

# _ifa_symbol_runtime_cell_killworker is the shared runner-stage
# kill/reclaim body. The family, expected-edge variable, and assertion
# function are the only family-specific inputs.
_ifa_symbol_runtime_cell_killworker() {
	local family="$1" expected_var="$2" assert_fn="$3"
	local cell="killworker_${family}" cell_start projector_pid reducer_before reducer_after holder_pid waiter_count control_snapshot
	cell_start=$(date +%s)
	log "cell kill-worker-after-runner-lease-wait (${family}): fresh stack"
	fresh_stack "${cell}"
	_ifa_symbol_runtime_fault_require_fresh_domains "${cell}"
	drive_all_cassettes "${cell}"
	ifa_symbol_runtime_drive "${cell}" "${bin_dir}" "${symbol_runtime_cassette}" 1 "${log_dir}" \
		|| die "${cell}: symbol-runtime cassette drive failed"
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_fault_start_runner_lease_hold "${cell}" "${family}" holder_pid \
		|| die "${cell}: could not acquire the production runner lease key"
	ifa_fault_require_no_projection_intent_waiter \
		"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${family}" "${cell}" \
		|| die "${cell}: pre-reducer negative control found an exact runner waiter"
	printf '%s: negative control: holder present with zero exact runner waiters before reducer start\n' "${cell}"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-before" reducer_before "${bin_dir}/eshu-reducer"
	waiter_count="$(ifa_fault_wait_for_claimed_projection_intent \
		"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "${family}" "${cell}")" \
		|| die "${cell}: no pending ${family} intent with a runner waiting on its exact lease key"
	printf '%s: non-vacuous: %s pending-intent/exact-waiter match(es) observed\n' "${cell}" "${waiter_count}"
	control_snapshot="$(_ifa_symbol_runtime_wait_for_code_calls_control "${CLAIMED_ROW_WAIT_TIMEOUT}")" \
		|| die "${cell}: independent code_calls projection did not complete while the runner lease hold was armed"
	printf '%s: independent control: code_calls total|pending=%s\n' "${cell}" "${control_snapshot}"
	ifa_det_stop_join_untrack_bg_pid "${reducer_before}" KILL \
		|| die "${cell}: could not kill and join the reducer parked on the lease key"
	ifa_fault_release_runner_lease_hold "${cell}" "${family}" "${holder_pid}" \
		|| die "${cell}: holder release or orphaned waiter drain failed"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-after" reducer_after "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	assert_no_dead_letters "${cell}"
	"${assert_fn}" "${cell}" "${bin_dir}" "${!expected_var}" \
		|| die "${cell}: reclaimed ${family} graph does not match the expected edge set"
	capture_digest "${cell}"
	assert_matches_baseline "${cell}" "baseline_${family}"
	teardown_cell "${cell}"
	wall_times[${cell}]=$(( $(date +%s) - cell_start ))
	printf '%s: cell wall time: %ss\n' "${cell}" "${wall_times[${cell}]}"
}

cell_killworker_handles_route() {
	_ifa_symbol_runtime_cell_killworker handles_route \
		handles_route_expected_edges ifa_handles_route_assert
}

cell_killworker_runs_in() {
	_ifa_symbol_runtime_cell_killworker runs_in \
		runs_in_expected_edges ifa_runs_in_assert
}

cell_killworker_invokes_cloud_action() {
	_ifa_symbol_runtime_cell_killworker invokes_cloud_action \
		invokes_cloud_action_expected_edges ifa_invokes_cloud_action_assert
}

# _ifa_symbol_runtime_cell_failgraphwrite is the shared body for all three
# family fail-graph-write-once-then-succeed cells: family (registry key),
# anchor (this family's own MERGE), and expected_edges/assert_fn (this
# family's own exact-set assertion) differ; everything else is identical.
# Not routed through ifa_fault_generic_cells.sh's
# _ifa_generic_cell_failgraphwrite: that dispatcher requires cell_kind=generic
# (die()s otherwise), and these three rows are cell_kind=custom.
_ifa_symbol_runtime_cell_failgraphwrite() {
	local family="$1" anchor="$2" expected_var="$3" assert_fn="$4"
	local cell="failgraphwrite_${family}" cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed (${family}): fresh stack"
	fresh_stack "${cell}"
	_ifa_symbol_runtime_fault_require_fresh_domains "${cell}"
	drive_all_cassettes "${cell}"
	ifa_symbol_runtime_drive "${cell}" "${bin_dir}" "${symbol_runtime_cassette}" 1 "${log_dir}" \
		|| die "${cell}: symbol-runtime cassette drive failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-${family}.json"
	ifa_fault_write_once_script "${fault_once_script}" "${anchor}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	assert_no_dead_letters "${cell}"
	"${assert_fn}" "${cell}" "${bin_dir}" "${!expected_var}" \
		|| die "${cell}: recovered ${family} graph does not match the expected edge set"
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${anchor}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		die "${cell}: the fault FIRED but on a different write than ${anchor} (marker contents above); the injection works, the anchor is pointed at the wrong statement"
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "${cell}: once-fired marker did not name the targeted ${family} MERGE (marker status ${marker_rc})"
	capture_digest "${cell}"
	assert_matches_baseline "${cell}" "baseline_${family}"
	teardown_cell "${cell}"
	wall_times[${cell}]=$(( $(date +%s) - cell_start ))
	printf '%s: cell wall time: %ss\n' "${cell}" "${wall_times[${cell}]}"
}

# cell_failgraphwrite_handles_route: anchored to
# go/internal/storage/cypher/canonical_handles_route_edges.go:19's
# HANDLES_ROUTE MERGE.
cell_failgraphwrite_handles_route() {
	_ifa_symbol_runtime_cell_failgraphwrite handles_route \
		"MERGE (f)-[rel:HANDLES_ROUTE]->(e)" \
		handles_route_expected_edges ifa_handles_route_assert
}

# cell_failgraphwrite_runs_in: anchored to
# go/internal/storage/cypher/canonical_runs_in_edges.go:27's RUNS_IN MERGE.
cell_failgraphwrite_runs_in() {
	_ifa_symbol_runtime_cell_failgraphwrite runs_in \
		"MERGE (func)-[rel:RUNS_IN]->(workload)" \
		runs_in_expected_edges ifa_runs_in_assert
}

# cell_failgraphwrite_invokes_cloud_action: anchored to
# go/internal/storage/cypher/canonical_invokes_cloud_action_edges.go:22's
# INVOKES_CLOUD_ACTION MERGE.
cell_failgraphwrite_invokes_cloud_action() {
	_ifa_symbol_runtime_cell_failgraphwrite invokes_cloud_action \
		"MERGE (func)-[rel:INVOKES_CLOUD_ACTION]->(action)" \
		invokes_cloud_action_expected_edges ifa_invokes_cloud_action_assert
}
