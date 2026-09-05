#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the kubernetes_namespace_environment
# fault-injection cells (#6309). Split from
# test-ifa-fault-injection-review-cases.sh for the same reason
# test-ifa-fault-injection-documentation-cases.sh was: the top-level static
# verifier and the review-case module both sit near the repository's 500-line
# cap. Sourced by scripts/test-verify-ifa-fault-injection.sh, and shares that
# script's ${repo_root} and `fail` helper.
#
# These cases are hermetic: every process, database, and signal the cells
# would touch is replaced by a shell stub, so they run in `make pre-pr`
# without Docker, Postgres, or a live gate. They prove the two seams that
# would otherwise fail silently in CI -- a lock that never actually blocked,
# and a teardown signalling a PID it no longer owns -- not that the cells
# recover a real graph, which only the live fault matrix can show.

kubernetes_namespace_environment_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh"
[[ -f "${kubernetes_namespace_environment_cells_lib}" ]] || fail "missing ${kubernetes_namespace_environment_cells_lib}"
bash -n "${kubernetes_namespace_environment_cells_lib}" || fail "ifa_fault_injection_kubernetes_namespace_environment_cells.sh has a syntax error"

# ifa_kubernetes_namespace_environment_start_fact_records_lock's whole job is to guarantee the
# fact_records blocker is actually HELD before the cell kills the
# reducer. If it returned success on a lock it never observed,
# cell_killworker_kubernetes_namespace_environment would kill a handler that had already committed,
# and "non-vacuous: N blocked claimed/running row(s) observed" would become a
# race rather than a fact. This case drives the poll loop to exhaustion with a
# stubbed query and requires a non-zero return, then proves the success path
# reports the granted lock and registers the holder for teardown.
test_ifa_kubernetes_namespace_environment_intent_lock_is_fail_closed() (
	source "${det_lib}"
	source "${kubernetes_namespace_environment_cells_lib}"

	local lock_count_output lock_holder_pid rc
	# Stubs: no psql, no docker, no real waiting. `sleep` is neutered so the
	# 60-iteration timeout path runs instantly instead of taking 15 seconds.
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf '%s\n' "$*" >>"${log_dir}/queries.log"; printf '%s\n' "${lock_count_output}"; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-kubernetes-namespace-environment-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# The lock never appears: every poll reports zero granted locks.
	lock_count_output="0"
	bg_pids=()
	lock_holder_pid=""
	rc=0
	ifa_kubernetes_namespace_environment_start_fact_records_lock nolock lock_holder_pid || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_kubernetes_namespace_environment_start_fact_records_lock returned 0 with no granted lock; the kill cell would then kill an unblocked handler and its non-vacuity claim would be a race"
	rg --quiet -- "blocker_snapshot" "${log_dir}/queries.log" \
		|| fail "ifa_kubernetes_namespace_environment_start_fact_records_lock exhausted its window without snapshotting the fact_records blockers; the live killworkerkubernetesnamespaceenvironment failure named nothing"

	# The lock is granted on the first poll.
	lock_count_output=" 1 "
	bg_pids=()
	lock_holder_pid=""
	ifa_kubernetes_namespace_environment_start_fact_records_lock haslock lock_holder_pid \
		|| fail "ifa_kubernetes_namespace_environment_start_fact_records_lock rejected a granted AccessExclusiveLock"
	[[ "${lock_holder_pid}" =~ ^[0-9]+$ ]] \
		|| fail "ifa_kubernetes_namespace_environment_start_fact_records_lock did not report the holder PID: ${lock_holder_pid}"
	[[ " ${bg_pids[*]} " == *" ${lock_holder_pid} "* ]] \
		|| fail "ifa_kubernetes_namespace_environment_start_fact_records_lock did not track the holder PID; teardown would leave the blocker running into the next cell"
)

