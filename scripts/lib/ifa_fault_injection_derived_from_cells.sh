#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# derived_from-targeted live fault cells (#6228 derived_from live leaf),
# mirroring scripts/lib/ifa_fault_injection_built_from_cells.sh's shape
# -- except the kill cell: this family shares its handler (and therefore
# its handler-stage wait on container_image_identity) with built_from,
# which owns that stage by first-landed tie-break (see
# rows/26_derived_from.sh). So the kill cell takes no table lock
# (blocker_kind=statement_hold, not table_lock): the hold decorator engages
# the DERIVED_FROM MERGE itself, post any retract, while the handler is
# genuinely mid-write. This domain's claim is NOT readiness-gated
# (container_image_identity carries no row in the reducer-claim readiness
# requirements --
# go/internal/storage/postgres/reducer_queue_readiness_sql.go names only
# *_materialization domains), so the tagged reducer starts as soon as the
# projector enqueues the row, with no prereq-scoped reducer and no readiness
# wait. That is the k8s shape, not the s3 shape: s3's
# s3_logs_to_materialization claim IS gated on
# canonical_nodes_committed/cloud_resource_uid, so its kill cell banks
# readiness through an aws_resource-scoped prereq reducer before the hold;
# doing that here would add a wait this domain's claim never needs.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_derived_from,
# killworker_derived_from,
# failgraphwrite_derived_from) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh, and the hold helpers from
# ifa_fault_injection_hold.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_derived_from.sh
# (ifa_derived_from_drive /
# ifa_derived_from_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-derived-from-cases.sh
# sources this file and drives
# ifa_derived_from_wait_for_hold_marker under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# the wait's fail-closed behavior will fail `make pre-pr`.
#
# derived_from is SINGLE-STAGE and DIRECT. The handler
# (ContainerImageIdentityHandler) claims a fact_work_items row
# under domain "container_image_identity", loads its facts via
# factload.LoadFactsForKinds -> FROM fact_records, and writes the
# DERIVED_FROM edges DIRECTLY to the graph through its provenance writer
# (go/internal/storage/cypher/edge/writer/derived_from.go), then
# acks. There is no shared_projection_intents row and no second-stage runner
# lease -- the two blocker kinds the registry schema offers that this family
# cannot use (see rows/26_derived_from.sh).
#
# The kill therefore engages the handler's WRITE, not a queue the handler
# shares: this family shares container_image_identity with built_from
# (rows/23 owns the handler-stage wait by first-landed tie-break), so a
# table lock on fact_records would hold a handler both families claim and
# the exclusivity test would count one kill as two per-family proofs. The
# hold decorator engages the DERIVED_FROM MERGE itself -- a write only
# this family performs -- which is the seam the registry row's
# blocker_kind=statement_hold names.
#
# ifa_derived_from_wait_for_hold_marker polls for the
# hold-statement-before-delegation marker file the tagged reducer's hold
# decorator writes the moment it engages the DERIVED_FROM MERGE, before
# it blocks. Waiting for it (rather than sleeping a fixed delay after the
# claim) is what makes the kill non-vacuous: kill -9 lands only once the
# worker is provably parked inside the held statement. Fail-closed
# throughout: a bad budget, or budget exhaustion with no marker, all return
# non-zero.
ifa_derived_from_wait_for_hold_marker() {
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

# cell_baseline_derived_from is this family's fault-free
# reference run. It exists because the derived_from cassette is driven ONLY by
# this family's own cells: ifa_fault_injection_driver.sh's header rules that a
# new family cassette is never added to drive_all_cassettes, since the extra
# succeeded reducer row enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# DERIVED_FROM edges and every cell of this family would mismatch it.
# These cells therefore compare against
# digests[baseline_derived_from] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_derived_from_retried: the
# fault-free excess-ATTEMPT total for domain
# container_image_identity, which the kill cell must exceed to
# prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item, so the row-count form saturates at 1 (see
# ifa_fault_count_retry_attempts).
cell_baseline_derived_from() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-derived-from: fresh stack"
	fresh_stack baseline_derived_from
	drive_all_cassettes baseline_derived_from
	ifa_derived_from_drive "baseline_derived_from" "${bin_dir}" "${derived_from_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-derived-from: eshu-ifa drive (derived_from family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_derived_from" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_derived_from" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_derived_from
	assert_no_dead_letters baseline_derived_from
	ifa_derived_from_assert "baseline_derived_from" "${bin_dir}" "${derived_from_expected_edges}" \
		|| die "baseline-derived-from: fault-free graph does not match the two-edge exact set"
	baseline_derived_from_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "container_image_identity")" \
		|| die "baseline-derived-from: could not count the fault-free container_image_identity retry-attempt baseline"
	printf 'baseline-derived-from: fault-free container_image_identity retry-attempt baseline: %s\n' "${baseline_derived_from_retried}"
	capture_digest baseline_derived_from
	teardown_cell baseline_derived_from
	wall_times[baseline_derived_from]=$(( $(date +%s) - cell_start ))
	printf 'baseline-derived-from: cell wall time: %ss\n' "${wall_times[baseline_derived_from]}"
}

