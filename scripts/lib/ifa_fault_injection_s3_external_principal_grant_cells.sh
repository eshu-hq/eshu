#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# s3_external_principal_grant-targeted live fault cells (#6228 s3-grant live
# leaf), mirroring scripts/lib/ifa_fault_injection_s3_logs_to_cells.sh's
# shape including the kill cell: this domain's claim is readiness-gated (see
# the readiness helper below), so the kill cell banks readiness through a
# prereq-scoped reducer before the lock goes on.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_s3_external_principal_grant,
# killworker_s3_external_principal_grant,
# failgraphwrite_s3_external_principal_grant) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_s3_external_principal_grant.sh
# (ifa_s3_external_principal_grant_drive /
# ifa_s3_external_principal_grant_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-s3-external-principal-grant-cases.sh
# sources this file and drives
# ifa_s3_external_principal_grant_start_fact_records_lock and
# ifa_s3_external_principal_grant_wait_for_readiness under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will fail
# `make pre-pr`.
#
# s3_external_principal_grant is SINGLE-STAGE and DIRECT. The handler
# (S3ExternalPrincipalGrantMaterializationHandler) claims a
# fact_work_items row under domain
# "s3_external_principal_grant_materialization", loads its facts via
# factload.LoadFactsForKinds -> FROM fact_records, and writes the
# GRANTS_ACCESS_TO edges DIRECTLY to the graph through its cypher writer
# (go/internal/storage/cypher/cloud/aws/s3/external_principal_grant_writer.go:34),
# then acks. There is no shared_projection_intents row and no second-stage
# runner lease -- the two blocker kinds the registry schema offers that this
# family cannot use (see rows/32_s3_external_principal_grant.sh).
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held.
#
# ifa_s3_external_principal_grant_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the
# s3_external_principal_grant_materialization handler blocks on its FIRST
# synchronous read -- the fact load -- while its fact_work_items claim is
# already held. The claim, heartbeat, and ack path never reads fact_records,
# so the row stays claimed and alive for the whole hold. fact_records, not
# fact_work_items: the cell's own wait predicate polls fact_work_items, and
# locking that table would block the poll too.
ifa_s3_external_principal_grant_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_s3g_lock_ keeps the longest name this family uses at 46 bytes.
	local app_name="ifa_s3g_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/s3-external-principal-grant-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/s3-external-principal-grant-lock-${cell}.log" 2>&1 &
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

# ifa_s3_external_principal_grant_release_fact_records_lock terminates
# the named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_s3_external_principal_grant_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_s3g_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_s3_external_principal_grant_wait_for_readiness banks this family's
# claim readiness BEFORE the fact_records lock is taken. Unlike the sibling
# derived_from direct family, this domain's claim is gated
# (go/internal/storage/postgres/reducer_queue_readiness_sql.go:276:
# s3_external_principal_grant_materialization requires the
# canonical_nodes_committed/cloud_resource_uid phase), and that phase is
# published by the aws_resource_materialization reducer during Handle --
# which reads fact_records. Locking first therefore deadlocks the cell by
# design: the scoped reducer idles against a still-pending row forever
# (proven live on the sibling iam_instance_profile_role domain: 120s, zero
# claims, row still pending at death). So the cell runs a prereq-scoped
# reducer first and this helper waits for exactly the acceptance unit the
# claim gate will ask for -- resolved off the grant row's own (scope,
# generation, entity_key-or-scope) identity, mirroring the gate's
# COALESCE(NULLIF(payload->>'entity_key', ''), scope_id) -- before the lock
# goes on. Fail-closed throughout: no identity row, a failed query, a
# malformed value, or budget exhaustion all return non-zero, and a
# non-numeric phase count is a failure, never a zero.
ifa_s3_external_principal_grant_wait_for_readiness() {
	local cell="$1" budget="$2"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: readiness wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local identity scope generation unit
	if ! identity="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT scope_id || '|' || generation_id || '|' || COALESCE(NULLIF(payload->>'entity_key', ''), scope_id) FROM fact_work_items WHERE stage = 'reducer' AND domain = 's3_external_principal_grant_materialization' ORDER BY created_at ASC, work_item_id ASC LIMIT 1;" \
		"${compose_file}")"; then
		printf '%s: readiness wait: grant identity query failed\n' "${cell}" >&2
		return 1
	fi
	identity="$(printf '%s' "${identity}" | tr -d '[:space:]')"
	IFS='|' read -r scope generation unit <<<"${identity}"
	if [[ -z "${scope}" || -z "${generation}" || -z "${unit}" ]]; then
		printf '%s: readiness wait: no grant row to resolve an identity from (got %q)\n' "${cell}" "${identity}" >&2
		return 1
	fi
	# Values come from our own queue rows, but they ride inside string
	# literals one level down: double any quote rather than trusting shape.
	scope="${scope//\'/\'\'}"
	generation="${generation//\'/\'\'}"
	unit="${unit//\'/\'\'}"
	local i phase_count
	for i in $(seq 1 "${budget}"); do
		if ! phase_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM graph_projection_phase_state WHERE scope_id = '${scope}' AND acceptance_unit_id = '${unit}' AND source_run_id = '${generation}' AND generation_id = '${generation}' AND keyspace = 'cloud_resource_uid' AND phase = 'canonical_nodes_committed';" \
			"${compose_file}")"; then
			printf '%s: readiness wait: phase poll query failed\n' "${cell}" >&2
			return 1
		fi
		phase_count="$(printf '%s' "${phase_count}" | tr -d '[:space:]')"
		if [[ "${phase_count}" =~ ^[1-9][0-9]*$ ]]; then
			printf '%s: readiness banked: canonical_nodes_committed/cloud_resource_uid for acceptance unit %s\n' "${cell}" "${unit}"
			return 0
		fi
		if [[ "${phase_count}" != "0" ]]; then
			printf '%s: readiness wait: phase poll returned non-numeric output %q\n' "${cell}" "${phase_count}" >&2
			return 1
		fi
		sleep 1
	done
	printf '%s: readiness wait: no canonical_nodes_committed/cloud_resource_uid phase for acceptance unit %s within %ss\n' "${cell}" "${unit}" "${budget}" >&2
	return 1
}

