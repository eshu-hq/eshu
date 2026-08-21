#!/usr/bin/env bash
# shellcheck disable=SC2154  # test_ifa_fault_shard_cases_fail comes from
# scripts/lib/test-ifa-fault-injection-shard-cases.sh, sourced alongside this
# file by scripts/test-verify-ifa-fault-injection.sh before either is called.
#
# run_ifa_fault_injection_deployable_unit_ordering_cases verifies the
# deployable_unit trio's dispatch-anchor invocation and dispatch ORDER.
# Extracted from scripts/lib/test-ifa-fault-injection-shard-cases.sh (which
# had itself absorbed this from
# scripts/lib/test-ifa-fault-injection-deployable-unit-cases.sh) to keep that
# file under the repository's 500-line cap -- extraction, not deletion: every
# comment below is the full, unabridged rationale. "$1" is the gate script
# path; the caller passes its own ${script} rather than this module
# recomputing repo_root, since deployable-unit-cases.sh is always sourced
# alongside a main mirror that already resolved it.
run_ifa_fault_injection_deployable_unit_ordering_cases() {
	local script="$1"

	# Anchored the same way as the twenty-one-cell loop in the shard-cases
	# module: needles carry the ifa_fault_shard_run prefix
	# (scripts/lib/ifa_fault_shard.sh), since every dispatch line now routes
	# through that wrapper for --shard skip support. STRICTLY STRONGER than a
	# bare-name check: an unwrapped cell would silently ignore --shard and run
	# in every shard instead of just its own -- a real defect with no other
	# detector, since the gate would still pass everywhere, just doing 4x the
	# work for these three cells while quietly diverging from the partition
	# this mirror claims to prove.
	local cell
	for cell in cell_baseline_deployable_unit cell_killworker_deployable_unit cell_failgraphwrite_deployable_unit; do
		rg --quiet -- "^ifa_fault_shard_run ${cell}\$" "${script}" \
			|| test_ifa_fault_shard_cases_fail "verifier does not invoke ${cell} via ifa_fault_shard_run on its own line -- missing entirely, or dispatched WITHOUT the wrapper"
	done

	# The baseline cell must dispatch before both fault cells: it is the sole
	# writer of digests[baseline_deployable_unit]
	# (scripts/lib/ifa_fault_injection_deployable_unit_cells.sh's
	# assert_matches_baseline calls), which the two fault cells below read.
	# This is also the co-location property the shard PARTITIONER depends on
	# for its one atomic group (scripts/lib/ifa_fault_shard.sh's
	# IFA_FAULT_ATOMIC_GROUPS): the trio must land in the same shard, in this
	# order, or a fault cell reads an unset digests key. A line-number
	# extraction that returns empty (rather than a real line) would otherwise
	# fail below with a misleading "not a number" instead of naming the real
	# cause, so each extracted line is validated as numeric first.
	local baseline_du_line killworker_du_line failgraphwrite_du_line
	baseline_du_line="$(rg -n --line-regexp -- 'ifa_fault_shard_run cell_baseline_deployable_unit' "${script}" | cut -d: -f1 || true)"
	killworker_du_line="$(rg -n --line-regexp -- 'ifa_fault_shard_run cell_killworker_deployable_unit' "${script}" | cut -d: -f1 || true)"
	failgraphwrite_du_line="$(rg -n --line-regexp -- 'ifa_fault_shard_run cell_failgraphwrite_deployable_unit' "${script}" | cut -d: -f1 || true)"
	[[ "${baseline_du_line}" =~ ^[0-9]+$ && "${killworker_du_line}" =~ ^[0-9]+$ && "${failgraphwrite_du_line}" =~ ^[0-9]+$ \
		&& "${baseline_du_line}" -lt "${killworker_du_line}" && "${baseline_du_line}" -lt "${failgraphwrite_du_line}" ]] \
		|| test_ifa_fault_shard_cases_fail "cell_baseline_deployable_unit must be dispatched before both deployable-unit fault cells"
}
