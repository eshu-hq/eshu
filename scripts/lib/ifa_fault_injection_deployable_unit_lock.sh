#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# deployable_unit_edges fault-cell lock target (#6149), split out of
# ifa_fault_injection_deployable_unit_cells.sh to keep that file under the
# repository's 500-line cap -- same reason ifa_deployable_unit_live_diagnostics.sh
# was split out of ifa_deployable_unit_live.sh. Sourced by
# verify-ifa-fault-injection.sh alongside that cells file; the driver owns
# strict mode and globals (use_compose, compose_file, FAULT_COMPOSE_PROJECT,
# ESHU_POSTGRES_DSN, log_dir, bg_pids, ifa_det_pg, ifa_det_untrack_bg_pid).
#
# cell_killworker_deployable_unit (ifa_fault_injection_deployable_unit_cells.sh)
# is the only caller of the lock/release and pre-kill isolation helpers below;
# cell_baseline_deployable_unit in that same file calls
# ifa_deployable_unit_require_admission_decisions_written.

# ifa_deployable_unit_require_admission_decisions_written fails closed unless
# admission_decisions carries at least one row for domain =
# 'deployable_unit_correlation'. This is the permanent version of a check that
# was, for one live run, done by hand instead of wired in -- and that gap is
# exactly the defect class this file exists to remove (a helper's assumption
# about what a handler writes, unverified against the handler's actual code).
#
# Both writeDeployableUnitAdmissionDecisions (go/internal/reducer/
# deployable_unit_admission_decisions.go) and the shared
# admissiondecision.WriteAdmissionDecisions it calls
# (go/internal/reducer/admissiondecision, issue #6061) return nil without
# writing anything when their input is empty or their writer is nil -- the
# same "returns early, the
# check downstream never sees anything happen" shape as
# publishIntentGraphPhase's two nil-exits. If this family's cassette ever
# stopped producing an admitted candidate, admission_decisions would carry
# zero rows for this domain, ifa_deployable_unit_start_admission_decisions_lock
# below would engage on a table that gets no traffic from this handler, and
# the kill cell would fail a third way for a third reason -- indistinguishable
# from the first two without this assertion naming it. Called from
# cell_baseline_deployable_unit, once, after that cell's maintenance pass and
# post-maintenance drain converge Handle to completion unblocked: this is a
# property of the fixture and the handler, identical across all three cells
# that drive the same cassette, so proving it once where Handle runs
# uncontested covers the fault cells too.
#
# Same fail-closed shape as ifa_deployable_unit_require_fresh_intents
# (ifa_fault_injection_deployable_unit_cells.sh), with the final comparison
# inverted (this asserts NON-zero, not zero): the query's own exit code, then
# empty output, then non-numeric output, each rejected as unknown rather than
# read as a pass or a fail in either direction -- only once all three are
# ruled out does a literal "0" fail the precondition.
ifa_deployable_unit_require_admission_decisions_written() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" postgres_dsn="$4" compose_file_arg="$5"
	local admission_count admission_count_rc
	if admission_count="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${postgres_dsn}" \
		"SELECT count(*) FROM admission_decisions WHERE domain = 'deployable_unit_correlation';" \
		"${compose_file_arg}")"; then
		admission_count_rc=0
	else
		admission_count_rc=$?
	fi
	if [[ "${admission_count_rc}" -ne 0 ]]; then
		printf '%s: admission_decisions precondition query FAILED (exit %s); treat this as unknown, not as a verdict\n' "${cell}" "${admission_count_rc}" >&2
		return "${admission_count_rc}"
	fi
	admission_count="$(printf '%s' "${admission_count}" | tr -d '[:space:]')"
	if [[ -z "${admission_count}" ]]; then
		printf '%s: admission_decisions precondition query returned empty output; treat that as unknown, not as zero\n' "${cell}" >&2
		return 1
	fi
	if [[ ! "${admission_count}" =~ ^[0-9]+$ ]]; then
		printf '%s: admission_decisions precondition query returned non-numeric output %q; treat that as unknown, not as zero\n' \
			"${cell}" "${admission_count}" >&2
		return 1
	fi
	if [[ "${admission_count}" == "0" ]]; then
		printf '%s: expected at least one admission_decisions row for domain=deployable_unit_correlation after the maintenance pass, got 0 -- the lock target below would engage on a table this handler never actually wrote for this fixture\n' "${cell}" >&2
		return 1
	fi
	printf '%s: confirmed %s admission_decisions row(s) for domain=deployable_unit_correlation -- the lock target below is a real write, not a race\n' "${cell}" "${admission_count}"
}

