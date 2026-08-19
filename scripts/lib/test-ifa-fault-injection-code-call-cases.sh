#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Focused behavioral regressions for code_calls fault-injection helpers.
# Sourced by scripts/test-verify-ifa-fault-injection.sh.

test_ifa_code_call_fresh_stack_intent_guard_is_typed_and_fail_closed() (
	source "${code_call_cells_lib}"
	declare -F ifa_code_call_require_fresh_intents >/dev/null \
		|| fail "code-call cells do not expose ifa_code_call_require_fresh_intents"

	local query_output query_rc output rc
	ifa_det_pg() {
		printf '%s' "${query_output}"
		return "${query_rc}"
	}

	query_output="ignored"
	query_rc=7
	rc=0
	output="$(ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -eq 7 ]] || fail "failed code-call precondition query returned ${rc}, want original exit 7"
	[[ "${output}" == *"precondition query FAILED (exit 7)"* ]] \
		|| fail "failed code-call precondition query did not preserve exit 7: ${output}"
	[[ "${output}" != *"survived fresh_stack"* ]] \
		|| fail "failed code-call precondition query was misreported as stale intents: ${output}"

	query_output=""
	query_rc=0
	rc=0
	output="$(ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "empty code-call precondition count was accepted"
	[[ "${output}" == *"returned empty output"* ]] \
		|| fail "empty code-call precondition count was not diagnosed distinctly: ${output}"

	query_output="not-a-count"
	query_rc=0
	rc=0
	output="$(ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "non-numeric code-call precondition count was accepted"
	[[ "${output}" == *"returned non-numeric output"* ]] \
		|| fail "non-numeric code-call precondition count was not diagnosed distinctly: ${output}"

	query_output=$' 3\n'
	query_rc=0
	rc=0
	output="$(ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "stale code-call intents were accepted"
	[[ "${output}" == *"3 code_calls intent row(s) survived fresh_stack"* ]] \
		|| fail "stale code-call intents were not reported as stale: ${output}"

	query_output=$' 0\n'
	query_rc=0
	ifa_code_call_require_fresh_intents test project 1 postgresql://unused compose.yaml >/dev/null \
		|| fail "zero code-call intents did not continue"
)

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

	# Drives the LIVE primitive, not the family wrapper. This family's kill cell
	# now goes through the generic dispatcher, which calls
	# ifa_fault_release_shared_intent_lock itself (ifa_fault_generic_cells.sh:198)
	# with the family name as the lock namespace; the old
	# ifa_code_call_release_intent_lock wrapper had no callers left and was
	# deleted. Testing the wrapper would have been a helper-only probe over an
	# unexercised subject -- the invariant below (a joined lock-holder PID must
	# stop being tracked, so teardown cannot signal a reused PID) is worth
	# keeping, but only against the code path that actually runs.
	ifa_fault_release_shared_intent_lock test code_calls "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined code-call lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined code-call lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)
