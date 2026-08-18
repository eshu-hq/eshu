#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2030,SC2031,SC2034,SC2154,SC2329
# Hermetic cleanup and ownership cases for the documentation ACK barrier.
# Sourced by scripts/test-verify-ifa-fault-injection.sh.


test_ifa_documentation_ack_barrier_cleanup_is_fail_closed() (
	source "${det_lib}"
	source "${documentation_barrier_lib}"
	local query_log response_file rc holder_pid
	query_log="$(mktemp -t ifa-documentation-ack-cleanup.XXXXXX)"
	response_file="$(mktemp -t ifa-documentation-ack-response.XXXXXX)"
	trap 'rm -f "${query_log}" "${response_file}"' EXIT
	holder_pid=41002
	bg_pids=("${holder_pid}")
	ifa_documentation_ack_barrier_active=1
	ifa_documentation_ack_holder_owned=1
	ifa_documentation_ack_ddl_owned=1
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	wait() { return 0; }
	kill() { return 0; }

	ifa_det_pg() {
		printf '%s\n' "$4" >>"${query_log}"
		local response rc_value
		{
			IFS= read -r response
			IFS= read -r rc_value
		} <"${response_file}"
		printf '%s' "${response}"
		return "${rc_value}"
	}
	sleep() { :; }

	printf 'ignored\n9\n' >"${response_file}"
	rc=0
	ifa_documentation_release_ack_barrier test "${holder_pid}" 42002 >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 9 ]] || fail "ACK barrier termination query failure returned ${rc}, want original exit 9"
	[[ " ${bg_pids[*]} " == *" ${holder_pid} "* ]] \
		|| fail "failed ACK barrier termination discarded holder ownership"

	: >"${query_log}"
	printf '0|false\n0\n' >"${response_file}"
	rc=0
	ifa_documentation_release_ack_barrier test "${holder_pid}" 42002 >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "ACK barrier cleanup accepted zero terminated holder sessions"
	[[ " ${bg_pids[*]} " == *" ${holder_pid} "* ]] \
		|| fail "zero-holder ACK cleanup discarded holder ownership"
	if rg --fixed-strings --quiet -- "DROP TRIGGER" "${query_log}"; then
		fail "ACK barrier cleanup dropped the trigger before proving holder termination"
	fi

	: >"${query_log}"
	bg_pids=("${holder_pid}")
	ifa_det_pg() {
		printf '%s\n' "$4" >>"${query_log}"
		if [[ "$4" == *"pg_stat_activity WHERE pid"* ]]; then
			printf '0\n'
			return 0
		fi
		if [[ "$4" == *"DROP TRIGGER"* ]]; then
			return 10
		fi
		if [[ "$4" == *"pg_terminate_backend"* ]]; then
			printf '1|true\n'
		fi
	}
	rc=0
	ifa_documentation_release_ack_barrier test "${holder_pid}" 42002 >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 10 ]] || fail "ACK barrier DDL cleanup failure returned ${rc}, want original exit 10"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "terminated and joined holder remained tracked after DDL cleanup failure"
	[[ -z "${ifa_documentation_ack_holder_pid:-}" ]] \
		|| fail "joined holder PID remained locally owned after DDL cleanup failure"

	# Global/teardown cleanup is idempotent and attempts every DB cleanup step
	# even when an earlier waiter termination query fails.
	: >"${query_log}"
	ifa_documentation_ack_barrier_active=1
	ifa_documentation_ack_waiter_pid=42001
	ifa_documentation_ack_holder_backend_pid=42002
	ifa_documentation_ack_holder_pid="${holder_pid}"
	ifa_documentation_ack_run_id="ifa_doc_123_456_789"
	ifa_documentation_ack_producers_safe=1
	bg_pids=("${holder_pid}")
	ifa_det_pg() {
		printf '%s\n' "$4" >>"${query_log}"
		if [[ "$4" == *"string_agg(waiter.pid"* ]]; then
			return 11
		fi
		if [[ "$4" == *"pg_terminate_backend"* ]]; then
			printf '1|true\n'
		elif [[ "$4" == *"pg_stat_activity WHERE pid"* ]]; then
			printf '0\n'
		fi
		return 0
	}
	rc=0
	ifa_documentation_cleanup_ack_barrier test >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "best-effort ACK cleanup hid a waiter termination query failure"
	rg --fixed-strings --quiet -- "ifa_documentation_ack_waiter_ifa_doc_123_456_789" "${query_log}" \
		|| fail "best-effort ACK cleanup did not census the exact run-tagged waiter"
	if rg --fixed-strings --quiet -- "holder.pid = 42002" "${query_log}"; then
		fail "ACK cleanup released its holder before proving the captured waiter gone"
	fi
	[[ " ${bg_pids[*]} " == *" ${holder_pid} "* && "${ifa_documentation_ack_holder_pid}" == "${holder_pid}" ]] \
		|| fail "ACK cleanup discarded holder ownership after waiter cleanup failed"
	rg --fixed-strings --quiet -- "DROP TRIGGER IF EXISTS" "${query_log}" \
		|| fail "best-effort ACK cleanup did not attempt trigger cleanup after termination failure"
	rg --fixed-strings --quiet -- "DROP FUNCTION IF EXISTS" "${query_log}" \
		|| fail "best-effort ACK cleanup did not attempt function cleanup after termination failure"
	local trigger_drop_line function_drop_line
	trigger_drop_line="$(rg --line-number --max-count 1 --fixed-strings -- "DROP TRIGGER IF EXISTS" "${query_log}" | cut -d: -f1)"
	function_drop_line="$(rg --line-number --max-count 1 --fixed-strings -- "DROP FUNCTION IF EXISTS" "${query_log}" | cut -d: -f1)"
	[[ -n "${trigger_drop_line}" && -n "${function_drop_line}" && "${trigger_drop_line}" -lt "${function_drop_line}" ]] \
		|| fail "best-effort ACK cleanup did not attempt bounded trigger and function drops independently"
)

