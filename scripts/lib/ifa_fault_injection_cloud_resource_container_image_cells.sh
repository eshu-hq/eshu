#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# cloud_resource_container_image-targeted live fault cells (#6228 slice D live
# leaf), mirroring the cloud_resource family's shape including the kill
# cell: this domain's claim is NOT readiness-gated
# (aws_cloud_image_materialization carries no row in the reducer-claim
# readiness requirements -- go/internal/storage/postgres/
# reducer_queue_readiness_sql.go names the sibling *_materialization domains
# but not this one), yet its in-handler sourceNodesReady gate runs BEFORE
# the fact load the kill cell's lock blocks. Locking as soon as the
# projector enqueues the row (the k8s shape) leaves the handler
# spin-deferring on a gate the lock starves while the kill destroys an
# idle worker -- proven live by PR #8740 shard 2/4 false-failing the
# attempts-above-baseline proof on a correct recovery. So the kill cell
# takes the s3/cloud_resource shape instead: an aws_resource-scoped prereq
# reducer banks the canonical_nodes_committed/cloud_resource_uid phase for
# the exact acceptance unit first, and only then does the lock go on, so
# the first claim past the open gate blocks deterministically in its fact
# load. The readiness deferral is non-counting and shared by the baseline
# and kill cells alike, and past the banked phase no deferral can fire, so
# the kill cell's attempts-above-baseline comparison proves re-execution
# rather than readiness churn.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_cloud_resource_container_image,
# killworker_cloud_resource_container_image,
# failgraphwrite_cloud_resource_container_image) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_cloud_resource_container_image.sh
# (ifa_cloud_resource_container_image_drive /
# ifa_cloud_resource_container_image_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-cloud-resource-container-image-cases.sh
# sources this file and drives
# ifa_cloud_resource_container_image_start_fact_records_lock and
# ifa_cloud_resource_container_image_release_fact_records_lock under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so
# editing either function's fail-closed behavior or its bg_pids bookkeeping
# will fail `make pre-pr`.
#
# cloud_resource_container_image is SINGLE-STAGE and DIRECT. The handler
# (AWSCloudImageMaterializationHandler) claims a fact_work_items row under
# domain "aws_cloud_image_materialization", loads its facts via
# factload.LoadFactsForKinds -> FROM fact_records, and writes the
# AWS_lambda_function_uses_image edges DIRECTLY to the graph through its
# container-image edge writer
# (go/internal/storage/cypher/cloud/aws/image/container_edge_writer.go),
# then acks. There is no shared_projection_intents row and no second-stage
# runner lease -- the two blocker kinds the registry schema offers that this
# family cannot use (see rows/24_cloud_resource_container_image.sh).
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. The handler struct embeds FactLoader
# (go/internal/reducer/awscloud/aws_cloud_image_materialization.go:100) and
# Handle refuses to run without it (:134-136) before passing it to
# factload.LoadFactsForKinds (:163) -- the pre-write window the lock needs.
#
# ifa_cloud_resource_container_image_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the aws_cloud_image_materialization
# handler blocks on its FIRST synchronous read -- the fact load -- while its
# fact_work_items claim is already held. The claim, heartbeat, and ack path
# never reads fact_records, so the row stays claimed and alive for the whole
# hold. fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and locking that table would block the poll too.
ifa_cloud_resource_container_image_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_crc_image_lock_ keeps the longest name this family uses at 41 bytes.
	local app_name="ifa_crc_image_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/cloud-resource-container-image-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/cloud-resource-container-image-lock-${cell}.log" 2>&1 &
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

# ifa_cloud_resource_container_image_release_fact_records_lock terminates
# the named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_cloud_resource_container_image_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_crc_image_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_cloud_resource_container_image_wait_for_readiness banks the
# canonical_nodes_committed/cloud_resource_uid phase BEFORE the
# fact_records lock is taken, mirroring the cloud_resource family's
# helper: aws_cloud_image_materialization's in-handler sourceNodesReady
# gate runs BEFORE its fact load, so locking first leaves the handler
# spin-deferring on a gate the lock starves (the projector's own fact
# reads can block behind it) while the kill destroys an idle worker --
# and the replacement's reclaim under the non-counting readiness class
# adds no attempt, false-failing the attempts-above-baseline proof on a
# correct recovery (proven live: PR #8740 shard 2/4). The acceptance unit
# is resolved off the image row's own identity, mirroring the gate's
# COALESCE(NULLIF(payload->>'entity_key', ''), scope_id) and the handler's
# gpphase.KeyFromScope derivation, so the poll waits on exactly the unit
# the handler's lookup will ask for. Fail-closed throughout: no identity
# row, a failed query, a non-numeric poll result, or budget exhaustion all
# return non-zero, and a non-numeric phase count is a failure, never a zero.
ifa_cloud_resource_container_image_wait_for_readiness() {
	local cell="$1" budget="$2"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: readiness wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local identity scope generation unit
	if ! identity="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT scope_id || '|' || generation_id || '|' || COALESCE(NULLIF(payload->>'entity_key', ''), scope_id) FROM fact_work_items WHERE stage = 'reducer' AND domain = 'aws_cloud_image_materialization' ORDER BY created_at ASC, work_item_id ASC LIMIT 1;" \
		"${compose_file}")"; then
		printf '%s: readiness wait: cloud image identity query failed\n' "${cell}" >&2
		return 1
	fi
	identity="$(printf '%s' "${identity}" | tr -d '[:space:]')"
	IFS='|' read -r scope generation unit <<<"${identity}"
	if [[ -z "${scope}" || -z "${generation}" || -z "${unit}" ]]; then
		printf '%s: readiness wait: no cloud image row to resolve an identity from (got %q)\n' "${cell}" "${identity}" >&2
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

