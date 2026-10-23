#!/usr/bin/env bash
# shellcheck disable=SC2154,SC2034
# Globals this file's functions read are either owned by
# scripts/verify-ifa-fault-injection.sh (use_compose, keep -- the gate reads
# them after ifa_fault_shard_parse_args returns; work_dir receives the durable
# current-cell marker before each selected cell runs) or are assigned by this
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
# IFA_FAULT_ALL_CELLS and IFA_FAULT_ATOMIC_GROUPS (both owned by
# scripts/lib/ifa_fault_shard_cells.sh, sourced below) are the only two
# places a cell name appears -- every function past them is
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
# Assignment is weighted and deterministic (ifa_fault_shard_assign_groups):
# each group weighs the sum of its cells' measured seconds
# (IFA_FAULT_CELL_WEIGHT_SECONDS), groups are taken heaviest first (ties by
# group index), and each goes to the least-loaded shard (ties to the lowest
# shard number). The same input data always yields the same membership for a
# given k/n, with no dependency on time, environment, or hashing. Groups were
# once dealt by position (group i to shard (i % n) + 1); every new trio then
# reshuffled every later group, and shards drifted to the 35-minute timeout.
IFA_FAULT_SHARD_BASELINE_CELL="cell_baseline"
readonly IFA_FAULT_SHARD_BASELINE_CELL

# Default shard count for `--shard k/8`. A single named constant so changing
# the default is a one-line edit.
readonly IFA_FAULT_SHARD_DEFAULT_N=8

# Most estimated cell seconds any one shard may carry. The fault-injection job
# times out at 35 minutes (2100s). Before the first cell, each shard spends
# ~210s on setup, the tagged unit tests, and host builds, then ~13s on its own
# cell_baseline (main run 39942214133). Real runs exceed the median-based
# estimate by up to 325s per shard (five main runs, 2026-10-23), because any
# cell can take an extra one-minute wait. 2100 - 225 - 375 = 1500 keeps that
# allowance. When the shard-balance mirror reports the busiest shard over
# this, raise IFA_FAULT_SHARD_DEFAULT_N and the workflow matrix with it,
# rather than the job timeout.
readonly IFA_FAULT_SHARD_BUDGET_SECONDS=1500

# The ordered cell list and co-location constraints live in
# scripts/lib/ifa_fault_shard_cells.sh (split out at the 500-line cap) and
# are sourced here through a variable path plus an rg pin, the same
# mechanism as the ifa_live_gate_* splits: the sourced-to-triggered drift
# walk cannot resolve the path, so nothing else would notice the source
# line being deleted -- and a deleted source leaves every function below
# iterating unset arrays.
ifa_fault_shard_cells_lib="${BASH_SOURCE[0]%/*}/ifa_fault_shard_cells.sh"
rg --quiet --fixed-strings --line-regexp -- 'source "${ifa_fault_shard_cells_lib}"' "${BASH_SOURCE[0]}" ||
	{
		printf 'ifa_fault_shard: cell roster must be sourced from scripts/lib/ifa_fault_shard_cells.sh\n' >&2
		exit 1
	}
# shellcheck source=scripts/lib/ifa_fault_shard_cells.sh
source "${ifa_fault_shard_cells_lib}"


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

# ifa_fault_shard_group_weights prints one "<seconds> <group index>" line per
# atomic group, in group order: the sum of its cells' entries in
# IFA_FAULT_CELL_WEIGHT_SECONDS, with IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS
# for a cell the table does not list. Returns non-zero, naming the cell, on
# a weight that is not a positive integer rather than letting the arithmetic
# fail where no caller would see it.
ifa_fault_shard_group_weights() {
	ifa_fault_shard_build_groups
	local i member w cell_w
	for i in "${!ifa_fault_shard_groups[@]}"; do
		w=0
		# shellcheck disable=SC2086
		for member in ${ifa_fault_shard_groups[i]}; do
			cell_w="${IFA_FAULT_CELL_WEIGHT_SECONDS[${member}]:-${IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS:-}}"
			if [[ ! "${cell_w}" =~ ^[1-9][0-9]*$ ]]; then
				printf 'ifa_fault_shard: weight for %s must be a positive integer of seconds, got %s\n' "${member}" "${cell_w:-<unset>}" >&2
				return 1
			fi
			w=$((w + cell_w))
		done
		printf '%s %s\n' "${w}" "${i}"
	done
}

# ifa_fault_shard_assign_groups assigns every atomic group to one of n shards
# and sets two caller-visible globals: ifa_fault_shard_group_shard (group
# index -> shard number) and ifa_fault_shard_load (shard number -> estimated
# seconds, excluding the per-shard cell_baseline every shard repeats).
# Heaviest group first, ties by group index; each goes to the least-loaded
# shard, ties to the lowest shard number. The sort runs under LC_ALL=C so
# every shard job computes the identical partition. Returns non-zero when a
# weight is malformed or a group ends up with no shard: a partial partition
# would run a smaller matrix and still pass.
ifa_fault_shard_assign_groups() {
	local n="$1"
	declare -ga ifa_fault_shard_group_shard=() ifa_fault_shard_load=()
	local k w i best weights sorted
	for ((k = 1; k <= n; k++)); do
		ifa_fault_shard_load[k]=0
	done
	# Built here, in this shell: ifa_fault_shard_group_weights below runs in a
	# command substitution, so the ifa_fault_shard_groups it builds never
	# reaches the caller, and ifa_fault_shard_select_cells reads this one.
	ifa_fault_shard_build_groups
	# Captured, not read from a process substitution, so a failure here
	# reaches the caller instead of vanishing.
	weights="$(ifa_fault_shard_group_weights)" || return 1
	sorted="$(printf '%s\n' "${weights}" | LC_ALL=C sort -k1,1nr -k2,2n)" || return 1
	while read -r w i; do
		best=1
		for ((k = 2; k <= n; k++)); do
			((ifa_fault_shard_load[k] < ifa_fault_shard_load[best])) && best="${k}"
		done
		ifa_fault_shard_group_shard[i]="${best}"
		ifa_fault_shard_load[best]=$((ifa_fault_shard_load[best] + w))
	done < <(printf '%s\n' "${sorted}")
	for i in "${!ifa_fault_shard_groups[@]}"; do
		if [[ -z "${ifa_fault_shard_group_shard[i]:-}" ]]; then
			printf 'ifa_fault_shard: group %s (%s) was assigned to no shard\n' "${i}" "${ifa_fault_shard_groups[i]}" >&2
			return 1
		fi
	done
}

