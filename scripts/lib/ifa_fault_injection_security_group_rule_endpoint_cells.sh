#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# security_group_rule_endpoint-targeted live fault cells (#6228 secgroup-pair
# live leaf, SECOND), mirroring
# scripts/lib/ifa_fault_injection_derived_from_cells.sh's shape -- except
# the kill cell: this family shares its handler (and therefore its
# handler-stage wait on security_group_reachability_materialization) with
# security_group_sg_rule, which owns that stage by first-landed tie-break
# (see rows/31_security_group_rule_endpoint.sh). So the kill cell takes no
# table lock (blocker_kind=statement_hold, not table_lock): the hold
# decorator engages the TO MERGE itself, post any retract and post the
# SG-edge write, while the handler is genuinely mid-write.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_security_group_rule_endpoint,
# killworker_security_group_rule_endpoint,
# failgraphwrite_security_group_rule_endpoint) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh, and the hold helpers from
# ifa_fault_injection_hold.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_security_group_reachability.sh
# (ifa_security_group_reachability_drive /
# ifa_security_group_rule_endpoint_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-security-group-rule-endpoint-cases.sh
# sources this file and drives
# ifa_security_group_rule_endpoint_wait_for_hold_marker under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so
# editing the wait's fail-closed behavior will fail `make pre-pr`.
#
# security_group_rule_endpoint is SINGLE-STAGE and DIRECT. The handler
# (SecurityGroupReachabilityMaterializationHandler) claims a
# fact_work_items row under domain
# "security_group_reachability_materialization", loads its facts via
# factload.LoadFactsForKinds -> FROM fact_records, and writes the TO edges
# DIRECTLY to the graph through its cypher writer
# (go/internal/storage/cypher/cloud/aws/security/group/security_reachability_edge_writer.go:101),
# then acks. There is no shared_projection_intents row and no second-stage
# runner lease -- the two blocker kinds the registry schema offers that this
# family cannot use (see rows/31_security_group_rule_endpoint.sh).
#
# The kill therefore engages the handler's WRITE, not a queue the handler
# shares: this family shares security_group_reachability_materialization
# with security_group_sg_rule (rows/31 owns the handler-stage wait by
# first-landed tie-break), so a table lock on fact_records would hold a
# handler both families claim and the exclusivity test would count one kill
# as two per-family proofs. The hold decorator engages the TO MERGE itself
# -- a write only this family performs -- which is the seam the registry
# row's blocker_kind=statement_hold names.
#
# Unlike the derived_from sibling, this domain's claim IS readiness-gated
# (the triple canonical-nodes-committed gate -- the s3 shape, not the k8s
# one), so the kill cell banks readiness through a prereq-scoped reducer
# before the hold is armed; arming it earlier would hold nothing the claim
# never reaches. The banking call below is
# ifa_security_group_sg_rule_wait_for_readiness from the sibling
# ifa_fault_injection_security_group_sg_rule_cells.sh -- same domain, same
# triple gate, one definition rather than a second copy to rot. Both files
# are sourced together by ifa_fault_injection_sources.sh; do not source
# this file without it.
#
# ifa_security_group_rule_endpoint_wait_for_hold_marker polls for the
# hold-statement-before-delegation marker file the tagged reducer's hold
# decorator writes the moment it engages the TO MERGE, before it blocks.
# Waiting for it (rather than sleeping a fixed delay after the claim) is
# what makes the kill non-vacuous: kill -9 lands only once the worker is
# provably parked inside the held statement. Fail-closed throughout: a bad
# budget, or budget exhaustion with no marker, all return non-zero.
ifa_security_group_rule_endpoint_wait_for_hold_marker() {
	local cell="$1" script_path="$2" budget="$3"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: hold-marker wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local marker="${script_path}.restart-sentinel.statement-held"
	local waited=0
	while [[ ! -f "${marker}" ]]; do
		if [[ "${waited}" -ge "${budget}" ]]; then
			printf '%s: hold-marker wait: no hold marker at %s within %ss -- the hold never engaged\n' "${cell}" "${marker}" "${budget}" >&2
			return 1
		fi
		sleep 1
		waited=$((waited + 1))
	done
	printf '%s: hold engaged: marker present at %s\n' "${cell}" "${marker}"
	return 0
}