# ifa_deployable_unit_wait_for_kill_isolation keeps the deployable-unit fault
# targeted even though kill -9 stops the whole reducer process. That process
# also owns the generic and repo-dependency shared-projection runners. Killing
# it as soon as deployable_unit_correlation is claimed can therefore abandon an
# unrelated partition lease; repo_dependency's production lease is five
# minutes, longer than this gate's four-minute post-kill drain.
#
# The target handler is already blocked on admission_decisions when this runs.
# In one PostgreSQL snapshot, each poll requires: every non-target work item is
# terminal, every shared intent is complete, and no cross-scope completion
# event can reopen more work. Once all three are zero, the only remaining
# producer is deployable_unit_correlation, which has no IntentWriter and cannot
# create a shared intent. An idle partition lease may remain, but there is no
# work behind it for the kill to strand. Other reducer workers remain fully
# concurrent while the barrier waits; no production lease or worker setting is
# changed.
#
# Args: cell compose_project use_compose dsn compose_file budget_seconds
ifa_deployable_unit_wait_for_kill_isolation() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" dsn="$4"
	local compose_file_arg="$5" budget="$6"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: pre-kill isolation budget must be a positive integer, got %q\n' "${cell}" "${budget}" >&2
		return 1
	fi

	local raw state query_rc
	if raw="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${dsn}" \
		"CREATE OR REPLACE FUNCTION pg_temp.ifa_wait_for_deployable_unit_kill_isolation(wait_seconds integer)
		 RETURNS text LANGUAGE plpgsql AS \$\$
		 DECLARE
		   other_fact_work bigint;
		   shared_intents bigint;
		   completion_events bigint;
		   deadline timestamptz := clock_timestamp() + make_interval(secs => wait_seconds);
		 BEGIN
		   LOOP
		     SELECT
		       (SELECT count(*) FROM fact_work_items
		         WHERE status NOT IN ('succeeded', 'superseded')
		           AND NOT (stage = 'reducer' AND domain = 'deployable_unit_correlation')),
		       (SELECT count(*) FROM shared_projection_intents WHERE completed_at IS NULL),
		       (SELECT count(*) FROM cross_scope_completion_events)
		       INTO other_fact_work, shared_intents, completion_events;
		     IF other_fact_work = 0 AND shared_intents = 0 AND completion_events = 0 THEN
		       RETURN '0|0|0';
		     END IF;
		     EXIT WHEN clock_timestamp() >= deadline;
		     PERFORM pg_sleep(0.05);
		   END LOOP;
		   RETURN other_fact_work || '|' || shared_intents || '|' || completion_events;
		 END
		 \$\$;
		 SELECT pg_temp.ifa_wait_for_deployable_unit_kill_isolation(${budget});" \
		 "${compose_file_arg}")"; then
		query_rc=0
	else
		query_rc=$?
	fi
	if [[ "${query_rc}" -ne 0 ]]; then
		printf '%s: pre-kill isolation query failed (exit %s); refusing to kill a reducer with unknown cross-family ownership\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	state="$(printf '%s\n' "${raw}" | tail -n 1 | tr -d '[:space:]')"
	if [[ "${state}" == "0|0|0" ]]; then
		printf '%s: pre-kill isolation: other fact work=0, shared intents=0, completion events=0\n' "${cell}"
		return 0
	fi
	if [[ ! "${state}" =~ ^[0-9]+\|[0-9]+\|[0-9]+$ ]]; then
		printf '%s: pre-kill isolation returned malformed state %q; refusing to treat unknown ownership as isolated\n' "${cell}" "${state}" >&2
		return 1
	fi
	printf '%s: pre-kill isolation timed out with other_fact_work|shared_intents|completion_events=%s; refusing a process-wide kill that could orphan another family\n' "${cell}" "${state}" >&2
	return 1
}

