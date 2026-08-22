#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154,SC2004
# SC2004: wall_times is driver-owned (declared in
# scripts/verify-ifa-fault-injection.sh, invisible to shellcheck when this
# file is linted alone); `wall_times[${cell}]=...` is an ASSOCIATIVE
# subscript -- word-expanded, not arithmetic -- and needs `${cell}` precisely
# because a bare `cell` would look up the literal key "cell", not this
# variable's value. Same false positive ifa_fault_generic_cells.sh's header
# documents for the identical pattern.
# Fault cells for the handles_route (#5995) / runs_in (#6000) /
# invokes_cloud_action (#5997) trio. Design A (arbiter-ruled, see
# .trio-notes/build-plan.md "Settled design"): blocker_kind=none for all
# three rows, so there is no cell_killworker_<family> here -- a handler-stage
# kill cell for any of these three would need wait_key=
# "code_call_materialization" (the only routed fact_work_items.domain for
# CodeCallMaterializationHandler, which is the sole handler
# buildSymbolRuntimeIntentRows writes through), byte-identical to code_calls'
# own row, rejected by TestIfaFamilyRegistryHandlerWaitKeysAreExclusive
# (go/internal/reducer/materialized_edge_family_blocker_shape_test.go:604-636)
# and, even absent that rejection, would observe/kill the SAME handler
# invocation code_calls' own cell_killworker_code_calls already proves -- not
# a distinct structural fact. The missing kill/reclaim dimension is a named,
# tracked gap (see the trio's coverage-row prose), not an oversight here.
#
# ONE shared baseline (cell_baseline_symbol_runtime) serves all three
# families: they share one cassette and one builder pass
# (reducer.ExtractSymbolRuntimeIntentRows), so driving three separate
# per-family baselines would replay the identical cassette three times for no
# additional proof. Its digest is aliased under all three families'
# digests[baseline_<family>] keys -- deliberate, so no downstream cell dies
# on an unset baseline digest (a standing failure mode; see
# .trio-notes/fault-cell-map.md section 6). Each family still gets its own
# cell_failgraphwrite_<family>, anchored to its own MERGE, because the
# once-fault decorator intercepts one Cypher statement at a time and the
# three families write three different relationship types.
#
# cell_kind=custom for all three registry rows: cell_killworker_family /
# cell_failgraphwrite_family both `die` for a cell_kind=custom family
# (ifa_fault_generic_cells.sh:404-412, 430-438), so these cells are
# hand-written and dispatched BY NAME from scripts/verify-ifa-fault-injection.sh,
# exactly like sql_relationships' and repo_dependency's custom cells.
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
