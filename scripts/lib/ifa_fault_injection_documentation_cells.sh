#!/usr/bin/env bash
# shellcheck disable=SC2034  # The reducer_pid locals are filled indirectly by
# ifa_det_start_bg via printf -v, so shellcheck sees the declaration but not
# the write.
# shellcheck disable=SC2154  # This file is sourced by
# scripts/verify-ifa-fault-injection.sh and reads globals it owns (bin_dir,
# tagged_bin_dir, log_dir, work_dir, use_compose, compose_file, wall_times,
# and the documentation_* anchors). Without this, linting the library
# standalone buries a genuinely new SC2154 in the ~30 expected ones the SQL
# and code_calls sibling libraries already carry.
#
# documentation_edges-targeted fault cells (#5994). Mirrors
# scripts/lib/ifa_fault_injection_sql_cells.sh's cell_killworker_sql /
# cell_failgraphwrite_sql exactly, one family later: without domain-scoped
# cells, the generic cell_killworker / cell_failgraphwrite in
# scripts/lib/ifa_fault_injection_cells.sh only ever prove recovery for
# whichever domain the demo cassette happens to schedule first (in practice
# gcp_resource_materialization), never documentation_materialization
# specifically -- the same #5555 gap the SQL cells closed for
# sql_relationship_materialization.
#
#   - cell_killworker_documentation provably targets the documentation work
#     item by scoping ifa_fault_wait_for_claimed to
#     domain=documentation_materialization.
#   - cell_failgraphwrite_documentation anchors the graph-write fault to the
#     DOCUMENTS edge MERGE and proves the fault fired via
#     ifa_fault_assert_once_fault_marker, reading the durable marker the fault
#     decorator writes at injection time -- never a log grep (#5974: a log
#     grep shelled out to `rg`, which the runner lacks, and its "command not
#     found" read as "no fault" for months).
#
# This file is a plain function library, not a script (no `set -euo
# pipefail`; see ifa_fault_injection_driver.sh's identical note). Every
# function here reads driver-owned globals (bin_dir, tagged_bin_dir, log_dir,
# work_dir, wall_times, documentation_edge_operation_match,
# documentation_expected_edges, CLAIMED_ROW_WAIT_TIMEOUT, log, die, plus the
# fresh_stack / drive_all_cassettes / run_drain_gate / assert_no_dead_letters
# / capture_digest / assert_matches_baseline / teardown_cell helpers) rather
# than taking them as arguments. Sources scripts/lib/ifa_documentation_live.sh
# for ifa_documentation_assert.

# cell_killworker_documentation: kill -9 the live host eshu-reducer process
# after a row is PROVABLY a documentation_materialization row (not any
# domain), then start a fresh reducer process and let the fixed 1-minute lease
# expire and get reclaimed. Mirrors cell_killworker_sql; the only difference
# is the domain-scoped wait_for_claimed precondition.
#
# What this proves, exactly: a documentation row was claimed before the kill,
# so the cell is aimed at documentation work rather than at whatever the demo
# cassette happens to schedule first. Proven by seeding the domain argument to
# a name no domain uses: the cell then times out naming that domain instead of
# latching an unrelated claimed row.
#
# What it does NOT prove: that the kill landed mid-handler. The documentation
# handler is short, so it can acknowledge its row between the claimed-row read
# and the kill, in which case the restart exercises an already-finished unit
# and the digest match afterwards says nothing about documentation recovery
# specifically. The separate graph-write cell supplies the durable
# family-targeted retry proof; together the two live cells back the
# manifest's documentation_edges fault row.
cell_killworker_documentation() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-documentation: fresh stack"
	fresh_stack killworkerdocumentation
	drive_all_cassettes killworkerdocumentation
	local projector_pid reducer_pid_before reducer_pid_after claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerdocumentation" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdocumentation-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "documentation_materialization")" \
		|| die "kill-worker-after-claim-documentation: no documentation_materialization row was ever claimed before the kill -- non-vacuous documentation-targeted precondition failed"
	printf 'kill-worker-after-claim-documentation: non-vacuous: %s claimed/running documentation_materialization row(s) observed before kill\n' "${claimed_before}"
	log "kill-worker-after-claim-documentation: kill -9 the live reducer (pid ${reducer_pid_before})"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	log "kill-worker-after-claim-documentation: start a fresh reducer process (1-minute lease expiry reclaim)"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdocumentation-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerdocumentation
	assert_no_dead_letters killworkerdocumentation
	ifa_documentation_assert "killworkerdocumentation" "${bin_dir}" "${documentation_expected_edges}" \
		|| die "kill-worker-after-claim-documentation: recovered graph does not match the two-edge exact set"
	capture_digest killworkerdocumentation
	assert_matches_baseline killworkerdocumentation
	teardown_cell killworkerdocumentation
	wall_times[killworkerdocumentation]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-documentation: cell wall time: %ss\n' "${wall_times[killworkerdocumentation]}"
}

