#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# deployable_unit_edges-targeted live fault cells (#5993). Sourced by
# verify-ifa-fault-injection.sh; the driver owns strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, GATE_DRAIN_TIMEOUT, digests,
# wall_times, bg_pids, sql_expected_edges, code_call_expected_edges, log,
# die), exactly like ifa_fault_injection_code_call_cells.sh. Also sources
# ifa_deployable_unit_live.sh's helpers
# (ifa_deployable_unit_live_assert_empty_before_maintenance,
# ifa_deployable_unit_live_run_maintenance_pass,
# ifa_deployable_unit_live_assert_readiness_opened,
# ifa_deployable_unit_live_report_intents_after_maintenance,
# ifa_deployable_unit_live_report_resolved_deploys_from_count,
# ifa_deployable_unit_live_report_correlation_reopen,
# ifa_deployable_unit_live_assert). Also sources
# ifa_fault_injection_deployable_unit_lock.sh's
# ifa_deployable_unit_require_admission_decisions_written,
# ifa_deployable_unit_start_admission_decisions_lock, and
# ifa_deployable_unit_release_admission_decisions_lock -- split into its own
# file (#6149) to keep this file under the repository's 500-line cap, the
# same reason ifa_deployable_unit_live_diagnostics.sh exists.
#
# BOTH cells below need one thing no sibling family's cells do: a bootstrap-
# index maintenance pass between the drive and the point where a fault can
# usefully target this family's work, because deployable_unit_correlation is
# gated shut on the FIRST pass (see ifa_deployable_unit_live.sh's header for
# the full traced rationale: the readiness gate
# CrossRepoRelationshipHandler.Resolve checks is never published without a
# maintenance pass in this gate's runtime). So the shape here is: fresh_stack
# -> drive_all_cassettes -> drive_deployable_unit_cassette -> a PRE-
# maintenance drain (so every other family reaches a terminal state cleanly)
# -> ONE bootstrap-index maintenance pass -> the fault is injected on the
# POST-maintenance drain, which is the first point deployable_unit_correlation
# has real work to do.
#
# drive_deployable_unit_cassette (ifa_fault_injection_driver.sh) is a
# SEPARATE call from drive_all_cassettes, immediately after it, in all three
# cells below -- not folded into drive_all_cassettes itself. Driving this
# family's cassette unconditionally into every one of the suite's eleven
# cells used to be the convenient choice, but duplicate-delivery's redelivery
# UPDATE (`WHERE stage = 'reducer' AND status = 'succeeded'`,
# ifa_fault_redeliver_succeeded) touches every succeeded reducer row
# regardless of admission outcome, and a live run proved that surface was
# never actually covered (#5993 review). Scoping the drive to only the three
# cells that need it removes the untested surface instead of trying to prove
# it safe after the fact.
#
# RULING (superseding an earlier draft that omitted baseline-digest comparison
# entirely): the shared digests[baseline] (cell_baseline,
# ifa_fault_injection_cells.sh) never runs a maintenance pass, so it has ZERO
# deployable_unit_edges materialization by construction -- comparing against
# it would never pass for a cell that DOES run the pass, fault or not. But
# ifa_deployable_unit_live_assert's exact-edge-set check and a whole-graph
# digest comparison prove DIFFERENT things: the exact-set check covers only
# this family's own relationship types, while the digest catches CROSS-family
# collateral damage from fault recovery (assert_matches_baseline's own die
# message calls a divergence "a real recovery/concurrency defect"). This
# family's fault path uniquely runs a bootstrap-index maintenance pass, which
# reopens every crossScopeCorrelationReopenDomains domain and republishes
# readiness -- collateral from a faulted-then-recovered maintenance leg is a
# plausible defect class nothing else in the suite would see. Dropping the
# digest check would have silently lost exactly that coverage.
#
# The fix is a family-scoped baseline, not an omission: cell_baseline_deployable_unit
# below runs the SAME fresh_stack -> drive -> pre-maintenance drain -> ONE
# maintenance pass -> post-maintenance drain shape as the two fault cells,
# fault-free, and captures digests[baseline_deployable_unit] +
# baseline_deployable_unit_retried. Both fault cells then call
# assert_matches_baseline with that key (assert_matches_baseline,
# ifa_fault_injection_driver.sh, now takes an optional baseline_key parameter
# defaulting to "baseline" so every other family's call sites are
# byte-identical). This restores the true invariant: every fault cell compares
# against a fault-free baseline with the SAME terminal state -- this family
# does not prove recovery differently from its siblings, it proves it
# identically, against its own correctly-shaped baseline.

