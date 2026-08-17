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
#   - cell_failgraphwrite_documentation anchors the graph-write fault to the
#     DOCUMENTS edge MERGE and proves the fault fired via
#     ifa_fault_assert_once_fault_marker, reading the durable marker the fault
#     decorator writes at injection time -- never a log grep (#5974: a log
#     grep shelled out to `rg`, which the runner lacks, and its "command not
#     found" read as "no fault" for months).
#
# THIS FAMILY HAS NO DETERMINISTIC MID-HANDLER FAULT CELL (#6149 follow-up
# item 8, both a ruling and its correction). Every sibling family with a
# kill-worker cell (code_calls, deployable_unit_correlation) has a Postgres
# write in Handle to hold a real ACCESS EXCLUSIVE lock on, which blocks the
# handler from acknowledging before a kill -9 lands. #6149 follow-up item 8
# first found that DocumentationEdgeMaterializationHandler.Handle performs no
# Postgres write at all (only two graph writes through the same EdgeWriter,
# confirmed at the interface, handler-struct-wiring, and concrete-writer-
# construction level in go/cmd/reducer/main.go) and, on that basis, replaced
# the wrong-table kill-worker lock with a domain-scoped expire-lease cell
# (forcing `claim_until = now()` on the claimed row, mirroring the generic
# cell_expirelease in ifa_fault_injection_cells.sh:115-141) instead of a held
# lock. That ALSO failed on the live matrix: the handler completed in
# `duration_seconds: 0.0073` (7ms) end to end, so by the time the forced-
# expiry UPDATE ran as its own separate step the row had already reached
# 'succeeded' and `WHERE status IN ('claimed', 'running')` matched zero rows.
# Nothing was expired, nothing was reclaimed, attempt_count never moved above
# baseline. Domain-scoping cell_expirelease reintroduced exactly the race the
# original lock was supposed to remove, just moved from "the lock blocks
# nothing" to "the trigger never lands" -- a different vacuous shape, not a
# fix.
#
# There is no existing mechanism in this harness that can hold a 7ms handler
# open long enough for either trigger (kill -9 or a forced-expiry UPDATE
# issued as a separate SQL round-trip) to land deterministically, because
# neither trigger blocks the handler itself -- they both race it. What WOULD
# close this: a hang/block fault mode in the in-binary decorator
# (go/internal/storage/cypher/fault_executor.go), which could pause the
# DOCUMENTS edge MERGE mid-transaction the same way
# fail-graph-write-once-then-succeed already intercepts it to fail it once --
# giving kill-worker or expire-lease a real window without needing a Postgres
# write to lock. That is new decorator capability, not a bash-harness change,
# and it is out of scope here; a future family whose handler is provably slow
# enough (or whose handler gains a real Postgres write) should re-evaluate
# before copying this family's gap forward.
#
# The loss this leaves is bounded and stated precisely, not implied: mid-
# handler interruption (a worker process dying, or a lease expiring, while
# this domain's row is claimed) is UNPROVEN for documentation_materialization.
# Injected graph-write failure is NOT unproven -- cell_failgraphwrite_documentation
# below exercises the family's real write path through the fault decorator's
# genuine fail-graph-write-once-then-succeed mode and remains full coverage
# for that fault class.
#
# The removal is safe, not just asserted -- two runs prove two different
# claims, and citing only one would conflate them. Cell counts below are
# DISPATCHED cells; derive them rather than trusting a number here, with
# `rg -c '^cell_[a-z_]+$' scripts/verify-ifa-fault-injection.sh` at either
# commit. The runs themselves are reproduced by `make ifa-fault-injection`
# at that commit; their logs are not committed (they carry local paths), so
# every line below is quoted verbatim AND cited by file:line against
# live_fi_cf2d033b1.log (the RED run) or live_fi_1f85dad68.log (the GREEN
# run) -- the quote is checkable without opening either file, the citation
# is only where it came from. Re-running reproduces the semantic lines and
# the digest; it will NOT reproduce the timings (8s, 69s, the handler
# durations), so a mismatch there is expected, not a discrepancy.
#
# The RED run at cf2d033b1 -- whose driver still dispatched
# cell_expirelease_documentation, one more than here -- established the cell
# could never fire. Same run, same mechanism, opposite outcomes. The generic
# cell_expirelease observed its row, forced expiry, and PASSED
# (live_fi_cf2d033b1.log:595,614):
#
#   expire-lease-mid-handler: non-vacuous: 1 claimed/running row(s) observed
#     before forced expiry
#   expire-lease-mid-handler: cell wall time: 8s
#
# while the documentation-scoped copy observed its row and FAILED
# (live_fi_cf2d033b1.log:1596):
#
#   verify-ifa-fault-injection: expire-lease-mid-handler-documentation:
#     documentation_materialization did not re-execute above its fault-free
#     retry baseline -- evidence of reclaim after the forced expiry
#
# The reason is handler width. BOTH documentation_materialization executions
# observed in that run were milliseconds wide, each at the end of a queue
# wait an order of magnitude longer (live_fi_cf2d033b1.log:3970 and :4048):
#
#   handler_duration_seconds 0.022566833  queue_wait_seconds 0.969149  (43x)
#   handler_duration_seconds 0.007342625  queue_wait_seconds 0.09283   (13x)
#
# Even the slower one leaves a ~23ms window for a forced expiry issued as a
# separate step after a two-stage observation -- see the mechanism above.
# Scoping the fault to a family whose handler is milliseconds wide
# reintroduces the race the lock was meant to remove. That is what justifies
# removal -- a green run afterward cannot show the removal was CORRECT, only
# that it was HARMLESS.
#
# The GREEN run at 1f85dad68 is the harmless-removal proof: FI_EXIT=0 --
# carrying, on its own, the fact that every dispatched cell completed -- and
# fail-graph-write-once-then-succeed-documentation stayed green (69s, digest
# 63558e35...) at the same digest value the family's other unscoped cells
# produce, so removing the racy reclaim cell did not perturb what the
# surviving cell proves. The #6149 deployable-unit trio (baseline,
# kill-worker, fail-graph-write) also stayed green in that same run,
# confirming this branch did not regress the family merged the day before.
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
# CLAIMED_ROW_WAIT_TIMEOUT are cell_baseline/reclaim-cell globals this file no
# longer reads now that the family has no reclaim cell -- see the gap note
# above.

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
