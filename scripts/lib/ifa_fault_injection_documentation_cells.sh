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
#   - cell_expirelease_documentation provably targets the documentation work
#     item by scoping both ifa_fault_wait_for_claimed and the forced-expiry
#     UPDATE to domain=documentation_materialization. It proves
#     lease-expiry-mid-handler reclaim (KindExpireLeaseMidHandler), NOT
#     killed-process reclaim (KindKillWorkerAfterClaim) -- deliberately, not
#     as a downgrade. #6149 follow-up item 8 found that
#     DocumentationEdgeMaterializationHandler.Handle performs no Postgres
#     write at all (only two graph writes through the same EdgeWriter,
#     confirmed at the interface, handler-struct-wiring, and concrete-writer-
#     construction level in go/cmd/reducer/main.go); every sibling family
#     that has a kill-worker cell (code_calls, deployable_unit_correlation)
#     does have a Postgres write in Handle to lock as the deterministic
#     blocker, so this family cannot make the kill-worker trigger
#     deterministic the way its siblings do. See the cell's own header for
#     the full reasoning and what it does and does not prove.
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
# baseline_documentation_retried IS set by this file's caller: cell_baseline
# (ifa_fault_injection_cells.sh, a shared file this worktree does not own)
# captures it the same way it captures baseline_code_call_retried, against a
# genuinely fault-free drive. (An earlier version of this comment claimed the
# splice had not landed yet; it had, and the comment had gone stale -- fixed
# here rather than left to mislead the next reader.)

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

# cell_expirelease_documentation proves documentation_materialization's
# reclaim path recovers a lease that expires while the handler may still be
# running, WITHOUT holding any lock -- there is no Postgres write in
# DocumentationEdgeMaterializationHandler.Handle to block on (#6149 follow-up
# item 8; see this file's own header for the full trace). This is a
# DIFFERENT guarantee than a kill-worker cell proves, not a weaker
# implementation of the same one:
#
#   - code_calls and deployable_unit_correlation each hold a genuine
#     ACCESS EXCLUSIVE lock on a Postgres table their own Handle writes,
#     which blocks the handler from acknowledging BEFORE a kill -9 lands --
#     that proves recovery from a worker PROCESS DYING mid-handler
#     (KindKillWorkerAfterClaim).
#   - This cell forces `claim_until = now()` on the genuinely claimed row
#     (mirroring cell_expirelease, the generic/unscoped sibling in
#     ifa_fault_injection_cells.sh:115-141, domain-scoped here the same way
#     the kill-worker cells already domain-scope their claimed-row wait) --
#     no process is killed, and the original handler goroutine may still be
#     running concurrently with the reclaim. That proves recovery from a
#     LEASE EXPIRING WHILE THE HANDLER MAY STILL BE ALIVE
#     (KindExpireLeaseMidHandler, the design doc's "opposite trigger" sibling
#     of kill-worker -- docs/internal/design/4389-ifa-conformance-platform.md,
#     Layer 4; faultreplay/script.go's KindExpireLeaseMidHandler doc comment).
#
# Do NOT read this cell as proving "a killed reducer process's claim on this
# domain gets reclaimed" -- it does not, and cannot, until this family gains
# some other durable write a lock could target. If Handle ever gains a
# Postgres write (an IntentWriter, an admission-decision write, anything),
# revisit whether a real kill-worker cell becomes possible here.
#
# UNPROVEN as committed: this needs a live fault-injection matrix run
# (Docker + NornicDB + Postgres) to confirm the forced expiry actually lands
# while a row is claimed and that the reclaim converges to the same
# three-edge exact set with attempt_count above baseline. Static gates below
# (syntax, structural pins, mirror tests) are green; the live behavior is
# not proven by this commit.
cell_expirelease_documentation() {
	local cell_start
	cell_start=$(date +%s)
	log "cell expire-lease-mid-handler-documentation: fresh stack"
	fresh_stack expireleasedocumentation
	drive_all_cassettes expireleasedocumentation
	local projector_pid reducer_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-expireleasedocumentation" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-expireleasedocumentation" reducer_pid "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "documentation_materialization")" \
		|| die "expire-lease-mid-handler-documentation: no documentation_materialization row was ever claimed -- non-vacuous precondition failed"
	printf 'expire-lease-mid-handler-documentation: non-vacuous: %s claimed/running documentation_materialization row(s) observed before forced expiry\n' "${claimed_before}"
	log "expire-lease-mid-handler-documentation: force claim_until = now() on documentation_materialization's claimed/running rows only (SQL, no kill, no lock)"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"UPDATE fact_work_items SET claim_until = now() WHERE stage = 'reducer' AND status IN ('claimed', 'running') AND domain = 'documentation_materialization';" \
		"${compose_file}" >/dev/null
	run_drain_gate expireleasedocumentation
	assert_no_dead_letters expireleasedocumentation
	ifa_documentation_assert "expireleasedocumentation" "${bin_dir}" "${documentation_expected_edges}" \
		|| die "expire-lease-mid-handler-documentation: recovered graph does not match the three-edge exact set"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_documentation_retried}" 15 "documentation_materialization" \
		|| die "expire-lease-mid-handler-documentation: documentation_materialization did not re-execute above its fault-free retry baseline -- evidence of reclaim after the forced expiry"
	capture_digest expireleasedocumentation
	assert_matches_baseline expireleasedocumentation
	teardown_cell expireleasedocumentation
	wall_times[expireleasedocumentation]=$(( $(date +%s) - cell_start ))
	printf 'expire-lease-mid-handler-documentation: cell wall time: %ss\n' "${wall_times[expireleasedocumentation]}"
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