# ifa_kubernetes_namespace_environment_release_fact_records_lock joins the lock-holder process, so that PID
# must stop being tracked as owned. If it stayed in bg_pids, teardown_cell
# would signal a PID the shell has already reaped -- and after PID reuse that
# signal lands on an unrelated process. Mirrors the code_calls and
# documentation cases of the same defect.
test_ifa_kubernetes_namespace_environment_released_lock_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${kubernetes_namespace_environment_cells_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-kubernetes-namespace-environment-lock-owner.XXXXXX)"
	trap 'rm -rf "${case_dir}"' EXIT
	holder_pid=41005
	survivor_pid=41006
	bg_pids=("${holder_pid}" "${survivor_pid}")
	use_compose=0
	FAULT_COMPOSE_PROJECT="test"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"

	ifa_det_pg() { return 0; }
	wait() { return 0; }
	kill() { printf '%s\n' "$@" >>"${case_dir}/kill.log"; }
	log() { :; }

	ifa_kubernetes_namespace_environment_release_fact_records_lock test "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined kubernetes-namespace-environment lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined kubernetes-namespace-environment lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)

# Postgres stores at most 64 bytes of application_name: a longer lock name is
# silently truncated, the grant poll on the full name misses forever, and
# the release's terminate-by-name misses too. The live
# killworkerkubernetesnamespaceenvironment cell died exactly this way with
# an 83-char name while holding its own granted lock. The start function
# must fail closed on an overlong name instead of polling a match that can
# never arrive -- even when the grant query itself reports success.
test_ifa_kubernetes_namespace_environment_overlong_lock_name_is_rejected() (
	source "${det_lib}"
	source "${kubernetes_namespace_environment_cells_lib}"

	local lock_holder_pid rc long_cell
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf ' 1 \n'; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-kubernetes-namespace-environment-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# 17-char ifa_k8s_env_lock_ prefix plus a cell this long exceeds
	# the 63-byte application_name cap.
	long_cell="killworker_kubernetes_namespace_environment_pad001"
	((${#long_cell} > 47)) || fail "test cell name too short to exercise the 63-byte application_name guard"
	bg_pids=()
	lock_holder_pid=""
	rc=0
	ifa_kubernetes_namespace_environment_start_fact_records_lock "${long_cell}" lock_holder_pid || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_kubernetes_namespace_environment_start_fact_records_lock accepted a lock name Postgres truncates; the grant poll would miss forever"
)

# The start function takes the lock under one application_name and the release
# function terminates it by name: if the two literals disagree, release
# terminates nothing, the holder leaks through its whole pg_sleep, and the
# replacement reducer's fact reads stay blocked for minutes while the cell
# still reports green. Surname-level typo bait after any rename, so pin the
# agreement behaviorally: capture both names off the stubbed query log and
# require them equal.
test_ifa_kubernetes_namespace_environment_lock_start_and_release_agree_on_name() (
	source "${det_lib}"
	source "${kubernetes_namespace_environment_cells_lib}"

	local lock_holder_pid start_name release_name
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf '%s\n' "$*" >>"${log_dir}/queries.log"; printf ' 1 \n'; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-kubernetes-namespace-environment-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	bg_pids=()
	lock_holder_pid=""
	ifa_kubernetes_namespace_environment_start_fact_records_lock samecell lock_holder_pid \
		|| fail "ifa_kubernetes_namespace_environment_start_fact_records_lock rejected a granted lock"
	ifa_kubernetes_namespace_environment_release_fact_records_lock samecell "${lock_holder_pid}"
	start_name="$(rg -o -- "application_name = '[^']*'" "${log_dir}/queries.log" | head -n 1)"
	release_name="$(rg -- "pg_terminate_backend" "${log_dir}/queries.log" | rg -o -- "application_name = '[^']*'" | head -n 1)"
	[[ -n "${start_name}" && "${start_name}" == "${release_name}" ]] \
		|| fail "lock start names ${start_name:-<none>} but release terminates ${release_name:-<none>}; the holder would leak"
)

run_ifa_fault_injection_kubernetes_namespace_environment_cases() {
	test_ifa_kubernetes_namespace_environment_intent_lock_is_fail_closed
	test_ifa_kubernetes_namespace_environment_released_lock_holder_is_not_torn_down_twice
	test_ifa_kubernetes_namespace_environment_overlong_lock_name_is_rejected
	test_ifa_kubernetes_namespace_environment_lock_start_and_release_agree_on_name
	test_ifa_direct_family_retry_attempts_above_counts_attempts_not_rows
	test_ifa_kubernetes_namespace_environment_kill_cell_proves_attempts_above_baseline
}

# ifa_fault_assert_retry_attempts_above compares TOTAL excess attempts
# (sum(attempt_count - 1)), not the COUNT of rows with attempt_count > 1. Each
# new-family cassette creates exactly one targeted work item, so the count
# form saturates at 1: a single natural retry in the fault-free baseline makes
# the baseline 1, the forced kill adds another attempt to the SAME row, the
# kill-run count stays 1, and 1 > 1 false-fails a correct recovery. The sum
# form has no ceiling -- the kill always adds attempts the baseline lacked.
# Hermetic: stubbed ifa_det_pg replays scripted sums, sleep neutered.
test_ifa_direct_family_retry_attempts_above_counts_attempts_not_rows() (
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_direct_family_live.sh
	source "${repo_root}/scripts/lib/ifa_direct_family_live.sh"

	declare -F ifa_fault_assert_retry_attempts_above >/dev/null \
		|| fail "ifa_fault_assert_retry_attempts_above is not defined; the single-row kill cells saturate the row-count form at 1"

	sleep() { :; }
	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	# Scripted sums, one per line: the assert helper reads them through a
	# command substitution (a subshell), so shell-array state would not
	# survive between polls -- a file does.
	pg_script_dir="$(mktemp -d -t ifa-retry-attempts-pg.XXXXXX)"
	trap 'rm -rf "${pg_script_dir}"' EXIT
	ifa_det_pg() {
		local line rest
		line="$(head -n 1 "${pg_script_dir}/sums")"
		rest="$(tail -n +2 "${pg_script_dir}/sums")"
		printf '%s\n' "${rest}" >"${pg_script_dir}/sums"
		printf '%s\n' "${line}"
	}

	# Saturation recovery: baseline already 1 (one natural retry), kill-run
	# polls 1 then 2 (the forced kill's extra attempt). Must pass.
	printf '1\n2\n2\n2\n2\n' >"${pg_script_dir}/sums"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" 1 5 "kubernetes_namespace_materialization" >/dev/null \
		|| fail "retry-attempts assert failed on 1-then-2 excess attempts above a baseline of 1; the saturated single-row kill cell would false-fail"

	# Stuck: polls never exceed the baseline. Must fail closed.
	printf '1\n1\n1\n' >"${pg_script_dir}/sums"
	rc=0
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" 1 3 "kubernetes_namespace_materialization" >/dev/null || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "retry-attempts assert passed while excess attempts never exceeded the baseline; an inert fault would green the kill cell"
)

# The k8s kill cell must prove attempt TOTALS above baseline, scoped to its
# own domain -- not the saturating row-count form, and not another family's
# domain. Static pin on the call: the behavior above proves what the helper
# means, this proves the cell reaches for it.
test_ifa_kubernetes_namespace_environment_kill_cell_proves_attempts_above_baseline() (
	rg --quiet -- "ifa_fault_assert_retry_attempts_above" "${kubernetes_namespace_environment_cells_lib}" \
		|| fail "kill-worker k8s cell does not call ifa_fault_assert_retry_attempts_above; the row-count form saturates at 1 for its single work item"
	# The call wraps across a backslash-newline, so match multiline: the
	# domain must reach THIS helper, not merely appear elsewhere in the file.
	rg -U --quiet -- '(?s)ifa_fault_assert_retry_attempts_above.{0,400}"kubernetes_namespace_materialization"' "${kubernetes_namespace_environment_cells_lib}" \
		|| fail "k8s kill cell does not pass its own materialization domain to ifa_fault_assert_retry_attempts_above"
)
