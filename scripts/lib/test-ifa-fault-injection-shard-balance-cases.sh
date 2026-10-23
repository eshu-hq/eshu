#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# The probes assign and read globals owned by scripts/lib/ifa_fault_shard.sh
# and its cell roster (the weight table, the default weight, the selection
# map), which shellcheck cannot see from this file.
# Hermetic mirror for the WEIGHT side of scripts/lib/ifa_fault_shard.sh: the
# measured per-cell weight table, the weighted shard assignment, and the
# per-shard time budget. No Docker, no compose, no Postgres. Sourced by
# scripts/test-verify-ifa-fault-injection.sh beside the exact-cover module
# (test-ifa-fault-injection-shard-cases.sh, which sits at the 500-line cap).
#
# Why this exists: until this module, cells were dealt to shards by position
# (group i went to shard (i % n) + 1). Every new family trio shifted every
# group after it, so the heavy groups (deployable_unit at ~155s per cell,
# workload_dependency at ~140s, the ~107s kill cells) landed wherever their
# position put them. Shards ran 24-36 minutes against the job's 35-minute
# timeout, and shards were cancelled at the timeout on main (9da4567be2,
# 3d0997929f) and on PR #8786. The weighted assignment balances on measured
# seconds instead, and the budget check below turns growth into a red mirror
# with a clear next step, rather than a cancelled shard on someone else's PR.
#
# Every check that guards something has a seeded-violation probe next to it:
# the check must go red on a planted defect and stay green on the real tree.

test_ifa_fault_shard_balance_fail() {
	printf 'test-ifa-fault-injection-shard-balance-cases: %s\n' "$*" >&2
	exit 1
}