# cell_baseline_s3_external_principal_grant is this family's fault-free
# reference run. It exists because the grant cassette is driven ONLY by
# this family's own cells: ifa_fault_injection_driver.sh's header rules that a
# new family cassette is never added to drive_all_cassettes, since the extra
# succeeded reducer row enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# GRANTS_ACCESS_TO edges and every cell of this family would mismatch it.
# These cells therefore compare against
# digests[baseline_s3_external_principal_grant] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_s3_external_principal_grant_retried: the
# fault-free excess-ATTEMPT total for domain
# s3_external_principal_grant_materialization, which the kill cell must
# exceed to prove the replacement reducer re-executed THIS domain rather
# than merely draining another queued row. Attempts, not rows: this cassette
# creates one work item, so the row-count form saturates at 1 (see
# ifa_fault_count_retry_attempts).
cell_baseline_s3_external_principal_grant() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-s3-external-principal-grant: fresh stack"
	fresh_stack baselines3grant
	drive_all_cassettes baselines3grant
	ifa_s3_external_principal_grant_drive "baseline_s3_external_principal_grant" "${bin_dir}" "${s3_external_principal_grant_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-s3-external-principal-grant: eshu-ifa drive (grant family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_s3_external_principal_grant" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_s3_external_principal_grant" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_s3_external_principal_grant
	assert_no_dead_letters baseline_s3_external_principal_grant
	ifa_s3_external_principal_grant_assert "baseline_s3_external_principal_grant" "${bin_dir}" "${s3_external_principal_grant_expected_edges}" \
		|| die "baseline-s3-external-principal-grant: fault-free graph does not match the four-edge exact set"
	baseline_s3_external_principal_grant_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "s3_external_principal_grant_materialization")" \
		|| die "baseline-s3-external-principal-grant: could not count the fault-free s3_external_principal_grant_materialization retry-attempt baseline"
	printf 'baseline-s3-external-principal-grant: fault-free s3_external_principal_grant_materialization retry-attempt baseline: %s\n' "${baseline_s3_external_principal_grant_retried}"
	capture_digest baseline_s3_external_principal_grant
	teardown_cell baseline_s3_external_principal_grant
	wall_times[baseline_s3_external_principal_grant]=$(( $(date +%s) - cell_start ))
	printf 'baseline-s3-external-principal-grant: cell wall time: %ss\n' "${wall_times[baseline_s3_external_principal_grant]}"
}

