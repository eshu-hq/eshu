#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the Ifa fault-injection helper libraries.
# Sourced by scripts/test-verify-ifa-fault-injection.sh so the top-level static
# verifier stays below the repository's 500-line cap.

test_ifa_fault_released_lock_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${code_call_cells_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-fault-lock-owner.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	holder_pid=41001
	survivor_pid=41002
	bg_pids=("${holder_pid}" "${survivor_pid}")
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"

	ifa_det_pg() { return 0; }
	wait() { return 0; }
	kill() { printf '%s\n' "$@" >>"${case_dir}/kill.log"; }
	log() { :; }

	ifa_code_call_release_intent_lock test "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined code-call lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined code-call lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)

run_ifa_fault_injection_review_cases() {
	test_ifa_fault_released_lock_holder_is_not_torn_down_twice
}
