#!/usr/bin/env bash
# shellcheck disable=SC2154,SC2034
# Globals this file's functions read are either owned by
# scripts/verify-ifa-fault-injection.sh (use_compose, keep -- the gate reads
# them after ifa_fault_shard_parse_args returns) or are assigned by this
# file's own ifa_fault_shard_parse_args (list_cells, shard_spec, shard_k,
# shard_n, ifa_fault_shard_selected) and then read by a sibling function in
# the SAME sourced shell process -- the same cross-function global pattern
# every other scripts/lib/ifa_fault_injection_*.sh file in this gate uses
# (see e.g. ifa_fault_injection_driver.sh's header). SC2034 disable: several
# of those same globals (use_compose, keep, and the named shard-count
# constant IFA_FAULT_SHARD_DEFAULT_N) are assigned or declared here but read
# only by the calling gate script or by the mirror test module -- a
# cross-file read the analyzer cannot see from this file alone, the mirror
# of the SC2034 case the gate script's own top-of-file disable comment
# documents in the other direction. This file is a plain function library,
# not a script: it deliberately does NOT set `set -euo pipefail` (sourcing
# it would rebind the caller's shell options).
#
# Shard selector for scripts/verify-ifa-fault-injection.sh. The gate drives
# its cells sequentially through a fresh Postgres+NornicDB stack per cell and
# takes 20+ minutes; this lets a caller split that matrix across parallel
# runners.
#
# --list-cells prints the gate's authoritative ordered cell list (one name
# per line) and exits 0. Combined with --shard k/n it prints only that
# shard's cells. Both forms are fully hermetic: no Docker, no compose, no
# binary builds, no Postgres -- ifa_fault_shard_parse_args below exits
# before scripts/verify-ifa-fault-injection.sh ever reaches its
# cassette-existence check, let alone a build or compose step.
#
# --shard k/n runs (or, with --list-cells, only lists) shard k of n.
#
# INPUT DATA VS. PARTITION ALGORITHM
#
# IFA_FAULT_ALL_CELLS and IFA_FAULT_ATOMIC_GROUPS below are the only two
# places a cell name appears in this file -- every function past them is
# name-agnostic and iterates whatever those two arrays currently hold.
#
# - IFA_FAULT_ALL_CELLS is the flat, ordered list of every non-baseline cell
#   the gate dispatches (cell_baseline itself is not in this list -- it is
#   the SHARED, non-family baseline, never partitioned, always repeated per
#   shard; see ifa_fault_shard_select_cells). It is deliberately NOT
#   readonly: scripts/lib/ifa_family_registry.sh is expected to become the
#   real source of this list, and swapping this array's assignment for a
#   registry-derived one is meant to be a one-block edit that touches no
#   function below it.
# - IFA_FAULT_ATOMIC_GROUPS is fault-injection-specific co-location
#   knowledge -- which cells share a digest a registry of cell names would
#   have no way to encode (see the PARTITIONING MODEL comment below) -- and
#   stays owned by this partitioner regardless of where IFA_FAULT_ALL_CELLS
#   comes from. Only cells that MUST travel together in the same shard need
#   an entry; every cell in IFA_FAULT_ALL_CELLS not named in any entry here
#   is its own singleton group.
#
# Nothing below assumes a specific cell count: the partition algorithm
# (ifa_fault_shard_build_groups, ifa_fault_shard_select_cells) walks
# whatever IFA_FAULT_ALL_CELLS currently holds, of whatever length.
#
# PARTITIONING MODEL
#
# Every non-baseline cell is independent EXCEPT for one constraint:
# cell_baseline (the SHARED, non-family baseline) is the sole writer of
# digests[baseline] (scripts/lib/ifa_fault_injection_driver.sh's
# assert_matches_baseline), which every recovery cell reads UNLESS it reads
# a family-scoped baseline digest instead. Every such carve-out is named,
# by construction, as the non-baseline members of an IFA_FAULT_ATOMIC_GROUPS
# entry below whose first element is a family-scoped `cell_baseline_<family>`
# -- read that array for the current roster rather than a prose list here,
# which has already gone stale more than once as families landed
# (inheritance, shell_exec, and workload_dependency were each missing from
# an earlier version of this same sentence, alongside the symbol-runtime
# trio). Each such recovery pair reads the digest written by its own family
# baseline. The deployable_unit family (#5993) additionally runs an
# extra bootstrap-index maintenance pass the shared baseline never runs (see
# scripts/lib/ifa_fault_injection_deployable_unit_cells.sh's header). A cell
# reading a family-scoped baseline digest MUST land in the same shard as the
# cell that writes it, in dispatch order, or it reads an unset digests key --
# that co-location requirement is exactly what an IFA_FAULT_ATOMIC_GROUPS
# entry encodes. cell_deltaretract reads no baseline digest at all
# (scripts/lib/ifa_fault_injection_delivery_cells.sh's header explains why
# comparing it to any baseline digest would be wrong), so it needs no entry
# either.
#
# Assignment is deterministic and stable: group index i (0-based, after
# IFA_FAULT_ATOMIC_GROUPS entries are merged into IFA_FAULT_ALL_CELLS's
# order by ifa_fault_shard_build_groups) goes to shard ((i % n) + 1), so the
# same input data always yields the same shard membership for a given k/n,
# with no dependency on time, environment, or hashing.
IFA_FAULT_SHARD_BASELINE_CELL="cell_baseline"
readonly IFA_FAULT_SHARD_BASELINE_CELL