# ifa_deployable_unit_start_admission_decisions_lock holds admission_decisions
# in ACCESS EXCLUSIVE MODE. Getting here took two wrong lock targets, both
# instructive:
#
#   1. shared_projection_intents (the code_calls-style default): this handler
#      has no IntentWriter and never touches that table at all
#      (DomainDeployableUnitEdges is absent from sharedProjectionDomains,
#      go/internal/reducer/intents/shared/worker/runner.go), so the lock never
#      engaged -- Handle ran to completion and marked the claimed
#      fact_work_items row 'succeeded' before kill -9 landed. Failed in CI:
#      "drain did not reach the snapshot's residual bound".
#   2. graph_projection_phase_state (Handle's LAST write, step 3 of 3 --
#      materializeDeployableUnitEdges, then writeDeployableUnitAdmissionDecisions,
#      then publishIntentGraphPhase): the lock DID engage, but that table has
#      FOURTEEN writing handlers against this gate's four reducer workers
#      (drive_workers, verify-ifa-fault-injection.sh). The workers filled with
#      other domains' items, all of them stalled on the same lock, and this
#      family's row was never claimed at all within the wait budget -- a
#      starvation failure, not a handler defect: "no claimed/running
#      fact_work_items row for domain=deployable_unit_correlation appeared
#      within 60s", with items demonstrably enqueued. A later write buys a
#      wider blocking window in TIME at the cost of a wider blast radius in
#      DOMAINS, and domain breadth is what starves a shared worker pool.
#
# admission_decisions (step 2 of 3, still after the graph write) has only
# THREE writing handlers in the whole codebase -- cloud_inventory_admission,
# package_source_correlation_handler, and this one -- and this gate drives
# neither of the other two (`rg -c 'cloud_inventory|package_source'
# scripts/lib/ifa_fault_injection_driver.sh` is 0). Within this harness this
# handler is the ONLY writer of this table, so starvation is impossible by
# construction, not by probability: nothing else competing for the workers
# ever queues a row that blocks on this lock.
#
# DO NOT "correct" this back to shared_projection_intents to match every
# sibling family's lock target. Every other family in this suite genuinely
# has an IntentWriter; this one does not, and admission_decisions is the only
# table this handler commits to that no other domain in this gate also
# writes. Record both reasons in one place so the next maintainer sees why
# this is the odd one out rather than rediscovering it by CI failure a third
# time. ifa_deployable_unit_require_admission_decisions_written above is the
# permanent proof that this table is genuinely written by this fixture, not
# a claim taken on trust.
#
# Consequence worth stating plainly, unlike code_calls' equivalent lock
# (which sits BEFORE that family's one durable write): because this lock
# lands AFTER the graph write (step 1), a kill -9 taken while blocked here
# kills a handler that has ALREADY written its CORRELATES_DEPLOYABLE_UNIT
# edge. The retry that follows re-executes Handle from scratch, so the graph
# write repeats as an idempotent MERGE and the admission-decision write that
# was blocked repeats as a fresh, successful one -- this proves recovery from
# a POST-write death, not a PRE-write death. It is still a real
# reclaim-and-re-execute proof: the kill cell (ifa_fault_injection_
# deployable_unit_cells.sh) snapshots attempt_count immediately after the
# post-kill drain reaches its residual bound and compares that SNAPSHOT
# against the fault-free baseline, not a live re-query at assertion time.
# CI caught why the live re-query is not equivalent: this family's own
# convergence loop (ifa_deployable_unit_live_converge_edges) can run another
# bootstrap-index maintenance pass after the drain, which reopens the
# recovered row and resets attempt_count back to 0 -- a live re-query taken
# after that point reads the repair's reset, not the recovery's evidence,
# and this family converges on maintenance pass 2 as its NORMAL path, so
# that race is the common case here, not a rare one.
ifa_deployable_unit_start_admission_decisions_lock() {
	local cell="$1" pid_var="$2"
	local app_name="ifa_deployable_unit_lock_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE admission_decisions IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/deployable-unit-admission-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/deployable-unit-admission-lock-${cell}.log" 2>&1 &
	fi
	local holder_pid=$!
	bg_pids+=("${holder_pid}")
	printf -v "${pid_var}" '%s' "${holder_pid}"

	local i lock_count
	for i in $(seq 1 60); do
		lock_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE a.application_name = '${app_name}' AND l.relation = 'admission_decisions'::regclass AND l.mode = 'AccessExclusiveLock' AND l.granted;" \
			"${compose_file}" | tr -d '[:space:]')"
		if [[ "${lock_count}" == "1" ]]; then
			return 0
		fi
		sleep 0.25
	done
	return 1
}

