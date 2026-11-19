#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Generic blocked-claim census, post-kill hold recheck, and monotonic
# re-execution proof for the shared_intent_lock generic kill cells (issue
# #10955, arbiter ruling recorded against the goal file).
#
# The kill cell's old retry fingerprint compared a final-state
# attempt_count > 1 row COUNT against a fault-free baseline that is itself 0
# or 1, so a kill window that never engaged (no backend ever parked behind
# the lock) reported "did not re-execute above baseline" -- a verdict about
# the recovery that was really about a vacuous kill window. These three
# helpers replace that comparison with per-row evidence:
#   1. ifa_fault_wait_for_blocked_claim -- before SIGKILL, one Postgres
#      snapshot must show the labeled shared_projection_intents holder, at
#      least one backend waiting (NOT granted) on that relation, and at
#      least one claimed/running row for the family's wait_key domain. The
#      claimed rows are captured as the killed set
#      (md5|attempt_count|last_attempt_epoch_us|lease_owner).
#   2. ifa_fault_assert_blocked_claims_still_held -- after SIGKILL and BEFORE
#      the lock release, every killed row must still be claimed/running. A
#      row that acked between the census and the kill dies as "blocker did
#      not engage ... (acked before kill)", not as a recovery failure. This
#      recheck, not waiter identity, carries soundness against a
#      cross-domain waiter coincidence on the shared table.
#   3. ifa_fault_assert_killed_claims_reexecuted -- after the post-kill
#      drain, every killed row must show a strictly larger attempt_count AND
#      a strictly later last_attempt_at than its kill-time snapshot. A
#      lease_owner change alone is rejected as proof (NULL after ack).
#
# How this differs from ifa_deployable_unit_wait_for_blocked_claim
# (ifa_fault_injection_deployable_unit_lock.sh), the precedent it is modeled
# on: admission_decisions has exactly one writer in this gate, so that
# census requires claimed == waiters (sole-writer premise). The shared table
# has many legitimate writers, so other domains may block behind the same
# holder; this census requires claimed > 0 instead and never equates the two
# counts. Copying the equality here would starve whenever an unrelated
# domain also parks, which is the normal case, not an edge.
#
# Sourced by ifa_fault_generic_cells.sh (self-sourced alongside the other
# mechanism files). Plain function library, not a script: the gate owns
# strict mode and the globals below (ifa_det_pg, FAULT_COMPOSE_PROJECT,
# use_compose, ESHU_POSTGRES_DSN, compose_file, log_dir).

# _ifa_fault_validate_blocked_claim_rows fails closed unless rows is a
# comma-joined, non-empty set of
# "md5(work_item_id) attempt_count last_attempt_epoch_us lease_owner"
# entries. The epoch must be numeric: a NULL last_attempt_at means the row
# was never attempt-stamped, and reading that as "epoch zero, trivially
# re-executed later" would green the monotonic proof on unknown state -- the
# same unknown-as-zero shape every sibling precondition rejects. lease_owner
# may be the literal "null" (coalesced NULL); it must still be present and
# pipe-free so the census state's '|' separators stay unambiguous.
_ifa_fault_validate_blocked_claim_rows() {
	local cell="$1" rows="$2" entry
	local entry_re='^[0-9a-f]{32} [0-9]+ [0-9]+ [^[:space:]|]+$'
	[[ -n "${rows}" ]] || { printf '%s: killed-claim set is empty; state is unknown\n' "${cell}" >&2; return 1; }
	local -a entries
	IFS=',' read -r -a entries <<<"${rows}"
	for entry in "${entries[@]}"; do
		[[ "${entry}" =~ ${entry_re} ]] || {
			printf '%s: killed-claim entry %q is not "md5 attempt_count last_attempt_epoch_us lease_owner"; state is unknown\n' "${cell}" "${entry}" >&2
			return 1
		}
	done
}

