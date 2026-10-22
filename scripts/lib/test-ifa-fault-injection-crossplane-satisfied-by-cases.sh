#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the crossplane_satisfied_by
# fault-injection cells (#6228 crossplane live leaf). Split into its own module for
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

xplane_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh"
[[ -f "${xplane_cells_lib}" ]] || fail "missing ${xplane_cells_lib}"
bash -n "${xplane_cells_lib}" || fail "ifa_fault_injection_crossplane_satisfied_by_cells.sh has a syntax error"

# ifa_crossplane_satisfied_by_start_fact_records_lock's whole job is to
# guarantee the fact_records blocker is actually HELD before the cell kills
# the reducer. If it returned success on a lock it never observed,
# cell_killworker_crossplane_satisfied_by would kill a handler that
# had already committed, and "non-vacuous: N blocked claimed/running row(s)
# observed" would become a race rather than a fact. This case drives the poll
# loop to exhaustion with a stubbed query and requires a non-zero return,
# then proves the success path reports the granted lock and registers the
# holder for teardown.
test_ifa_xplane_intent_lock_is_fail_closed() (
	source "${det_lib}"
	source "${xplane_cells_lib}"

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
	log_dir="$(mktemp -d -t ifa-xplane-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# The lock never appears: every poll reports zero granted locks.
	lock_count_output="0"
	bg_pids=()
	lock_holder_pid=""
	rc=0
	ifa_crossplane_satisfied_by_start_fact_records_lock nolock lock_holder_pid || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_crossplane_satisfied_by_start_fact_records_lock returned 0 with no granted lock; the kill cell would then kill an unblocked handler and its non-vacuity claim would be a race"
	rg --quiet -- "blocker_snapshot" "${log_dir}/queries.log" \
		|| fail "ifa_crossplane_satisfied_by_start_fact_records_lock exhausted its window without snapshotting the fact_records blockers; the live killworkerkubernetesnamespaceenvironment failure named nothing"

	# The lock is granted on the first poll.
	lock_count_output=" 1 "
	bg_pids=()
	lock_holder_pid=""
	ifa_crossplane_satisfied_by_start_fact_records_lock haslock lock_holder_pid \
		|| fail "ifa_crossplane_satisfied_by_start_fact_records_lock rejected a granted AccessExclusiveLock"
	[[ "${lock_holder_pid}" =~ ^[0-9]+$ ]] \
		|| fail "ifa_crossplane_satisfied_by_start_fact_records_lock did not report the holder PID: ${lock_holder_pid}"
	[[ " ${bg_pids[*]} " == *" ${lock_holder_pid} "* ]] \
		|| fail "ifa_crossplane_satisfied_by_start_fact_records_lock did not track the holder PID; teardown would leave the blocker running into the next cell"
)

# ifa_crossplane_satisfied_by_release_fact_records_lock joins the
# lock-holder process, so that PID must stop being tracked as owned. If it
# stayed in bg_pids, teardown_cell would signal a PID the shell has already
# reaped -- and after PID reuse that signal lands on an unrelated process.
# Mirrors the code_calls and documentation cases of the same defect.
test_ifa_xplane_released_lock_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${xplane_cells_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-xplane-lock-owner.XXXXXX)"
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

	ifa_crossplane_satisfied_by_release_fact_records_lock test "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined xplane lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined xplane lock-holder PID; PID reuse could target an unrelated process"
	fi
	rg --line-regexp --quiet -- "${survivor_pid}" "${case_dir}/kill.log" \
		|| fail "teardown stopped tracking the still-owned background PID"
)

