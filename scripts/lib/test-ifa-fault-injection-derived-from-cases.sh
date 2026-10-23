#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the derived_from
# fault-injection cells (#6228 derived_from live leaf). Split into its own module for
# the same reason test-ifa-fault-injection-documentation-cases.sh was: the
# top-level static verifier and the review-case module both sit near the
# repository's 500-line cap. Sourced by
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
# There is deliberately no readiness-wait case here (contrast the crc-image
# sibling): this handler carries no in-handler readiness gate before its
# fact load, so the hold is armed as soon as the projector enqueues the row
# with no wait to test.

derived_from_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_derived_from_cells.sh"
[[ -f "${derived_from_cells_lib}" ]] || fail "missing ${derived_from_cells_lib}"
bash -n "${derived_from_cells_lib}" || fail "ifa_fault_injection_derived_from_cells.sh has a syntax error"


# ifa_derived_from_wait_for_hold_marker is the kill cell's rendezvous:
# kill -9 lands only once the hold decorator's marker proves the worker is
# parked inside the DERIVED_FROM MERGE. Cases: the marker already
# present returns success; a missing marker exhausts the budget and fails
# (an inert hold must never read as "held"); a bad budget fails instead of
# polling forever or returning success.
test_ifa_derived_from_hold_marker_wait_cases() (
	source "${det_lib}"
	source "${derived_from_cells_lib}"

	local rc work script
	work="$(mktemp -d -t ifa-derived-from-holdmarker.XXXXXX)"
	trap 'rm -rf "${work}"' EXIT
	script="${work}/fault-hold.json"
	sleep() { :; }

	# Marker already present: success without consuming budget.
	: >"${script}.restart-sentinel.statement-held"
	rc=0
	ifa_derived_from_wait_for_hold_marker holdcell "${script}" 5 || rc=$?
	[[ "${rc}" -eq 0 ]] \
		|| fail "ifa_derived_from_wait_for_hold_marker failed while the hold marker exists"

	# Marker never appears: exhausts the budget and fails.
	rm -f "${script}.restart-sentinel.statement-held"
	rc=0
	ifa_derived_from_wait_for_hold_marker holdcell "${script}" 2 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_derived_from_wait_for_hold_marker returned success with no hold marker; the kill would land on an unheld worker"

	# Bad budget: fails instead of polling.
	rc=0
	ifa_derived_from_wait_for_hold_marker holdcell "${script}" 0 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_derived_from_wait_for_hold_marker accepted a zero budget"
)

# The seam's kill cell must exist in the sourced lib: the gate dispatches
# cell_killworker_derived_from by name, and a rename without a dispatch
# update would die only inside the live shard. The hold helpers it relies
# on must exist too: a missing write or assert helper would die at the
# same late point.
test_ifa_derived_from_kill_cell_is_defined() (
	source "${repo_root}/scripts/lib/ifa_fault_injection_hold.sh"
	source "${derived_from_cells_lib}"
	declare -F cell_killworker_derived_from >/dev/null \
		|| fail "cell_killworker_derived_from is not defined in ${derived_from_cells_lib}"
	declare -F ifa_fault_write_hold_script >/dev/null \
		|| fail "ifa_fault_write_hold_script is not defined in scripts/lib/ifa_fault_injection_hold.sh"
	declare -F ifa_fault_assert_hold_marker >/dev/null \
		|| fail "ifa_fault_assert_hold_marker is not defined in scripts/lib/ifa_fault_injection_hold.sh"
	declare -F ifa_derived_from_wait_for_hold_marker >/dev/null \
		|| fail "ifa_derived_from_wait_for_hold_marker is not defined in ${derived_from_cells_lib}"
)

# The kill cell must engage the family's own write: if the hold script it
# writes named any operation_match but the DERIVED_FROM MERGE anchor, the
# hold would park the worker in another family's statement and the
# statement-stage exclusivity the registry row claims would be fiction.
# Pin the pair: the cell must pass the anchor variable to the write
# helper, and the helper must render that match into the script.
test_ifa_derived_from_hold_script_targets_derived_merge() (
	source "${repo_root}/scripts/lib/ifa_fault_injection_hold.sh"

	local work script
	work="$(mktemp -d -t ifa-derived-from-holdscript.XXXXXX)"
	trap 'rm -rf "${work}"' EXIT
	script="${work}/fault-hold.json"
	rg --quiet -- 'ifa_fault_write_hold_script "\$\{hold_script\}" "\$\{derived_from_edge_operation_match\}"' "${derived_from_cells_lib}" \
		|| fail "kill derived_from cell does not write its hold script with derived_from_edge_operation_match; the hold could engage another family's write"
	ifa_fault_write_hold_script "${script}" 'MERGE (img)-[rel:DERIVED_FROM' \
		|| fail "ifa_fault_write_hold_script rejected the DERIVED_FROM operation match"
	rg --quiet -- 'MERGE \(img\)-\[rel:DERIVED_FROM' "${script}" \
		|| fail "hold script does not carry the DERIVED_FROM MERGE operation match; the hold would engage the wrong write"
)

run_ifa_fault_injection_derived_from_cases() {
	test_ifa_derived_from_hold_marker_wait_cases
	test_ifa_derived_from_kill_cell_is_defined
	test_ifa_derived_from_hold_script_targets_derived_merge
	test_ifa_derived_from_kill_cell_proves_attempts_above_baseline
	test_ifa_derived_from_merge_anchor_is_prefix
}

# The DERIVED_FROM MERGE anchor the fail-graph-write cell targets: the
# writer's template carries the scope_id/evidence_source identity map, so
# only the PREFIX form matches executed statement text. A full `]->(base)`
# spelling would match nothing and the fault would never fire.
test_ifa_derived_from_merge_anchor_is_prefix() (
	require_anchors_lib "derived_from DERIVED_FROM MERGE operation_match anchor" 'derived_from_edge_operation_match="MERGE (img)-[rel:DERIVED_FROM"'
)

# Same saturating single-row shape as the k8s kill cell (proved behaviorally
# in test_ifa_direct_family_retry_attempts_above_counts_attempts_not_rows):
# one work item means the row-count form caps at 1, so the derived_from kill
# cell must reach for the attempt-totals helper with its own domain.
test_ifa_derived_from_kill_cell_proves_attempts_above_baseline() (
	rg -U --quiet -- '(?s)ifa_fault_assert_retry_attempts_above.{0,400}"container_image_identity"' "${derived_from_cells_lib}" \
		|| fail "kill-worker derived_from cell does not pass its own identity domain to ifa_fault_assert_retry_attempts_above; the row-count form saturates at 1 for its single work item"
)
