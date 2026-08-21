#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the submodule_pin_edges fault-injection
# cells (#6002), mirroring test-ifa-fault-injection-codeowners-cases.sh's
# shape exactly (same table_lock:fact_records mechanism -- see
# ifa_fault_injection_submodule_pin_cells.sh's header for why this family is
# cell_kind=custom rather than the generic dispatcher). Sourced by
# scripts/test-verify-ifa-fault-injection.sh, and shares that script's
# ${repo_root} and `fail` helper.
#
# These cases are hermetic: every process, database, and signal the cells
# would touch is replaced by a shell stub, so they run in `make pre-pr`
# without Docker, Postgres, or a live gate. They prove the two seams that
# would otherwise fail silently in CI -- a lock that never actually blocked,
# and a teardown signalling a PID it no longer owns -- not that the cells
# recover a real graph, which only the live fault matrix can show.

submodule_pin_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_submodule_pin_cells.sh"
[[ -f "${submodule_pin_cells_lib}" ]] || fail "missing ${submodule_pin_cells_lib}"
bash -n "${submodule_pin_cells_lib}" || fail "ifa_fault_injection_submodule_pin_cells.sh has a syntax error"

# ifa_submodule_pin_start_fact_records_lock's whole job is to guarantee the
# fact_records blocker is actually HELD before the cell kills the reducer.
# If it returned success on a lock it never observed,
# cell_killworker_submodule_pin would kill a handler that had already
# committed, and "non-vacuous: N blocked claimed/running row(s) observed"
# would become a race rather than a fact. This case drives the poll loop to
# exhaustion with a stubbed query and requires a non-zero return, then
# proves the success path reports the granted lock and registers the holder
# for teardown.
test_ifa_submodule_pin_fact_records_lock_is_fail_closed() (
	source "${det_lib}"
	source "${submodule_pin_cells_lib}"

	local lock_count_output lock_holder_pid rc
	# Stubs: no psql, no docker, no real waiting. `sleep` is neutered so the
	# 60-iteration timeout path runs instantly instead of taking 15 seconds.
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf '%s\n' "${lock_count_output}"; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-submodule-pin-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# The lock never appears: every poll reports zero granted locks.
	lock_count_output="0"
	bg_pids=()
	lock_holder_pid=""
	rc=0
	ifa_submodule_pin_start_fact_records_lock nolock lock_holder_pid || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_submodule_pin_start_fact_records_lock returned 0 with no granted lock; the kill cell would then kill an unblocked handler and its non-vacuity claim would be a race"

	# The lock is granted on the first poll.
	lock_count_output=" 1 "
	bg_pids=()
	lock_holder_pid=""
	ifa_submodule_pin_start_fact_records_lock haslock lock_holder_pid \
		|| fail "ifa_submodule_pin_start_fact_records_lock rejected a granted AccessExclusiveLock"
	[[ "${lock_holder_pid}" =~ ^[0-9]+$ ]] \
		|| fail "ifa_submodule_pin_start_fact_records_lock did not report the holder PID: ${lock_holder_pid}"
	[[ " ${bg_pids[*]} " == *" ${lock_holder_pid} "* ]] \
		|| fail "ifa_submodule_pin_start_fact_records_lock did not track the holder PID; teardown would leave the blocker running into the next cell"
)

# ifa_submodule_pin_release_fact_records_lock joins the lock-holder process,
# so that PID must stop being tracked as owned. If it stayed in bg_pids,
# teardown_cell would signal a PID the shell has already reaped -- and after
# PID reuse that signal lands on an unrelated process. Mirrors the
# code_calls, documentation, and codeowners cases of the same defect.
test_ifa_submodule_pin_released_lock_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${submodule_pin_cells_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-submodule-pin-lock-owner.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	holder_pid=41007
	survivor_pid=41008
	bg_pids=("${holder_pid}" "${survivor_pid}")
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"

	ifa_det_pg() { return 0; }
	wait() { return 0; }
	kill() { printf '%s\n' "$@" >>"${case_dir}/kill.log"; }
	log() { :; }

	ifa_submodule_pin_release_fact_records_lock test "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined submodule-pin lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined submodule-pin lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)

run_ifa_fault_injection_submodule_pin_cases() {
	test_ifa_submodule_pin_fact_records_lock_is_fail_closed
	test_ifa_submodule_pin_released_lock_holder_is_not_torn_down_twice
}
