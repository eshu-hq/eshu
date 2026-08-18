#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154,SC2004
# SC2004 is a false positive here: wall_times is driver-owned (`declare -A
# wall_times` in the sourcing script, invisible to shellcheck when this file
# is linted alone), so `wall_times[$cell]=...` is an ASSOCIATIVE subscript --
# word-expanded, not arithmetic -- and needs $cell precisely because a bare
# `cell` would look up the literal key "cell", not this variable's value.
# Every sibling *_cells.sh file hits the identical cross-file blindness for
# SC2034/SC2154; this file adds the one more shellcheck already can't resolve
# without seeing the caller.
#
# Generic, registry-driven fault-injection cells (companion to
# scripts/lib/ifa_family_registry.sh). Before this file, adding a family's
# kill-worker and fail-graph-write proof meant hand-copying one of
# ifa_fault_injection_{sql,code_call,rationale}_cells.sh's cell functions and
# retargeting its lock/wait/anchor by hand.
#
# THIS FILE is the dispatcher only: cell_killworker_family and
# cell_failgraphwrite_family, the shared kill/reclaim/drain/assert skeleton
# every blocker_kind reuses, the uniform fail-graph-write cell, and the
# wait_stage lookup. Per-mechanism logic lives in its own file so each stays
# well under the 500-line cap with room for the eight more families still to
# land, and so it is obvious which mechanism a new family opts into:
#   - ifa_fault_generic_shared_intent_lock.sh -- the shared_intent_lock
#     blocker (precondition + acquire/release). The ONE mechanism this PR
#     wires to a real consumer (code_calls, rationale_edges).
#   - ifa_fault_generic_table_lock.sh -- the table_lock:<name> blocker
#     (precondition + acquire/release). Built and unit-tested; UNEXERCISED in
#     this PR (deployable_unit_edges stays cell_kind=custom on its own
#     already-proven cell -- see ifa_family_registry.sh's IFA_FAMILY_CELL_KIND
#     comment for that scoping call).
#   - ifa_fault_generic_runner_wait.sh -- the wait_stage=runner non-vacuity
#     predicate. UNPROVEN: no family declares wait_stage=runner today.
#
# cell_kind=custom families (documentation_edges' ACK barrier,
# deployable_unit_edges' table_lock kept on its bespoke cell,
# codeowners_ownership_edges' not-yet-implemented ack_barrier target) are NOT
# forced into the generic shape. cell_killworker_family and
# cell_failgraphwrite_family below dispatch straight to that family's own
# hand-written cell function (custom_killworker_fn / custom_failgraphwrite_fn
# in the registry) instead -- see ifa_family_registry.sh's schema comment for
# why cell_kind exists.
#
# This file is a plain function library, not a script (no `set -euo
# pipefail`; sourcing it would rebind the caller's shell options -- same note
# every sibling *_cells.sh file carries). It is meant to be sourced by
# scripts/verify-ifa-fault-injection.sh, which owns strict mode and every
# global read below (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose,
# compose_file, FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN,
# CLAIMED_ROW_WAIT_TIMEOUT, wall_times, bg_pids, log, die, plus the
# fresh_stack / drive_all_cassettes / run_drain_gate / assert_no_dead_letters
# / capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh).
#
# WIRING, NOT YET DONE (coordinate with that file's owner; this file's own
# author does not edit scripts/verify-ifa-fault-injection.sh or the two
# per-family cell libraries below):
#   1. Add `source ifa_fault_generic_cells.sh` to verify-ifa-fault-injection.sh's
#      sourcing block (this file self-sources its own three mechanism files
#      and ifa_family_registry.sh, so one new source line is enough).
#   2. In scripts/lib/ifa_fault_injection_code_call_cells.sh, replace the
#      bodies of cell_killworker_code_calls and cell_failgraphwrite_code_calls
#      with one-line delegations: `cell_killworker_family code_calls` and
#      `cell_failgraphwrite_family code_calls`, respectively. Do the same in
#      scripts/lib/ifa_fault_injection_rationale_cells.sh for
#      cell_killworker_rationale / cell_failgraphwrite_rationale ->
#      `cell_killworker_family rationale_edges` / `cell_failgraphwrite_family
#      rationale_edges`. The `ifa_fault_shard_run cell_killworker_code_calls`
#      etc. call sites in verify-ifa-fault-injection.sh do NOT change.
#      ifa_fault_shard_run ($1=cell, then `"${cell}"` with no further args) is
#      a SHARD FILTER, not an argument forwarder: its whole job is deciding
#      whether the named zero-arg cell function runs at all under --shard
#      k/n, not passing data into it. It never took a family argument for any
#      existing cell either, so it has no way to thread one through now. The
#      family binding therefore has to live inside the cell function's own
#      (now one-line) body, same as every other family's cell already does --
#      NOT on the dispatch line. Do not try to pass the family as a second
#      arg to ifa_fault_shard_run; it silently ignores anything past $1.
_ifa_generic_cells_lib_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if ! declare -F ifa_fault_require_fresh_domain_intents >/dev/null; then
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "${_ifa_generic_cells_lib_dir}/ifa_fault_injection_common.sh"
fi
if ! declare -F ifa_family_registry_names >/dev/null; then
	# shellcheck source=scripts/lib/ifa_family_registry.sh
	source "${_ifa_generic_cells_lib_dir}/ifa_family_registry.sh"