# ifa_fault_wait_for_blocked_claim is the kill cell's non-vacuity census for
# shared_intent_lock families. One snapshot must show the labeled
# shared_projection_intents holder, at least one backend waiting (NOT
# granted) on that relation with wait_event_type Lock, and at least one
# claimed/running reducer row for wait_key. On success the claimed rows are
# written to rows_var and the "holders|waiters|claimed" census to state_var
# for the kill snapshot; both callers need them and a single output cannot
# carry both shapes without reparsing.
#
# Args: cell namespace wait_key budget_seconds rows_var state_var
ifa_fault_wait_for_blocked_claim() {
	local cell="$1" namespace="$2" wait_key="$3" budget="$4" rows_var="$5" state_var="$6"
	local app_name="ifa_${namespace}_lock_${cell}" raw state query_rc holders waiters claimed rows
	if [[ ! "${cell}" =~ ^[a-z0-9_]+$ || ! "${namespace}" =~ ^[a-z0-9_]+$ ]]; then
		printf 'ifa_fault_wait_for_blocked_claim: invalid cell %q or namespace %q\n' "${cell}" "${namespace}" >&2
		return 1
	fi
	if [[ ! "${wait_key}" =~ ^[a-z0-9_]+$ ]]; then
		printf 'ifa_fault_wait_for_blocked_claim: wait_key must match ^[a-z0-9_]+$, got %q\n' "${wait_key}" >&2
		return 1
	fi
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ || ! "${rows_var}" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ || ! "${state_var}" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]]; then
		printf 'ifa_fault_wait_for_blocked_claim: invalid budget %q, rows variable %q, or state variable %q\n' "${budget}" "${rows_var}" "${state_var}" >&2
		return 1
	fi
	if raw="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"/* generic shared_intent_lock blocked-claim census */ CREATE OR REPLACE FUNCTION pg_temp.ifa_wait_for_generic_blocked_claim(wait_seconds integer)
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
		        AND held.relation = 'shared_projection_intents'::regclass AND held.mode = 'AccessExclusiveLock';
		     SELECT count(DISTINCT waiter.pid) INTO waiters
		       FROM pg_catalog.pg_locks AS lock_row
		       JOIN pg_catalog.pg_stat_activity AS waiter ON waiter.pid = lock_row.pid
		      WHERE lock_row.relation = 'shared_projection_intents'::regclass AND NOT lock_row.granted
		        AND waiter.wait_event_type = 'Lock' AND waiter.application_name <> '${app_name}';
		     SELECT count(*), string_agg(md5(work_item_id) || ' ' || attempt_count || ' '
		              || coalesce((extract(epoch FROM last_attempt_at) * 1000000)::bigint::text, 'null')
		              || ' ' || coalesce(lease_owner, 'null'),
		              ',' ORDER BY work_item_id)
		       INTO claimed, claimed_rows
		       FROM fact_work_items
		      WHERE stage = 'reducer' AND domain = '${wait_key}' AND status IN ('claimed', 'running');
		     IF holders = 1 AND waiters > 0 AND claimed > 0 THEN
		       RETURN holders || '|' || waiters || '|' || claimed || '|' || claimed_rows;
		     END IF;
		     EXIT WHEN clock_timestamp() >= deadline;
		     PERFORM pg_sleep(0.01);
		   END LOOP;
		   RETURN holders || '|' || waiters || '|' || claimed || '|' || coalesce(claimed_rows, '');
		 END
		 \$\$;
		 SELECT pg_temp.ifa_wait_for_generic_blocked_claim(${budget});" \
		"${compose_file}")"; then :; else
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
	if [[ "${holders}" != "1" || "${waiters}" -eq 0 || "${claimed}" -eq 0 ]]; then
		printf '%s: blocker did not engage \u2014 no backend waiting on shared_projection_intents lock (holders|waiters|claimed=%s|%s|%s)\n' \
			"${cell}" "${holders}" "${waiters}" "${claimed}" >&2
		return 1
	fi
	_ifa_fault_validate_blocked_claim_rows "${cell}" "${rows}" || return 1
	printf -v "${rows_var}" '%s' "${rows}"
	printf -v "${state_var}" '%s' "${holders}|${waiters}|${claimed}"
	printf '%s: non-vacuous: %s %s claim(s) parked behind the shared_projection_intents holder (%s waiter(s))\n' "${cell}" "${claimed}" "${wait_key}" "${waiters}"
}

