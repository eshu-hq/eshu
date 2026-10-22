#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# derived_from-targeted live fault cells (#6228 derived_from live leaf),
# mirroring scripts/lib/ifa_fault_injection_built_from_cells.sh's shape
# including the kill cell: this domain's claim is NOT readiness-gated
# (container_image_identity carries no row in the reducer-claim readiness
# requirements --
# go/internal/storage/postgres/reducer_queue_readiness_sql.go names only
# *_materialization domains), so the kill cell takes the lock as soon as the
# projector enqueues the row, with no prereq-scoped reducer and no readiness
# wait. That is the k8s shape, not the s3 shape: s3's
# s3_logs_to_materialization claim IS gated on
# canonical_nodes_committed/cloud_resource_uid, so its kill cell banks
# readiness through an aws_resource-scoped prereq reducer before the lock;
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
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_derived_from.sh
# (ifa_derived_from_drive /
# ifa_derived_from_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-derived-from-cases.sh
# sources this file and drives
# ifa_derived_from_start_fact_records_lock and
# ifa_derived_from_release_fact_records_lock under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will fail
# `make pre-pr`.
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
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. The handler struct embeds FactLoader
# (go/internal/reducer/containerimage/container_image_identity.go:73) and Handle
# refuses to run without it (:105-107) before passing it to
# factload.LoadFactsForKinds (:127) -- the pre-write window the lock needs.
#
# ifa_derived_from_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the container_image_identity
# handler blocks on its FIRST synchronous read -- the fact load -- while its
# fact_work_items claim is already held. The claim, heartbeat, and ack path
# never reads fact_records, so the row stays claimed and alive for the whole
# hold. fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and locking that table would block the poll too.
ifa_derived_from_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_derived_from_lock_ keeps the longest name this family uses at 44 bytes.
	local app_name="ifa_derived_from_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/derived-from-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/derived-from-lock-${cell}.log" 2>&1 &
	fi
	local holder_pid=$!
	bg_pids+=("${holder_pid}")
	printf -v "${pid_var}" '%s' "${holder_pid}"

	local i lock_count
	for i in $(seq 1 60); do
		lock_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE a.application_name = '${app_name}' AND l.relation = 'fact_records'::regclass AND l.mode = 'AccessExclusiveLock' AND l.granted;" \
			"${compose_file}" | tr -d '[:space:]')"
		if [[ "${lock_count}" == "1" ]]; then
			return 0
		fi
		sleep 0.25
	done
	# Exhausted without a grant: snapshot who holds or waits on
	# fact_records before reporting failure, so the next reader sees the
	# blocker instead of a bare timeout. Added after the live
	# killworkerkubernetesnamespaceenvironment run died on the sibling
	# family's identical window with the projector mid-sweep and named
	# nothing; this family's cell takes its lock at the same point.
	printf '%s: fact_records lock blockers at acquisition failure:\n' "${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT 'blocker_snapshot' AS snapshot_kind, a.application_name, l.locktype, l.mode, l.granted, a.state, left(a.query, 120) AS query FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE l.relation = 'fact_records'::regclass ORDER BY 2, 3;" \
		"${compose_file}" 2>/dev/null | sed 's/^/  /' || true
	return 1
}

# ifa_derived_from_release_fact_records_lock terminates
# the named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_derived_from_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_derived_from_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
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
	log "cell kill-worker-after-claim-derived-from: fresh stack"
	fresh_stack killworkerderivedfrom
	drive_all_cassettes killworkerderivedfrom
	ifa_derived_from_drive "killworkerderivedfrom" "${bin_dir}" "${derived_from_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-derived-from: eshu-ifa drive (derived_from family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerderivedfrom" projector_pid "${bin_dir}/eshu-projector"
	# container_image_identity's reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status) before
	# going further: without it there is nothing to kill, and locking
	# fact_records before that intent exists risks starving the projector's
	# OWN fact reads for this scope too, so the row could never be created
	# for as long as the lock is held -- not merely delayed.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "container_image_identity" 1 >/dev/null \
		|| die "kill-worker-after-claim-derived-from: container_image_identity was never enqueued by the projector"
	# No readiness gate on this domain (unlike s3's
	# s3_logs_to_materialization): the claim fires as soon as the row is
	# pending, so the lock goes on now. A prereq-scoped reducer would add a
	# wait this domain's claim never needs.
	ifa_derived_from_start_fact_records_lock "killworkerderivedfrom" lock_holder_pid \
		|| die "kill-worker-after-claim-derived-from: could not acquire the deterministic fact_records read blocker"
	# Scoped to container_image_identity only: with the lock already
	# held, an unscoped reducer's workers can all grab OTHER pending domains
	# that also read fact_records and get stuck there for the lock's full
	# duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's row.
	# The claim fires without waiting on any phase: this domain carries no
	# readiness requirement, so the lock starves nothing the claim still needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerderivedfrom-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=container_image_identity" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "container_image_identity")" \
		|| die "kill-worker-after-claim-derived-from: no container_image_identity row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-derived-from: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_derived_from_release_fact_records_lock "killworkerderivedfrom" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerderivedfrom-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerderivedfrom
	assert_no_dead_letters killworkerderivedfrom
	ifa_derived_from_assert "killworkerderivedfrom" "${bin_dir}" "${derived_from_expected_edges}" \
		|| die "kill-worker-after-claim-derived-from: recovered graph does not match the two-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_derived_from_retried}" 15 "container_image_identity" \
		|| die "kill-worker-after-claim-derived-from: container_image_identity did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkerderivedfrom
	assert_matches_baseline killworkerderivedfrom baseline_derived_from
	teardown_cell killworkerderivedfrom
	wall_times[killworkerderivedfrom]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-derived-from: cell wall time: %ss\n' "${wall_times[killworkerderivedfrom]}"
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