# ifa_deployable_unit_release_admission_decisions_lock terminates the named
# lock-holder backend and joins its local psql/docker process, mirroring
# ifa_fault_release_shared_intent_lock (ifa_fault_injection_common.sh), which is
# what the generic shared_intent_lock cells call now that the per-family
# wrappers are gone.
ifa_deployable_unit_release_admission_decisions_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_deployable_unit_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_deployable_unit_wait_for_readiness_fence waits, at the database level,
# until at least one live deployable_unit_correlation row can pass Handle's
# resolution readiness gate: its own relationship generation is active, and no
# active scope holds the corpus fence (a current generation row that is not
# active, or a missing row with live deployment_mapping work). It mirrors
# incompleteScopeRelationshipGenerationsPredicate
# (go/internal/storage/postgres/relationship_schema.go).
#
# It runs after the before-reducer starts, not before: that reducer's own
# deployment_mapping work is what activates the generation (run 36125876678:
# cross-repo resolution started 3 s after the first claims). Until the fence
# opens, every claim fails readiness with a NON-counting class and never
# reaches admission_decisions. The census below is the proof of a parked
# claim; this wait makes a missed fence fail with its own message.
#
# Args: cell compose_project use_compose dsn compose_file budget_seconds
ifa_deployable_unit_wait_for_readiness_fence() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" dsn="$4" compose_file_arg="$5" budget="$6"
	local raw state query_rc
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: readiness fence budget must be a positive integer, got %q\n' "${cell}" "${budget}" >&2
		return 1
	fi
	if raw="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${dsn}" \
		"/* deployable_unit readiness fence */ CREATE OR REPLACE FUNCTION pg_temp.ifa_wait_for_deployable_unit_readiness(wait_seconds integer)
		 RETURNS text LANGUAGE plpgsql AS \$\$
		 DECLARE
		   ready_rows bigint;
		   fence_holders bigint;
		   deadline timestamptz := clock_timestamp() + make_interval(secs => wait_seconds);
		 BEGIN
		   LOOP
		     SELECT
		       (SELECT count(*) FROM fact_work_items AS w
		         WHERE w.stage = 'reducer' AND w.domain = 'deployable_unit_correlation'
		           AND w.status IN ('pending', 'claimed', 'running', 'retrying')
		           AND EXISTS (SELECT 1 FROM relationship_generations AS rg
		                        WHERE rg.generation_id = w.generation_id AND rg.status = 'active')),
		       (SELECT count(*) FROM ingestion_scopes AS s
		         WHERE s.status = 'active'
		           AND (EXISTS (SELECT 1 FROM relationship_generations AS rg
		                         WHERE rg.scope = s.scope_id AND rg.generation_id = s.active_generation_id
		                           AND rg.status <> 'active')
		                OR (NOT EXISTS (SELECT 1 FROM relationship_generations AS rg
		                                 WHERE rg.scope = s.scope_id AND rg.generation_id = s.active_generation_id)
		                    AND EXISTS (SELECT 1 FROM fact_work_items AS m
		                                 WHERE m.stage = 'reducer' AND m.domain = 'deployment_mapping'
		                                   AND m.scope_id = s.scope_id
		                                   AND m.status IN ('pending', 'claimed', 'running', 'retrying')))))
		       INTO ready_rows, fence_holders;
		     IF ready_rows > 0 AND fence_holders = 0 THEN
		       RETURN ready_rows || '|0';
		     END IF;
		     EXIT WHEN clock_timestamp() >= deadline;
		     PERFORM pg_sleep(0.05);
		   END LOOP;
		   RETURN ready_rows || '|' || fence_holders;
		 END
		 \$\$;
		 SELECT pg_temp.ifa_wait_for_deployable_unit_readiness(${budget});" \
		"${compose_file_arg}")"; then :; else
		query_rc=$?
		printf '%s: readiness fence query FAILED (exit %s); state is unknown\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	state="$(printf '%s\n' "${raw}" | tail -n 1 | tr -d '[:space:]')"
	if [[ ! "${state}" =~ ^[0-9]+\|[0-9]+$ ]]; then
		printf '%s: readiness fence returned malformed state %q; state is unknown\n' "${cell}" "${state}" >&2
		return 1
	fi
	if [[ "${state}" == *"|0" && "${state%%|*}" -gt 0 ]]; then
		printf '%s: readiness fence open: %s deployable_unit_correlation row(s) with an active own generation, 0 corpus-fence holders\n' "${cell}" "${state%%|*}"
		return 0
	fi
	printf '%s: readiness fence did not open within %ss (ready_rows|fence_holders=%s)\n' "${cell}" "${budget}" "${state}" >&2
	return 1
}