test_ifa_documentation_cleanup_requires_exact_backend_outcomes() (
	source "${det_lib}"
	source "${documentation_barrier_lib}"
	local case_dir scenario waiter_result holder_result waiter_remaining holder_remaining rc holder_pid=41002
	case_dir="$(mktemp -d -t ifa-documentation-outcomes.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	wait() { return 0; }
	kill() { return 0; }
	sleep() { :; }
	ifa_det_pg() {
		printf '%s\n' "$4" >>"${case_dir}/queries"
		if [[ "$4" == *"string_agg(waiter.pid"* ]]; then
			if [[ "${scenario}" != "already_gone" && "${scenario}" != "partial_start" \
				&& ( ! -s "${case_dir}/waiter-terminated" || "${waiter_result}" != "1|true" ) ]]; then printf '42001'; fi
		elif [[ "$4" == *"string_agg(holder.pid"* ]]; then
			if [[ "${scenario}" != "already_gone" && ( ! -s "${case_dir}/holder-terminated" || "${holder_result}" != "1|true" ) ]]; then printf '42002'; fi
		elif [[ "$4" == *"waiter.pid = 42001"* && "$4" == *"pg_terminate_backend"* ]]; then
			printf '1' >"${case_dir}/waiter-terminated"
			printf '%s' "${waiter_result}"
		elif [[ "$4" == *"holder.pid = 42002"* && "$4" == *"pg_terminate_backend"* ]]; then
			printf '1' >"${case_dir}/holder-terminated"
			printf '%s' "${holder_result}"
		elif [[ "$4" == *"pid = 42001"* ]]; then
			printf '%s' "${waiter_remaining}"
		elif [[ "$4" == *"pid = 42002"* ]]; then
			printf '%s' "${holder_remaining}"
		fi
		return 0
	}
	reset_barrier_state() {
		rm -f "${case_dir}/waiter-terminated" "${case_dir}/holder-terminated"
		ifa_documentation_ack_barrier_active=1
		ifa_documentation_ack_holder_owned=1
		ifa_documentation_ack_ddl_owned=1
		ifa_documentation_ack_barrier_cell="test"
		ifa_documentation_ack_waiter_pid=42001
		ifa_documentation_ack_holder_backend_pid=42002
		ifa_documentation_ack_holder_pid="${holder_pid}"
		ifa_documentation_ack_producers_safe=1
		bg_pids=("${holder_pid}")
	}

	for scenario in waiter_false waiter_zero_live holder_false holder_zero_live; do
		waiter_result="1|true"
		holder_result="1|true"
		waiter_remaining=0
		holder_remaining=0
		case "${scenario}" in
		waiter_false) waiter_result="1|false" ;;
		waiter_zero_live) waiter_result="0|false"; waiter_remaining=1 ;;
		holder_false) holder_result="1|false" ;;
		holder_zero_live) holder_result="0|false"; holder_remaining=1 ;;
		esac
		reset_barrier_state
		rc=0
		ifa_documentation_cleanup_ack_barrier test >/dev/null 2>&1 || rc=$?
		[[ "${rc}" -ne 0 ]] \
			|| fail "ACK cleanup accepted ${scenario}; exact stopped-or-already-gone proof is required"
	done

	scenario=already_gone
	waiter_result="0|false"
	holder_result="0|false"
	waiter_remaining=0
	holder_remaining=0
	reset_barrier_state
	ifa_documentation_cleanup_ack_barrier test \
		|| fail "ACK cleanup rejected captured waiter/holder PIDs that were already proven gone"

	scenario=partial_start
	holder_result="1|true"
	holder_remaining=0
	: >"${case_dir}/queries"
	ifa_documentation_ack_barrier_active=0
	ifa_documentation_ack_holder_owned=1
	ifa_documentation_ack_ddl_owned=0
	ifa_documentation_ack_barrier_cell="test"
	ifa_documentation_ack_run_id="ifa_doc_123_456_789"
	ifa_documentation_ack_waiter_pid=""
	ifa_documentation_ack_holder_backend_pid=""
	ifa_documentation_ack_holder_pid="${holder_pid}"
	ifa_documentation_ack_producers_safe=1
	bg_pids=("${holder_pid}")
	ifa_documentation_cleanup_ack_barrier test \
		|| fail "ACK cleanup rejected a real pre-census holder partial start"
	[[ -s "${case_dir}/holder-terminated" ]] \
		|| fail "pre-census holder partial start was not discovered and terminated"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* && "${ifa_documentation_ack_holder_owned}" == "0" ]] \
		|| fail "pre-census holder partial start was not joined, untracked, and cleared"
	if rg --fixed-strings --quiet -- "CREATE FUNCTION" "${case_dir}/queries" \
		|| rg --fixed-strings --quiet -- "DROP TRIGGER" "${case_dir}/queries" \
		|| rg --fixed-strings --quiet -- "DROP FUNCTION" "${case_dir}/queries"; then
		fail "holder-only partial start created or dropped DDL"
	fi
)