# ifa_fault_shard_loads prints one "<shard> <estimated seconds>" line per
# shard of n, from ifa_fault_shard_assign_groups.
ifa_fault_shard_loads() {
	local n="$1" k
	ifa_fault_shard_assign_groups "${n}" || return 1
	for ((k = 1; k <= n; k++)); do
		printf '%s %s\n' "${k}" "${ifa_fault_shard_load[k]}"
	done
}

# ifa_fault_shard_check_weights returns non-zero, naming the entry, when
# IFA_FAULT_CELL_WEIGHT_SECONDS holds a key that is not in IFA_FAULT_ALL_CELLS
# (a stale entry: its renamed successor silently costs the default) or a
# weight that is not a positive integer, or when the default weight is not a
# positive integer.
ifa_fault_shard_check_weights() {
	local -A known=()
	local cell rc=0
	for cell in "${IFA_FAULT_ALL_CELLS[@]}"; do
		known["${cell}"]=1
	done
	[[ "${IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS:-}" =~ ^[1-9][0-9]*$ ]] || {
		printf 'ifa_fault_shard: IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS must be a positive integer, got %s\n' "${IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS:-<unset>}" >&2
		rc=1
	}
	for cell in "${!IFA_FAULT_CELL_WEIGHT_SECONDS[@]}"; do
		if [[ -z "${known[${cell}]:-}" ]]; then
			printf 'ifa_fault_shard: weight for %s names no cell in IFA_FAULT_ALL_CELLS (stale entry)\n' "${cell}" >&2
			rc=1
		fi
		if [[ ! "${IFA_FAULT_CELL_WEIGHT_SECONDS[${cell}]}" =~ ^[1-9][0-9]*$ ]]; then
			printf 'ifa_fault_shard: weight for %s must be a positive integer of seconds, got %s\n' "${cell}" "${IFA_FAULT_CELL_WEIGHT_SECONDS[${cell}]}" >&2
			rc=1
		fi
	done
	return "${rc}"
}

# ifa_fault_shard_check_budget returns non-zero when any shard of n carries
# more than IFA_FAULT_SHARD_BUDGET_SECONDS of estimated cell time, printing
# every shard's estimate and the fix.
ifa_fault_shard_check_budget() {
	local n="$1" k over=0
	ifa_fault_shard_assign_groups "${n}" || return 1
	for ((k = 1; k <= n; k++)); do
		((ifa_fault_shard_load[k] > IFA_FAULT_SHARD_BUDGET_SECONDS)) && over=1
	done
	((over == 0)) && return 0
	printf 'ifa_fault_shard: a shard is over its %ss budget of estimated cell time:\n' "${IFA_FAULT_SHARD_BUDGET_SECONDS}" >&2
	for ((k = 1; k <= n; k++)); do
		printf '  shard %s/%s: %ss\n' "${k}" "${n}" "${ifa_fault_shard_load[k]}" >&2
	done
	printf 'ifa_fault_shard: raise IFA_FAULT_SHARD_DEFAULT_N, the workflow matrix, and the registry check_names together; do not raise the job timeout instead\n' >&2
	return 1
}

# ifa_fault_shard_select_cells prints cell_baseline (always -- see the file
# header) plus every cell in the atomic groups ifa_fault_shard_assign_groups
# gives to shard k of n, in IFA_FAULT_ALL_CELLS order (the gate's dispatch
# order, so a family baseline still precedes its recovery cells).
ifa_fault_shard_select_cells() {
	local k="$1" n="$2"
	ifa_fault_shard_assign_groups "${n}" || return 1
	printf '%s\n' "${IFA_FAULT_SHARD_BASELINE_CELL}"
	local i member
	for i in "${!ifa_fault_shard_groups[@]}"; do
		if ((ifa_fault_shard_group_shard[i] == k)); then
			# shellcheck disable=SC2086
			for member in ${ifa_fault_shard_groups[i]}; do
				printf '%s\n' "${member}"
			done
		fi
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
		echo "verify-ifa-fault-injection: --shard requires a k/n argument (e.g. --shard 2/8)" >&2
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
	printf '%s\n' "${cell}" >"${work_dir}/current-cell"
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
			ifa_fault_shard_select_cells "${shard_k}" "${shard_n}" || exit 1
		else
			ifa_fault_shard_all_cells
		fi
		exit 0
	fi

	declare -gA ifa_fault_shard_selected=()
	if [[ -n "${shard_spec}" ]]; then
		# Captured first so a failed partition aborts the gate rather than
		# leaving only cell_baseline selected.
		local c selected
		selected="$(ifa_fault_shard_select_cells "${shard_k}" "${shard_n}")" || exit 1
		while IFS= read -r c; do
			[[ -n "${c}" ]] && ifa_fault_shard_selected["${c}"]=1
		done < <(printf '%s\n' "${selected}")
	fi
}