cell_killworker_derived_from() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-statement-hold-derived-from: fresh stack"
	fresh_stack killworkerderivedfrom
	drive_all_cassettes killworkerderivedfrom
	ifa_derived_from_drive "killworkerderivedfrom" "${bin_dir}" "${derived_from_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-statement-hold-derived-from: eshu-ifa drive (derived_from family) failed"
	local projector_pid reducer_pid_before reducer_pid_after claimed_before hold_script marker_rc
	ifa_det_start_bg "${log_dir}" "projector-killworkerderivedfrom" projector_pid "${bin_dir}/eshu-projector"
	# container_image_identity's reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status) BEFORE
	# the sentinel goes down: without the row there is nothing to hold in
	# flight and nothing to kill.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "container_image_identity" 1 >/dev/null \
		|| die "kill-statement-hold-derived-from: container_image_identity was never enqueued by the projector"
	# No readiness gate on this domain (unlike s3's
	# s3_logs_to_materialization): the claim fires as soon as the row is
	# pending, so the hold is armed now. A prereq-scoped reducer would add a
	# wait this domain's claim never needs.
	# The hold script plus the sentinel it blocks on. Order matters: the
	# sentinel must exist before the tagged reducer starts, or the first
	# matching write would engage-and-immediately-release before any
	# observer could rendezvous on it.
	hold_script="${work_dir}/fault-hold-derived-from.json"
	ifa_fault_write_hold_script "${hold_script}" "${derived_from_edge_operation_match}" \
		|| die "kill-statement-hold-derived-from: could not write the hold fault script"
	: >"${hold_script}.restart-sentinel" \
		|| die "kill-statement-hold-derived-from: could not create the hold sentinel"
	# Scoped to container_image_identity only: no other pending domain can
	# starve this domain of a worker (same reasoning as the vault kill cell).
	ifa_det_start_bg "${log_dir}" "reducer-killworkerderivedfrom-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=container_image_identity" "ESHU_IFA_FAULT_SCRIPT=${hold_script}" "${tagged_bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "container_image_identity")" \
		|| die "kill-statement-hold-derived-from: no container_image_identity row was claimed while its write was held"
	printf 'kill-statement-hold-derived-from: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	# The hold rendezvous: the marker names the exact DERIVED_FROM MERGE
	# the held worker engaged, written synchronously before it blocked.
	# kill -9 lands only after this wait succeeds -- killing on the claim
	# alone could land before the handler reaches the held write.
	ifa_derived_from_wait_for_hold_marker "killworkerderivedfrom" "${hold_script}" "${CLAIMED_ROW_WAIT_TIMEOUT}" \
		|| die "kill-statement-hold-derived-from: the hold never engaged, so this cell proved nothing about DERIVED_FROM kill/reclaim"
	marker_rc=0
	ifa_fault_assert_hold_marker "${hold_script}" "${derived_from_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means
		# the hold ENGAGED but on a different write than the anchor names.
		# Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "kill-statement-hold-derived-from: the hold ENGAGED but on a different write than ${derived_from_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "kill-statement-hold-derived-from: hold marker assertion failed (marker status ${marker_rc})"
	# Announced, not merely checked, like the failgraphwrite cell's marker
	# line above: a passing run must say what fired.
	printf 'kill-statement-hold-derived-from: non-vacuous: hold marker names the targeted DERIVED_FROM MERGE (written by the fault decorator at engage time, not read from the reducer log)\n'
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	rm -f "${hold_script}.restart-sentinel"
	# Restart UNFAULTED: the replacement reducer runs the untagged binary
	# with no fault script, so the reclaim re-executes retract plus both
	# edge sets with no hold in the way -- recovery, not a second fault.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerderivedfrom-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerderivedfrom
	assert_no_dead_letters killworkerderivedfrom
	ifa_derived_from_assert "killworkerderivedfrom" "${bin_dir}" "${derived_from_expected_edges}" \
		|| die "kill-statement-hold-derived-from: recovered graph does not match the two-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_derived_from_retried}" 15 "container_image_identity" \
		|| die "kill-statement-hold-derived-from: container_image_identity did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkerderivedfrom
	assert_matches_baseline killworkerderivedfrom baseline_derived_from
	teardown_cell killworkerderivedfrom
	wall_times[killworkerderivedfrom]=$(( $(date +%s) - cell_start ))
	printf 'kill-statement-hold-derived-from: cell wall time: %ss\n' "${wall_times[killworkerderivedfrom]}"
}