# cell_baseline_cloud_resource_container_image is this family's fault-free
# reference run. It exists because the cloud_resource_container_image
# cassette is driven ONLY by this family's own cells:
# ifa_fault_injection_driver.sh's header rules that a new family cassette is
# never added to drive_all_cassettes, since the extra succeeded reducer row
# enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# AWS_lambda_function_uses_image edges and every cell of this family would
# mismatch it. These cells therefore compare against
# digests[baseline_cloud_resource_container_image] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_cloud_resource_container_image_retried: the
# fault-free excess-ATTEMPT total for domain
# aws_cloud_image_materialization, which the kill cell must exceed to
# prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item, so the row-count form saturates at 1 (see
# ifa_fault_count_retry_attempts).
cell_baseline_cloud_resource_container_image() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-cloud-resource-container-image: fresh stack"
	fresh_stack baseline_crcimage
	drive_all_cassettes baseline_crcimage
	ifa_cloud_resource_container_image_drive "baseline_crcimage" "${bin_dir}" "${cloud_resource_container_image_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-cloud-resource-container-image: eshu-ifa drive (cloud_resource_container_image family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_crcimage" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_crcimage" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_crcimage
	assert_no_dead_letters baseline_crcimage
	ifa_cloud_resource_container_image_assert "baseline_crcimage" "${bin_dir}" "${cloud_resource_container_image_expected_edges}" \
		|| die "baseline-cloud-resource-container-image: fault-free graph does not match the two-edge exact set"
	baseline_cloud_resource_container_image_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "aws_cloud_image_materialization")" \
		|| die "baseline-cloud-resource-container-image: could not count the fault-free aws_cloud_image_materialization retry-attempt baseline"
	printf 'baseline-cloud-resource-container-image: fault-free aws_cloud_image_materialization retry-attempt baseline: %s\n' "${baseline_cloud_resource_container_image_retried}"
	# Captured under the BASELINE KEY, not the cell label: the kill and
	# fail-graph-write cells assert against
	# digests[baseline_cloud_resource_container_image]. Capturing under the
	# label leaves that key unset and the match dies on an unbound variable
	# under the gate's `set -u` (found by the targeted live proof, which got
	# past the attempts assert the old lock-first shape never reached).
	capture_digest baseline_cloud_resource_container_image
	teardown_cell baseline_crcimage
	wall_times[baseline_crcimage]=$(( $(date +%s) - cell_start ))
	printf 'baseline-cloud-resource-container-image: cell wall time: %ss\n' "${wall_times[baseline_crcimage]}"
}