# cell_failgraphwrite_documentation: the tagged (-tags ifafaultinjection)
# eshu-reducer with ESHU_IFA_FAULT_SCRIPT pointed at a queue-retry fault
# script that fails the DOCUMENTS edge MERGE exactly once. Proves the fault
# fired from the once-fired marker FaultingExecutor writes at injection time,
# naming the statement it actually hit -- NOT from the reducer's log (#5974).
cell_failgraphwrite_documentation() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-documentation: fresh stack"
	fresh_stack failgraphwritedocumentation

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
		# stack when Postgres was simply unreachable.
		set +e
		pre_intents="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			'SELECT count(*) FROM shared_projection_intents;' "${compose_file}")"
		pre_intents_rc=$?
		set -e
		[[ "${pre_intents_rc}" -eq 0 ]] \
			|| die "fail-graph-write-once-then-succeed-documentation: the fresh-stack precondition query FAILED (exit ${pre_intents_rc}); that says nothing about whether the stack is fresh -- fix the query or the backend before reading this cell's result (#5974)"
		pre_intents="$(printf '%s' "${pre_intents}" | tr -d '[:space:]')"
		[[ "${pre_intents}" =~ ^[0-9]+$ ]] \
			|| die "fail-graph-write-once-then-succeed-documentation: the fresh-stack precondition query returned non-numeric output; treat that as unknown, not as zero (#5974)"
		[[ "${pre_intents}" == "0" ]] \
			|| die "fail-graph-write-once-then-succeed-documentation: ${pre_intents} shared_projection_intents row(s) survived fresh_stack -- the stack is not fresh, so this cell would replay completed work and prove nothing (#5974)"
		printf 'fail-graph-write-once-then-succeed-documentation: fresh-stack precondition: 0 shared_projection_intents\n'
	else
		printf 'fail-graph-write-once-then-succeed-documentation: fresh-stack precondition SKIPPED (--no-compose owns the stack; surviving intents are not a leak)\n'
	fi

	drive_all_cassettes failgraphwritedocumentation
	local fault_once_script_documentation projector_pid reducer_pid
	fault_once_script_documentation="${work_dir}/fault-once-then-succeed-documentation.json"
	ifa_fault_write_once_script "${fault_once_script_documentation}" "${documentation_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritedocumentation" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritedocumentation" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script_documentation}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritedocumentation
	assert_no_dead_letters failgraphwritedocumentation

	# Probe 2 (#5974): separate "the DOCUMENTS edge was never written in this
	# cell" from "it was written here and the fault missed it". Without this, a
	# missing marker has two explanations and the failure message has to guess
	# between them.
	log "fail-graph-write-once-then-succeed-documentation: probe documentation edges and this cell's intent window"
	ifa_documentation_assert "failgraphwritedocumentation" "${bin_dir}" "${documentation_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-documentation: the documentation edge set does not match the expected set after the drain. assert-edges is set-exact, so this covers a MISSING DOCUMENTS edge (the MERGE never ran here, and the fault had nothing to intercept) AND an extra, duplicated, or wrong-typed edge (a write ran and produced something else). Read the assert-edges diff above before deciding which -- they point at different code (#5974)."
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE completed_at IS NULL) AS pending, min(created_at) AS first_created, max(completed_at) AS last_completed FROM shared_projection_intents WHERE projection_domain = 'documentation_edges';" \
		"${compose_file}" | sed 's/^/  documentation_edges intent window: /'
	capture_digest failgraphwritedocumentation
	assert_matches_baseline failgraphwritedocumentation
	# #5974: assert on the durable marker the fault decorator writes, not the
	# reducer's captured stderr. The log route raced the flush and made the SQL
	# sibling cell inert in CI while passing locally for weeks.
	local marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script_documentation}" "${documentation_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		die "fail-graph-write-once-then-succeed-documentation: the fault FIRED but on a different write than ${documentation_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement (#5974)."
	fi
	if [[ "${marker_rc}" -ne 0 ]]; then
		# No marker at all. Look for a write failure with a bash substring scan --
		# NOT an external tool. rg is absent on the CI runner that hosts this
		# gate, so a route that shells out to it fails silently the same way
		# #5974 describes for the SQL cell.
		local reducer_log_contents=""
		if [[ -r "${log_dir}/reducer-failgraphwritedocumentation.log" ]]; then
			reducer_log_contents="$(cat "${log_dir}/reducer-failgraphwritedocumentation.log" 2>/dev/null || true)"
		fi
		if [[ "${reducer_log_contents}" == *"${IFA_ONCE_MARKER_WRITE_FAILED_PREFIX}"* ]]; then
			printf '%s\n' "${reducer_log_contents}" | sed -n "/${IFA_ONCE_MARKER_WRITE_FAILED_PREFIX}/p" >&2 || true
			die "fail-graph-write-once-then-succeed-documentation: the marker WRITE FAILED (line above). The fault may well have fired -- this is an instrument failure, not evidence about the fault (#5974)."
		fi
		printf '\n=== sentinel family on disk (base: %s) ===\n' "${fault_once_script_documentation}" >&2
		ls -la "${fault_once_script_documentation}"* 2>&1 | sed 's/^/  /' >&2 || true
		printf '=== end sentinel family ===\n\n' >&2
		die "fail-graph-write-once-then-succeed-documentation: no once-fired marker beside ${fault_once_script_documentation}, and no marker-write failure reported. Probe 2 above already proved whether the targeted documentation edge was written at all, so read that result with the listing to tell 'the MERGE never ran here' apart from 'it ran and the anchor missed it' (#5974)."
	fi
	printf 'fail-graph-write-once-then-succeed-documentation: non-vacuous: once-fired marker names the targeted DOCUMENTS edge MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	teardown_cell failgraphwritedocumentation
	wall_times[failgraphwritedocumentation]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-documentation: cell wall time: %ss\n' "${wall_times[failgraphwritedocumentation]}"
}