# ifa_deployable_unit_require_fresh_intents fails closed unless a fresh
# compose stack has a numeric zero count of CORRELATES_DEPLOYABLE_UNIT edges
# in the live graph. This USED to query shared_projection_intents WHERE
# projection_domain = 'deployable_unit_edges', mirroring
# ifa_code_call_require_fresh_intents -- vacuous for this family for the
# exact reason ifa_deployable_unit_live_assert_empty_before_maintenance's own
# header documents (ifa_deployable_unit_live.sh): this handler never writes
# shared_projection_intents at all, so that count is always 0 whether the
# stack is genuinely fresh or not. This check landed in the same review that
# established that fact and should have been repointed with it (#6149
# review). Repointed to the graph, the only place these edges exist,
# mirroring ifa_deployable_unit_live_assert_empty_before_maintenance exactly
# -- same four fail-closed properties (dump/count failed, empty, non-numeric,
# non-zero), each rejected distinctly; empty and non-numeric read as unknown,
# never as a legitimate zero.
#
# ifa_deployable_unit_fresh_stack_dump_path is deliberately NOT `local`, for
# the same reason ifa_deployable_unit_before_probe_dump_path (that sibling
# function) is not: a `RETURN` trap referencing a `local` variable can fire
# after this function's own return, on the NEXT function to return anywhere
# in this shell, once its local binding is already gone -- confirmed live
# earlier on this same branch ("dump_path: unbound variable" aborted the
# fault-injection matrix). Named distinctly from that sibling's global to
# avoid any doubt about which dump each one is using.
#
# Args: cell bin_dir
ifa_deployable_unit_require_fresh_intents() {
	local cell="$1" bin_dir="$2"
	local count
	if ! command -v jq >/dev/null 2>&1; then
		printf '%s: fresh-stack precondition requires jq, which is not on PATH; treat this as unknown, not as a verdict\n' "${cell}" >&2
		return 1
	fi
	ifa_deployable_unit_fresh_stack_dump_path="$(mktemp)" || {
		printf '%s: fresh-stack precondition could not create a scratch file for the graph dump; treat this as unknown, not as a verdict\n' "${cell}" >&2
		return 1
	}
	trap 'rm -f "${ifa_deployable_unit_fresh_stack_dump_path}"' RETURN
	if ! "${bin_dir}/eshu-ifa" graph-dump -out "${ifa_deployable_unit_fresh_stack_dump_path}"; then
		printf '%s: fresh-stack precondition graph-dump FAILED; treat this as unknown, not as a verdict\n' "${cell}" >&2
		return 1
	fi
	if ! count="$(jq '[.edges[] | select(.type == "CORRELATES_DEPLOYABLE_UNIT")] | length' "${ifa_deployable_unit_fresh_stack_dump_path}")"; then
		printf '%s: fresh-stack precondition could not count CORRELATES_DEPLOYABLE_UNIT edges in the graph dump; treat this as unknown, not as a verdict\n' "${cell}" >&2
		return 1
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
		printf '%s: %s CORRELATES_DEPLOYABLE_UNIT edge(s) survived fresh_stack\n' "${cell}" "${count}" >&2
		return 1
	fi
}