# ifa_fault_assert_blocked_claims_still_held runs after SIGKILL and BEFORE
# the lock release: every killed row must still be claimed/running. A row
# that acked in the census-to-kill window was never interrupted, so the kill
# proved nothing about it; that dies here as a non-engaged blocker, not
# downstream as a failed recovery.
#
# Args: cell compose_project use_compose dsn compose_file wait_key killed_rows
ifa_fault_assert_blocked_claims_still_held() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" dsn="$4" compose_file_arg="$5" wait_key="$6" rows="$7"
	local entry digest values="" expected=0 raw state query_rc
	if [[ ! "${wait_key}" =~ ^[a-z0-9_]+$ ]]; then
		printf '%s: wait_key must match ^[a-z0-9_]+$, got %q\n' "${cell}" "${wait_key}" >&2
		return 1
	fi
	_ifa_fault_validate_blocked_claim_rows "${cell}" "${rows}" || return 1
	local -a entries
	IFS=',' read -r -a entries <<<"${rows}"
	for entry in "${entries[@]}"; do
		read -r digest _ _ _ <<<"${entry}"
		values+="${values:+,}('${digest}')"
		expected=$((expected + 1))
	done
	if raw="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${dsn}" \
		"/* generic shared_intent_lock post-kill hold recheck */ WITH killed(digest) AS (VALUES ${values}) SELECT count(*)::text || '|' || count(*) FILTER (WHERE w.status IN ('claimed', 'running'))::text FROM killed LEFT JOIN fact_work_items AS w ON md5(w.work_item_id) = killed.digest AND w.stage = 'reducer' AND w.domain = '${wait_key}';" \
		"${compose_file_arg}")"; then :; else
		query_rc=$?
		printf '%s: post-kill hold recheck query FAILED (exit %s); state is unknown\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	state="$(printf '%s\n' "${raw}" | tail -n 1 | tr -d '[:space:]')"
	if [[ "${state}" != "${expected}|${expected}" ]]; then
		printf '%s: blocker did not engage on %s row (acked before kill): still-held %s, want %s|%s (killed set: %s)\n' \
			"${cell}" "${wait_key}" "${state}" "${expected}" "${expected}" "${rows}" >&2
		return 1
	fi
	printf '%s: every killed claim (%s) still held after SIGKILL\n' "${cell}" "${expected}"
}

# ifa_fault_assert_killed_claims_reexecuted proves the replacement reducer
# re-executed every claim the kill interrupted: each killed work item now
# has a larger attempt_count AND a later last_attempt_at than it had at kill
# time. Both move in the same claim UPDATE when an expired claimed row is
# reclaimed, so a row that merely reappeared without a new attempt cannot
# satisfy both. Call right after the post-kill drain, before any maintenance
# pass can reopen the row and reset attempt_count (that reset is the live
# re-query hazard deployable_unit's own header documents). This REPLACES the
# old final-state count-vs-baseline poll for generic shared_intent_lock KILL
# cells: a per-exact-row monotonic check is strictly more specific than a
# domain count strictly exceeding a 0-or-1 baseline, so this is not the
# prohibited loosening.
#
# Args: cell compose_project use_compose dsn compose_file wait_key killed_rows
ifa_fault_assert_killed_claims_reexecuted() {
	local cell="$1" compose_project="$2" use_compose_arg="$3" dsn="$4" compose_file_arg="$5" wait_key="$6" rows="$7"
	local entry digest attempt epoch values="" expected=0 raw state query_rc
	if [[ ! "${wait_key}" =~ ^[a-z0-9_]+$ ]]; then
		printf '%s: wait_key must match ^[a-z0-9_]+$, got %q\n' "${cell}" "${wait_key}" >&2
		return 1
	fi
	_ifa_fault_validate_blocked_claim_rows "${cell}" "${rows}" || return 1
	local -a entries
	IFS=',' read -r -a entries <<<"${rows}"
	for entry in "${entries[@]}"; do
		read -r digest attempt epoch _ <<<"${entry}"
		values+="${values:+,}('${digest}',${attempt},${epoch})"
		expected=$((expected + 1))
	done
	if raw="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${dsn}" \
		"/* generic shared_intent_lock killed-claim re-execution */ WITH killed(digest, attempt_count, last_attempt_us) AS (VALUES ${values}) SELECT count(*)::text || '|' || count(*) FILTER (WHERE w.attempt_count > killed.attempt_count AND (extract(epoch FROM w.last_attempt_at) * 1000000)::bigint > killed.last_attempt_us)::text FROM killed LEFT JOIN fact_work_items AS w ON md5(w.work_item_id) = killed.digest AND w.stage = 'reducer' AND w.domain = '${wait_key}';" \
		"${compose_file_arg}")"; then :; else
		query_rc=$?
		printf '%s: killed-claim re-execution query FAILED (exit %s); state is unknown\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	state="$(printf '%s\n' "${raw}" | tail -n 1 | tr -d '[:space:]')"
	if [[ "${state}" != "${expected}|${expected}" ]]; then
		printf '%s: killed %s claims were not re-executed with a new attempt by the replacement: %s, want %s|%s (killed set: %s)\n' \
			"${cell}" "${wait_key}" "${state}" "${expected}" "${expected}" "${rows}" >&2
		return 1
	fi
	printf '%s: every killed claim (%s) was reclaimed and re-executed with a new attempt\n' "${cell}" "${expected}"
}