# cell_failgraphwrite_derived_from fails the live
# DERIVED_FROM MERGE exactly once, proves the fault decorator's durable
# marker names that write, and requires the full two-edge set to converge
# without dead letters. Anchor read off EXECUTED statement text, not source
# text: the writer's template reads MERGE (img)-[rel:DERIVED_FROM {
# (go/internal/storage/cypher/edge/writer/derived_from.go), with the
# scope_id/evidence_source identity the writer stamps per run, so the
# anchor carries the PREFIX form. A full `]->(base)` spelling would match
# no executed statement and the fault would never fire, giving a green cell
# that tested nothing.
cell_failgraphwrite_derived_from() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-derived-from: fresh stack"
	fresh_stack failgraphwritederivedfrom
	drive_all_cassettes failgraphwritederivedfrom
	ifa_derived_from_drive "failgraphwritederivedfrom" "${bin_dir}" "${derived_from_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-derived-from: eshu-ifa drive (derived_from family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-derived-from.json"
	ifa_fault_write_once_script "${fault_once_script}" "${derived_from_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritederivedfrom" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritederivedfrom" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritederivedfrom
	assert_no_dead_letters failgraphwritederivedfrom
	ifa_derived_from_assert "failgraphwritederivedfrom" "${bin_dir}" "${derived_from_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-derived-from: recovered graph does not match the two-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'container_image_identity';" \
		"${compose_file}" | sed 's/^/  container_image_identity work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${derived_from_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-derived-from: the fault FIRED but on a different write than ${derived_from_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-derived-from: no once-fired marker at all -- the fault never fired, so this cell proved nothing about DERIVED_FROM recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-derived-from: non-vacuous: once-fired marker names the targeted DERIVED_FROM MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwritederivedfrom
	assert_matches_baseline failgraphwritederivedfrom baseline_derived_from
	teardown_cell failgraphwritederivedfrom
	wall_times[failgraphwritederivedfrom]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-derived-from: cell wall time: %ss\n' "${wall_times[failgraphwritederivedfrom]}"
}
