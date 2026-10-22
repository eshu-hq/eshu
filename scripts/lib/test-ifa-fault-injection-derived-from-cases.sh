#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the derived_from
# fault-injection cells (#6228 derived_from live leaf). Split into its own module for
# the same reason test-ifa-fault-injection-documentation-cases.sh was: the
# top-level static verifier and the review-case module both sit near the
# repository's 500-line cap. Sourced by
# scripts/test-verify-ifa-fault-injection.sh, and shares that script's
# ${repo_root} and `fail` helper.
#
# These cases are hermetic: every process, database, and signal the cells
# would touch is replaced by a shell stub, so they run in `make pre-pr`
# without Docker, Postgres, or a live gate. They prove the two seams that
# would otherwise fail silently in CI -- a lock that never actually blocked,
# and a teardown signalling a PID it no longer owns -- not that the cells
# recover a real graph, which only the live fault matrix can show.
#
# There is deliberately no readiness-wait case here (contrast the crc-image
# sibling): this handler carries no in-handler readiness gate before its
# fact load, so the kill cell takes the lock first with no wait to test.

derived_from_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_derived_from_cells.sh"
[[ -f "${derived_from_cells_lib}" ]] || fail "missing ${derived_from_cells_lib}"
bash -n "${derived_from_cells_lib}" || fail "ifa_fault_injection_derived_from_cells.sh has a syntax error"

# ifa_derived_from_start_fact_records_lock's whole job is to guarantee the
# fact_records blocker is actually HELD before the cell kills the
# reducer. If it returned success on a lock it never observed,
# cell_killworker_derived_from would kill a handler that
# had already committed, and "non-vacuous: N blocked claimed/running row(s)
# observed" would become a race rather than a fact. This case drives the poll
# loop to exhaustion with a stubbed query and requires a non-zero return,
# then proves the success path reports the granted lock and registers the
# holder for teardown.
test_ifa_derived_from_intent_lock_is_fail_closed() (
	source "${det_lib}"
	source "${derived_from_cells_lib}"

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
	log_dir="$(mktemp -d -t ifa-derived-from-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# The lock never appears: every poll reports zero granted locks.
	lock_count_output="0"
	bg_pids=()
	lock_holder_pid=""
	rc=0
	ifa_derived_from_start_fact_records_lock nolock lock_holder_pid || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_derived_from_start_fact_records_lock returned 0 with no granted lock; the kill cell would then kill an unblocked handler and its non-vacuity claim would be a race"
	rg --quiet -- "blocker_snapshot" "${log_dir}/queries.log" \
		|| fail "ifa_derived_from_start_fact_records_lock exhausted its window without snapshotting the fact_records blockers; the live killworkerkubernetesnamespaceenvironment failure named nothing"

	# The lock is granted on the first poll.
	lock_count_output=" 1 "
	bg_pids=()
	lock_holder_pid=""
	ifa_derived_from_start_fact_records_lock haslock lock_holder_pid \
		|| fail "ifa_derived_from_start_fact_records_lock rejected a granted AccessExclusiveLock"
	[[ "${lock_holder_pid}" =~ ^[0-9]+$ ]] \
		|| fail "ifa_derived_from_start_fact_records_lock did not report the holder PID: ${lock_holder_pid}"
	[[ " ${bg_pids[*]} " == *" ${lock_holder_pid} "* ]] \
		|| fail "ifa_derived_from_start_fact_records_lock did not track the holder PID; teardown would leave the blocker running into the next cell"
)

# ifa_derived_from_release_fact_records_lock joins the lock-holder process, so that PID
# must stop being tracked as owned. If it stayed in bg_pids, teardown_cell
# would signal a PID the shell has already reaped -- and after PID reuse that
# signal lands on an unrelated process. Mirrors the code_calls and
# documentation cases of the same defect.
test_ifa_derived_from_released_lock_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${derived_from_cells_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-derived-from-lock-owner.XXXXXX)"
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

	ifa_derived_from_release_fact_records_lock test "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined derived-from lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined derived-from lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)