# Default shard count for `--shard k/4`. A single named constant so changing
# the default is a one-line edit.
readonly IFA_FAULT_SHARD_DEFAULT_N=4

# The ordered cell list -- see "INPUT DATA VS. PARTITION ALGORITHM" above.
# Today this mirrors scripts/verify-ifa-fault-injection.sh's own dispatch
# order by hand; it is expected to become registry-derived later.
IFA_FAULT_ALL_CELLS=(
	cell_killworker
	cell_expirelease
	cell_failgraphwrite
	cell_restartbackend
	cell_killworker_sql
	cell_killworker_code_calls
	cell_killworker_documentation
	cell_killworker_rationale
	cell_duplicatedelivery
	cell_deltaretract
	cell_failgraphwrite_sql
	cell_failgraphwrite_code_calls
	cell_failgraphwrite_documentation
	cell_failgraphwrite_rationale
	cell_baseline_deployable_unit
	cell_killworker_deployable_unit
	cell_failgraphwrite_deployable_unit
	# codeowners_ownership_edges (#5992/#6160): a family-scoped baseline plus
	# two recovery cells, the deployable_unit shape. Its baseline writes
	# digests[baseline_codeowners], so the trio must stay co-located -- see
	# IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_codeowners
	cell_killworker_codeowners
	cell_failgraphwrite_codeowners
	cell_baseline_repo_dependency
	cell_killworker_repo_dependency
	cell_failgraphwrite_repo_dependency
	# submodule_pin_edges (#6002): the same family-scoped-baseline-plus-two
	# shape as codeowners_ownership_edges immediately above, and for the same
	# reason (cell_kind=custom on a table_lock:fact_records blocker -- see
	# scripts/lib/ifa_fault_injection_submodule_pin_cells.sh's header for why
	# this family does NOT use the generic table_lock dispatcher). Its
	# baseline writes digests[baseline_submodule_pin], so the trio must stay
	# co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_submodule_pin
	cell_killworker_submodule_pin
	cell_failgraphwrite_submodule_pin
	# kubernetes_namespace_environment + iam_instance_profile_role (#6309):
	# the first direct-materialization families in this gate, same
	# family-scoped-baseline-plus-two shape as codeowners_ownership_edges
	# above (cell_kind=custom on a table_lock:fact_records blocker). Each
	# baseline writes its own digests[baseline_<family>], so each trio must
	# stay co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_kubernetes_namespace_environment
	cell_killworker_kubernetes_namespace_environment
	cell_failgraphwrite_kubernetes_namespace_environment
	cell_baseline_iam_instance_profile_role
	cell_killworker_iam_instance_profile_role
	cell_failgraphwrite_iam_instance_profile_role
	cell_baseline_inheritance
	cell_killworker_inheritance
	cell_failgraphwrite_inheritance
	cell_baseline_shell_exec
	cell_killworker_shell_exec
	cell_failgraphwrite_shell_exec
	cell_baseline_workload_dependency
	cell_killworker_workload_dependency
	cell_failgraphwrite_workload_dependency
	# handles_route/runs_in/invokes_cloud_action (#5995/#6000/#5997): ONE
	# shared trio baseline (all three families come from one cassette and one
	# builder pass, buildSymbolRuntimeIntentRows), each family's own
	# cell_failgraphwrite_<family>, and each family's runner-lease kill/reclaim
	# cell. The baseline writes
	# digests[baseline_handles_route]/[baseline_runs_in]/
	# [baseline_invokes_cloud_action] (one digest, three keys), so the
	# seven-cell group must stay co-located -- see IFA_FAULT_ATOMIC_GROUPS below.
	cell_baseline_symbol_runtime
	cell_failgraphwrite_handles_route
	cell_failgraphwrite_runs_in
	cell_failgraphwrite_invokes_cloud_action
	cell_killworker_handles_route
	cell_killworker_runs_in
	cell_killworker_invokes_cloud_action
)