# cell_baseline_deployable_unit is this family's fault-free reference: fresh
# stack -> drive -> pre-maintenance drain (asserts zero edges, proving the
# readiness gate is genuinely shut, not racy) -> ONE bootstrap-index
# maintenance pass -> post-maintenance drain -> exact-set assert -> capture
# digests[baseline_deployable_unit] and baseline_deployable_unit_retried.
# Both fault cells below compare against this, not the shared cell_baseline,
# for the reason in this file's header.
cell_baseline_deployable_unit() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-deployable-unit: fresh stack"
	fresh_stack baseline_deployable_unit
	ifa_deployable_unit_require_fresh_intents "baseline-deployable-unit" "${bin_dir}" \
		|| die "baseline-deployable-unit: fresh-stack precondition failed"
	drive_all_cassettes baseline_deployable_unit
	drive_deployable_unit_cassette baseline_deployable_unit

	log "baseline-deployable-unit: pre-maintenance drain (deployable_unit_correlation is gated shut here by design)"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_deployable_unitpre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_deployable_unitpre" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_deployable_unitpre
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
	ifa_deployable_unit_live_assert_empty_before_maintenance "${bin_dir}" \
		|| die "baseline-deployable-unit: expected zero deployable_unit_edges rows before the maintenance pass"
	# The other families this cell drives (sql_relationships, code_calls) are
	# NOT gated: they already converged in the drain above. Asserting them
	# here, then again after the maintenance pass, gives an explicit
	# attribution point for the maintenance pass's one unproven risk (a
	# reconcile path misreading the zero-repo filesystem collection as a
	# removal and retracting facts) -- a divergence here names the exact
	# family the pass corrupted, rather than only a whole-graph digest
	# mismatch against this cell's own baseline.
	"${bin_dir}/eshu-ifa" assert-edges -domain sql_relationships -expected "${sql_expected_edges}" \
		|| die "baseline-deployable-unit: sql_relationships does not match its exact set BEFORE the maintenance pass"
	"${bin_dir}/eshu-ifa" assert-edges -domain code_calls -expected "${code_call_expected_edges}" \
		|| die "baseline-deployable-unit: code_calls does not match its exact set BEFORE the maintenance pass"

	ifa_deployable_unit_live_run_maintenance_pass "baseline_deployable_unit" "${bin_dir}" "${log_dir}" \
		|| die "baseline-deployable-unit: bootstrap-index maintenance pass failed"

	ifa_det_start_bg "${log_dir}" "projector-baseline_deployable_unit" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_deployable_unit" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_deployable_unit
	assert_no_dead_letters baseline_deployable_unit
	ifa_deployable_unit_live_assert_readiness_opened "${log_dir}" "reducer-baseline_deployable_unit" "baseline_deployable_unit" \
		|| die "baseline-deployable-unit: post-maintenance reducer log does not prove the readiness gate opened"
	# Permanent proof that admission_decisions is a real write for this
	# fixture, not a claim taken on trust -- see
	# ifa_deployable_unit_require_admission_decisions_written's own header.
	# Runs once, here, because it is a property of the fixture and the
	# handler shared by all three cells that drive this cassette, and this
	# is the one cell where Handle runs to completion unblocked.
	ifa_deployable_unit_require_admission_decisions_written \
		"baseline-deployable-unit" "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		|| die "baseline-deployable-unit: admission_decisions precondition for the fault cells' lock target failed"
	"${bin_dir}/eshu-ifa" assert-edges -domain sql_relationships -expected "${sql_expected_edges}" \
		|| die "baseline-deployable-unit: sql_relationships no longer matches its exact set AFTER the maintenance pass -- the maintenance pass corrupted an unrelated family"
	"${bin_dir}/eshu-ifa" assert-edges -domain code_calls -expected "${code_call_expected_edges}" \
		|| die "baseline-deployable-unit: code_calls no longer matches its exact set AFTER the maintenance pass -- the maintenance pass corrupted an unrelated family"
	ifa_deployable_unit_live_report_intents_after_maintenance "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}"
	ifa_deployable_unit_live_report_resolved_deploys_from_count "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}"
	ifa_deployable_unit_live_report_correlation_reopen "${log_dir}" "baseline_deployable_unit"
	if ! ifa_deployable_unit_live_assert "${bin_dir}" "${deployable_unit_expected_edges}"; then
		local converge_rc=0
		ifa_deployable_unit_live_converge_edges "baseline_deployable_unit" "${bin_dir}" "${log_dir}" \
			"${deployable_unit_expected_edges}" run_drain_gate baseline_deployable_unit || converge_rc=$?
		case "${converge_rc}" in
		0) ;;
		2) die "baseline-deployable-unit: a maintenance-pass convergence retry crashed (bootstrap-index itself failed), not an eventual-consistency timeout" ;;
		3) die "baseline-deployable-unit: a maintenance-pass convergence retry's drain failed, not an eventual-consistency timeout" ;;
		*) die "baseline-deployable-unit: deployable_unit_edges did not converge to its one-edge exact set within the maintenance-pass convergence bound (fault-free baseline must materialize this family's edge before the recovery cells compare against it)" ;;
		esac
	fi
	capture_digest baseline_deployable_unit

	# Snapshot the fault-free retry count so the fault cells can prove their
	# injected fault ADDED a retry this identical drive did not produce on its
	# own, mirroring cell_baseline's baseline_retried/baseline_code_call_retried
	# capture exactly -- deployable_unit_correlation only does real work AFTER
	# the maintenance pass, so this is the reference flow that establishes what
	# "zero natural retries" means for this domain, not an assumed literal.
	baseline_deployable_unit_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "deployable_unit_correlation")"
	baseline_deployable_unit_retried="${baseline_deployable_unit_retried:-0}"
	printf 'baseline-deployable-unit: fault-free deployable_unit_correlation retried rows (attempt_count>1): %s\n' "${baseline_deployable_unit_retried}"

	teardown_cell baseline_deployable_unit
	wall_times[baseline_deployable_unit]=$(( $(date +%s) - cell_start ))
	printf 'baseline-deployable-unit: cell wall time: %ss\n' "${wall_times[baseline_deployable_unit]}"
}