fi
if ! declare -F _ifa_generic_require_intent_writer >/dev/null; then
	# shellcheck source=scripts/lib/ifa_fault_generic_shared_intent_lock.sh
	source "${_ifa_generic_cells_lib_dir}/ifa_fault_generic_shared_intent_lock.sh"
fi
if ! declare -F _ifa_generic_table_lock_start >/dev/null; then
	# shellcheck source=scripts/lib/ifa_fault_generic_table_lock.sh
	source "${_ifa_generic_cells_lib_dir}/ifa_fault_generic_table_lock.sh"
fi
if ! declare -F ifa_fault_wait_for_claimed_projection_intent >/dev/null; then
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${_ifa_generic_cells_lib_dir}/ifa_fault_generic_runner_wait.sh"
fi

# ---------------------------------------------------------------------------
# shared helpers
# ---------------------------------------------------------------------------

# _ifa_generic_assert_edges runs `eshu-ifa assert-edges` for family, reading
# its -domain (the family name itself -- every registered family's assert-
# edges domain equals its registry key, verified against
# scripts/verify-ifa-fault-injection.sh's existing per-family cells) and
# -expected (the registry's cassette_var/expected_var indirection) from the
# registry rather than a hardcoded per-family call.
_ifa_generic_assert_edges() {
	local family="$1" expected_var expected
	expected_var="$(ifa_family_expected_var "${family}")" || return 1
	if [[ -z "${expected_var}" ]]; then
		printf '_ifa_generic_assert_edges: no expected_var registered for family %s\n' "${family}" >&2
		return 1
	fi
	expected="${!expected_var}"
	"${bin_dir}/eshu-ifa" assert-edges -domain "${family}" -expected "${expected}"
}

# _ifa_generic_wait_for_claimed dispatches the non-vacuity wait predicate on
# wait_stage: "handler" polls fact_work_items (ifa_fault_wait_for_claimed,
# ifa_fault_injection_common.sh), "runner" polls shared_projection_intents
# (ifa_fault_wait_for_claimed_projection_intent,
# ifa_fault_generic_runner_wait.sh -- UNPROVEN, see that file's header).
_ifa_generic_wait_for_claimed() {
	local family="$1" budget="$2" wait_stage wait_key
	wait_stage="$(ifa_family_wait_stage "${family}")" || return 1
	wait_key="$(ifa_family_wait_key "${family}")" || return 1
	case "${wait_stage}" in
	handler)
		ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${budget}" "${wait_key}"
		;;
	runner)
		ifa_fault_wait_for_claimed_projection_intent "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${budget}" "${wait_key}"
		;;
	*)
		printf '_ifa_generic_wait_for_claimed: %s has unknown wait_stage %q\n' "${family}" "${wait_stage}" >&2
		return 1
		;;
	esac
}