run_ifa_fault_injection_shard_balance_cases() {
	local repo_root shard_lib
	repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	shard_lib="${repo_root}/scripts/lib/ifa_fault_shard.sh"
	[[ -f "${shard_lib}" ]] || test_ifa_fault_shard_balance_fail "missing ${shard_lib}"

	# The exact-cover module usually sourced the library already in this shell,
	# and a second source dies on its readonly constants.
	if [[ -z "${IFA_FAULT_SHARD_BUDGET_SECONDS:-}" ]]; then
		# shellcheck source=scripts/lib/ifa_fault_shard.sh
		source "${shard_lib}"
	fi
	local n="${IFA_FAULT_SHARD_DEFAULT_N}"
	declare -F ifa_fault_shard_check_weights >/dev/null \
		|| test_ifa_fault_shard_balance_fail "ifa_fault_shard_check_weights is not defined -- the weight table has no validator"
	declare -F ifa_fault_shard_check_budget >/dev/null \
		|| test_ifa_fault_shard_balance_fail "ifa_fault_shard_check_budget is not defined -- shard growth has no budget guard"
	declare -F ifa_fault_shard_loads >/dev/null \
		|| test_ifa_fault_shard_balance_fail "ifa_fault_shard_loads is not defined -- per-shard estimates are not reported"

	# WEIGHT TABLE VALIDITY. A key that names no cell is a stale entry: the
	# cell was renamed or removed, and its real successor silently falls back
	# to the default weight. A non-integer or zero weight would make the
	# arithmetic below lie.
	ifa_fault_shard_check_weights \
		|| test_ifa_fault_shard_balance_fail "weight table is invalid on the real tree"
	(IFA_FAULT_CELL_WEIGHT_SECONDS['cell_no_such_cell']=70; ifa_fault_shard_check_weights) >/dev/null 2>&1 \
		&& test_ifa_fault_shard_balance_fail "seeded stale weight key (cell_no_such_cell) was accepted"
	(IFA_FAULT_CELL_WEIGHT_SECONDS['cell_killworker']=abc; ifa_fault_shard_check_weights) >/dev/null 2>&1 \
		&& test_ifa_fault_shard_balance_fail "seeded non-numeric weight was accepted"
	(IFA_FAULT_CELL_WEIGHT_SECONDS['cell_killworker']=0; ifa_fault_shard_check_weights) >/dev/null 2>&1 \
		&& test_ifa_fault_shard_balance_fail "seeded zero weight was accepted"
	(IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS=0; ifa_fault_shard_check_weights) >/dev/null 2>&1 \
		&& test_ifa_fault_shard_balance_fail "seeded zero default weight was accepted"
	(unset IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS; ifa_fault_shard_check_weights) >/dev/null 2>&1 \
		&& test_ifa_fault_shard_balance_fail "unset default weight was accepted"

	# FAIL CLOSED. The mirror catches a bad weight on a PR, but the shard job
	# itself must not turn one into a smaller matrix. A weight that breaks the
	# arithmetic once printed an error inside a process substitution, which
	# set -e never sees, and the shard ran cell_baseline alone and passed.
	# Every entry point that computes a partition must exit non-zero instead.
	local bad_weight='IFA_FAULT_CELL_WEIGHT_SECONDS[cell_killworker_sql]=1.5'
	(eval "${bad_weight}"; ifa_fault_shard_assign_groups "${n}") >/dev/null 2>&1 \
		&& test_ifa_fault_shard_balance_fail "ifa_fault_shard_assign_groups returned 0 on a malformed weight"
	(eval "${bad_weight}"; ifa_fault_shard_select_cells 1 "${n}") >/dev/null 2>&1 \
		&& test_ifa_fault_shard_balance_fail "ifa_fault_shard_select_cells returned 0 on a malformed weight"
	(eval "${bad_weight}"; ifa_fault_shard_parse_args "${shard_lib}" --list-cells --shard "1/${n}") >/dev/null 2>&1 \
		&& test_ifa_fault_shard_balance_fail "--list-cells --shard exited 0 on a malformed weight"
	# parse_args must exit, not return: on return the gate carries on with an
	# empty selection and skips every cell, cell_baseline included.
	local returned
	# The probe exits non-zero when it passes; `|| true` keeps the caller's
	# set -e from treating that as the mirror failing.
	returned="$( (eval "${bad_weight}"; ifa_fault_shard_parse_args "${shard_lib}" --shard "1/${n}"; printf 'returned with %s selected\n' "${#ifa_fault_shard_selected[@]}") 2>/dev/null)" || true
	[[ "${returned}" != *returned* ]] \
		|| test_ifa_fault_shard_balance_fail "--shard ${returned} on a malformed weight, so the gate would run a partial matrix"

	# BUDGET. The estimated cell time of the busiest shard must fit the budget
	# the library states next to IFA_FAULT_SHARD_DEFAULT_N. When a new family
	# pushes a shard over, this goes red and says to add a shard, which is the
	# moment to do it: before CI starts cancelling shards at the timeout.
	ifa_fault_shard_check_budget "${n}" \
		|| test_ifa_fault_shard_balance_fail "the busiest of ${n} shards is over IFA_FAULT_SHARD_BUDGET_SECONDS on the real tree"
	(IFA_FAULT_CELL_WEIGHT_SECONDS['cell_killworker']=$((IFA_FAULT_SHARD_BUDGET_SECONDS + 1)); ifa_fault_shard_check_budget "${n}") >/dev/null 2>&1 \
		&& test_ifa_fault_shard_balance_fail "seeded over-budget cell (cell_killworker at budget+1s) passed the budget check"

	# BALANCE. Greedy least-loaded assignment guarantees that the busiest and
	# the idlest shard differ by at most the heaviest single group: when the
	# busiest shard took its last group it was the idlest one. Positional
	# round-robin breaks this on the real roster (measured: a 544s spread at
	# n=8 against a 466s heaviest group), so this is the check that fails if
	# the partitioner regresses to dealing by position.
	local loads min_load max_load heaviest
	loads="$(ifa_fault_shard_loads "${n}")" \
		|| test_ifa_fault_shard_balance_fail "ifa_fault_shard_loads ${n} exited non-zero"
	min_load="$(printf '%s\n' "${loads}" | awk 'NR == 1 || $2 < m { m = $2 } END { print m }')"
	max_load="$(printf '%s\n' "${loads}" | awk 'NR == 1 || $2 > m { m = $2 } END { print m }')"
	heaviest="$(ifa_fault_shard_group_weights | awk 'NR == 1 || $1 > m { m = $1 } END { print m }')"
	[[ "$(printf '%s\n' "${loads}" | wc -l | tr -d ' ')" == "${n}" ]] \
		|| test_ifa_fault_shard_balance_fail "ifa_fault_shard_loads ${n} did not report exactly ${n} shards:
${loads}"
	((max_load - min_load <= heaviest)) \
		|| test_ifa_fault_shard_balance_fail "shard spread ${max_load}s - ${min_load}s exceeds the heaviest group (${heaviest}s); assignment is not least-loaded:
${loads}"

	# DETERMINISM. Every shard job computes its own membership independently,
	# so two calls must agree exactly or a cell could run in two shards or in
	# none. The exact-cover module proves cover for one evaluation; this
	# proves repeated evaluations match.
	# Each shard must also own real cells: two empty memberships compare equal,
	# and an assignment whose group list never reached this shell produced
	# exactly that while every check above stayed green.
	local k first second
	for ((k = 1; k <= n; k++)); do
		first="$(ifa_fault_shard_select_cells "${k}" "${n}")"
		second="$(ifa_fault_shard_select_cells "${k}" "${n}")"
		[[ "${first}" == "${second}" ]] \
			|| test_ifa_fault_shard_balance_fail "shard ${k}/${n} membership differs between two evaluations"
		[[ "$(printf '%s\n' "${first}" | wc -l | tr -d ' ')" -gt 1 ]] \
			|| test_ifa_fault_shard_balance_fail "shard ${k}/${n} owns no cell besides cell_baseline"
	done

	printf 'test-ifa-fault-injection-shard-balance-cases: pass (%d shards, busiest %ss of %ss budget, spread %ss)\n' \
		"${n}" "${max_load}" "${IFA_FAULT_SHARD_BUDGET_SECONDS}" "$((max_load - min_load))"
}