# Co-location constraints -- see "INPUT DATA VS. PARTITION ALGORITHM" above.
# Each entry is a space-separated list of two or more cell names (all of
# which must also appear in IFA_FAULT_ALL_CELLS) that must never be split
# across shards. A cell not named in any entry here is its own singleton
# group.
# Deliberately NOT readonly, same rationale as IFA_FAULT_ALL_CELLS above: a
# caller (including a test) must be able to reassign this array in a subshell to
# exercise ifa_fault_shard_build_groups' merge path with synthetic groups.
# Nothing does so today -- stated as available headroom, not as a guard that
# exists. An earlier version of this comment said the real matrix "has only ever
# had one" group and credited a
# test_ifa_fault_shard_multi_group_colocation for covering the multi-group case;
# there are three groups, immediately below, and no function by that name exists
# anywhere in the repo. What DOES check these groups is
# run_ifa_fault_injection_atomic_group_ordering_cases in
# scripts/lib/test-ifa-fault-injection-shard-cases.sh, which asserts each
# group's baseline dispatches before its other members.
#
# One entry per line: the two used to share a physical line separated by a tab,
# which is the likeliest reason "only ever had one" survived several reads.
IFA_FAULT_ATOMIC_GROUPS=(
	"cell_baseline_deployable_unit cell_killworker_deployable_unit cell_failgraphwrite_deployable_unit"
	"cell_baseline_codeowners cell_killworker_codeowners cell_failgraphwrite_codeowners"
	"cell_baseline_repo_dependency cell_killworker_repo_dependency cell_failgraphwrite_repo_dependency"
	"cell_baseline_submodule_pin cell_killworker_submodule_pin cell_failgraphwrite_submodule_pin"
	"cell_baseline_kubernetes_namespace_environment cell_killworker_kubernetes_namespace_environment cell_failgraphwrite_kubernetes_namespace_environment"
	"cell_baseline_iam_instance_profile_role cell_killworker_iam_instance_profile_role cell_failgraphwrite_iam_instance_profile_role"
	"cell_baseline_inheritance cell_killworker_inheritance cell_failgraphwrite_inheritance"
	"cell_baseline_shell_exec cell_killworker_shell_exec cell_failgraphwrite_shell_exec"
	"cell_baseline_workload_dependency cell_killworker_workload_dependency cell_failgraphwrite_workload_dependency"
	# handles_route/runs_in/invokes_cloud_action (#5995/#6000/#5997): one
	# shared baseline serves all three families. Baseline named FIRST:
	# it writes digests[baseline_<family>] for all three families, and each
	# recovery cell compares against its own key -- split across shards,
	# a comparing cell would run where its digest was never written and die on
	# an unset baseline (the same standing hazard every other group here
	# exists to prevent).
	"cell_baseline_symbol_runtime cell_failgraphwrite_handles_route cell_failgraphwrite_runs_in cell_failgraphwrite_invokes_cloud_action cell_killworker_handles_route cell_killworker_runs_in cell_killworker_invokes_cloud_action"
)