# _ifa_generic_write_kill_snapshot freezes the kill-window evidence to
# ${log_dir}/${cell}-kill-snapshot.txt at kill time: the census that
# authorized SIGKILL, the killed set it captured, and the live pg_locks rows
# on shared_projection_intents (the holder must still hold: this runs after
# the kill, before the release). The diagnostics completeness gate records
# this file, so a recovery asserted without kill-window evidence fails
# closed there instead of passing on a missing file.
#
# Args: cell blocker_kind namespace wait_key killed_rows census_state claimed_before
_ifa_generic_write_kill_snapshot() {
	local cell="$1" blocker_kind="$2" namespace="$3" wait_key="$4" killed_rows="$5" census_state="$6" claimed_before="$7"
	local snapshot locks
	if [[ ! "${cell}" =~ ^[a-z0-9_]+$ ]]; then
		printf '_ifa_generic_write_kill_snapshot: invalid cell %q\n' "${cell}" >&2
		return 1
	fi
	snapshot="${log_dir}/${cell}-kill-snapshot.txt"
	tmp_snapshot="${snapshot}.tmp"
	# Write-then-rename: a pg failure must not leave a partial snapshot that
	# a post-hoc reader could mistake for complete (#10955 review P3).
	rm -f "${tmp_snapshot}"
	{
		printf 'cell: %s\nblocker_kind: %s\nnamespace: %s\nwait_key: %s\n' "${cell}" "${blocker_kind}" "${namespace}" "${wait_key}"
		if [[ "${blocker_kind}" == "shared_intent_lock" ]]; then
			printf 'census: holders|waiters|claimed=%s\nkilled_rows: %s\n' "${census_state}" "${killed_rows}"
			printf 'pg_locks on shared_projection_intents at snapshot time:\n'
			ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
				"SELECT a.application_name || '|' || l.mode || '|' || CASE WHEN l.granted THEN 'granted' ELSE 'waiting' END || '|' || l.pid FROM pg_locks AS l JOIN pg_stat_activity AS a ON a.pid = l.pid WHERE l.relation = 'shared_projection_intents'::regclass ORDER BY 1;" \
				"${compose_file}" || { rm -f "${tmp_snapshot}"; return 1; }
		else
			printf 'claimed_before: %s\n' "${claimed_before}"
		fi
	} >"${tmp_snapshot}" || { rm -f "${tmp_snapshot}"; return 1; }
	[[ -s "${tmp_snapshot}" ]] || { printf '%s: kill snapshot %s is empty; state is unknown\n' "${cell}" "${snapshot}" >&2; rm -f "${tmp_snapshot}"; return 1; }
	mv "${tmp_snapshot}" "${snapshot}"
	printf '%s: kill-window snapshot written to %s\n' "${cell}" "${snapshot}"
}