# cell_baseline_security_group_rule_endpoint is this family's fault-free
# reference run. It exists because the reachability cassette is driven ONLY
# by this pair's own cells: ifa_fault_injection_driver.sh's header rules
# that a new family cassette is never added to drive_all_cassettes, since
# the extra succeeded reducer row enlarges ifa_fault_redeliver_succeeded's
# row set inside cell_duplicatedelivery. So the shared `baseline` digest
# contains no TO edges and every cell of this family would mismatch it.
# These cells therefore compare against
# digests[baseline_security_group_rule_endpoint] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_security_group_rule_endpoint_retried: the
# fault-free excess-ATTEMPT total for domain
# security_group_reachability_materialization, which the kill cell must
# exceed to prove the replacement reducer re-executed THIS domain rather
# than merely draining another queued row. Attempts, not rows: this
# cassette creates one edge work item, so the row-count form saturates at 1
# (see ifa_fault_count_retry_attempts).
cell_baseline_security_group_rule_endpoint() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-security-group-rule-endpoint: fresh stack"
	fresh_stack baselinesgendpoint
	drive_all_cassettes baselinesgendpoint
	ifa_security_group_reachability_drive "baseline_security_group_rule_endpoint" "${bin_dir}" "${security_group_reachability_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-security-group-rule-endpoint: eshu-ifa drive (security-group family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_security_group_rule_endpoint" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_security_group_rule_endpoint" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_security_group_rule_endpoint
	assert_no_dead_letters baseline_security_group_rule_endpoint
	ifa_security_group_rule_endpoint_assert "baseline_security_group_rule_endpoint" "${bin_dir}" "${security_group_rule_endpoint_expected_edges}" \
		|| die "baseline-security-group-rule-endpoint: fault-free graph does not match the five-edge exact set"
	baseline_security_group_rule_endpoint_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "security_group_reachability_materialization")" \
		|| die "baseline-security-group-rule-endpoint: could not count the fault-free security_group_reachability_materialization retry-attempt baseline"
	printf 'baseline-security-group-rule-endpoint: fault-free security_group_reachability_materialization retry-attempt baseline: %s\n' "${baseline_security_group_rule_endpoint_retried}"
	capture_digest baseline_security_group_rule_endpoint
	teardown_cell baseline_security_group_rule_endpoint
	wall_times[baseline_security_group_rule_endpoint]=$(( $(date +%s) - cell_start ))
	printf 'baseline-security-group-rule-endpoint: cell wall time: %ss\n' "${wall_times[baseline_security_group_rule_endpoint]}"
}

cell_killworker_security_group_rule_endpoint() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-statement-hold-security-group-rule-endpoint: fresh stack"
	fresh_stack killworkersgendpoint
	drive_all_cassettes killworkersgendpoint
	ifa_security_group_reachability_drive "killworker_security_group_rule_endpoint" "${bin_dir}" "${security_group_reachability_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-statement-hold-security-group-rule-endpoint: eshu-ifa drive (security-group family) failed"
	local projector_pid reducer_pid_prereq reducer_pid_before reducer_pid_after claimed_before hold_script marker_rc
	ifa_det_start_bg "${log_dir}" "projector-killworker_security_group_rule_endpoint" projector_pid "${bin_dir}/eshu-projector"
	# security_group_reachability_materialization's reducer intent is created
	# by the PROJECTOR, not ingestion. Wait for the row to exist (any status)
	# BEFORE the sentinel goes down: without the row there is nothing to
	# hold in flight and nothing to kill.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "security_group_reachability_materialization" 1 >/dev/null \
		|| die "kill-statement-hold-security-group-rule-endpoint: security_group_reachability_materialization was never enqueued by the projector"
	# Readiness BEFORE the hold: this domain's claim is gated on the three
	# canonical-nodes-committed phases (the s3 shape, not the derived k8s
	# one). A prereq-scoped reducer (the three node domains only) runs
	# unblocked until all three phases are banked; only then is the hold
	# armed. Arming earlier idles the hold against a row the claim gate
	# never releases.
	ifa_det_start_bg "${log_dir}" "reducer-killworker_security_group_rule_endpoint-prereq" reducer_pid_prereq \
		env "ESHU_REDUCER_CLAIM_DOMAINS=security_group_rule_materialization,security_group_cidr_materialization,aws_resource_materialization" "${bin_dir}/eshu-reducer"
	ifa_security_group_sg_rule_wait_for_readiness "killworker_security_group_rule_endpoint" 300 \
		|| die "kill-statement-hold-security-group-rule-endpoint: claim readiness never banked"
	# The hold script plus the sentinel it blocks on. Order matters: the
	# sentinel must exist before the tagged reducer starts, or the first
	# matching write would engage-and-immediately-release before any
	# observer could rendezvous on it.
	hold_script="${work_dir}/fault-hold-security-group-rule-endpoint.json"
	ifa_fault_write_hold_script "${hold_script}" "${security_group_rule_endpoint_edge_operation_match}" \
		|| die "kill-statement-hold-security-group-rule-endpoint: could not write the hold fault script"
	: >"${hold_script}.restart-sentinel" \
		|| die "kill-statement-hold-security-group-rule-endpoint: could not create the hold sentinel"
	# Scoped to security_group_reachability_materialization only: no other
	# pending domain can starve this domain of a worker (same reasoning as
	# the vault kill cell).
	ifa_det_start_bg "${log_dir}" "reducer-killworker_security_group_rule_endpoint-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=security_group_reachability_materialization" "ESHU_IFA_FAULT_SCRIPT=${hold_script}" "${tagged_bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "security_group_reachability_materialization")" \
		|| die "kill-statement-hold-security-group-rule-endpoint: no security_group_reachability_materialization row was claimed while its write was held"
	printf 'kill-statement-hold-security-group-rule-endpoint: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	# The hold rendezvous: the marker names the exact TO MERGE the held
	# worker engaged, written synchronously before it blocked. kill -9 lands
	# only after this wait succeeds -- killing on the claim alone could land
	# before the handler reaches the held write.
	ifa_security_group_rule_endpoint_wait_for_hold_marker "killworker_security_group_rule_endpoint" "${hold_script}" "${CLAIMED_ROW_WAIT_TIMEOUT}" \
		|| die "kill-statement-hold-security-group-rule-endpoint: the hold never engaged, so this cell proved nothing about TO kill/reclaim"
	marker_rc=0
	ifa_fault_assert_hold_marker "${hold_script}" "${security_group_rule_endpoint_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means
		# the hold ENGAGED but on a different write than the anchor names.
		# Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "kill-statement-hold-security-group-rule-endpoint: the hold ENGAGED but on a different write than ${security_group_rule_endpoint_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "kill-statement-hold-security-group-rule-endpoint: hold marker assertion failed (marker status ${marker_rc})"
	# Announced, not merely checked, like the failgraphwrite cell's marker
	# line above: a passing run must say what fired.
	printf 'kill-statement-hold-security-group-rule-endpoint: non-vacuous: hold marker names the targeted TO MERGE (written by the fault decorator at engage time, not read from the reducer log)\n'
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	rm -f "${hold_script}.restart-sentinel"
	# Restart UNFAULTED: the replacement reducer runs the untagged binary
	# with no fault script, so the reclaim re-executes retract plus both
	# edge sets with no hold in the way -- recovery, not a second fault.
	ifa_det_start_bg "${log_dir}" "reducer-killworker_security_group_rule_endpoint-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworker_security_group_rule_endpoint
	assert_no_dead_letters killworker_security_group_rule_endpoint
	ifa_security_group_rule_endpoint_assert "killworker_security_group_rule_endpoint" "${bin_dir}" "${security_group_rule_endpoint_expected_edges}" \
		|| die "kill-statement-hold-security-group-rule-endpoint: recovered graph does not match the five-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_security_group_rule_endpoint_retried}" 15 "security_group_reachability_materialization" \
		|| die "kill-statement-hold-security-group-rule-endpoint: security_group_reachability_materialization did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworker_security_group_rule_endpoint
	assert_matches_baseline killworker_security_group_rule_endpoint baseline_security_group_rule_endpoint
	teardown_cell killworker_security_group_rule_endpoint
	wall_times[killworker_security_group_rule_endpoint]=$(( $(date +%s) - cell_start ))
	printf 'kill-statement-hold-security-group-rule-endpoint: cell wall time: %ss\n' "${wall_times[killworker_security_group_rule_endpoint]}"
}