# ifa_deployable_unit_wait_for_blocked_claim is the kill cell's non-vacuity
# census. One snapshot must show the labeled admission_decisions holder, at
# least one backend waiting (NOT granted) on that relation, and exactly as many
# claimed/running deployable_unit_correlation rows as waiters. Within this gate
# the handler is the only admission_decisions writer (see the lock helper's
# header), so each waiter is one parked claim; a claim that is about to fail
# readiness has no waiter and keeps the census polling. On success the claimed
# rows are written to output_var as "md5(work_item_id) attempt_count
# last_attempt_epoch_us" entries joined by commas. The digest keeps arbitrary
# work-item text out of the shell and out of the later SQL literal.
#
# Args: cell compose_project use_compose dsn compose_file budget_seconds output_var
ifa_deployable_unit_wait_for_blocked_claim() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" dsn="$4" compose_file_arg="$5" budget="$6" output_var="$7"
	local app_name="ifa_deployable_unit_lock_${cell}" raw state query_rc holders waiters claimed rows
	if [[ ! "${cell}" =~ ^[a-z0-9_]+$ || ! "${budget}" =~ ^[1-9][0-9]*$ || ! "${output_var}" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]]; then
		printf 'ifa_deployable_unit_wait_for_blocked_claim: invalid cell %q, budget %q, or output variable %q\n' "${cell}" "${budget}" "${output_var}" >&2
		return 1
	fi
	if raw="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${dsn}" \
		"/* deployable_unit blocked-claim census */ CREATE OR REPLACE FUNCTION pg_temp.ifa_wait_for_deployable_unit_blocked_claim(wait_seconds integer)
		 RETURNS text LANGUAGE plpgsql AS \$\$
		 DECLARE
		   holders bigint; waiters bigint; claimed bigint; claimed_rows text;
		   deadline timestamptz := clock_timestamp() + make_interval(secs => wait_seconds);
		 BEGIN
		   LOOP
		     PERFORM pg_stat_clear_snapshot();
		     SELECT count(DISTINCT holder.pid) INTO holders
		       FROM pg_catalog.pg_stat_activity AS holder
		       JOIN pg_catalog.pg_locks AS held ON held.pid = holder.pid
		      WHERE holder.application_name = '${app_name}' AND held.granted
		        AND held.relation = 'admission_decisions'::regclass AND held.mode = 'AccessExclusiveLock';
		     SELECT count(DISTINCT waiter.pid) INTO waiters
		       FROM pg_catalog.pg_locks AS lock_row
		       JOIN pg_catalog.pg_stat_activity AS waiter ON waiter.pid = lock_row.pid
		      WHERE lock_row.relation = 'admission_decisions'::regclass AND NOT lock_row.granted
		        AND waiter.wait_event_type = 'Lock' AND waiter.application_name <> '${app_name}';
		     SELECT count(*), string_agg(md5(work_item_id) || ' ' || attempt_count || ' '
		              || coalesce((extract(epoch FROM last_attempt_at) * 1000000)::bigint::text, 'null'),
		              ',' ORDER BY work_item_id)
		       INTO claimed, claimed_rows
		       FROM fact_work_items
		      WHERE stage = 'reducer' AND domain = 'deployable_unit_correlation' AND status IN ('claimed', 'running');
		     IF holders = 1 AND waiters > 0 AND claimed = waiters THEN
		       RETURN holders || '|' || waiters || '|' || claimed || '|' || claimed_rows;
		     END IF;
		     EXIT WHEN clock_timestamp() >= deadline;
		     PERFORM pg_sleep(0.01);
		   END LOOP;
		   RETURN holders || '|' || waiters || '|' || claimed || '|' || coalesce(claimed_rows, '');
		 END
		 \$\$;
		 SELECT pg_temp.ifa_wait_for_deployable_unit_blocked_claim(${budget});" \
		"${compose_file_arg}")"; then :; else
		query_rc=$?
		printf '%s: blocked-claim census query FAILED (exit %s); state is unknown\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	state="$(printf '%s\n' "${raw}" | tail -n 1)"
	IFS='|' read -r holders waiters claimed rows <<<"${state}"
	if [[ ! "${holders}" =~ ^[0-9]+$ || ! "${waiters}" =~ ^[0-9]+$ || ! "${claimed}" =~ ^[0-9]+$ ]]; then
		printf '%s: blocked-claim census returned malformed state %q; state is unknown\n' "${cell}" "${state}" >&2
		return 1
	fi
	if [[ "${holders}" != "1" || "${waiters}" -eq 0 || "${claimed}" != "${waiters}" ]]; then
		printf '%s: no claim parked behind the admission_decisions holder within %ss (holders|waiters|claimed=%s|%s|%s); a claimed row with no waiter is a readiness deferral, not an in-flight handler\n' \
			"${cell}" "${budget}" "${holders}" "${waiters}" "${claimed}" >&2
		return 1
	fi
	_ifa_deployable_unit_validate_killed_rows "${cell}" "${rows}" || return 1
	printf -v "${output_var}" '%s' "${rows}"
	printf '%s: non-vacuous: %s claim(s) parked behind the admission_decisions holder (%s waiter(s))\n' "${cell}" "${claimed}" "${waiters}"
}