# Postgres stores at most 63 bytes of application_name: a longer lock name is
# silently truncated, the grant poll on the full name misses forever, and
# the release's terminate-by-name misses too -- proven live by the sibling
# kubernetes family's 83-char name dying while holding its own granted
# lock. The start function must fail closed on an overlong name instead of
# polling a match that can never arrive -- even when the grant query itself
# reports success.
test_ifa_derived_from_overlong_lock_name_is_rejected() (
	source "${det_lib}"
	source "${derived_from_cells_lib}"

	local lock_holder_pid rc long_cell
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf ' 1 \n'; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-derived-from-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# 22-char ifa_derived_from_lock_ prefix plus a cell this long exceeds
	# the 63-byte application_name cap.
	long_cell="killworker_derived_from_xpad01234567890123"
	((${#long_cell} > 41)) || fail "test cell name too short to exercise the 63-byte application_name guard"
	bg_pids=()
	lock_holder_pid=""
	rc=0
	ifa_derived_from_start_fact_records_lock "${long_cell}" lock_holder_pid || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_derived_from_start_fact_records_lock accepted a lock name Postgres truncates; the grant poll would miss forever"
)

# The start function takes the lock under one application_name and the release
# function terminates it by name: if the two literals disagree, release
# terminates nothing, the holder leaks through its whole pg_sleep, and the
# replacement reducer's fact reads stay blocked for minutes while the cell
# still reports green. Surname-level typo bait after any rename, so pin the
# agreement behaviorally: capture both names off the stubbed query log and
# require them equal.
test_ifa_derived_from_lock_start_and_release_agree_on_name() (
	source "${det_lib}"
	source "${derived_from_cells_lib}"

	local lock_holder_pid start_name release_name
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf '%s\n' "$*" >>"${log_dir}/queries.log"; printf ' 1 \n'; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-derived-from-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	bg_pids=()
	lock_holder_pid=""
	ifa_derived_from_start_fact_records_lock samecell lock_holder_pid \
		|| fail "ifa_derived_from_start_fact_records_lock rejected a granted lock"
	ifa_derived_from_release_fact_records_lock samecell "${lock_holder_pid}"
	start_name="$(rg -o -- "application_name = '[^']*'" "${log_dir}/queries.log" | head -n 1)"
	release_name="$(rg -- "pg_terminate_backend" "${log_dir}/queries.log" | rg -o -- "application_name = '[^']*'" | head -n 1)"
	[[ -n "${start_name}" && "${start_name}" == "${release_name}" ]] \
		|| fail "lock start names ${start_name:-<none>} but release terminates ${release_name:-<none>}; the holder would leak"
)

run_ifa_fault_injection_derived_from_cases() {
	test_ifa_derived_from_intent_lock_is_fail_closed
	test_ifa_derived_from_released_lock_holder_is_not_torn_down_twice
	test_ifa_derived_from_overlong_lock_name_is_rejected
	test_ifa_derived_from_lock_start_and_release_agree_on_name
	test_ifa_derived_from_kill_cell_proves_attempts_above_baseline
	test_ifa_derived_from_merge_anchor_is_prefix
}

# The DERIVED_FROM MERGE anchor the fail-graph-write cell targets: the
# writer's template carries the scope_id/evidence_source identity map, so
# only the PREFIX form matches executed statement text. A full `]->(base)`
# spelling would match nothing and the fault would never fire.
test_ifa_derived_from_merge_anchor_is_prefix() (
	require_anchors_lib "derived_from DERIVED_FROM MERGE operation_match anchor" 'derived_from_edge_operation_match="MERGE (img)-[rel:DERIVED_FROM"'
)

# Same saturating single-row shape as the k8s kill cell (proved behaviorally
# in test_ifa_direct_family_retry_attempts_above_counts_attempts_not_rows):
# one work item means the row-count form caps at 1, so the derived_from kill
# cell must reach for the attempt-totals helper with its own domain.
test_ifa_derived_from_kill_cell_proves_attempts_above_baseline() (
	rg -U --quiet -- '(?s)ifa_fault_assert_retry_attempts_above.{0,400}"container_image_identity"' "${derived_from_cells_lib}" \
		|| fail "kill-worker derived_from cell does not pass its own identity domain to ifa_fault_assert_retry_attempts_above; the row-count form saturates at 1 for its single work item"
)
