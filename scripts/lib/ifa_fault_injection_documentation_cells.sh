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
#   - cell_killworker_documentation provably targets the exact documentation
#     fixture work item and blocks its Postgres ACK on a scoped advisory-lock
#     trigger after the graph write. It proves that exact ACK backend is
#     blocked, kills the host reducer, terminates the orphaned backend, and
#     verifies the attempt-1 lease tuple survived before releasing the barrier
#     for replacement-process reclaim. The post-recovery attempt_count must
#     exceed the fault-free baseline; a digest match alone is insufficient.
#   - cell_failgraphwrite_documentation anchors the graph-write fault to the
#     DOCUMENTS edge MERGE and proves the fault fired via
#     ifa_fault_assert_once_fault_marker, reading the durable marker the fault
#     decorator writes at injection time -- never a log grep (#5974: a log
#     grep shelled out to `rg`, which the runner lacks, and its "command not
#     found" read as "no fault" for months).
#
# MID-HANDLER INTERRUPTION IS PROVEN FOR THIS FAMILY, BY AN ACK BARRIER
# (#5998, superseding the gap #6149 follow-up item 8 recorded here).
#
# The obstacle that ruling identified is real. DocumentationEdgeMaterialization
# Handler.Handle performs no Postgres write at all (only two graph writes
# through the same EdgeWriter), so there is no row a kill-worker cell can lock,
# and the handler finishes in single-digit milliseconds -- measured at
# duration_seconds 0.0073 -- so a forced-expiry UPDATE issued as its own
# round-trip races it and matches zero rows. Both mechanisms tried there, a
# wrong-table kill-worker lock and then a domain-scoped expire-lease, failed
# for that reason. The conclusion drawn was that only a hang/block fault mode
# in the in-binary decorator could close it.
#
# cell_killworker_documentation closes it without new decorator capability, by
# not racing the handler at all. Instead of blocking Handle, it blocks the
# QUEUE ACK that always follows it:
#
#   - a BEFORE UPDATE trigger on public.fact_work_items whose WHEN clause is
#     scoped to this family's exact stage, domain, scope and generation, and to
#     the claimed-or-running -> succeeded, lease-cleared transition;
#   - the trigger takes pg_advisory_xact_lock(5998, 5994);
#   - a SEPARATE session holds that advisory lock, so killing the host reducer
#     cannot release it and the orphaned backend can be fenced first.
#
# Because the trigger fires synchronously inside the ACK UPDATE, there is no
# window to miss and handler width is irrelevant. That is the specific reason
# this is not a third variant of the two mechanisms that failed.
#
# Probed before adoption on the reference validation host (16 vCPU, 123 GiB,
# Linux x86_64, Docker 29.3.1, postgres:18-alpine) on 2026-08-17, driving the
# committed helpers in this family rather than a transcription of them, three
# runs of three:
#
#   - the ACK session sits in wait_event_type=Lock while the work item stays
#     claimed with its lease owner intact;
#   - a control row in a different domain, same scope, updates freely, so the
#     WHEN clause is scoped and not a blanket block on a hot table;
#   - killing the acking session leaves the item un-acked and the advisory lock
#     still granted to the holder pid, not to the dead session;
#   - releasing the holder completes the item exactly once;
#   - the drop leaves zero triggers and zero functions behind.
#
# What that probe did NOT cover, stated so it is not read as more than it is:
# it drove a psql session issuing the same ACK UPDATE, not the real
# eshu-reducer, and ended it with pg_terminate_backend rather than SIGKILL of a
# reducer process. It proves the barrier mechanism. The live fault matrix is
# what proves this cell end to end.
#
# This file is a plain function library, not a script (no `set -euo
# pipefail`; see ifa_fault_injection_driver.sh's identical note). Every
# function here reads driver-owned globals (bin_dir, tagged_bin_dir, log_dir,
# work_dir, wall_times, use_compose, compose_file, documentation_edge_operation_match,
# documentation_expected_edges, FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN,
# bg_pids, log, die, plus the fresh_stack / drive_all_cassettes /
# run_drain_gate / assert_no_dead_letters / capture_digest /
# assert_matches_baseline / teardown_cell helpers) rather than taking them as
# arguments. Sources scripts/lib/ifa_documentation_live.sh for
# ifa_documentation_assert. baseline_documentation_retried and
# CLAIMED_ROW_WAIT_TIMEOUT are cell_baseline/reclaim-cell globals that
# cell_killworker_documentation reads: the first is the fault-free retry
# baseline its post-recovery attempt_count must exceed, the second bounds the
# wait for the blocked ACK backend to appear.

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
# baseline_documentation_retried is captured by cell_baseline against the
# genuinely fault-free drive, alongside the other family-specific retry
# baselines, before any recovery cell runs; this file only consumes it.
#
# Repointed to the graph, the only place DOCUMENTS edges exist, mirroring
# ifa_deployable_unit_require_fresh_intents's graph-dump-plus-jq shape --
# same four fail-closed properties (dump/count failed, empty, non-numeric,
# non-zero), each rejected distinctly; empty and non-numeric read as unknown,
# never as a legitimate zero. Unlike that sibling, this function still
# captures and returns the exact dump/count exit code on failure (an
# explicit `if out="$(...)"; then rc=0; else rc=$?; fi`, not a bare
# assignment or a plain `if ! ...; then return 1; fi`), matching this
# family's own idiom elsewhere in this file, and the mirror test pins the
# exact returned code.
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