# ---------------------------------------------------------------------------
# _ifa_generic_cell_killworker_body is THE MANDATORY PRECONDITION ASSERTS'
# caller-facing skeleton: the ONE kill/reclaim/drain/assert sequence every
# blocker_kind shares -- only blocker acquisition/release differs, and that
# is a two-line case here rather than three ~35-line near-duplicate
# functions. blocker_kind is "none" | "shared_intent_lock" | "table_lock";
# blocker_arg is the namespace (family) or table name -- ignored for "none".
#
# Callers (the per-mechanism thin wrappers in the sibling files above) run
# their own precondition BEFORE calling this: a blocker aimed at a
# table/queue a family's handler never touches does not engage, so Handle
# runs to completion and acks before kill -9 lands, proving ordinary
# baseline recovery under a name that claims domain-scoped reclaim
# (ifa_fault_injection_deployable_unit_lock.sh:81-86 records this happening
# once already, in CI). The precondition must fire before any lock is even
# attempted, not buried inside this shared body.
# ---------------------------------------------------------------------------
_ifa_generic_cell_killworker_body() {
	local family="$1" cell="$2" blocker_kind="$3" blocker_arg="$4" cell_start
	cell_start=$(date +%s)
	log "cell generic-kill-worker-after-claim (${family}, blocker=${blocker_kind}${blocker_arg:+:${blocker_arg}}): fresh stack"
	fresh_stack "${cell}"
	drive_all_cassettes "${cell}"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid="" claimed_before
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	case "${blocker_kind}" in
	shared_intent_lock)
		ifa_fault_start_shared_intent_lock "${cell}" "${blocker_arg}" lock_holder_pid \
			|| die "${cell}: could not acquire the deterministic shared_projection_intents blocker"
		;;
	table_lock)
		_ifa_generic_table_lock_start "${cell}" "${blocker_arg}" lock_holder_pid \
			|| die "${cell}: could not acquire the deterministic ${blocker_arg} blocker"
		;;
	esac
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(_ifa_generic_wait_for_claimed "${family}" "${CLAIMED_ROW_WAIT_TIMEOUT}")" \
		|| die "${cell}: no ${family} row was claimed before the kill -- non-vacuous precondition failed"
	printf '%s: non-vacuous: %s claimed/running %s row(s) observed before kill\n' "${cell}" "${claimed_before}" "${family}"
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid_before}" KILL \
		|| die "${cell}: could not stop, join, and untrack the killed reducer"
	case "${blocker_kind}" in
	shared_intent_lock) ifa_fault_release_shared_intent_lock "${cell}" "${blocker_arg}" "${lock_holder_pid}" ;;
	table_lock) _ifa_generic_table_lock_release "${cell}" "${blocker_arg}" "${lock_holder_pid}" ;;
	esac
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	assert_no_dead_letters "${cell}"
	_ifa_generic_assert_edges "${family}" || die "${cell}: recovered graph does not match the expected edge set"
	if [[ "${blocker_kind}" == "shared_intent_lock" ]]; then
		local retry_var
		retry_var="$(ifa_family_retry_baseline_var "${family}" 2>/dev/null || true)"
		if [[ -n "${retry_var}" ]]; then
			ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
				"${!retry_var}" 15 "$(ifa_family_wait_key "${family}")" \
				|| die "${cell}: ${family} did not re-execute above its fault-free retry baseline"
		else
			printf '%s: no retry-baseline variable registered for %s -- this cell does NOT prove a durable retry above baseline (register one in IFA_FAMILY_RETRY_BASELINE_VAR to close that gap)\n' "${cell}" "${family}"
		fi
	fi
	capture_digest "${cell}"
	assert_matches_baseline "${cell}"
	teardown_cell "${cell}"
	wall_times[$cell]=$(( $(date +%s) - cell_start ))
	printf '%s: cell wall time: %ss\n' "${cell}" "${wall_times[${cell}]}"
}

# _ifa_generic_cell_killworker_none is blocker_kind=none's thin wrapper --
# too small to earn its own mechanism file. No precondition: there is no
# blocker to fail to engage.
_ifa_generic_cell_killworker_none() {
	local family="$1" cell="genkillworker${1//_/}"
	_ifa_generic_cell_killworker_body "${family}" "${cell}" none ""
}

# ---------------------------------------------------------------------------
# generic fail-graph-write cell. Uniform across every blocker_kind: the
# once-fault decorator intercepts at the Cypher layer directly (see
# ifa_fault_write_once_script), so no lock/wait blocker is needed here even
# for a family whose kill-worker cell needs one -- verified against every
# existing example (cell_failgraphwrite_sql/code_calls/rationale/
# documentation all share this exact shape regardless of that family's
# kill-worker blocker_kind). This is why it stays in the dispatcher file
# rather than any one mechanism file -- it is not mechanism-specific.
# ---------------------------------------------------------------------------