# cell_failgraphwrite_security_group_rule_endpoint fails the live
# TO MERGE exactly once, proves the fault decorator's durable marker names
# that write, and requires the full five-edge set to converge without dead
# letters. Anchor read off EXECUTED statement text, not source text: the
# writer's template reads MERGE (rule)-[rel:TO]->(target)
# (go/internal/storage/cypher/cloud/aws/security/group/security_reachability_edge_writer.go:101),
# with the target-node label filled per row from the closed endpoint
# vocabulary -- the relationship token itself is static, so the anchor
# carries the literal. A `%s` form would match no executed statement and
# the fault would never fire, giving a green cell that tested nothing.
cell_failgraphwrite_security_group_rule_endpoint() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-security-group-rule-endpoint: fresh stack"
	fresh_stack failgraphwritesgendpoint
	drive_all_cassettes failgraphwritesgendpoint
	ifa_security_group_reachability_drive "failgraphwrite_security_group_rule_endpoint" "${bin_dir}" "${security_group_reachability_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-security-group-rule-endpoint: eshu-ifa drive (security-group family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-security-group-rule-endpoint.json"
	ifa_fault_write_once_script "${fault_once_script}" "${security_group_rule_endpoint_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwrite_security_group_rule_endpoint" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwrite_security_group_rule_endpoint" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwrite_security_group_rule_endpoint
	assert_no_dead_letters failgraphwrite_security_group_rule_endpoint
	ifa_security_group_rule_endpoint_assert "failgraphwrite_security_group_rule_endpoint" "${bin_dir}" "${security_group_rule_endpoint_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-security-group-rule-endpoint: recovered graph does not match the five-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'security_group_reachability_materialization';" \
		"${compose_file}" | sed 's/^/  security_group_reachability_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${security_group_rule_endpoint_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-security-group-rule-endpoint: the fault FIRED but on a different write than ${security_group_rule_endpoint_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-security-group-rule-endpoint: no once-fired marker at all -- the fault never fired, so this cell proved nothing about TO recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-security-group-rule-endpoint: non-vacuous: once-fired marker names the targeted TO MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwrite_security_group_rule_endpoint
	assert_matches_baseline failgraphwrite_security_group_rule_endpoint baseline_security_group_rule_endpoint
	teardown_cell failgraphwrite_security_group_rule_endpoint
	wall_times[failgraphwrite_security_group_rule_endpoint]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-security-group-rule-endpoint: cell wall time: %ss\n' "${wall_times[failgraphwrite_security_group_rule_endpoint]}"
}
