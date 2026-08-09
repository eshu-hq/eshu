#!/usr/bin/env bash
# shellcheck disable=SC2034  # The reducer_/projector_pid locals are
# filled indirectly by ifa_det_start_bg via printf -v, so shellcheck
# sees the declaration but not the write.
# shellcheck disable=SC2154  # This file is sourced by
# scripts/verify-ifa-fault-injection.sh and reads globals it owns
# (bin_dir, log_dir, work_dir, use_compose, compose_file, wall_times,
# baseline_retried, and the *_operation_match anchors). Without this,
# linting the library standalone buries a genuinely new SC2154 in ~30
# expected ones.
# SQL-relationship-targeted fault cells (issue #5555). scripts/lib/
# ifa_fault_injection_cells.sh's cell_killworker / cell_expirelease /
# cell_failgraphwrite prove kill/lease/graph-write recovery, but their
# non-vacuity preconditions are untargeted: ifa_fault_wait_for_claimed waits
# for ANY claimed reducer row (in practice gcp_resource_materialization, the
# first domain the demo cassette schedules), and cell_failgraphwrite's fault
# is anchored to a CloudResource MERGE. The SQL relationship family
# (sql_relationship_materialization / sql_relationships) drains cleanly
# AFTER those faults have already hit GCP work, so an SQL-handler recovery
# regression was never actually tested -- a confirmed-false fault-coverage
# claim, waived in specs/ifa-materialized-edge-coverage.v1.yaml pointing at
# this issue. These two cells close that gap:
#
#   - cell_killworker_sql provably targets the SQL work item by scoping
#     ifa_fault_wait_for_claimed to domain=sql_relationship_materialization.
#   - cell_failgraphwrite_sql anchors the graph-write fault to a SQL edge
#     MERGE (QUERIES_TABLE) instead of CloudResource, and proves the fault
#     fired via ifa_fault_assert_once_fault_marker, reading the marker the
#     fault decorator writes at injection time. It does not read a log:
#     sql_relationship_materialization's graph writes ride the async
#     shared-projection intent path, so no fact_work_items attempt_count
#     signal exists for it, and the log route that filled that gap could not
#     distinguish a fault that never fired from a log line that arrived late
#     (#5974).
#
# This file is a plain function library, not a script (no `set -euo
# pipefail`; see ifa_fault_injection_driver.sh's identical note). Every
# function here reads driver-owned globals (bin_dir, tagged_bin_dir,
# log_dir, work_dir, wall_times, sql_edge_operation_match, baseline_retried,
# CLAIMED_ROW_WAIT_TIMEOUT, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers) rather
# than taking them as arguments.

# cell_killworker_sql: kill -9 the live host eshu-reducer process after a row
# is PROVABLY an sql_relationship_materialization row (not any domain), then
# start a fresh reducer process and let the fixed 1-minute lease expire and
# get reclaimed. Mirrors cell_killworker; the only difference is the
# domain-scoped wait_for_claimed precondition.
#
# What this proves, exactly: a SQL relationship row was claimed before the kill,
# so the cell is aimed at SQL work rather than at whatever the demo cassette
# happens to schedule first -- which is the #5555 defect. Proven by seeding the
# domain argument to a name no domain uses: the cell then times out naming that
# domain instead of latching an unrelated claimed row.
#
# What it does NOT prove: that the kill landed mid-handler. The SQL handler is
# short, so it can acknowledge its row between the claimed-row read and the
# kill, in which case the restart exercises an already-finished unit and the
# digest match afterwards says nothing about SQL recovery specifically. Closing
# that needs an assertion that the SQL row was actually reclaimed and re-executed
# after the restart, which is tracked with the graph-write cell in #5974. Do not
# read this cell as fault-recovery coverage for the SQL family until then -- the
# manifest keeps that dimension waived for exactly this reason.
cell_killworker_sql() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-sql: fresh stack"
	fresh_stack killworkersql
	drive_all_cassettes killworkersql
	local projector_pid reducer_pid_before reducer_pid_after claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkersql" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-killworkersql-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "sql_relationship_materialization")" \
		|| die "kill-worker-after-claim-sql: no sql_relationship_materialization row was ever claimed before the kill -- non-vacuous SQL-targeted precondition failed"
	printf 'kill-worker-after-claim-sql: non-vacuous: %s claimed/running sql_relationship_materialization row(s) observed before kill\n' "${claimed_before}"
	log "kill-worker-after-claim-sql: kill -9 the live reducer (pid ${reducer_pid_before})"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	log "kill-worker-after-claim-sql: start a fresh reducer process (1-minute lease expiry reclaim)"
	ifa_det_start_bg "${log_dir}" "reducer-killworkersql-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkersql
	assert_no_dead_letters killworkersql
	capture_digest killworkersql
	assert_matches_baseline killworkersql
	teardown_cell killworkersql
	wall_times[killworkersql]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-sql: cell wall time: %ss\n' "${wall_times[killworkersql]}"
}

