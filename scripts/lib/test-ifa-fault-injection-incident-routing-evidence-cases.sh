#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the incident_routing_evidence
# fault-injection cells (#6228 incident_routing_evidence live leaf). Split
# into its own module for the same reason
# test-ifa-fault-injection-documentation-cases.sh was: the top-level static
# verifier and the review-case module both sit near the repository's
# 500-line cap. Sourced by scripts/test-verify-ifa-fault-injection.sh, and
# shares that script's ${repo_root} and `fail` helper.
#
# These cases are hermetic: every process, database, and signal the cells
# would touch is replaced by a shell stub, so they run in `make pre-pr`
# without Docker, Postgres, or a live gate. They prove the seam that would
# otherwise fail silently in CI -- a lock that never actually blocked --
# not that the cells recover a real graph, which only the live fault matrix
# can show. There is no readiness-wait case on purpose: this domain carries
# no claim-time readiness requirement and the handler holds no in-handler
# gate before its load, so the kill cell takes the crossplane shape (lock
# as soon as the row exists) rather than the observability_coverage
# readiness-banked shape.

incident_routing_evidence_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_incident_routing_evidence_cells.sh"
[[ -f "${incident_routing_evidence_cells_lib}" ]] || fail "missing ${incident_routing_evidence_cells_lib}"
bash -n "${incident_routing_evidence_cells_lib}" || fail "ifa_fault_injection_incident_routing_evidence_cells.sh has a syntax error"

# ifa_incident_routing_evidence_start_fact_records_lock's whole job is to
# guarantee the fact_records blocker is actually HELD before the cell kills
# the reducer. If it returned success on a lock it never observed,
# cell_killworker_incident_routing_evidence would kill a handler that had already
# committed, and "non-vacuous: N blocked claimed/running row(s) observed"
# would become a race rather than a fact. This case drives the poll loop to
# exhaustion with a stubbed query and requires a non-zero return, then proves
# the success path reports the granted lock and registers the holder for
# teardown.
test_ifa_incident_routing_evidence_intent_lock_is_fail_closed() (
	source "${det_lib}"
	source "${incident_routing_evidence_cells_lib}"

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
	log_dir="$(mktemp -d -t ifa-incident-routing-evidence-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# The lock never appears: every poll reports zero granted locks.
	lock_count_output="0"
	bg_pids=()
	lock_holder_pid=""
	rc=0
	ifa_incident_routing_evidence_start_fact_records_lock nolock lock_holder_pid || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_incident_routing_evidence_start_fact_records_lock returned 0 with no granted lock; the kill cell would then kill an unblocked handler and its non-vacuity claim would be a race"
	rg --quiet -- "blocker_snapshot" "${log_dir}/queries.log" \
		|| fail "ifa_incident_routing_evidence_start_fact_records_lock exhausted its window without snapshotting the fact_records blockers; the live killworkerkubernetesnamespaceenvironment failure named nothing"

	# The lock is granted on the first poll.
	lock_count_output=" 1 "
	bg_pids=()
	lock_holder_pid=""
	ifa_incident_routing_evidence_start_fact_records_lock haslock lock_holder_pid \
		|| fail "ifa_incident_routing_evidence_start_fact_records_lock rejected a granted AccessExclusiveLock"
	[[ "${lock_holder_pid}" =~ ^[0-9]+$ ]] \
		|| fail "ifa_incident_routing_evidence_start_fact_records_lock did not report the holder PID: ${lock_holder_pid}"
	[[ " ${bg_pids[*]} " == *" ${lock_holder_pid} "* ]] \
		|| fail "ifa_incident_routing_evidence_start_fact_records_lock did not track the holder PID; teardown would leave the blocker running into the next cell"
)