# cell_killworker_deployable_unit proves a genuinely in-flight
# deployable_unit_correlation handler is reclaimed after process death,
# AFTER the maintenance pass has opened the readiness gate. The
# admission_decisions table lock -- Handle's SECOND write, still after the
# graph write; see ifa_deployable_unit_start_admission_decisions_lock's own
# header (ifa_fault_injection_deployable_unit_lock.sh) for why
# shared_projection_intents cannot block this handler at all,
# why graph_projection_phase_state (Handle's third write) starves this
# family's row against this gate's shared four-worker pool instead of
# blocking it usefully, and for the post-write-death consequence that
# follows from locking a write this late -- prevents the handler from
# acknowledging before kill; attempt_count > 0 (this domain has no natural
# retries in a clean maintenance-pass run, so any positive count is the
# fault's fingerprint) proves the replacement reducer re-executed
# deployable_unit_correlation, not merely another queued row.
#
# THE RETRY EVIDENCE IS SNAPSHOTTED, NOT RE-QUERIED (found live in CI):
# attempt_count is captured immediately after the post-kill drain reaches
# its residual bound, before this cell's own edge-assert/converge_edges
# steps run. A LIVE re-query at assertion time is not equivalent -- this
# family's convergence loop (ifa_deployable_unit_live_converge_edges) can
# run another bootstrap-index maintenance pass, which reopens the recovered
# row and resets attempt_count to 0 (ReopenSucceeded, reducer_queue_
# replay.go), erasing the recovery's own evidence AFTER recovery already
# succeeded. Whether that race fires depends only on whether the recovered
# row reached 'succeeded' before the maintenance pass enumerated succeeded
# rows -- and this family converges on maintenance pass 2 as its NORMAL
# path, so the failing case is the common one, not a rare one. See
# ifa_deployable_unit_live_converge_edges's own header (ifa_deployable_
# unit_live_converge.sh) for the general shape of this hazard.
cell_killworker_deployable_unit() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-deployable-unit: fresh stack"
	fresh_stack killworkerdeployableunit
	ifa_deployable_unit_require_fresh_intents "kill-worker-after-claim-deployable-unit" "${bin_dir}" \
		|| die "kill-worker-after-claim-deployable-unit: fresh-stack precondition failed"
	drive_all_cassettes killworkerdeployableunit
	drive_deployable_unit_cassette killworkerdeployableunit

	log "kill-worker-after-claim-deployable-unit: pre-maintenance drain (deployable_unit_correlation is gated shut here by design)"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-killworkerdeployableunitpre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdeployableunitpre" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerdeployableunitpre
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
	ifa_deployable_unit_live_assert_empty_before_maintenance "${bin_dir}" \
		|| die "kill-worker-after-claim-deployable-unit: expected zero deployable_unit_edges rows before the maintenance pass"

	ifa_deployable_unit_live_run_maintenance_pass "killworkerdeployableunit" "${bin_dir}" "${log_dir}" \
		|| die "kill-worker-after-claim-deployable-unit: bootstrap-index maintenance pass failed"

	local lock_holder_pid claimed_before reducer_pid_before reducer_pid_after
	ifa_deployable_unit_start_admission_decisions_lock "killworkerdeployableunit" lock_holder_pid \
		|| die "kill-worker-after-claim-deployable-unit: could not acquire the deterministic admission_decisions blocker"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdeployableunit-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "deployable_unit_correlation")" \
		|| die "kill-worker-after-claim-deployable-unit: no deployable_unit_correlation row was claimed while its durable write was blocked"
	printf 'kill-worker-after-claim-deployable-unit: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_deployable_unit_release_admission_decisions_lock "killworkerdeployableunit" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerdeployableunit-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerdeployableunit
	# Snapshot HERE, not at the assertion below: by the time run_drain_gate
	# returns, the recovered row is already 'succeeded' with attempt_count
	# intact, and nothing between here and this snapshot can have reopened it
	# -- so a single direct read, not a poll, is correct. Everything after
	# this line (the edge assert and its convergence-loop retries) can run
	# another maintenance pass that reopens this same row and resets
	# attempt_count to 0; reading the count again after that point would read
	# the reset, not the recovery. See this cell's own header and
	# ifa_deployable_unit_live_converge_edges's for why.
	local killed_retried killed_retried_rc
	if killed_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "deployable_unit_correlation")"; then
		killed_retried_rc=0
	else
		killed_retried_rc=$?
	fi
	assert_no_dead_letters killworkerdeployableunit
	ifa_deployable_unit_live_assert_readiness_opened "${log_dir}" "reducer-killworkerdeployableunit-after" "killworkerdeployableunit" \
		|| die "kill-worker-after-claim-deployable-unit: post-maintenance reducer log does not prove the readiness gate opened"
	ifa_deployable_unit_live_report_intents_after_maintenance "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}"
	ifa_deployable_unit_live_report_resolved_deploys_from_count "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}"
	ifa_deployable_unit_live_report_correlation_reopen "${log_dir}" "killworkerdeployableunit"
	if ! ifa_deployable_unit_live_assert "${bin_dir}" "${deployable_unit_expected_edges}"; then
		local converge_rc=0
		ifa_deployable_unit_live_converge_edges "killworkerdeployableunit" "${bin_dir}" "${log_dir}" \
			"${deployable_unit_expected_edges}" run_drain_gate killworkerdeployableunit || converge_rc=$?
		case "${converge_rc}" in
		0)
			# A fault cell that needed extra convergence passes recovered
			# under materially different conditions than one that converged
			# on the first post-fault drain -- worth its own line so a
			# reader diffing this cell's log against a clean run can see it,
			# rather than both cases printing the identical final assertion.
			printf 'kill-worker-after-claim-deployable-unit: converged via the convergence loop -- extra bootstrap-index maintenance passes were needed; the post-fault drain alone did not reach the one-edge exact set\n'
			;;
		2) die "kill-worker-after-claim-deployable-unit: a maintenance-pass convergence retry crashed (bootstrap-index itself failed), not an eventual-consistency timeout" ;;
		3) die "kill-worker-after-claim-deployable-unit: a maintenance-pass convergence retry's drain failed, not an eventual-consistency timeout" ;;
		*) die "kill-worker-after-claim-deployable-unit: recovered graph did not converge to the one-edge exact set within the maintenance-pass convergence bound" ;;
		esac
	fi
	# Assert on the SNAPSHOT captured right after the drain, not a fresh
	# query here -- by this point the edge-assert/converge_edges steps above
	# may already have run a maintenance pass that reopened and zeroed this
	# row's attempt_count. ifa_fault_assert_retried_above (which polls a
	# live query) is deliberately NOT used here for that reason.
	#
	# The rc check below is checked against killed_retried_rc, captured by
	# the if/else at the snapshot line above -- the family idiom used by
	# every sibling precondition in this family (e.g.
	# ifa_deployable_unit_require_admission_decisions_written,
	# ifa_fault_injection_deployable_unit_lock.sh). Capturing rc through
	# if/else, rather than a bare `killed_retried="$(...)"` assignment, is
	# not stylistic here: under this script's `set -euo pipefail`, a bare
	# assignment's failing command substitution aborts the whole script on
	# that line, so a failed query would die with ifa_fault_count_retried's
	# own generic message and never reach any of the three checks below.
	# The if/else keeps the failure inside this cell's control, so it can be
	# named and rejected as unknown right here, at the point that actually
	# needs the answer -- same fail-closed shape as the rest of this family:
	# query failed, empty, and non-numeric are each rejected distinctly as
	# unknown, never read as a legitimate zero or a legitimate pass.
	[[ "${killed_retried_rc}" -eq 0 ]] \
		|| die "kill-worker-after-claim-deployable-unit: retried-row snapshot query FAILED (exit ${killed_retried_rc}); treat this as unknown, not as a verdict"
	[[ -n "${killed_retried}" ]] \
		|| die "kill-worker-after-claim-deployable-unit: retried-row snapshot query returned empty output; treat this as unknown, not as zero"
	[[ "${killed_retried}" =~ ^[0-9]+$ ]] \
		|| die "kill-worker-after-claim-deployable-unit: retried-row snapshot query returned non-numeric output '${killed_retried}'; treat this as unknown, not as zero"
	[[ "${killed_retried}" -gt "${baseline_deployable_unit_retried}" ]] \
		|| die "kill-worker-after-claim-deployable-unit: deployable_unit_correlation did not re-execute above its fault-free retry baseline (snapshot ${killed_retried}, baseline ${baseline_deployable_unit_retried})"
	capture_digest killworkerdeployableunit
	assert_matches_baseline killworkerdeployableunit baseline_deployable_unit
	teardown_cell killworkerdeployableunit
	wall_times[killworkerdeployableunit]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-deployable-unit: cell wall time: %ss\n' "${wall_times[killworkerdeployableunit]}"
}

