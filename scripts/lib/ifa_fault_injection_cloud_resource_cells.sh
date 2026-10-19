#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# cloud_resource-targeted live fault cells (#8358), mirroring
# scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh's shape:
# the targeted domain's claim is readiness-gated, so the kill cell banks
# readiness through a prereq-scoped reducer before the lock goes on.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_cloud_resource, killworker_cloud_resource,
# failgraphwrite_cloud_resource) against a live Postgres + Neo4j stack.
# The cells rely on the driver's strict mode and globals (bin_dir,
# tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_cloud_resource.sh
# (ifa_cloud_resource_drive / ifa_cloud_resource_assert); the cassette and
# expected-edge variables come
# from scripts/lib/ifa_family_fixtures.sh.
#
# THREE HANDLERS, ONE TARGETED DOMAIN. cloud_resource is three writers
# (AWS, GCP, Azure) behind one port, fanned out under
# aws_relationship_materialization / gcp_relationship_materialization /
# azure_relationship_materialization. These cells target the AWS handler
# (AWSRelationshipMaterializationHandler: claims under
# aws_relationship_materialization, loads facts via FactLoader ->
# fact_records, writes AWS_<verb> edges directly through
# cypher.CloudResourceEdgeWriter) and assert the FULL four-edge predicate
# set on recovery, so convergence of the untouched GCP and Azure edges is
# part of the proof. All three handlers share the FactLoader shape and the
# canonical_nodes_committed/cloud_resource_uid claim gate
# (reducer_queue_readiness_sql.go), so the kill mechanism proven here is
# the same one each half would need.
#
# The lock target is the handler's first synchronous READ: ACCESS
# EXCLUSIVE on fact_records blocks the fact load while the
# fact_work_items claim (which never reads fact_records) stays held.
# fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and locking that table would block the poll too.

# ifa_cloud_resource_start_fact_records_lock holds an ACCESS EXCLUSIVE lock
# on fact_records so the aws_relationship_materialization handler blocks on
# its fact load while its claim is already held.
ifa_cloud_resource_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63
	# bytes, and a truncated name makes the grant poll below miss forever
	# (proven live by the sibling families). ifa_cloud_lock_ keeps the
	# longest cell name this family uses well under the cap.
	local app_name="ifa_cloud_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/cloud-resource-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/cloud-resource-lock-${cell}.log" 2>&1 &
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
	# fact_records before reporting failure (see the sibling cells for the
	# incident that added this).
	printf '%s: fact_records lock blockers at acquisition failure:\n' "${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT 'blocker_snapshot' AS snapshot_kind, a.application_name, l.locktype, l.mode, l.granted, a.state, left(a.query, 120) AS query FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE l.relation = 'fact_records'::regclass ORDER BY 2, 3;" \
		"${compose_file}" 2>/dev/null | sed 's/^/  /' || true
	return 1
}

# ifa_cloud_resource_release_fact_records_lock terminates the named
# lock-holder backend, then joins its local psql/docker process before the
# replacement reducer starts.
ifa_cloud_resource_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_cloud_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_cloud_resource_wait_for_readiness banks the AWS relationship claim's
# readiness BEFORE the fact_records lock is taken, mirroring the iam
# family's helper: aws_relationship_materialization requires the
# canonical_nodes_committed/cloud_resource_uid phase, published by the
# aws_resource_materialization reducer during Handle -- which reads
# fact_records. Locking first deadlocks the cell by design. The acceptance
# unit is resolved off the aws relationship row's own identity, mirroring
# the gate's COALESCE(NULLIF(payload->>'entity_key', ''), scope_id).
# Fail-closed throughout.
ifa_cloud_resource_wait_for_readiness() {
	local cell="$1" budget="$2"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: readiness wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local identity scope generation unit
	if ! identity="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT scope_id || '|' || generation_id || '|' || COALESCE(NULLIF(payload->>'entity_key', ''), scope_id) FROM fact_work_items WHERE stage = 'reducer' AND domain = 'aws_relationship_materialization' ORDER BY created_at ASC, work_item_id ASC LIMIT 1;" \
		"${compose_file}")"; then
		printf '%s: readiness wait: aws relationship identity query failed\n' "${cell}" >&2
		return 1
	fi
	identity="$(printf '%s' "${identity}" | tr -d '[:space:]')"
	IFS='|' read -r scope generation unit <<<"${identity}"
	if [[ -z "${scope}" || -z "${generation}" || -z "${unit}" ]]; then
		printf '%s: readiness wait: no aws relationship row to resolve an identity from (got %q)\n' "${cell}" "${identity}" >&2
		return 1
	fi
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

# cell_baseline_cloud_resource is this family's fault-free reference run.
# It exists because the cloud cassette is driven ONLY by this family's own
# cells (drive_all_cassettes never carries a family cassette), so the
# shared `baseline` digest contains none of its edges; these cells compare
# against digests[baseline_cloud_resource]. It also establishes
# baseline_cloud_resource_retried: the fault-free excess-ATTEMPT total for
# aws_relationship_materialization, which the kill cell must exceed (see
# ifa_fault_count_retry_attempts for attempts over rows).
cell_baseline_cloud_resource() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-cloud-resource: fresh stack"
	fresh_stack baseline_cloud_resource
	drive_all_cassettes baseline_cloud_resource
	ifa_cloud_resource_drive "baseline_cloud_resource" "${bin_dir}" "${cloud_resource_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-cloud-resource: eshu-ifa drive (cloud family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_cloud_resource" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_cloud_resource" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_cloud_resource
	assert_no_dead_letters baseline_cloud_resource
	ifa_cloud_resource_assert "baseline_cloud_resource" "${bin_dir}" "${cloud_resource_expected_edges}" \
		|| die "baseline-cloud-resource: fault-free graph does not match the four-edge exact set"
	baseline_cloud_resource_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "aws_relationship_materialization")" \
		|| die "baseline-cloud-resource: could not count the fault-free aws_relationship_materialization retry-attempt baseline"
	printf 'baseline-cloud-resource: fault-free aws_relationship_materialization retry-attempt baseline: %s\n' "${baseline_cloud_resource_retried}"
	capture_digest baseline_cloud_resource
	teardown_cell baseline_cloud_resource
	wall_times[baseline_cloud_resource]=$(( $(date +%s) - cell_start ))
	printf 'baseline-cloud-resource: cell wall time: %ss\n' "${wall_times[baseline_cloud_resource]}"
}