# ifa_incident_routing_evidence_release_fact_records_lock joins the lock-holder
# process, so that PID must stop being tracked as owned. If it stayed in
# bg_pids, teardown_cell would signal a PID the shell has already reaped --
# and after PID reuse that signal lands on an unrelated process. Mirrors the
# s3_logs_to case of the same defect.
test_ifa_incident_routing_evidence_released_lock_holder_is_not_torn_down_twice() (
	source "${det_lib}"
	source "${driver_lib}"
	source "${incident_routing_evidence_cells_lib}"
	declare -F ifa_det_untrack_bg_pid >/dev/null \
		|| fail "determinism helpers do not expose ifa_det_untrack_bg_pid"

	local case_dir holder_pid survivor_pid
	case_dir="$(mktemp -d -t ifa-incident-routing-evidence-lock-owner.XXXXXX)"
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

	ifa_incident_routing_evidence_release_fact_records_lock test "${holder_pid}"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "joined incident-routing-evidence lock-holder PID remained in tracked ownership"
	teardown_cell test
	if rg --line-regexp --quiet -- "${holder_pid}" "${case_dir}/kill.log"; then
		fail "teardown signaled the joined incident-routing-evidence lock-holder PID; PID reuse could target an unrelated process"
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
test_ifa_incident_routing_evidence_overlong_lock_name_is_rejected() (
	source "${det_lib}"
	source "${incident_routing_evidence_cells_lib}"

	local lock_holder_pid rc long_cell
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf ' 1 \n'; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-incident-routing-evidence-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# 18-char ifa_incroute_lock_ prefix plus a cell this long exceeds
	# the 63-byte application_name cap.
	long_cell="killworker_incident_routing_evidence_xpad012345"
	((${#long_cell} > 45)) || fail "test cell name too short to exercise the 63-byte application_name guard"
	bg_pids=()
	lock_holder_pid=""
	rc=0
	ifa_incident_routing_evidence_start_fact_records_lock "${long_cell}" lock_holder_pid || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_incident_routing_evidence_start_fact_records_lock accepted a lock name Postgres truncates; the grant poll would miss forever"
)

# The start function takes the lock under one application_name and the release
# function terminates it by name: if the two literals disagree, release
# terminates nothing, the holder leaks through its whole pg_sleep, and the
# replacement reducer's fact reads stay blocked for minutes while the cell
# still reports green. Surname-level typo bait after any rename, so pin the
# agreement behaviorally: capture both names off the stubbed query log and
# require them equal.
test_ifa_incident_routing_evidence_lock_start_and_release_agree_on_name() (
	source "${det_lib}"
	source "${incident_routing_evidence_cells_lib}"

	local lock_holder_pid start_name release_name
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() { printf '%s\n' "$*" >>"${log_dir}/queries.log"; printf ' 1 \n'; }

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-incident-routing-evidence-lock.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	bg_pids=()
	lock_holder_pid=""
	ifa_incident_routing_evidence_start_fact_records_lock samecell lock_holder_pid \
		|| fail "ifa_incident_routing_evidence_start_fact_records_lock rejected a granted lock"
	ifa_incident_routing_evidence_release_fact_records_lock samecell "${lock_holder_pid}"
	start_name="$(rg -o -- "application_name = '[^']*'" "${log_dir}/queries.log" | head -n 1)"
	release_name="$(rg -- "pg_terminate_backend" "${log_dir}/queries.log" | rg -o -- "application_name = '[^']*'" | head -n 1)"
	[[ -n "${start_name}" && "${start_name}" == "${release_name}" ]] \
		|| fail "lock start names ${start_name:-<none>} but release terminates ${release_name:-<none>}; the holder would leak"
)

run_ifa_fault_injection_incident_routing_evidence_cases() {
	test_ifa_incident_routing_evidence_intent_lock_is_fail_closed
	test_ifa_incident_routing_evidence_released_lock_holder_is_not_torn_down_twice
	test_ifa_incident_routing_evidence_overlong_lock_name_is_rejected
	test_ifa_incident_routing_evidence_lock_start_and_release_agree_on_name
	test_ifa_incident_routing_evidence_kill_cell_proves_attempts_above_baseline
	test_ifa_incident_routing_evidence_merge_anchor_is_prefix
}

# Same saturating single-row shape as the k8s kill cell (proved behaviorally
# in test_ifa_direct_family_retry_attempts_above_counts_attempts_not_rows):
# one work item means the row-count form caps at 1, so the
# incident_routing_evidence kill cell must reach for the attempt-totals helper
# with its own domain.
test_ifa_incident_routing_evidence_kill_cell_proves_attempts_above_baseline() (
	rg -U --quiet -- '(?s)ifa_fault_assert_retry_attempts_above.{0,400}"incident_routing_materialization"' "${incident_routing_evidence_cells_lib}" \
		|| fail "kill-worker incident_routing_evidence cell does not pass its own materialization domain to ifa_fault_assert_retry_attempts_above; the row-count form saturates at 1 for its single work item"
)

# The HAS_*_ROUTING MERGE anchor the fail-graph-write cell targets: the
# writer's template reads `MERGE (incident)-[rel:%s]->(routing)` in source
# and fills the type per row from the closed 3-member routing-slot
# vocabulary, so only the PREFIX form matches executed statement text.
# Pinning a full `]->(routing)` spelling names no single executed statement
# (three types share the prefix) and pinning one filled type would leave the
# other two unfaulted.
test_ifa_incident_routing_evidence_merge_anchor_is_prefix() (
	require_anchors_lib "incident_routing_evidence HAS_*_ROUTING MERGE operation_match anchor" 'incident_routing_evidence_edge_operation_match="MERGE (incident)-[rel:HAS_"'
)