# cell_failgraphwrite_deployable_unit fails the live CORRELATES_DEPLOYABLE_UNIT
# MERGE exactly once, proves the fault decorator's durable marker names that
# write, and requires the one-edge exact set to converge without dead
# letters. Same pre-maintenance-drain / maintenance-pass / post-maintenance
# fault shape as cell_killworker_deployable_unit above.
cell_failgraphwrite_deployable_unit() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-deployable-unit: fresh stack"
	fresh_stack failgraphwritedeployableunit
	ifa_deployable_unit_require_fresh_intents "fail-graph-write-once-then-succeed-deployable-unit" "${bin_dir}" \
		|| die "fail-graph-write-once-then-succeed-deployable-unit: fresh-stack precondition failed"
	drive_all_cassettes failgraphwritedeployableunit
	drive_deployable_unit_cassette failgraphwritedeployableunit

	log "fail-graph-write-once-then-succeed-deployable-unit: pre-maintenance drain (deployable_unit_correlation is gated shut here by design)"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritedeployableunitpre" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritedeployableunitpre" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritedeployableunitpre
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
	ifa_deployable_unit_live_assert_empty_before_maintenance "${bin_dir}" \
		|| die "fail-graph-write-once-then-succeed-deployable-unit: expected zero deployable_unit_edges rows before the maintenance pass"

	ifa_deployable_unit_live_run_maintenance_pass "failgraphwritedeployableunit" "${bin_dir}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-deployable-unit: bootstrap-index maintenance pass failed"

	local fault_once_script marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-deployable-unit.json"
	ifa_fault_write_once_script "${fault_once_script}" "${deployable_unit_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritedeployableunit" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritedeployableunit" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritedeployableunit
	assert_no_dead_letters failgraphwritedeployableunit
	ifa_deployable_unit_live_assert_readiness_opened "${log_dir}" "reducer-failgraphwritedeployableunit" "failgraphwritedeployableunit" \
		|| die "fail-graph-write-once-then-succeed-deployable-unit: post-maintenance reducer log does not prove the readiness gate opened"
	ifa_deployable_unit_live_report_intents_after_maintenance "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}"
	ifa_deployable_unit_live_report_resolved_deploys_from_count "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}"
	ifa_deployable_unit_live_report_correlation_reopen "${log_dir}" "failgraphwritedeployableunit"
	if ! ifa_deployable_unit_live_assert "${bin_dir}" "${deployable_unit_expected_edges}"; then
		local converge_rc=0
		ifa_deployable_unit_live_converge_edges "failgraphwritedeployableunit" "${bin_dir}" "${log_dir}" \
			"${deployable_unit_expected_edges}" run_drain_gate failgraphwritedeployableunit || converge_rc=$?
		case "${converge_rc}" in
		0)
			# See cell_killworker_deployable_unit's identical marker above:
			# a fault cell that needed extra convergence passes is a
			# materially different result from one that converged on the
			# first post-fault drain, and today both printed the same line.
			printf 'fail-graph-write-once-then-succeed-deployable-unit: converged via the convergence loop -- extra bootstrap-index maintenance passes were needed; the post-fault drain alone did not reach the one-edge exact set\n'
			;;
		2) die "fail-graph-write-once-then-succeed-deployable-unit: a maintenance-pass convergence retry crashed (bootstrap-index itself failed), not an eventual-consistency timeout" ;;
		3) die "fail-graph-write-once-then-succeed-deployable-unit: a maintenance-pass convergence retry's drain failed, not an eventual-consistency timeout" ;;
		*) die "fail-graph-write-once-then-succeed-deployable-unit: recovered graph did not converge to the one-edge exact set within the maintenance-pass convergence bound" ;;
		esac
	fi
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${deployable_unit_edge_operation_match}" || marker_rc=$?
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-deployable-unit: once-fired marker did not name the targeted CORRELATES_DEPLOYABLE_UNIT MERGE (marker status ${marker_rc})"
	capture_digest failgraphwritedeployableunit
	assert_matches_baseline failgraphwritedeployableunit baseline_deployable_unit
	teardown_cell failgraphwritedeployableunit
	wall_times[failgraphwritedeployableunit]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-deployable-unit: cell wall time: %ss\n' "${wall_times[failgraphwritedeployableunit]}"
}