cell_killworker_s3_external_principal_grant() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-s3-external-principal-grant: fresh stack"
	fresh_stack killworkers3grant
	drive_all_cassettes killworkers3grant
	ifa_s3_external_principal_grant_drive "killworker_s3_external_principal_grant" "${bin_dir}" "${s3_external_principal_grant_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-s3-external-principal-grant: eshu-ifa drive (grant family) failed"
	local projector_pid reducer_pid_prereq reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworker_s3_external_principal_grant" projector_pid "${bin_dir}/eshu-projector"
	# s3_external_principal_grant_materialization's reducer intent is created
	# by the PROJECTOR, not ingestion. Wait for the row to exist (any status)
	# before going further: without it there is nothing to make ready and
	# nothing to kill.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "s3_external_principal_grant_materialization" 1 >/dev/null \
		|| die "kill-worker-after-claim-s3-external-principal-grant: s3_external_principal_grant_materialization was never enqueued by the projector"
	# Readiness BEFORE the lock -- the shape that distinguishes this family
	# from the kubernetes kill cell. This domain's claim is gated on the
	# canonical_nodes_committed/cloud_resource_uid phase, which the
	# aws_resource_materialization reducer publishes during Handle -- and
	# Handle reads fact_records. A prereq-scoped reducer (aws_resource
	# only, so the grant row can never be claimed early) runs unblocked
	# until the exact acceptance unit the gate will ask for is banked. Only
	# then does the lock go on. Locking first deadlocks by design: the claim
	# gate never opens while the lock is held, so the grant-scoped reducer
	# would idle against a still-pending row until the wait budget dies.
	ifa_det_start_bg "${log_dir}" "reducer-killworker_s3_external_principal_grant-prereq" reducer_pid_prereq \
		env "ESHU_REDUCER_CLAIM_DOMAIN=aws_resource_materialization" "${bin_dir}/eshu-reducer"
	ifa_s3_external_principal_grant_wait_for_readiness "killworker_s3_external_principal_grant" 300 \
		|| die "kill-worker-after-claim-s3-external-principal-grant: claim readiness never banked"
	ifa_s3_external_principal_grant_start_fact_records_lock "killworker_s3_external_principal_grant" lock_holder_pid \
		|| die "kill-worker-after-claim-s3-external-principal-grant: could not acquire the deterministic fact_records read blocker"
	# Scoped to s3_external_principal_grant_materialization only: with the
	# lock already held, an unscoped reducer's workers can all grab OTHER
	# pending domains that also read fact_records and get stuck there for the
	# lock's full duration, starving this domain of a worker even though its
	# own row already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL
	# itself (domain = ANY(...)), so this instance never attempts another
	# domain's row. The claim fires because readiness was banked above: the
	# gate's phase rows persist, so the lock starves nothing the claim still
	# needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworker_s3_external_principal_grant-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=s3_external_principal_grant_materialization" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "s3_external_principal_grant_materialization")" \
		|| die "kill-worker-after-claim-s3-external-principal-grant: no s3_external_principal_grant_materialization row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-s3-external-principal-grant: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_s3_external_principal_grant_release_fact_records_lock "killworker_s3_external_principal_grant" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworker_s3_external_principal_grant-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworker_s3_external_principal_grant
	assert_no_dead_letters killworker_s3_external_principal_grant
	ifa_s3_external_principal_grant_assert "killworker_s3_external_principal_grant" "${bin_dir}" "${s3_external_principal_grant_expected_edges}" \
		|| die "kill-worker-after-claim-s3-external-principal-grant: recovered graph does not match the four-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_s3_external_principal_grant_retried}" 15 "s3_external_principal_grant_materialization" \
		|| die "kill-worker-after-claim-s3-external-principal-grant: s3_external_principal_grant_materialization did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworker_s3_external_principal_grant
	assert_matches_baseline killworker_s3_external_principal_grant baseline_s3_external_principal_grant
	teardown_cell killworker_s3_external_principal_grant
	wall_times[killworker_s3_external_principal_grant]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-s3-external-principal-grant: cell wall time: %ss\n' "${wall_times[killworker_s3_external_principal_grant]}"
}

# cell_failgraphwrite_s3_external_principal_grant fails the live
# GRANTS_ACCESS_TO MERGE exactly once, proves the fault decorator's durable
# marker names that write, and requires the full four-edge set to converge
# without dead letters. Anchor read off EXECUTED statement text, not source
# text: the writer's template reads MERGE (source)-[rel:%s]->(principal)
# (go/internal/storage/cypher/cloud/aws/s3/external_principal_grant_writer.go:34),
# filled from a closed single-member vocabulary screened per row, so the
# anchor MUST carry the INTERPOLATED type
# MERGE (source)-[rel:GRANTS_ACCESS_TO]->(principal). A literal %s matches
# no executed statement and the fault would never fire, giving a green cell
# that tested nothing.
cell_failgraphwrite_s3_external_principal_grant() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-s3-external-principal-grant: fresh stack"
	fresh_stack failgraphwrites3grant
	drive_all_cassettes failgraphwrites3grant
	ifa_s3_external_principal_grant_drive "failgraphwrite_s3_external_principal_grant" "${bin_dir}" "${s3_external_principal_grant_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-s3-external-principal-grant: eshu-ifa drive (grant family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-s3-external-principal-grant.json"
	ifa_fault_write_once_script "${fault_once_script}" "${s3_external_principal_grant_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwrite_s3_external_principal_grant" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwrite_s3_external_principal_grant" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwrite_s3_external_principal_grant
	assert_no_dead_letters failgraphwrite_s3_external_principal_grant
	ifa_s3_external_principal_grant_assert "failgraphwrite_s3_external_principal_grant" "${bin_dir}" "${s3_external_principal_grant_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-s3-external-principal-grant: recovered graph does not match the four-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 's3_external_principal_grant_materialization';" \
		"${compose_file}" | sed 's/^/  s3_external_principal_grant_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${s3_external_principal_grant_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-s3-external-principal-grant: the fault FIRED but on a different write than ${s3_external_principal_grant_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-s3-external-principal-grant: no once-fired marker at all -- the fault never fired, so this cell proved nothing about GRANTS_ACCESS_TO recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-s3-external-principal-grant: non-vacuous: once-fired marker names the targeted GRANTS_ACCESS_TO MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwrite_s3_external_principal_grant
	assert_matches_baseline failgraphwrite_s3_external_principal_grant baseline_s3_external_principal_grant
	teardown_cell failgraphwrite_s3_external_principal_grant
	wall_times[failgraphwrite_s3_external_principal_grant]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-s3-external-principal-grant: cell wall time: %ss\n' "${wall_times[failgraphwrite_s3_external_principal_grant]}"
}