cell_killworker_cloud_resource_container_image() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-cloud-resource-container-image: fresh stack"
	fresh_stack killworkercrcimage
	drive_all_cassettes killworkercrcimage
	ifa_cloud_resource_container_image_drive "killworkercrcimage" "${bin_dir}" "${cloud_resource_container_image_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-cloud-resource-container-image: eshu-ifa drive (cloud_resource_container_image family) failed"
	local projector_pid reducer_pid_prereq reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkercrcimage" projector_pid "${bin_dir}/eshu-projector"
	# aws_cloud_image_materialization's reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status) before
	# going further: without it there is nothing to make ready and nothing
	# to kill, and locking fact_records before that intent exists risks
	# starving the projector's OWN fact reads for this scope too, so the row
	# could never be created for as long as the lock is held -- not merely
	# delayed.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "aws_cloud_image_materialization" 1 >/dev/null \
		|| die "kill-worker-after-claim-cloud-resource-container-image: aws_cloud_image_materialization was never enqueued by the projector"
	# Readiness BEFORE the lock -- the s3/cloud_resource shape, not the k8s
	# one. This domain carries no claim-time readiness row, and its
	# in-handler sourceNodesReady gate runs BEFORE the fact load the lock
	# blocks: locking first leaves the handler spin-deferring (non-counting,
	# so the attempt total never moves) while the kill destroys an idle
	# worker, and the attempts-above-baseline proof false-fails on a correct
	# recovery (proven live: PR #8740 shard 2/4). A prereq-scoped reducer
	# (aws_resource only, so the image row can never be claimed early) runs
	# unblocked until the exact acceptance unit the handler's lookup will
	# ask for is banked. Only then does the lock go on, so the first claim
	# past the open gate blocks deterministically in its fact load.
	ifa_det_start_bg "${log_dir}" "reducer-killworkercrcimage-prereq" reducer_pid_prereq \
		env "ESHU_REDUCER_CLAIM_DOMAIN=aws_resource_materialization" "${bin_dir}/eshu-reducer"
	ifa_cloud_resource_container_image_wait_for_readiness "killworkercrcimage" 300 \
		|| die "kill-worker-after-claim-cloud-resource-container-image: claim readiness never banked"
	ifa_cloud_resource_container_image_start_fact_records_lock "killworkercrcimage" lock_holder_pid \
		|| die "kill-worker-after-claim-cloud-resource-container-image: could not acquire the deterministic fact_records read blocker"
	# Scoped to aws_cloud_image_materialization only: with the lock already
	# held, an unscoped reducer's workers can all grab OTHER pending domains
	# that also read fact_records and get stuck there for the lock's full
	# duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's row.
	# The claim fires without waiting on any phase: this domain carries no
	# claim-time readiness requirement, so the lock starves nothing the
	# claim still needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkercrcimage-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=aws_cloud_image_materialization" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "aws_cloud_image_materialization")" \
		|| die "kill-worker-after-claim-cloud-resource-container-image: no aws_cloud_image_materialization row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-cloud-resource-container-image: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_cloud_resource_container_image_release_fact_records_lock "killworkercrcimage" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkercrcimage-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkercrcimage
	assert_no_dead_letters killworkercrcimage
	ifa_cloud_resource_container_image_assert "killworkercrcimage" "${bin_dir}" "${cloud_resource_container_image_expected_edges}" \
		|| die "kill-worker-after-claim-cloud-resource-container-image: recovered graph does not match the two-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_cloud_resource_container_image_retried}" 15 "aws_cloud_image_materialization" \
		|| die "kill-worker-after-claim-cloud-resource-container-image: aws_cloud_image_materialization did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkercrcimage
	assert_matches_baseline killworkercrcimage baseline_cloud_resource_container_image
	teardown_cell killworkercrcimage
	wall_times[killworkercrcimage]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-cloud-resource-container-image: cell wall time: %ss\n' "${wall_times[killworkercrcimage]}"
}

# cell_failgraphwrite_cloud_resource_container_image fails the live
# AWS_lambda_function_uses_image MERGE exactly once, proves the fault
# decorator's durable marker names that write, and requires the full
# two-edge set to converge without dead letters. Anchor read off EXECUTED
# statement text, not source text: the writer's template reads
# MERGE (source)-[rel:%s]->(target)
# (go/internal/storage/cypher/cloud/aws/image/container_edge_writer.go:72-75),
# with the %s filled per row from the closed single-member vocabulary, so
# the anchor carries the interpolated
# MERGE (source)-[rel:AWS_lambda_function_uses_image]->(target) form. A
# literal `%s` spelling would match no executed statement and the fault
# would never fire, giving a green cell that tested nothing.
cell_failgraphwrite_cloud_resource_container_image() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-cloud-resource-container-image: fresh stack"
	fresh_stack failgraphwritecrcimage
	drive_all_cassettes failgraphwritecrcimage
	ifa_cloud_resource_container_image_drive "failgraphwritecrcimage" "${bin_dir}" "${cloud_resource_container_image_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-cloud-resource-container-image: eshu-ifa drive (cloud_resource_container_image family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-cloud-resource-container-image.json"
	ifa_fault_write_once_script "${fault_once_script}" "${cloud_resource_container_image_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritecrcimage" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritecrcimage" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritecrcimage
	assert_no_dead_letters failgraphwritecrcimage
	ifa_cloud_resource_container_image_assert "failgraphwritecrcimage" "${bin_dir}" "${cloud_resource_container_image_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-cloud-resource-container-image: recovered graph does not match the two-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'aws_cloud_image_materialization';" \
		"${compose_file}" | sed 's/^/  aws_cloud_image_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${cloud_resource_container_image_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-cloud-resource-container-image: the fault FIRED but on a different write than ${cloud_resource_container_image_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-cloud-resource-container-image: no once-fired marker at all -- the fault never fired, so this cell proved nothing about AWS_lambda_function_uses_image recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-cloud-resource-container-image: non-vacuous: once-fired marker names the targeted AWS_lambda_function_uses_image MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwritecrcimage
	assert_matches_baseline failgraphwritecrcimage baseline_cloud_resource_container_image
	teardown_cell failgraphwritecrcimage
	wall_times[failgraphwritecrcimage]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-cloud-resource-container-image: cell wall time: %ss\n' "${wall_times[failgraphwritecrcimage]}"
}