test_ifa_documentation_cleanup_censuses_uncaptured_sessions_and_stops_in_parallel() (
	source "${det_lib}"
	source "${documentation_barrier_lib}"
	local case_dir mode rc waiter_seen holder_seen first_wait last_signal
	case_dir="$(mktemp -d -t ifa-documentation-census.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	ifa_documentation_ack_run_id="ifa_doc_123_456_789"
	sleep() { :; }
	kill() { printf 'kill:%s\n' "$*" >>"${case_dir}/events"; return 0; }
	wait() {
		printf 'wait:%s\n' "$1" >>"${case_dir}/events"
		[[ "${mode}" != "join-fail" ]] || return 127
	}
	ifa_det_pg() {
		printf '%s\n' "$4" >>"${case_dir}/queries"
		if [[ "$4" == *"string_agg(waiter.pid"* ]]; then
			case "${mode}" in
			waiter-ambiguous) printf '42001,42003'; return 0 ;;
			waiter-query-fail) return 12 ;;
			esac
			if [[ "${mode}" == "mismatch" || ! -s "${case_dir}/waiter-terminated" ]]; then printf '42001'; fi
		elif [[ "$4" == *"string_agg(holder.pid"* ]]; then
			case "${mode}" in
			holder-ambiguous) printf '42002,42004'; return 0 ;;
			holder-query-fail) return 13 ;;
			esac
			if [[ ! -s "${case_dir}/holder-terminated" ]]; then printf '42002'; fi
		elif [[ "$4" == *"waiter.pid = 42001"* && "$4" == *"pg_terminate_backend"* ]]; then
			printf '1' >"${case_dir}/waiter-terminated"; printf '1|true'
		elif [[ "$4" == *"holder.pid = 42002"* && "$4" == *"pg_terminate_backend"* ]]; then
			printf '1' >"${case_dir}/holder-terminated"; printf '1|true'
		elif [[ "$4" == *"pg_stat_activity WHERE pid"* ]]; then
			printf '0'
		fi
		return 0
	}
	reset_cleanup() {
		: >"${case_dir}/events"; : >"${case_dir}/queries"
		rm -f "${case_dir}/waiter-terminated" "${case_dir}/holder-terminated"
		ifa_documentation_ack_barrier_active=1
		ifa_documentation_ack_holder_owned=1
		ifa_documentation_ack_ddl_owned=1
		ifa_documentation_ack_barrier_cell="test"
		ifa_documentation_ack_waiter_pid=""
		ifa_documentation_ack_holder_backend_pid=""
		ifa_documentation_ack_holder_pid=""
		ifa_documentation_ack_producers_safe=1
		ifa_documentation_ack_producer_pids=()
		bg_pids=()
	}

	mode=uncaptured
	reset_cleanup
	ifa_documentation_cleanup_ack_barrier test \
		|| fail "cleanup rejected one uncaptured run-tagged waiter and holder"
	waiter_seen="$(rg --count --fixed-strings -- 'waiter.pid = 42001' "${case_dir}/queries")"
	holder_seen="$(rg --count --fixed-strings -- 'holder.pid = 42002' "${case_dir}/queries")"
	[[ "${waiter_seen}" -eq 1 && "${holder_seen}" -eq 1 ]] \
		|| fail "cleanup did not terminate each uncaptured run-tagged backend exactly once"
	local holder_census_query holder_termination_query
	holder_census_query="$(rg --fixed-strings --max-count 1 -- 'string_agg(holder.pid' "${case_dir}/queries")"
	holder_termination_query="$(rg --fixed-strings --max-count 1 -- 'holder.pid = 42002' "${case_dir}/queries")"
	[[ "${holder_census_query}" != *"held.granted"* && "${holder_termination_query}" != *"held.granted"* ]] \
		|| fail "cleanup ignores a run-tagged holder that is still waiting for the advisory key"

	for mode in waiter-ambiguous holder-ambiguous waiter-query-fail holder-query-fail; do
		reset_cleanup
		rc=0
		ifa_documentation_cleanup_ack_barrier test >/dev/null 2>&1 || rc=$?
		[[ "${rc}" -ne 0 ]] || fail "cleanup accepted ${mode} census output"
	done

	mode=mismatch
	reset_cleanup
	ifa_documentation_ack_waiter_pid=42099
	rc=0
	ifa_documentation_cleanup_ack_barrier test >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "cleanup accepted a captured waiter that disagreed with its run-tagged census"
	if rg --fixed-strings --quiet -- 'holder.pid = 42002' "${case_dir}/queries"; then
		fail "cleanup released the holder after waiter PID reconciliation failed"
	fi

	mode=parallel
	reset_cleanup
	bg_pids=(41001 41002 41003)
	ifa_documentation_ack_holder_pid=41003
	ifa_documentation_stop_ack_producers || fail "parallel producer stop rejected joinable producers"
	last_signal="$(rg --line-number --fixed-strings -- 'kill:-s TERM 41002' "${case_dir}/events" | cut -d: -f1)"
	first_wait="$(rg --line-number --max-count 1 -- 'wait:' "${case_dir}/events" | cut -d: -f1)"
	[[ -n "${last_signal}" && -n "${first_wait}" && "${last_signal}" -lt "${first_wait}" ]] \
		|| fail "producer shutdown joined before every producer received TERM"
	if rg --fixed-strings --quiet -- '41003' "${case_dir}/events"; then
		fail "producer shutdown signaled or joined the advisory holder"
	fi

	mode=join-fail
	reset_cleanup
	bg_pids=(41001)
	rc=0
	ifa_documentation_cleanup_ack_barrier test >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "cleanup hid a producer join failure"
	[[ " ${bg_pids[*]} " == *" 41001 "* ]] \
		|| fail "cleanup discarded ownership of an unjoined producer"
	if rg --fixed-strings --quiet -- 'holder.pid = 42002' "${case_dir}/queries"; then
		fail "cleanup released the holder after producer join failure"
	fi
)