# cell_failgraphwrite_sql: the tagged (-tags ifafaultinjection) eshu-reducer
# with ESHU_IFA_FAULT_SCRIPT pointed at a queue-retry fault script that fails
# a SQL edge MERGE (QUERIES_TABLE) exactly once. Proves the fault fired from
# the once-fired marker FaultingExecutor writes at injection time, naming the
# statement it actually hit -- NOT from the reducer's log. The log route was
# retired in #5974: it could not tell "the fault never fired" apart from "the
# log line arrived late", and it read the former as the latter for weeks.
#
# HELD OUT of the default cell list. With the marker in place CI answered the
# question the log could not: no marker is written there at all, so the fault
# genuinely does not fire in CI while firing locally in 9s. See the call site
# in scripts/verify-ifa-fault-injection.sh and #5974.
cell_failgraphwrite_sql() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-sql: fresh stack"
	fresh_stack failgraphwritesql

	# Probe 1 (#5974): a genuinely fresh stack has no shared-projection intents.
	# Survivors mean this cell is replaying an earlier cell's completed work --
	# intent IDs are deterministic and completed rows are never reopened, so the
	# drive produces no new graph writes, nothing reaches the fault decorator,
	# and every later assertion still passes on edges that are already there.
	local pre_intents
	pre_intents="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		'SELECT count(*) FROM shared_projection_intents;' "${compose_file}" | tr -d '[:space:]')"
	[[ "${pre_intents}" == "0" ]] \
		|| die "fail-graph-write-once-then-succeed-sql: ${pre_intents} shared_projection_intents row(s) survived fresh_stack -- the stack is not fresh, so this cell would replay completed work and prove nothing (#5974)"
	printf 'fail-graph-write-once-then-succeed-sql: fresh-stack precondition: 0 shared_projection_intents\n'

	drive_all_cassettes failgraphwritesql
	local fault_once_script_sql projector_pid reducer_pid
	fault_once_script_sql="${work_dir}/fault-once-then-succeed-sql.json"
	ifa_fault_write_once_script "${fault_once_script_sql}" "${sql_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritesql" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritesql" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script_sql}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritesql
	assert_no_dead_letters failgraphwritesql

	# Probe 2 (#5974): separate "the SQL edge was never written in this cell"
	# from "it was written here and the fault missed it". Without this, a missing
	# marker has two explanations and the failure message has to guess between
	# them -- which is how this issue stayed open on a wrong root cause.
	log "fail-graph-write-once-then-succeed-sql: probe SQL edges and this cell's intent window"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain sql_relationships \
		-expected "${sql_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-sql: the SQL edge set is absent or wrong after the drain -- the QUERIES_TABLE MERGE never ran in this cell, so the fault had nothing to intercept. The divergence is in the cassette drive, not the fault plumbing (#5974)."
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE completed_at IS NULL) AS pending, min(created_at) AS first_created, max(completed_at) AS last_completed FROM shared_projection_intents WHERE projection_domain = 'sql_relationships';" \
		"${compose_file}" | sed 's/^/  sql_relationships intent window: /'
	capture_digest failgraphwritesql
	assert_matches_baseline failgraphwritesql
	# #5974: assert on the durable marker the fault decorator writes, not the
	# reducer's captured stderr. The log route raced the flush and made this
	# cell inert in CI while passing locally.
	ifa_fault_assert_once_fault_marker "${fault_once_script_sql}" "${sql_edge_operation_match}" \
		|| die "fail-graph-write-once-then-succeed-sql: no once-fired marker naming ${sql_edge_operation_match} beside ${fault_once_script_sql}. The probes above already ruled out the cheap explanations, so with them green this means the statement flowed and the fault did not intercept it -- OR the marker write itself failed, which the reducer log reports with the prefix \"ifa fault: once-fired marker write failed\". Check for that line before concluding the fault never fired. The marker is written by FaultingExecutor at injection time (go/internal/storage/cypher/fault_executor.go), so its absence means the QUERIES_TABLE MERGE anchor never matched a real write: check sql_edge_operation_match against go/internal/storage/cypher/edge_writer_sql.go and canonical.go before treating this cell as usable."
	printf 'fail-graph-write-once-then-succeed-sql: non-vacuous: once-fired marker names the targeted SQL edge MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	teardown_cell failgraphwritesql
	wall_times[failgraphwritesql]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-sql: cell wall time: %ss\n' "${wall_times[failgraphwritesql]}"
}
