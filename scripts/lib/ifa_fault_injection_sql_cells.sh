#!/usr/bin/env bash
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
#     fired via ifa_fault_assert_sql_graph_write_fired -- the
#     shared-projection error log go/internal/reducer/shared_projection_
#     runner.go now emits, since sql_relationship_materialization's graph
#     writes ride the async shared-projection intent path (no
#     fact_work_items attempt_count signal exists for that path; see that
#     function's doc comment).
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
# a SQL edge MERGE (QUERIES_TABLE) exactly once. Proves the fault fired via
# the sql_relationships shared-projection error log line, checked only AFTER
# run_drain_gate already returned success (so the check cannot race the live
# event -- see ifa_fault_assert_sql_graph_write_fired's doc comment).
cell_failgraphwrite_sql() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-sql: fresh stack"
	fresh_stack failgraphwritesql
	drive_all_cassettes failgraphwritesql
	local fault_once_script_sql projector_pid reducer_pid reducer_log
	fault_once_script_sql="${work_dir}/fault-once-then-succeed-sql.json"
	ifa_fault_write_once_script "${fault_once_script_sql}" "${sql_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritesql" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritesql" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script_sql}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritesql
	assert_no_dead_letters failgraphwritesql
	capture_digest failgraphwritesql
	assert_matches_baseline failgraphwritesql
	reducer_log="${log_dir}/reducer-failgraphwritesql.log"
	ifa_fault_assert_sql_graph_write_fired "${reducer_log}" \
		|| die "fail-graph-write-once-then-succeed-sql: the scripted fault never fired -- no sql_relationships shared-projection partition-processing error naming the injected fault text appeared in ${reducer_log} within budget. An inert script, not a pass. Root-cause the QUERIES_TABLE MERGE anchor (sql_edge_operation_match) against the real go/internal/storage/cypher/edge_writer_sql.go output, or the shared-projection error-log wiring (go/internal/reducer/shared_projection_runner.go), before treating this cell as usable."
	printf 'fail-graph-write-once-then-succeed-sql: non-vacuous: sql_relationships partition-processing error logged for the injected fault (checked after drain, not a live race)\n'
	teardown_cell failgraphwritesql
	wall_times[failgraphwritesql]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-sql: cell wall time: %ss\n' "${wall_times[failgraphwritesql]}"
}