# Postgres stores at most 63 bytes of application_name: a longer lock name is
# silently truncated, the grant poll on the full name misses forever, and the
# release's terminate-by-name misses too -- proven live by the sibling
# kubernetes family's 83-char name dying while holding its own granted
# lock. The start function must fail closed on an overlong name instead of
# polling a match that can never arrive -- even when the grant query itself
# reports success.
test_ifa_xplane_overlong_lock_name_is_rejected() (
	source "${det_lib}"
	source "${xplane_cells_lib}"

	local lock_holder_pid rc long_cell
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf ' 1 \n'; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-xplane-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# 17-char ifa_xplane_lock_ prefix plus a cell this long exceeds
	# the 63-byte application_name cap.
	long_cell="killworker_xplane_xpad012345678901234567890123"
	((${#long_cell} > 46)) || fail "test cell name too short to exercise the 63-byte application_name guard"
	bg_pids=()
	lock_holder_pid=""
	rc=0
	ifa_crossplane_satisfied_by_start_fact_records_lock "${long_cell}" lock_holder_pid || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_crossplane_satisfied_by_start_fact_records_lock accepted a lock name Postgres truncates; the grant poll would miss forever"
)

# The start function takes the lock under one application_name and the release
# function terminates it by name: if the two literals disagree, release
# terminates nothing, the holder leaks through its whole pg_sleep, and the
# replacement reducer's fact reads stay blocked for minutes while the cell
# still reports green. Surname-level typo bait after any rename, so pin the
# agreement behaviorally: capture both names off the stubbed query log and
# require them equal.
test_ifa_xplane_lock_start_and_release_agree_on_name() (
	source "${det_lib}"
	source "${xplane_cells_lib}"

	local lock_holder_pid start_name release_name
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf '%s\n' "$*" >>"${log_dir}/queries.log"; printf ' 1 \n'; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-xplane-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	bg_pids=()
	lock_holder_pid=""
	ifa_crossplane_satisfied_by_start_fact_records_lock samecell lock_holder_pid \
		|| fail "ifa_crossplane_satisfied_by_start_fact_records_lock rejected a granted lock"
	ifa_crossplane_satisfied_by_release_fact_records_lock samecell "${lock_holder_pid}"
	start_name="$(rg -o -- "application_name = '[^']*'" "${log_dir}/queries.log" | head -n 1)"
	release_name="$(rg -- "pg_terminate_backend" "${log_dir}/queries.log" | rg -o -- "application_name = '[^']*'" | head -n 1)"
	[[ -n "${start_name}" && "${start_name}" == "${release_name}" ]] \
		|| fail "lock start names ${start_name:-<none>} but release terminates ${release_name:-<none>}; the holder would leak"
)

run_ifa_fault_injection_crossplane_satisfied_by_cases() {
	test_ifa_xplane_intent_lock_is_fail_closed
	test_ifa_xplane_released_lock_holder_is_not_torn_down_twice
	test_ifa_xplane_overlong_lock_name_is_rejected
	test_ifa_xplane_lock_start_and_release_agree_on_name
	test_ifa_xplane_kill_cell_proves_attempts_above_baseline
	test_ifa_xplane_merge_anchor_is_interpolated
}

# The SATISFIED_BY MERGE anchor the fail-graph-write cell targets: the
# writer's template is a fixed MERGE with no vocabulary substitution, so
# only that exact spelling matches executed statement text. A parameterized
# form would match nothing and the fault would never fire.
test_ifa_xplane_merge_anchor_is_interpolated() (
	require_anchors_lib "crossplane_satisfied_by SATISFIED_BY MERGE operation_match anchor" 'crossplane_satisfied_by_edge_operation_match="MERGE (claim)-[rel:SATISFIED_BY]->(xrd)"'
)

# Same saturating single-row shape as the k8s kill cell (proved behaviorally
# in test_ifa_direct_family_retry_attempts_above_counts_attempts_not_rows):
# one work item means the row-count form caps at 1, so the xplane kill
# cell must reach for the attempt-totals helper with its own domain. Two
# rows here (one per scope), but each row still saturates the same way.
test_ifa_xplane_kill_cell_proves_attempts_above_baseline() (
	rg -U --quiet -- '(?s)ifa_fault_assert_retry_attempts_above.{0,400}"crossplane_satisfied_by_materialization"' "${xplane_cells_lib}" \
		|| fail "kill-worker xplane cell does not pass its own satisfied-by materialization domain to ifa_fault_assert_retry_attempts_above; the row-count form saturates for its per-scope work items"
)