_ifa_deployable_unit_validate_killed_rows() {
	local cell="$1" rows="$2" entry
	local entry_re='^[0-9a-f]{32} [0-9]+ [0-9]+$'
	[[ -n "${rows}" ]] || { printf '%s: killed-claim set is empty; state is unknown\n' "${cell}" >&2; return 1; }
	local -a entries
	IFS=',' read -r -a entries <<<"${rows}"
	for entry in "${entries[@]}"; do
		[[ "${entry}" =~ ${entry_re} ]] || {
			printf '%s: killed-claim entry %q is not "md5 attempt_count last_attempt_epoch_us"; state is unknown\n' "${cell}" "${entry}" >&2
			return 1
		}
	done
}

# ifa_deployable_unit_assert_killed_claims_reexecuted proves the replacement
# reducer re-executed every claim the kill interrupted: each killed work item
# now has a larger attempt_count and a later last_attempt_at than it had at
# kill time. Both move in the same claim UPDATE when an expired claimed row is
# reclaimed (reducerClaimAttemptCountCaseSQL only preserves attempt_count for a
# 'retrying' row with a non-counting class). This replaces a domain row count
# compared against a fault-free baseline that is itself 0 or 1 (run
# 35945606336). Take it right after the post-kill drain, before any
# maintenance pass can reopen the row and reset attempt_count.
#
# Args: cell compose_project use_compose dsn compose_file killed_rows
ifa_deployable_unit_assert_killed_claims_reexecuted() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" dsn="$4" compose_file_arg="$5" rows="$6"
	local entry values="" expected=0 raw state query_rc digest attempt epoch
	_ifa_deployable_unit_validate_killed_rows "${cell}" "${rows}" || return 1
	local -a entries
	IFS=',' read -r -a entries <<<"${rows}"
	for entry in "${entries[@]}"; do
		read -r digest attempt epoch <<<"${entry}"
		values+="${values:+,}('${digest}',${attempt},${epoch})"
		expected=$((expected + 1))
	done
	if raw="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${dsn}" \
		"/* deployable_unit killed-claim re-execution */ WITH killed(digest, attempt_count, last_attempt_us) AS (VALUES ${values}) SELECT count(*)::text || '|' || count(*) FILTER (WHERE w.attempt_count > killed.attempt_count AND (extract(epoch FROM w.last_attempt_at) * 1000000)::bigint > killed.last_attempt_us)::text FROM killed LEFT JOIN fact_work_items AS w ON md5(w.work_item_id) = killed.digest AND w.stage = 'reducer' AND w.domain = 'deployable_unit_correlation';" \
		"${compose_file_arg}")"; then :; else
		query_rc=$?
		printf '%s: killed-claim re-execution query FAILED (exit %s); state is unknown\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	state="$(printf '%s\n' "${raw}" | tail -n 1 | tr -d '[:space:]')"
	if [[ "${state}" != "${expected}|${expected}" ]]; then
		printf '%s: killed claims re-executed by the replacement: %s, want %s|%s (killed set: %s)\n' "${cell}" "${state}" "${expected}" "${expected}" "${rows}" >&2
		return 1
	fi
	printf '%s: every killed claim (%s) was reclaimed and re-executed with a new attempt\n' "${cell}" "${expected}"
}