# ifa_fault_shard_build_groups walks IFA_FAULT_ALL_CELLS in order and merges
# in any IFA_FAULT_ATOMIC_GROUPS co-location constraint, populating the
# global indexed array ifa_fault_shard_groups: one entry per atomic group
# (a space-separated cell-name list), in the order the group's earliest
# member appears in IFA_FAULT_ALL_CELLS. Every ifa_fault_shard_* function
# below that needs the group list calls this first rather than caching it,
# so a caller (including a test) that mutates IFA_FAULT_ALL_CELLS or
# IFA_FAULT_ATOMIC_GROUPS between calls always sees a freshly rebuilt
# partition -- cheap, since this gate has at most a few dozen cells.
ifa_fault_shard_build_groups() {
	local -A cell_to_group=()
	local group cell
	for group in "${IFA_FAULT_ATOMIC_GROUPS[@]}"; do
		# Intentional word-splitting: an entry is a space-separated cell list.
		# shellcheck disable=SC2086
		for cell in ${group}; do
			cell_to_group["${cell}"]="${group}"
		done
	done

	ifa_fault_shard_groups=()
	local -A emitted=()
	for cell in "${IFA_FAULT_ALL_CELLS[@]}"; do
		[[ -n "${emitted[${cell}]:-}" ]] && continue
		if [[ -n "${cell_to_group[${cell}]:-}" ]]; then
			local g="${cell_to_group[${cell}]}" member
			ifa_fault_shard_groups+=("${g}")
			# shellcheck disable=SC2086
			for member in ${g}; do
				emitted["${member}"]=1
			done
		else
			ifa_fault_shard_groups+=("${cell}")
			emitted["${cell}"]=1
		fi
	done
}

# ifa_fault_shard_all_cells prints the full, ordered cell list (cell_baseline
# first, then every atomic group's members in order) -- one name per line.
# Matches scripts/verify-ifa-fault-injection.sh's own dispatch order exactly,
# so a maintainer diffing the two by eye can confirm IFA_FAULT_ALL_CELLS is
# current.
ifa_fault_shard_all_cells() {
	printf '%s\n' "${IFA_FAULT_SHARD_BASELINE_CELL}"
	ifa_fault_shard_build_groups
	local group member
	for group in "${ifa_fault_shard_groups[@]}"; do
		# shellcheck disable=SC2086
		for member in ${group}; do
			printf '%s\n' "${member}"
		done
	done
}

# ifa_fault_shard_select_cells prints cell_baseline (always -- see the file
# header) plus every cell in the atomic groups assigned to shard k of n.
ifa_fault_shard_select_cells() {
	local k="$1" n="$2"
	printf '%s\n' "${IFA_FAULT_SHARD_BASELINE_CELL}"
	ifa_fault_shard_build_groups
	local i=0 group member
	for group in "${ifa_fault_shard_groups[@]}"; do
		if (( (i % n) + 1 == k )); then
			# shellcheck disable=SC2086
			for member in ${group}; do
				printf '%s\n' "${member}"
			done
		fi
		i=$(( i + 1 ))
	done
}