_ifa_generic_cell_failgraphwrite() {
	local family="$1" cell="genfailgraphwrite${1//_/}" cell_start
	cell_start=$(date +%s)
	log "cell generic-fail-graph-write-once-then-succeed (${family}): fresh stack"
	fresh_stack "${cell}"
	if [[ "${use_compose}" -eq 1 ]]; then
		ifa_fault_require_fresh_domain_intents "${cell}" "${family}" \
			"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
			|| die "${cell}: fresh-stack precondition failed"
	else
		printf '%s: fresh-stack precondition SKIPPED (--no-compose owns the stack; surviving intents are not a leak)\n' "${cell}"
	fi
	drive_all_cassettes "${cell}"
	local anchor fault_once_script projector_pid reducer_pid marker_rc
	anchor="$(ifa_family_anchor "${family}")" || die "${cell}: no anchor registered for ${family}"
	fault_once_script="${work_dir}/fault-once-then-succeed-${family}.json"
	ifa_fault_write_once_script "${fault_once_script}" "${anchor}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	assert_no_dead_letters "${cell}"
	_ifa_generic_assert_edges "${family}" || die "${cell}: recovered graph does not match the expected edge set"
	# Operator-facing diagnostic (informational only -- gates nothing): which
	# domain's intent window this cell was actually watching. shared_projection_
	# intents.projection_domain is keyed by the family/registry name itself
	# (verified against every bespoke cell this replaced: the old code_calls and
	# rationale_edges cells both filtered on their own family name here, not
	# their wait_key), so this generalizes with no registry lookup beyond the
	# family argument this function already has. At 3am when a generic cell
	# fails, this is how an operator tells whether it was watching the right
	# domain at all.
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE completed_at IS NULL) AS pending FROM shared_projection_intents WHERE projection_domain = '${family}';" \
		"${compose_file}" | sed "s/^/  ${family} intent window: /"
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${anchor}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		die "${cell}: the fault FIRED but on a different write than ${anchor} (marker contents above); the injection works, the anchor is pointed at the wrong statement"
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "${cell}: once-fired marker did not name the targeted MERGE (marker status ${marker_rc})"
	capture_digest "${cell}"
	assert_matches_baseline "${cell}"
	teardown_cell "${cell}"
	wall_times[$cell]=$(( $(date +%s) - cell_start ))
	printf '%s: cell wall time: %ss\n' "${cell}" "${wall_times[${cell}]}"
}

# ---------------------------------------------------------------------------
# public dispatchers -- the only two entry points this file expects a driver
# to call.
# ---------------------------------------------------------------------------

# cell_killworker_family runs family's kill-worker-after-claim cell:
# cell_kind=custom dispatches to that family's own hand-written function
# (failing loudly, never silently, when none is registered or defined yet);
# cell_kind=generic builds the shape from blocker_kind, delegating to the
# matching mechanism file's thin wrapper.
cell_killworker_family() {
	local family="$1" cell_kind blocker_kind
	cell_kind="$(ifa_family_cell_kind "${family}")" || die "cell_killworker_family: unknown family ${family}"
	if [[ "${cell_kind}" == "custom" ]]; then
		local fn
		fn="$(ifa_family_custom_killworker_fn "${family}")" \
			|| die "cell_killworker_family: ${family} declares no custom_killworker_fn row"
		[[ -n "${fn}" ]] \
			|| die "cell_killworker_family: ${family} is cell_kind=custom but declares no kill-worker cell function yet (not implemented for this family; see its ifa_family_registry.sh row comment)"
		declare -F "${fn}" >/dev/null \
			|| die "cell_killworker_family: ${family}'s declared kill-worker function '${fn}' is not defined -- source its cell library before calling this dispatcher"
		"${fn}"
		return $?
	fi
	blocker_kind="$(ifa_family_blocker_kind "${family}")" || die "cell_killworker_family: unknown family ${family}"
	case "${blocker_kind}" in
	none) _ifa_generic_cell_killworker_none "${family}" ;;
	shared_intent_lock) _ifa_generic_cell_killworker_shared_intent_lock "${family}" ;;
	table_lock:*) _ifa_generic_cell_killworker_table_lock "${family}" "${blocker_kind#table_lock:}" ;;
	*) die "cell_killworker_family: ${family} has unsupported blocker_kind '${blocker_kind}' for cell_kind=generic (ack_barrier requires cell_kind=custom)" ;;
	esac
}

# cell_failgraphwrite_family runs family's fail-graph-write-once-then-succeed
# cell: cell_kind=custom dispatches the same way cell_killworker_family does;
# cell_kind=generic always builds the uniform anchor-based shape regardless
# of blocker_kind (see the section header above for why blocker_kind is
# irrelevant to this cell).
cell_failgraphwrite_family() {
	local family="$1" cell_kind
	cell_kind="$(ifa_family_cell_kind "${family}")" || die "cell_failgraphwrite_family: unknown family ${family}"
	if [[ "${cell_kind}" == "custom" ]]; then
		local fn
		fn="$(ifa_family_custom_failgraphwrite_fn "${family}")" \
			|| die "cell_failgraphwrite_family: ${family} declares no custom_failgraphwrite_fn row"
		[[ -n "${fn}" ]] \
			|| die "cell_failgraphwrite_family: ${family} is cell_kind=custom but declares no fail-graph-write cell function yet (not implemented for this family; see its ifa_family_registry.sh row comment)"
		declare -F "${fn}" >/dev/null \
			|| die "cell_failgraphwrite_family: ${family}'s declared fail-graph-write function '${fn}' is not defined -- source its cell library before calling this dispatcher"
		"${fn}"
		return $?
	fi
	_ifa_generic_cell_failgraphwrite "${family}"
}
