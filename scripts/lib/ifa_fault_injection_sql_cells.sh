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
	# Only meaningful when this script owns the stack. Under --no-compose
	# fresh_stack deliberately skips teardown, so surviving intents are the
	# operator's pre-existing state rather than a leak, and asserting zero would
	# fail a legitimately-configured run.
	if [[ "${use_compose}" -eq 1 ]]; then
		local pre_intents pre_intents_rc
		# Captured WITHOUT a pipe so a failed query is distinguishable from a
		# count. Piping through tr collapses an error into an empty string, and
		# an empty string is not "0" -- the cell would then die reporting a stale
		# stack when Postgres was simply unreachable. Guessing a cause from an
		# ambiguous signal is the exact defect this cell exists to remove.
		set +e
		pre_intents="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			'SELECT count(*) FROM shared_projection_intents;' "${compose_file}")"
		pre_intents_rc=$?
		set -e
		[[ "${pre_intents_rc}" -eq 0 ]] \
			|| die "fail-graph-write-once-then-succeed-sql: the fresh-stack precondition query FAILED (exit ${pre_intents_rc}); that says nothing about whether the stack is fresh -- fix the query or the backend before reading this cell's result (#5974)"
		pre_intents="$(printf '%s' "${pre_intents}" | tr -d '[:space:]')"
		[[ "${pre_intents}" =~ ^[0-9]+$ ]] \
			|| die "fail-graph-write-once-then-succeed-sql: the fresh-stack precondition query returned non-numeric output; treat that as unknown, not as zero (#5974)"
		[[ "${pre_intents}" == "0" ]] \
			|| die "fail-graph-write-once-then-succeed-sql: ${pre_intents} shared_projection_intents row(s) survived fresh_stack -- the stack is not fresh, so this cell would replay completed work and prove nothing (#5974)"
		printf 'fail-graph-write-once-then-succeed-sql: fresh-stack precondition: 0 shared_projection_intents\n'
	else
		printf 'fail-graph-write-once-then-succeed-sql: fresh-stack precondition SKIPPED (--no-compose owns the stack; surviving intents are not a leak)\n'
	fi

	drive_all_cassettes failgraphwritesql
	local fault_once_script_sql projector_pid reducer_pid observed_ops
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
		|| die "fail-graph-write-once-then-succeed-sql: the SQL relationship edge set does not match the expected set after the drain. assert-edges is set-exact, so this covers a MISSING QUERIES_TABLE edge (the MERGE never ran here, and the fault had nothing to intercept) AND an extra, duplicated, or wrong-typed edge (a write ran and produced something else). Read the assert-edges diff above before deciding which -- they point at different code (#5974)."
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE completed_at IS NULL) AS pending, min(created_at) AS first_created, max(completed_at) AS last_completed FROM shared_projection_intents WHERE projection_domain = 'sql_relationships';" \
		"${compose_file}" | sed 's/^/  sql_relationships intent window: /'
	capture_digest failgraphwritesql
	assert_matches_baseline failgraphwritesql
	# #5974: assert on the durable marker the fault decorator writes, not the
	# reducer's captured stderr. The log route raced the flush and made this
	# cell inert in CI while passing locally.
	# Distinguish the assertion's two failure modes rather than reporting both as
	# "no marker". 1 = the marker is absent (the fault never fired). 2 = it exists
	# but names a different write. Conflating them is what made this cell report
	# a working fault as inert for weeks (#5974).
	local marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script_sql}" "${sql_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		die "fail-graph-write-once-then-succeed-sql: the fault FIRED but on a different write than ${sql_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement (#5974)."
	fi
	if [[ "${marker_rc}" -ne 0 ]]; then
		# No marker at all. Look for a write failure with a bash substring scan --
		# NOT an external tool. The previous version used rg, which is absent on
		# this CI runner, so its "command not found" exit read as "no failure
		# found". A checker that cannot run must never look like a clean result.
		local reducer_log_contents=""
		if [[ -r "${log_dir}/reducer-failgraphwritesql.log" ]]; then
			reducer_log_contents="$(cat "${log_dir}/reducer-failgraphwritesql.log" 2>/dev/null || true)"
		fi
		if [[ "${reducer_log_contents}" == *"${IFA_ONCE_MARKER_WRITE_FAILED_PREFIX}"* ]]; then
			printf '%s\n' "${reducer_log_contents}" | sed -n "/${IFA_ONCE_MARKER_WRITE_FAILED_PREFIX}/p" >&2 || true
			die "fail-graph-write-once-then-succeed-sql: the marker WRITE FAILED (line above). The fault may well have fired -- this is an instrument failure, not evidence about the fault (#5974)."
		fi
		printf '\n=== sentinel family on disk (base: %s) ===\n' "${fault_once_script_sql}" >&2
		ls -la "${fault_once_script_sql}"* 2>&1 | sed 's/^/  /' >&2 || true
		printf '=== end sentinel family ===\n\n' >&2
		observed_ops="${fault_once_script_sql}.restart-sentinel.observed-operations"
		if [[ -s "${observed_ops}" ]]; then
			printf '\n=== statement shapes this cell actually executed (anchor: %s) ===\n' \
				"${sql_edge_operation_match}" >&2
			# cat, NOT sort -u. The record holds multi-line statements; sorting
			# sorts LINES and shreds them into alphabetised fragments. Dedup
			# already happened in Go, keyed on the full statement text.
			cat "${observed_ops}" >&2
			printf '=== end observed statements ===\n\n' >&2
		else
			printf 'fail-graph-write-once-then-succeed-sql: no observed-operations record at %s -- the executor saw no statements at all while armed, which points at wiring rather than the anchor\n' \
				"${observed_ops}" >&2
		fi
		die "fail-graph-write-once-then-succeed-sql: no once-fired marker beside ${fault_once_script_sql}, and no marker-write failure reported. The listing and observed statements above say which of those is true."
	fi
	printf 'fail-graph-write-once-then-succeed-sql: non-vacuous: once-fired marker names the targeted SQL edge MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	teardown_cell failgraphwritesql
	wall_times[failgraphwritesql]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-sql: cell wall time: %ss\n' "${wall_times[failgraphwritesql]}"
}