# ifa_fault_shard_parse_spec validates a "k/n" --shard argument and, on
# success, sets the caller-visible globals shard_k and shard_n. On failure it
# prints a clear message to stderr and returns 2 (never partially setting
# shard_k/shard_n) -- the caller decides whether to `exit` with that code, so
# this function stays safely callable from a test subshell.
ifa_fault_shard_parse_spec() {
	local spec="$1"
	if [[ -z "${spec}" ]]; then
		echo "verify-ifa-fault-injection: --shard requires a k/n argument (e.g. --shard 2/4)" >&2
		return 2
	fi
	if [[ "${spec}" != */* ]]; then
		echo "verify-ifa-fault-injection: --shard argument '${spec}' is malformed, expected k/n" >&2
		return 2
	fi
	local k_raw="${spec%%/*}" n_raw="${spec#*/}"
	if [[ ! "${k_raw}" =~ ^[0-9]+$ || ! "${n_raw}" =~ ^[0-9]+$ ]]; then
		echo "verify-ifa-fault-injection: --shard argument '${spec}' is not numeric, expected k/n" >&2
		return 2
	fi
	# 10# forces base-10 so a leading zero (e.g. "08") is never read as an
	# invalid octal literal.
	local k=$((10#${k_raw})) n=$((10#${n_raw}))
	if (( n < 1 )); then
		echo "verify-ifa-fault-injection: --shard n must be >= 1, got ${n}" >&2
		return 2
	fi
	if (( k < 1 || k > n )); then
		echo "verify-ifa-fault-injection: --shard k must satisfy 1 <= k <= n, got k=${k} n=${n}" >&2
		return 2
	fi
	shard_k="${k}"
	shard_n="${n}"
	return 0
}

# ifa_fault_shard_run invokes cell function "$1" unless a shard was
# requested and this cell was not assigned to it, in which case it prints a
# one-line skip notice and returns 0 without running the cell. cell_baseline
# is always present in ifa_fault_shard_selected (ifa_fault_shard_select_cells
# emits it unconditionally), so it is never skipped.
ifa_fault_shard_run() {
	local cell="$1"
	if [[ -n "${shard_spec:-}" && -z "${ifa_fault_shard_selected[${cell}]:-}" ]]; then
		printf '%s: skipped (not assigned to shard %s/%s)\n' "${cell}" "${shard_k}" "${shard_n}"
		return 0
	fi
	"${cell}"
}

# ifa_fault_shard_parse_args parses scripts/verify-ifa-fault-injection.sh's
# full argument list, including the two flags this file adds. "$1" MUST be
# the gate script's own ${BASH_SOURCE[0]}, so -h/--help's `sed` reads the
# CALLING script's embedded usage doc, not this library file's -- BASH_SOURCE
# is per stack-frame, and this function's frame is this file regardless of
# who calls it. It sets the caller-visible globals use_compose, keep,
# list_cells, shard_spec, shard_k, shard_n, and ifa_fault_shard_selected.
# --list-cells and any --shard parse failure `exit` directly from here: safe,
# because this function always runs sourced into the gate's own shell, never
# a subshell, and it does so before the gate script reaches any stack setup
# or build step.
ifa_fault_shard_parse_args() {
	local self_path="$1"
	shift
	use_compose=1
	keep=0
	list_cells=0
	shard_spec=""
	shard_k=""
	shard_n=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--no-compose) use_compose=0 ;;
		--keep) keep=1 ;;
		--list-cells) list_cells=1 ;;
		--shard)
			shift
			[[ $# -gt 0 ]] || { echo "verify-ifa-fault-injection: --shard requires a k/n argument" >&2; exit 2; }
			shard_spec="$1"
			;;
		-h | --help)
			# Anchored on the "# Usage:" sentinel through the following blank
			# line, not hardcoded line numbers (F-6): the Usage block in
			# scripts/verify-ifa-fault-injection.sh moves as its own header
			# grows, and a hardcoded range silently starts printing shellcheck
			# directives and generic file header prose instead once it does.
			sed -n '/^# Usage:$/,/^$/p' "${self_path}"
			exit 0
			;;
		*)
			echo "verify-ifa-fault-injection: unknown argument: $1" >&2
			exit 2
			;;
		esac
		shift
	done

	if [[ -n "${shard_spec}" ]]; then
		ifa_fault_shard_parse_spec "${shard_spec}" || exit "$?"
	fi

	if [[ "${list_cells}" -eq 1 ]]; then
		if [[ -n "${shard_spec}" ]]; then
			ifa_fault_shard_select_cells "${shard_k}" "${shard_n}"
		else
			ifa_fault_shard_all_cells
		fi
		exit 0
	fi

	declare -gA ifa_fault_shard_selected=()
	if [[ -n "${shard_spec}" ]]; then
		local c
		while IFS= read -r c; do
			[[ -n "${c}" ]] && ifa_fault_shard_selected["${c}"]=1
		done < <(ifa_fault_shard_select_cells "${shard_k}" "${shard_n}")
	fi
}