# cell_killworker_documentation proves a genuinely in-flight documentation
# handler is reclaimed after process death. The scoped ACK barrier proves the
# graph write completed while the queue row stayed on its attempt-1 lease; the
# retry-count delta proves the replacement reducer later reclaimed that row.
cell_killworker_documentation() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-documentation: fresh stack"
	fresh_stack killworkerdocumentation
	drive_all_cassettes killworkerdocumentation
	local projector_pid reducer_pid_before reducer_pid_after holder_pid holder_backend_pid
	local claimed_before blocked_pair waiter_pid blocked_holder_pid claim_before claim_after
	ifa_det_start_bg "${log_dir}" "projector-killworkerdocumentation" projector_pid "${bin_dir}/eshu-projector"
	ifa_documentation_start_ack_barrier "killworkerdocumentation" holder_pid holder_backend_pid \
		|| die "kill-worker-after-claim-documentation: could not install and acquire the deterministic fact_work_items ACK barrier"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdocumentation-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "documentation_materialization")" \
		|| die "kill-worker-after-claim-documentation: no documentation_materialization row was claimed"
	blocked_pair="$(ifa_documentation_wait_for_blocked_ack "killworkerdocumentation" "${CLAIMED_ROW_WAIT_TIMEOUT}")" \
		|| die "kill-worker-after-claim-documentation: exact documentation ACK did not block on its advisory holder"
	IFS='|' read -r waiter_pid blocked_holder_pid <<<"${blocked_pair}"
	[[ "${blocked_holder_pid}" == "${holder_backend_pid}" ]] \
		|| die "kill-worker-after-claim-documentation: ACK waiter was blocked by backend ${blocked_holder_pid}, want holder ${holder_backend_pid}"
	ifa_documentation_ack_waiter_pid="${waiter_pid}"
	claim_before="$(ifa_documentation_claim_snapshot "killworkerdocumentation")" \
		|| die "kill-worker-after-claim-documentation: exact attempt-1 documentation claim was not retained"
	ifa_documentation_assert "killworkerdocumentation-blocked" "${bin_dir}" "${documentation_expected_edges}" \
		|| die "kill-worker-after-claim-documentation: graph did not reach the exact three-edge set before ACK blocking"
	printf 'kill-worker-after-claim-documentation: non-vacuous: %s claimed row; ACK backend %s blocked by holder %s after exact graph write\n' \
		"${claimed_before}" "${waiter_pid}" "${holder_backend_pid}"
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid_before}" KILL \
		|| die "kill-worker-after-claim-documentation: failed to stop, join, and untrack the owned reducer"
	ifa_documentation_terminate_blocked_ack "killworkerdocumentation" "${waiter_pid}" \
		|| die "kill-worker-after-claim-documentation: failed to terminate the exact orphaned ACK backend"
	ifa_documentation_wait_for_ack_backend_gone "killworkerdocumentation" "${waiter_pid}" 15 \
		|| die "kill-worker-after-claim-documentation: orphaned ACK backend or advisory wait survived termination"
	claim_after="$(ifa_documentation_claim_snapshot "killworkerdocumentation")" \
		|| die "kill-worker-after-claim-documentation: documentation claim was not retained after ACK backend termination"
	[[ "${claim_after}" == "${claim_before}" ]] \
		|| die "kill-worker-after-claim-documentation: attempt/lease tuple changed while fencing the orphaned ACK backend"
	ifa_documentation_release_ack_barrier "killworkerdocumentation" "${holder_pid}" "${holder_backend_pid}" \
		|| die "kill-worker-after-claim-documentation: failed to release and remove the documentation ACK barrier"
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
	#
	# This used to also print a documentation_edges shared_projection_intents
	# "intent window" (total/pending/first_created/last_completed) right below
	# the assert. It always printed total=0, pending=0, and two NULLs, for the
	# same structural reason ifa_documentation_require_fresh_documents_edges's
	# own header explains: documentationEdgeMaterializationHandler has no
	# IntentWriter and never writes that table, so the query could not report
	# anything else regardless of whether this cell was healthy or broken
	# (#6149 follow-up item 9). During a failure investigation an
	# unconditional zero reads as a finding ("nothing landed"), not as the
	# structural non-signal it actually was -- removed rather than repointed
	# at the graph, since ifa_documentation_assert immediately above already
	# is a fail-closed, graph-based, exact-set check with its own diff on
	# failure; a second graph-based count here would report strictly less
	# than that diff already does.
	log "fail-graph-write-once-then-succeed-documentation: probe documentation edges"
	ifa_documentation_assert "failgraphwritedocumentation" "${bin_dir}" "${documentation_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-documentation: the documentation edge set does not match the expected set after the drain. assert-edges is set-exact, so this covers a MISSING DOCUMENTS edge (the MERGE never ran here, and the fault had nothing to intercept) AND an extra, duplicated, or wrong-typed edge (a write ran and produced something else). Read the assert-edges diff above before deciding which -- they point at different code (#5974)."
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
