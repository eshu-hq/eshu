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
#     domain=documentation_materialization, holds a deterministic
#     shared_projection_intents lock so the short handler cannot acknowledge
#     between the claimed-row observation and the kill (the code_calls #5991
#     pattern, cell_killworker_code_calls -- the newer and stronger proof
#     shape than the plain cell_killworker_sql one, adopted here on review),
#     and asserts a post-recovery attempt_count > fault-free-baseline delta,
#     not just a digest match.
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
# work_dir, wall_times, use_compose, compose_file, documentation_edge_operation_match,
# documentation_expected_edges, baseline_documentation_retried,
# CLAIMED_ROW_WAIT_TIMEOUT, FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, bg_pids,
# log, die, plus the fresh_stack / drive_all_cassettes / run_drain_gate /
# assert_no_dead_letters / capture_digest / assert_matches_baseline /
# teardown_cell helpers) rather than taking them as arguments. Sources
# scripts/lib/ifa_documentation_live.sh for ifa_documentation_assert.
#
# baseline_documentation_retried is NOT set by this file -- it must be
# captured against a genuinely fault-free drive, the same way
# baseline_code_call_retried is captured inside cell_baseline
# (ifa_fault_injection_cells.sh:74-76, a shared file this worktree does not
# own). Until that splice lands, cell_killworker_documentation's retry-delta
# assertion below reads an unset global and fails closed rather than silently
# comparing against zero.

# ifa_documentation_require_fresh_documents_edges fails closed unless a fresh
# stack has a numeric zero count of DOCUMENTS edges in the live graph. This
# USED to be ifa_documentation_require_fresh_intents, querying
# shared_projection_intents WHERE projection_domain = 'documentation_edges',
# mirroring ifa_code_call_require_fresh_intents -- vacuous for this family:
# documentationEdgeMaterializationHandler (documentation_edge_materialization.go)
# calls only h.EdgeWriter.WriteEdges (:90); unlike code_call_materialization's
# handler, it has no IntentWriter field and never calls UpsertIntents. The
# only INSERT INTO shared_projection_intents in the repo is reachable solely
# through UpsertIntents (shared_intents_upsert.go), so this count was always 0
# whether the stack was genuinely fresh or not -- the same vacuous-check class
# #6149 found and fixed for ifa_deployable_unit_require_fresh_intents
# (scripts/lib/ifa_fault_injection_deployable_unit_cells.sh), for the same
# reason: the handler this precondition is meant to guard never writes the
# table it queried.
#
# Repointed to the graph, the only place DOCUMENTS edges exist, mirroring
# ifa_deployable_unit_require_fresh_intents's graph-dump-plus-jq shape --
# same four fail-closed properties (dump/count failed, empty, non-numeric,
# non-zero), each rejected distinctly; empty and non-numeric read as unknown,
# never as a legitimate zero. Unlike that sibling, this function still
# captures and returns the exact dump/count exit code on failure (an
# explicit `if out="$(...)"; then rc=0; else rc=$?; fi`, not a bare
# assignment or a plain `if ! ...; then return 1; fi`), matching this
# family's OWN prior idiom -- ifa_documentation_release_intent_lock and the
# retry-count snapshot in cell_killworker_documentation below use the same
# shape, and the mirror test pins the exact returned code.
#
# Renamed from ifa_documentation_require_fresh_intents: the old name asserted
# a claim about intents that was never true for this family, and a
# vacuous-but-plausible name is exactly the trap #6149 tripped on for its
# deployable-unit sibling.
#
# ifa_documentation_fresh_stack_dump_path is deliberately NOT `local`: a
# `RETURN` trap referencing a `local` variable can fire after this function's
# own return, on the NEXT function to return anywhere in this shell, once its
# local binding is already gone -- confirmed live on the deployable-unit
# sibling ("dump_path: unbound variable" aborted that fault-injection matrix,
# ifa_deployable_unit_fresh_stack_dump_path's own header). Named distinctly
# from every sibling family's global to avoid any doubt about which dump each
# one is using.
#
# Args: cell bin_dir
ifa_documentation_require_fresh_documents_edges() {
	local cell="$1" bin_dir="$2"
	local count count_rc dump_rc
	if ! command -v jq >/dev/null 2>&1; then
		printf '%s: fresh-stack precondition requires jq, which is not on PATH; treat this as unknown, not as a verdict\n' "${cell}" >&2
		return 1
	fi
	ifa_documentation_fresh_stack_dump_path="$(mktemp)" || {
		printf '%s: fresh-stack precondition could not create a scratch file for the graph dump; treat this as unknown, not as a verdict\n' "${cell}" >&2
		return 1
	}
	trap 'rm -f "${ifa_documentation_fresh_stack_dump_path}"' RETURN
	if "${bin_dir}/eshu-ifa" graph-dump -out "${ifa_documentation_fresh_stack_dump_path}"; then
		dump_rc=0
	else
		dump_rc=$?
	fi
	if [[ "${dump_rc}" -ne 0 ]]; then
		printf '%s: fresh-stack precondition graph-dump FAILED (exit %s); treat this as unknown, not as a verdict\n' "${cell}" "${dump_rc}" >&2
		return "${dump_rc}"
	fi
	if count="$(jq '[.edges[] | select(.type == "DOCUMENTS")] | length' "${ifa_documentation_fresh_stack_dump_path}")"; then
		count_rc=0
	else
		count_rc=$?
	fi
	if [[ "${count_rc}" -ne 0 ]]; then
		printf '%s: fresh-stack precondition could not count DOCUMENTS edges in the graph dump (exit %s); treat this as unknown, not as a verdict\n' "${cell}" "${count_rc}" >&2
		return "${count_rc}"
	fi
	count="$(printf '%s' "${count}" | tr -d '[:space:]')"
	if [[ -z "${count}" ]]; then
		printf '%s: fresh-stack precondition edge count came back empty; treat that as unknown, not as zero\n' "${cell}" >&2
		return 1
	fi
	if [[ ! "${count}" =~ ^[0-9]+$ ]]; then
		printf '%s: fresh-stack precondition edge count %q is non-numeric; treat that as unknown, not as zero\n' \
			"${cell}" "${count}" >&2
		return 1
	fi
	if [[ "${count}" != "0" ]]; then
		printf '%s: %s DOCUMENTS edge(s) survived fresh_stack\n' "${cell}" "${count}" >&2
		return 1
	fi
	printf '%s: fresh-stack precondition: 0 DOCUMENTS edges in the graph\n' "${cell}"
}