cell_killworker_cloud_resource() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-cloud-resource: fresh stack"
	fresh_stack killworkercloudresource
	drive_all_cassettes killworkercloudresource
	ifa_cloud_resource_drive "killworkercloudresource" "${bin_dir}" "${cloud_resource_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-cloud-resource: eshu-ifa drive (cloud family) failed"
	local projector_pid reducer_pid_prereq reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkercloudresource" projector_pid "${bin_dir}/eshu-projector"
	# The relationship intents are created by the PROJECTOR. Wait for the
	# AWS row to exist (any status) before going further.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "aws_relationship_materialization" 1 >/dev/null \
		|| die "kill-worker-after-claim-cloud-resource: aws_relationship_materialization was never enqueued by the projector"
	# Readiness BEFORE the lock (see ifa_cloud_resource_wait_for_readiness):
	# a prereq-scoped reducer (aws_resource only, so the relationship row
	# can never be claimed early) banks the phase, and only then does the
	# lock go on.
	ifa_det_start_bg "${log_dir}" "reducer-killworkercloudresource-prereq" reducer_pid_prereq \
		env "ESHU_REDUCER_CLAIM_DOMAIN=aws_resource_materialization" "${bin_dir}/eshu-reducer"
	ifa_cloud_resource_wait_for_readiness "killworkercloudresource" 300 \
		|| die "kill-worker-after-claim-cloud-resource: claim readiness never banked"
	ifa_cloud_resource_start_fact_records_lock "killworkercloudresource" lock_holder_pid \
		|| die "kill-worker-after-claim-cloud-resource: could not acquire the deterministic fact_records read blocker"
	# Scoped to aws_relationship_materialization only, so this instance
	# never attempts another domain's row while the lock is held (see the
	# iam sibling for the starvation shape this avoids).
	ifa_det_start_bg "${log_dir}" "reducer-killworkercloudresource-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=aws_relationship_materialization" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "aws_relationship_materialization")" \
		|| die "kill-worker-after-claim-cloud-resource: no aws_relationship_materialization row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-cloud-resource: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_cloud_resource_release_fact_records_lock "killworkercloudresource" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkercloudresource-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkercloudresource
	assert_no_dead_letters killworkercloudresource
	ifa_cloud_resource_assert "killworkercloudresource" "${bin_dir}" "${cloud_resource_expected_edges}" \
		|| die "kill-worker-after-claim-cloud-resource: recovered graph does not match the four-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_cloud_resource_retried}" 15 "aws_relationship_materialization" \
		|| die "kill-worker-after-claim-cloud-resource: aws_relationship_materialization did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkercloudresource
	assert_matches_baseline killworkercloudresource baseline_cloud_resource
	teardown_cell killworkercloudresource
	wall_times[killworkercloudresource]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-cloud-resource: cell wall time: %ss\n' "${wall_times[killworkercloudresource]}"
}

# cell_failgraphwrite_cloud_resource fails the live AWS writer MERGE for
# the cassette's first verb exactly once, proves the fault decorator's
# durable marker names that write, and requires the full four-edge set to
# converge without dead letters. Anchor read off EXECUTED statement text:
# the template reads MERGE (source)-[rel:%s]->(target) with the %s filled
# per row from AWSRelationshipCypherTypePrefix + verb, so the anchor MUST
# carry the interpolated AWS_lambda_function_uses_execution_role. It is NOT
# AWS_RELATIONSHIP, the statement-metadata label.
cell_failgraphwrite_cloud_resource() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-cloud-resource: fresh stack"
	fresh_stack failgraphwritecloudresource
	drive_all_cassettes failgraphwritecloudresource
	ifa_cloud_resource_drive "failgraphwritecloudresource" "${bin_dir}" "${cloud_resource_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-cloud-resource: eshu-ifa drive (cloud family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-cloud-resource.json"
	ifa_fault_write_once_script "${fault_once_script}" "${cloud_resource_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritecloudresource" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritecloudresource" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritecloudresource
	assert_no_dead_letters failgraphwritecloudresource
	ifa_cloud_resource_assert "failgraphwritecloudresource" "${bin_dir}" "${cloud_resource_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-cloud-resource: recovered graph does not match the four-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'aws_relationship_materialization';" \
		"${compose_file}" | sed 's/^/  aws_relationship_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${cloud_resource_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		die "fail-graph-write-once-then-succeed-cloud-resource: the fault FIRED but on a different write than ${cloud_resource_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-cloud-resource: no once-fired marker at all -- the fault never fired, so this cell proved nothing about AWS writer recovery (marker status ${marker_rc})"
	printf 'fail-graph-write-once-then-succeed-cloud-resource: non-vacuous: once-fired marker names the targeted AWS writer MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwritecloudresource
	assert_matches_baseline failgraphwritecloudresource baseline_cloud_resource
	teardown_cell failgraphwritecloudresource
	wall_times[failgraphwritecloudresource]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-cloud-resource: cell wall time: %ss\n' "${wall_times[failgraphwritecloudresource]}"
}
