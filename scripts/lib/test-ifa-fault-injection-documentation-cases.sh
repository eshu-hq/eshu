#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the documentation_edges fault-injection
# cells (#5994). Split from test-ifa-fault-injection-review-cases.sh to stay
# under the repository's 500-line cap; mirrors that file's two code_calls
# cases (test_ifa_code_call_fresh_stack_intent_guard_is_typed_and_fail_closed,
# test_ifa_fault_released_lock_holder_is_not_torn_down_twice) exactly, one
# family later, because documentation_edges adopted the same
# fresh-intents-guard / intent-lock shape cell_killworker_code_calls
# established. Sourced by scripts/test-verify-ifa-fault-injection.sh
# alongside test-ifa-fault-injection-review-cases.sh, so the top-level static
# verifier stays below the repository's 500-line cap; both files share that
# script's ${documentation_cells_lib}/${det_lib}/${driver_lib} globals and
# `fail` helper.

test_ifa_documentation_fresh_stack_intent_guard_is_typed_and_fail_closed() (
	source "${documentation_cells_lib}"
	declare -F ifa_documentation_require_fresh_intents >/dev/null \
		|| fail "documentation cells do not expose ifa_documentation_require_fresh_intents"

	local query_output query_rc output rc
	ifa_det_pg() {
		printf '%s' "${query_output}"
		return "${query_rc}"
	}

	query_output="ignored"
	query_rc=7
	rc=0
	output="$(ifa_documentation_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -eq 7 ]] || fail "failed documentation precondition query returned ${rc}, want original exit 7"
	[[ "${output}" == *"precondition query FAILED (exit 7)"* ]] \
		|| fail "failed documentation precondition query did not preserve exit 7: ${output}"
	[[ "${output}" != *"survived fresh_stack"* ]] \
		|| fail "failed documentation precondition query was misreported as stale intents: ${output}"

	query_output=""
	query_rc=0
	rc=0
	output="$(ifa_documentation_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "empty documentation precondition count was accepted"
	[[ "${output}" == *"returned empty output"* ]] \
		|| fail "empty documentation precondition count was not diagnosed distinctly: ${output}"

	query_output="not-a-count"
	query_rc=0
	rc=0
	output="$(ifa_documentation_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "non-numeric documentation precondition count was accepted"
	[[ "${output}" == *"returned non-numeric output"* ]] \
		|| fail "non-numeric documentation precondition count was not diagnosed distinctly: ${output}"

	query_output=$' 3\n'
	query_rc=0
	rc=0
	output="$(ifa_documentation_require_fresh_intents test project 1 postgresql://unused compose.yaml 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "stale documentation intents were accepted"
	[[ "${output}" == *"3 documentation_edges intent row(s) survived fresh_stack"* ]] \
		|| fail "stale documentation intents were not reported as stale: ${output}"

	query_output=$' 0\n'
	query_rc=0
	ifa_documentation_require_fresh_intents test project 1 postgresql://unused compose.yaml >/dev/null \
		|| fail "zero documentation intents did not continue"
)

test_ifa_documentation_released_lock_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${documentation_cells_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-fault-lock-owner.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	holder_pid=41003
	survivor_pid=41004
	bg_pids=("${holder_pid}" "${survivor_pid}")
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"

	ifa_det_pg() { return 0; }
	wait() { return 0; }
	kill() { printf '%s\n' "$@" >>"${case_dir}/kill.log"; }
	log() { :; }

	ifa_documentation_release_intent_lock test "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined documentation lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined documentation lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)
