#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the security_group_rule_endpoint
# fault-injection cells (#6228 secgroup-pair live leaf, SECOND). Split into
# its own module for the same reason
# test-ifa-fault-injection-documentation-cases.sh was: the top-level static
# verifier and the review-case module both sit near the repository's
# 500-line cap. Sourced by
# scripts/test-verify-ifa-fault-injection.sh, and shares that script's
# ${repo_root} and `fail` helper.
#
# These cases are hermetic: every process, database, and signal the cells
# would touch is replaced by a shell stub, so they run in `make pre-pr`
# without Docker, Postgres, or a live gate. They prove the seams that
# would otherwise fail silently in CI -- a hold that never actually
# engaged, a kill landing on an unheld worker, and a marker naming the
# wrong write -- not that the cells recover a real graph, which only the
# live fault matrix can show.
#
# There is deliberately no lock case here (contrast the sg_rule sibling):
# this row's blocker_kind is statement_hold, so the kill cell takes no
# table lock. There is deliberately no readiness-wait case either: the
# triple-gate banking helper lives in the sg_rule sibling file (one
# definition, shared domain) and is covered by that module.

sgte_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_security_group_rule_endpoint_cells.sh"
[[ -f "${sgte_cells_lib}" ]] || fail "missing ${sgte_cells_lib}"
bash -n "${sgte_cells_lib}" || fail "ifa_fault_injection_security_group_rule_endpoint_cells.sh has a syntax error"


# ifa_security_group_rule_endpoint_wait_for_hold_marker is the kill cell's
# rendezvous: kill -9 lands only once the hold decorator's marker proves
# the worker is parked inside the TO MERGE. Cases: the marker already
# present returns success; a missing marker exhausts the budget and fails
# (an inert hold must never read as "held"); a bad budget fails instead of
# polling forever or returning success.
test_ifa_security_group_rule_endpoint_hold_marker_wait_cases() (
	source "${det_lib}"
	source "${sgte_cells_lib}"

	local rc work script
	work="$(mktemp -d -t ifa-sgte-holdmarker.XXXXXX)"
	trap 'rm -rf "${work}"' EXIT
	script="${work}/fault-hold.json"
	sleep() { :; }

	# Marker already present: success without consuming budget.
	: >"${script}.restart-sentinel.statement-held"
	rc=0
	ifa_security_group_rule_endpoint_wait_for_hold_marker holdcell "${script}" 5 || rc=$?
	[[ "${rc}" -eq 0 ]] \
		|| fail "ifa_security_group_rule_endpoint_wait_for_hold_marker failed while the hold marker exists"

	# Marker never appears: exhausts the budget and fails.
	rm -f "${script}.restart-sentinel.statement-held"
	rc=0
	ifa_security_group_rule_endpoint_wait_for_hold_marker holdcell "${script}" 2 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_security_group_rule_endpoint_wait_for_hold_marker returned success with no hold marker; the kill would land on an unheld worker"

	# Bad budget: fails instead of polling.
	rc=0
	ifa_security_group_rule_endpoint_wait_for_hold_marker holdcell "${script}" 0 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_security_group_rule_endpoint_wait_for_hold_marker accepted a zero budget"
)

# The seam's kill cell must exist in the sourced lib: the gate dispatches
# cell_killworker_security_group_rule_endpoint by name, and a rename without
# a dispatch update would die only inside the live shard. The hold helpers it
# relies on must exist too: a missing write or assert helper would die at the
# same late point.
test_ifa_security_group_rule_endpoint_kill_cell_is_defined() (
	source "${repo_root}/scripts/lib/ifa_fault_injection_hold.sh"
	source "${sgte_cells_lib}"
	declare -F cell_killworker_security_group_rule_endpoint >/dev/null \
		|| fail "cell_killworker_security_group_rule_endpoint is not defined in ${sgte_cells_lib}"
	declare -F ifa_fault_write_hold_script >/dev/null \
		|| fail "ifa_fault_write_hold_script is not defined in scripts/lib/ifa_fault_injection_hold.sh"
	declare -F ifa_fault_assert_hold_marker >/dev/null \
		|| fail "ifa_fault_assert_hold_marker is not defined in scripts/lib/ifa_fault_injection_hold.sh"
	declare -F ifa_security_group_rule_endpoint_wait_for_hold_marker >/dev/null \
		|| fail "ifa_security_group_rule_endpoint_wait_for_hold_marker is not defined in ${sgte_cells_lib}"
)

# The kill cell must engage the family's own write: if the hold script it
# writes named any operation_match but the TO MERGE anchor, the hold would
# park the worker in another family's statement and the statement-stage
# exclusivity the registry row claims would be fiction. Pin the pair: the
# cell must pass the anchor variable to the write helper, and the helper
# must render that match into the script.
test_ifa_security_group_rule_endpoint_hold_script_targets_to_merge() (
	source "${repo_root}/scripts/lib/ifa_fault_injection_hold.sh"

	local work script
	work="$(mktemp -d -t ifa-sgte-holdscript.XXXXXX)"
	trap 'rm -rf "${work}"' EXIT
	script="${work}/fault-hold.json"
	rg --quiet -- 'ifa_fault_write_hold_script "\$\{hold_script\}" "\$\{security_group_rule_endpoint_edge_operation_match\}"' "${sgte_cells_lib}" \
		|| fail "kill rule_endpoint cell does not write its hold script with security_group_rule_endpoint_edge_operation_match; the hold could engage another family's write"
	ifa_fault_write_hold_script "${script}" 'MERGE (rule)-[rel:TO]->(target)' \
		|| fail "ifa_fault_write_hold_script rejected the TO operation match"
	rg --quiet -- 'MERGE \(rule\)-\[rel:TO\]->\(target\)' "${script}" \
		|| fail "hold script does not carry the TO MERGE operation match; the hold would engage the wrong write"
)

run_ifa_fault_injection_security_group_rule_endpoint_cases() {
	test_ifa_security_group_rule_endpoint_hold_marker_wait_cases
	test_ifa_security_group_rule_endpoint_kill_cell_is_defined
	test_ifa_security_group_rule_endpoint_hold_script_targets_to_merge
	test_ifa_security_group_rule_endpoint_kill_cell_proves_attempts_above_baseline
	test_ifa_security_group_rule_endpoint_merge_anchor_is_literal
}

# Same saturating single-row shape as the k8s kill cell (proved behaviorally
# in test_ifa_direct_family_retry_attempts_above_counts_attempts_not_rows):
# one work item means the row-count form caps at 1, so the rule_endpoint
# kill cell must reach for the attempt-totals helper with its own domain.
test_ifa_security_group_rule_endpoint_kill_cell_proves_attempts_above_baseline() (
	rg -U --quiet -- '(?s)ifa_fault_assert_retry_attempts_above.{0,400}"security_group_reachability_materialization"' "${sgte_cells_lib}" \
		|| fail "kill-worker rule_endpoint cell does not pass its own materialization domain to ifa_fault_assert_retry_attempts_above; the row-count form saturates at 1 for its single work item"
)

# The TO MERGE anchor the fail-graph-write cell targets: the writer's
# template carries a static relationship token (the %s fills the
# target-node label), so only the LITERAL form matches executed statement
# text. A parameterized spelling would match nothing and the fault would
# never fire.
test_ifa_security_group_rule_endpoint_merge_anchor_is_literal() (
	require_anchors_lib "rule_endpoint TO MERGE operation_match anchor" 'security_group_rule_endpoint_edge_operation_match="MERGE (rule)-[rel:TO]->(target)"'
)