test_ifa_documentation_released_ack_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${documentation_barrier_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-fault-lock-owner.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	holder_pid=41003
	survivor_pid=41004
	bg_pids=("${holder_pid}" "${survivor_pid}")
	ifa_documentation_ack_barrier_active=1
	ifa_documentation_ack_holder_owned=1
	ifa_documentation_ack_ddl_owned=1
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"

	ifa_det_pg() {
		if [[ "$4" == *"pg_terminate_backend"* ]]; then
			printf '1|true\n'
		elif [[ "$4" == *"pg_stat_activity WHERE pid"* ]]; then
			printf '0\n'
		fi
		return 0
	}
	wait() { return 0; }
	kill() { printf '%s\n' "$@" >>"${case_dir}/kill.log"; }
	log() { :; }

	ifa_documentation_release_ack_barrier test "${holder_pid}" 42002
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined documentation lock-holder PID remained in tracked ownership"
	: >"${case_dir}/kill.log"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined documentation lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)

test_ifa_documentation_contending_run_never_owns_or_drops_winner_ddl() (
	source "${det_lib}"
	source "${documentation_barrier_lib}"
	declare -F ifa_documentation_start_ack_holder >/dev/null \
		|| fail "documentation ACK barrier has no holder-first startup seam"
	local case_dir holder_pid holder_backend_pid rc
	case_dir="$(mktemp -d -t ifa-documentation-race.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	log_dir="${case_dir}"
	bg_pids=()
	psql() {
		printf '%s\n' "$*" >>"${case_dir}/clients"
		if [[ "$*" == *"ifa_doc_111_222_333"* || "$*" == *"ifa_doc_777_888_999"* ]]; then
			/bin/sleep 30
		fi
	}
	sleep() { :; }
	ifa_det_pg() {
		printf '%s\n' "$4" >>"${case_dir}/queries"
		if [[ "$4" == *"holder.pid::text || '|'"* ]]; then
			if [[ "$4" == *"ifa_doc_777_888_999"* ]]; then
				[[ "${ifa_documentation_ack_holder_owned}" == "1" ]] || return 15
				return 14
			fi
			if [[ "$4" == *"ifa_doc_111_222_333"* && ! -f "${case_dir}/winner-terminated" ]]; then printf '42001|true'; fi
			if [[ "$4" == *"ifa_doc_444_555_666"* ]]; then printf '42002|false'; fi
		elif [[ "$4" == *"string_agg(holder.pid"* ]]; then
			if [[ "$4" == *"ifa_doc_111_222_333"* && ! -f "${case_dir}/winner-terminated" ]]; then printf '42001'; fi
		elif [[ "$4" == *"SELECT ((SELECT count(*)"* ]]; then
			printf '0|1|1'
		elif [[ "$4" == *"CREATE FUNCTION"* ]]; then
			printf 'winner' >"${case_dir}/artifacts"
		elif [[ "$4" == *"holder.pid = 42001"* && "$4" == *"pg_terminate_backend"* ]]; then
			printf '1' >"${case_dir}/winner-terminated"; printf '1|true'
		elif [[ "$4" == *"holder.pid = 42002"* && "$4" == *"pg_terminate_backend"* ]]; then
			printf '1|true'
		elif [[ "$4" == *"pg_stat_activity WHERE pid"* ]]; then
			printf '0'
		elif [[ "$4" == *"DROP TRIGGER"* || "$4" == *"DROP FUNCTION"* ]]; then
			rm -f "${case_dir}/artifacts"
		fi
		return 0
	}

	ifa_documentation_ack_run_id="ifa_doc_111_222_333"
	ifa_documentation_ack_holder_owned=0
	ifa_documentation_ack_ddl_owned=0
	ifa_documentation_start_ack_barrier winner holder_pid holder_backend_pid \
		|| fail "race winner did not acquire the fixed advisory key and install its ACK barrier"
	[[ "${holder_pid}" =~ ^[1-9][0-9]*$ && "${holder_backend_pid}" == "42001" ]] \
		|| fail "holder-first wrapper did not return its exact client/backend ownership"
	[[ -f "${case_dir}/artifacts" ]] || fail "race winner did not retain its DDL artifacts"

	rc=0
	(
		bg_pids=()
		ifa_documentation_ack_run_id="ifa_doc_444_555_666"
		ifa_documentation_ack_holder_owned=0
		ifa_documentation_ack_ddl_owned=0
		local loser_pid loser_backend
		ifa_documentation_start_ack_barrier loser loser_pid loser_backend >/dev/null 2>&1 && exit 20
		[[ "${ifa_documentation_ack_holder_owned}" == "0" && "${ifa_documentation_ack_ddl_owned}" == "0" ]] || exit 21
	) || rc=$?
	[[ "${rc}" -eq 0 ]] || fail "race loser did not fail cleanly before DDL ownership (rc ${rc})"
	[[ -f "${case_dir}/artifacts" ]] || fail "race loser dropped the winner's DDL artifacts"
	[[ "$(rg --count --fixed-strings -- 'CREATE FUNCTION' "${case_dir}/queries")" -eq 1 ]] \
		|| fail "race loser issued CREATE DDL"
	if rg --fixed-strings --quiet -- "DROP TRIGGER" "${case_dir}/queries"; then
		fail "race loser issued DROP DDL"
	fi

	(
		bg_pids=()
		ifa_documentation_ack_run_id="ifa_doc_777_888_999"
		ifa_documentation_ack_holder_owned=0
		local failed_pid failed_backend query_rc=0
		ifa_documentation_start_ack_holder queryfail failed_pid failed_backend >/dev/null 2>&1 || query_rc=$?
		[[ "${query_rc}" -eq 14 && "${ifa_documentation_ack_holder_owned}" == "1" ]] || exit 22
		ifa_det_stop_join_untrack_bg_pid "${ifa_documentation_ack_holder_pid}" TERM
	) || fail "holder-query failure discarded conservative cleanup ownership"

	ifa_documentation_release_ack_barrier winner "${holder_pid}" "${holder_backend_pid}" \
		|| fail "race winner cleanup failed"
)