# ifa_documentation_start_intent_lock holds the first durable write used by
# documentation_materialization. This makes the claimed-row observation and
# kill deterministic: the handler cannot acknowledge between the observation
# and kill, and the post-restart attempt_count proof identifies the same
# domain. Mirrors ifa_code_call_start_intent_lock exactly.
ifa_documentation_start_intent_lock() {
	local cell="$1" pid_var="$2"
	local app_name="ifa_documentation_lock_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE shared_projection_intents IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/documentation-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/documentation-lock-${cell}.log" 2>&1 &
	fi
	local holder_pid=$!
	bg_pids+=("${holder_pid}")
	printf -v "${pid_var}" '%s' "${holder_pid}"

	local i lock_count
	for i in $(seq 1 60); do
		lock_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE a.application_name = '${app_name}' AND l.relation = 'shared_projection_intents'::regclass AND l.mode = 'AccessExclusiveLock' AND l.granted;" \
			"${compose_file}" | tr -d '[:space:]')"
		if [[ "${lock_count}" == "1" ]]; then
			return 0
		fi
		sleep 0.25
	done
	return 1
}

# ifa_documentation_release_intent_lock terminates the named lock-holder
# backend, then joins its local psql/docker process before the replacement
# reducer starts. Mirrors ifa_code_call_release_intent_lock exactly.
ifa_documentation_release_intent_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_documentation_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_killworker_documentation proves a genuinely in-flight documentation
# handler is reclaimed after process death. The table lock prevents the short
# handler from acknowledging before kill; attempt_count > the clean baseline
# proves the replacement reducer re-executed that domain, not merely another
# queued row. Mirrors cell_killworker_code_calls (#5991) -- adopted over the
# simpler cell_killworker_sql shape on review because a marker/digest match
# alone does not prove the kill landed mid-handler, only that recovery
# reached the same end state; the retry-count delta closes that gap.
cell_killworker_documentation() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-documentation: fresh stack"
	fresh_stack killworkerdocumentation
	drive_all_cassettes killworkerdocumentation
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerdocumentation" projector_pid "${bin_dir}/eshu-projector"
	ifa_documentation_start_intent_lock "killworkerdocumentation" lock_holder_pid \
		|| die "kill-worker-after-claim-documentation: could not acquire the deterministic shared_projection_intents blocker"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdocumentation-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "documentation_materialization")" \
		|| die "kill-worker-after-claim-documentation: no documentation_materialization row was claimed while its durable write was blocked"
	printf 'kill-worker-after-claim-documentation: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_documentation_release_intent_lock "killworkerdocumentation" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdocumentation-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerdocumentation
	assert_no_dead_letters killworkerdocumentation
	ifa_documentation_assert "killworkerdocumentation" "${bin_dir}" "${documentation_expected_edges}" \
		|| die "kill-worker-after-claim-documentation: recovered graph does not match the three-edge exact set"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_documentation_retried}" 15 "documentation_materialization" \
		|| die "kill-worker-after-claim-documentation: documentation_materialization did not re-execute above its fault-free retry baseline"
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

	# Probe 1 (#5974): a genuinely fresh stack has no DOCUMENTS edges in the
	# live graph (see ifa_documentation_require_fresh_documents_edges's own
	# header for why this no longer queries shared_projection_intents).
	# Survivors mean this cell is replaying an earlier cell's completed work
	# -- intent IDs are deterministic and completed rows are never reopened,
	# so the drive produces no new graph writes, nothing reaches the fault
	# decorator, and every later assertion still passes on edges that are
	# already there. Only meaningful when this script owns the stack;
	# --no-compose skips it (see ifa_documentation_require_fresh_documents_edges).
	if [[ "${use_compose}" -eq 1 ]]; then
		ifa_documentation_require_fresh_documents_edges "fail-graph-write-once-then-succeed-documentation" \
			"${bin_dir}" \
			|| die "fail-graph-write-once-then-succeed-documentation: fresh-stack precondition failed"
	else
		printf 'fail-graph-write-once-then-succeed-documentation: fresh-stack precondition SKIPPED (--no-compose owns the stack; surviving DOCUMENTS edges are not a leak)\n'
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
