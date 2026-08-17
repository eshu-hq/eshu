#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154  # Exported constants and driver-owned globals
# are consumed across sourced fault-injection libraries.
# Shared helpers for scripts/verify-ifa-fault-injection.sh (issue #4580 P6
# slice S5, design doc docs/internal/design/4389-ifa-conformance-platform.md
# Layer 4, "deterministic fault injection"). Extends
# scripts/lib/ifa_determinism_common.sh (build/wait/pg/start_bg) with the
# fault-specific mechanics that script needs and no sibling Ifá verifier does:
# writing a faultreplay v1 fault-script JSON fixture
# (go/internal/replay/faultreplay/script.go) for the two fault kinds that DO
# have a live-Postgres/live-NornicDB seam (fail-graph-write-once-then-succeed
# and restart-backend-between-phase-groups, both wired only under the
# ifafaultinjection build tag into go/cmd/reducer), polling
# fact_work_items for a claimed/running row (the non-vacuity precondition for
# kill-worker-after-claim and expire-lease-mid-handler, neither of which has a
# live-Postgres WorkSource decorator -- see verify-ifa-fault-injection.sh's own
# header comment for why those two cells act directly on the process/row
# instead), and watching for the restart-backend-between-phase-groups sentinel
# file so the caller can restart the graph backend while the tagged reducer is
# deliberately blocked on it.
#
# This file is a plain function library, not a script: it deliberately does
# NOT set `set -euo pipefail` (sourcing it would rebind the caller's shell
# options). The calling script owns strict mode, its own log/die helpers, and
# its own exit trap. Every function here returns a non-zero status and prints
# its own diagnostic to stderr on failure; callers decide whether to `die`.

# ifa_fault_write_once_script writes a faultreplay v1 fault-script JSON fixture
# for exactly one fail-graph-write-once-then-succeed fault to path. lane MUST
# be "queue-retry" or "executor-retry" (faultreplay.LaneQueueRetry /
# LaneExecutorRetry); operation_match is the Cypher substring
# go/internal/storage/cypher.FaultingExecutor.onceMatches checks every
# Execute/ExecuteGroup/ExecutePhaseGroup call against (see that file's
# onceMatches). Substring, not a fixed statement_ordinal: an ordinal position
# is not stable across this gate's combined demo-org + synth-multiscope drive,
# whose exact call interleaving depends on every other reducer domain also in
# flight, but the CloudResource MERGE text itself is a fixed, grep-stable
# anchor regardless of when it fires.
ifa_fault_write_once_script() {
	local path="$1" operation_match="$2" lane="$3"
	cat >"${path}" <<-JSON
		{
		  "version": 1,
		  "faults": [
		    {
		      "kind": "fail-graph-write-once-then-succeed",
		      "trigger": {"operation_match": "${operation_match}"},
		      "target": {"lane": "${lane}"}
		    }
		  ]
		}
	JSON
}

# ifa_fault_write_restart_script writes a faultreplay v1 fault-script JSON
# fixture for exactly one restart-backend-between-phase-groups fault to path,
# firing after the after_phase_groups'th completed ExecuteGroup/
# ExecutePhaseGroup call (go/internal/storage/cypher.FaultingExecutor.
# maybeRestartAfterGroup). after_phase_groups=1 is deliberately the FIRST
# completed group, not some later one: any positive ordinal proves the same
# recovery mechanism (a real graph-backend restart between two committed
# groups), and firing on the first group removes any dependency on this run's
# specific domain-processing order or group count staying above whatever
# larger ordinal a later choice would need.
ifa_fault_write_restart_script() {
	local path="$1" after_phase_groups="$2"
	cat >"${path}" <<-JSON
		{
		  "version": 1,
		  "faults": [
		    {
		      "kind": "restart-backend-between-phase-groups",
		      "trigger": {"after_phase_groups": ${after_phase_groups}}
		    }
		  ]
		}
	JSON
}

# ifa_fault_wait_for_claimed polls fact_work_items until at least one reducer
# row reaches status claimed/running (the precondition kill-worker-after-claim
# and expire-lease-mid-handler both need before they act -- acting before any
# row is actually claimed would make the fault vacuous, matching this
# platform's "measured inert" lesson), printing the observed count to stdout on
# success. The polling loop runs inside Postgres on one connection: starting a
# fresh docker-exec + psql process per sample is slower than the reducer's
# millisecond-scale claimed window and can miss every real claim. Returns
# non-zero if the budget (in whole seconds) elapses first.
#
# An optional 6th arg scopes the wait to one fact_work_items.domain value
# (e.g. "sql_relationship_materialization") instead of "any reducer domain" --
# issue #5555's kill-worker-after-claim-sql cell needs this to prove the
# claimed/killed row was provably SQL-relationship work, not whichever domain
# (in practice gcp_resource_materialization) the driven cassettes happen to
# schedule first.
#
# Args: compose_project use_compose dsn compose_file [budget_seconds=60] [domain]
ifa_fault_wait_for_claimed() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local budget="${5:-60}" domain="${6:-}"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		echo "ifa_fault_wait_for_claimed: budget must be a positive integer, got ${budget}" >&2
		return 1
	fi
	# The domain lands inside a SQL string literal that is itself embedded in a
	# CREATE OR REPLACE FUNCTION body, so a value carrying a quote would break out
	# of two levels at once. Every caller passes a hardcoded constant today; this
	# validates anyway, because the next caller may not.
	local domain_clause=""
	if [[ -n "${domain}" ]]; then
		if [[ ! "${domain}" =~ ^[a-z0-9_]+$ ]]; then
			echo "ifa_fault_wait_for_claimed: domain must match ^[a-z0-9_]+$, got ${domain}" >&2
			return 1
		fi
		domain_clause=" AND domain = '${domain}'"
	fi
	local count
	count="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"CREATE OR REPLACE FUNCTION pg_temp.ifa_wait_for_claimed(wait_seconds integer)
		 RETURNS integer LANGUAGE plpgsql AS \$\$
		 DECLARE
		   observed integer;
		   deadline timestamptz := clock_timestamp() + make_interval(secs => wait_seconds);
		 BEGIN
		   LOOP
		     SELECT count(*) INTO observed
		       FROM fact_work_items
		      WHERE stage = 'reducer' AND status IN ('claimed', 'running')${domain_clause};
		     IF observed > 0 THEN
		       RETURN observed;
		     END IF;
		     EXIT WHEN clock_timestamp() >= deadline;
		     PERFORM pg_sleep(0.001);
		   END LOOP;
		   RETURN 0;
		 END
		 \$\$;
		 SELECT pg_temp.ifa_wait_for_claimed(${budget});" \
		"${compose_file}" | tail -n 1 | tr -d '[:space:]')"
	if [[ -n "${count}" && "${count}" -gt 0 ]]; then
		printf '%s' "${count}"
		return 0
	fi
	if [[ -n "${domain}" ]]; then
		echo "ifa_fault_wait_for_claimed: no claimed/running fact_work_items row for domain=${domain} appeared within ${budget}s" >&2
	else
		echo "ifa_fault_wait_for_claimed: no claimed/running fact_work_items row appeared within ${budget}s" >&2
	fi
	return 1
}

# ifa_fault_require_fresh_domain_intents fails closed unless projection_domain
# has exactly zero durable shared intents. Call only after fresh_stack when the
# verifier owns the Compose lifecycle; --no-compose state belongs to its caller.
#
# Args: cell projection_domain compose_project use_compose dsn compose_file
ifa_fault_require_fresh_domain_intents() {
	local cell="$1" domain="$2" compose_project="$3" use_compose_arg="$4"
	local postgres_dsn="$5" compose_file_arg="$6"
	local count count_rc
	if [[ ! "${domain}" =~ ^[a-z0-9_]+$ ]]; then
		printf '%s: projection domain must match ^[a-z0-9_]+$, got %q\n' "${cell}" "${domain}" >&2
		return 1
	fi
	if count="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${postgres_dsn}" \
		"SELECT count(*) FROM shared_projection_intents WHERE projection_domain = '${domain}';" \
		"${compose_file_arg}")"; then
		count_rc=0
	else
		count_rc=$?
	fi
	if [[ "${count_rc}" -ne 0 ]]; then
		printf '%s: fresh-stack precondition query FAILED (exit %s)\n' "${cell}" "${count_rc}" >&2
		return "${count_rc}"
	fi
	count="$(printf '%s' "${count}" | tr -d '[:space:]')"
	if [[ -z "${count}" ]]; then
		printf '%s: fresh-stack precondition query returned empty output; treat that as unknown, not as zero\n' "${cell}" >&2
		return 1
	fi
	if [[ ! "${count}" =~ ^[0-9]+$ ]]; then
		printf '%s: fresh-stack precondition query returned non-numeric output %q; treat that as unknown, not as zero\n' \
			"${cell}" "${count}" >&2
		return 1
	fi
	if [[ "${count}" != "0" ]]; then
		printf '%s: %s %s intent row(s) survived fresh_stack\n' "${cell}" "${count}" "${domain}" >&2
		return 1
	fi
}

# ifa_fault_start_shared_intent_lock holds the first shared-intent write so a
# short domain handler cannot acknowledge between the claimed-row observation
# and kill. namespace and cell are validated before interpolation into SQL.
#
# Args: cell namespace pid_var
ifa_fault_start_shared_intent_lock() {
	local cell="$1" namespace="$2" pid_var="$3"
	if [[ ! "${cell}" =~ ^[a-z0-9_]+$ || ! "${namespace}" =~ ^[a-z0-9_]+$ ]]; then
		printf 'ifa_fault_start_shared_intent_lock: cell and namespace must match ^[a-z0-9_]+$\n' >&2
		return 1
	fi
	local app_name="ifa_${namespace}_lock_${cell}"
	local log_namespace="${namespace//_/-}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE shared_projection_intents IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/${log_namespace}-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/${log_namespace}-lock-${cell}.log" 2>&1 &
	fi
	local holder_pid=$!
	bg_pids+=("${holder_pid}")
	printf -v "${pid_var}" '%s' "${holder_pid}"

	local i lock_count
	for i in $(seq 1 60); do
		lock_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE a.application_name = '${app_name}' AND l.relation = 'shared_projection_intents'::regclass AND l.mode = 'AccessExclusiveLock' AND l.granted;" \
			"${compose_file}" | tr -d '[:space:]')"
		if [[ "${lock_count}" == "1" ]]; then
			return 0
		fi
		sleep 0.25
	done
	return 1
}

# ifa_fault_release_shared_intent_lock terminates the named backend, joins the
# local psql/docker process, and removes its PID from cleanup ownership.
#
# Args: cell namespace holder_pid
ifa_fault_release_shared_intent_lock() {
	local cell="$1" namespace="$2" holder_pid="$3"
	if [[ ! "${cell}" =~ ^[a-z0-9_]+$ || ! "${namespace}" =~ ^[a-z0-9_]+$ ]]; then
		printf 'ifa_fault_release_shared_intent_lock: cell and namespace must match ^[a-z0-9_]+$\n' >&2
		return 1
	fi
	local app_name="ifa_${namespace}_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_fault_count_retried prints the number of succeeded fact_work_items
# reducer rows with attempt_count > 1 for domain (default
# gcp_resource_materialization) -- the durable fingerprint of a write intent
# that failed a first attempt, re-queued, and succeeded on re-claim.
# graph_write_timeout is a counting failure class (not a non-counting
# readiness class), so the re-claim bumps attempt_count, and Ack preserves it
# on success, so this survives recovery and is committed the instant the
# retry path runs. Used to snapshot the fault-free baseline (cell 1) and, via
# ifa_fault_assert_retried_above, to prove the fault ADDED a retry the
# baseline lacked. This replaced a log grep that raced the reducer's stderr
# flush (the injected-failure line reached the captured file a minute-plus
# after the event in CI, so a bounded log poll flaked). This mechanism only
# applies to domains a fact_work_items handler writes graph edges for
# directly inside Handle() (e.g. gcp_resource_materialization); it does NOT
# apply to sql_relationship_materialization, whose graph writes ride the
# async shared-projection intent path instead -- see
# ifa_fault_assert_once_fault_marker below for that domain's fired-fault
# signal.
#
# Args: compose_project use_compose dsn compose_file [domain=gcp_resource_materialization]
ifa_fault_count_retried() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local domain="${5:-gcp_resource_materialization}"
	# Same guard as ifa_fault_wait_for_claimed: domain is interpolated into a SQL
	# literal below, so reject anything that could close the quote.
	if [[ ! "${domain}" =~ ^[a-z0-9_]+$ ]]; then
		echo "ifa_fault_count_retried: domain must match ^[a-z0-9_]+$, got ${domain}" >&2
		return 1
	fi
	ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*) FROM fact_work_items WHERE stage = 'reducer' AND status = 'succeeded' AND attempt_count > 1 AND domain = '${domain}';" \
		"${compose_file}" | tr -d '[:space:]'
}

# ifa_fault_assert_retried_above proves the queue-retry fault genuinely fired by
# asserting the retried-intent count for domain STRICTLY EXCEEDS the fault-free
# baseline captured in cell 1. Requiring a strict increase over baseline -- rather
# than merely count > 0 -- makes the signal specific to the injected fault: a
# natural counting-class retry (a real NornicDB deadlock / TransactionExecutionLimit,
# or a transient EntityNotFound under the concurrent projector+reducer this gate
# runs) would also appear in the identical fault-free baseline drive, so it cannot
# green the check on its own while the decorator sits silently inert (the
# "measured inert" hazard). Polls to absorb read latency; returns non-zero (die)
# if the count never exceeds baseline within the budget.
#
# Args: compose_project use_compose dsn compose_file baseline [budget_seconds=15] [domain=gcp_resource_materialization]
ifa_fault_assert_retried_above() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local baseline="$5" budget="${6:-15}" domain="${7:-gcp_resource_materialization}"
	local i count
	for i in $(seq 1 "${budget}"); do
		count="$(ifa_fault_count_retried "${compose_project}" "${use_compose}" "${dsn}" "${compose_file}" "${domain}")"
		if [[ -n "${count}" && "${count}" -gt "${baseline}" ]]; then
			printf '%s' "${count}"
			return 0
		fi
		sleep 1
	done
	return 1
}

# IFA_ONCE_MARKER_WRITE_FAILED_PREFIX must stay byte-identical to
# OnceFiredMarkerWriteFailedPrefix in
# go/internal/storage/cypher/fault_executor_marker.go. The gate greps the
# reducer log for it, so drift makes the search silently find nothing --
# indistinguishable from "the marker write never failed". The mirror asserts the
# two match.
IFA_ONCE_MARKER_WRITE_FAILED_PREFIX="ifa fault: once-fired marker write failed"

# ifa_fault_assert_once_fault_marker proves the scripted once-fault fired by
# reading the marker FaultingExecutor writes at the moment it injects the fault
# (go/internal/storage/cypher/fault_executor.go, #5974), rather than polling the
# reducer's captured stderr.
#
# Why not the log: the stderr route races the logger's flush. An earlier
# log-grep assertion in this file was abandoned after the injected-failure line
# reached the captured file a minute-plus after the event in CI -- see
# ifa_fault_count_retried's comment above. cell_failgraphwrite_sql then reused
# that technique and went inert in CI for the same reason. The marker is written
# synchronously by the goroutine injecting the fault, before the failing write
# returns, so it cannot arrive late relative to the fault's own effects.
#
# A missing marker is a hard failure: it means the fault never fired, which is
# exactly the inert-gate condition this assertion exists to catch.
#
# Args: fault_script_path expected_operation_substring
ifa_fault_assert_once_fault_marker() {
	local script_path="$1" expected="$2"
	local marker="${script_path}.restart-sentinel.once-fired"
	if [[ ! -f "${marker}" ]]; then
		echo "ifa_fault_assert_once_fault_marker: no once-fired marker at ${marker} -- the scripted fault never fired. An inert script, not a pass." >&2
		return 1  # 1 = absent; 2 = present but wrong operation. Callers must not conflate them.
	fi
	# Bash substring match, NOT an external tool. This used `rg`, which is not
	# installed on the fault-injection CI runner: it exited "command not found",
	# the non-zero status read as "the marker does not name the operation", and a
	# fault that had fired perfectly was reported as never firing. That single
	# missing binary is the whole of #5974 -- weeks of a working gate reported as
	# broken, because a checker's absence is indistinguishable from a negative
	# result when you only look at the exit code.
	local contents
	contents="$(cat "${marker}")" || {
		echo "ifa_fault_assert_once_fault_marker: could not read ${marker}; treat this as unknown, not as a verdict about the fault" >&2
		return 1
	}
	if [[ "${contents}" != *"${expected}"* ]]; then
		echo "ifa_fault_assert_once_fault_marker: marker ${marker} exists but does not name the targeted operation ${expected}; the fault fired on a different write" >&2
		echo "--- marker contents ---" >&2
		printf '%s\n' "${contents}" >&2
		return 2
	fi
	return 0
}


# ifa_fault_watch_restart_sentinel polls for sentinel_path's appearance (the
# file go/internal/storage/cypher.FaultingExecutor.maybeRestartAfterGroup
# writes and then blocks on, waiting for its removal), and once seen:
# restarts the compose project's nornicdb service (a plain `restart`, not
# `down -v` -- this is a backend outage, not a fresh database, mirroring the
# proven T3 recovery mechanism), waits for both backends to report ready
# again, then removes the sentinel to release the blocked reducer call.
# Writes "fired" or "not-fired" to result_file so the caller (who normally
# backgrounds this function with `&` alongside the golden-corpus-gate drain
# poll) can assert non-vacuity after the drain completes. Intended to be
# called as `ifa_fault_watch_restart_sentinel ... & bg_pids+=($!)`, not
# awaited synchronously, since the tagged reducer stays blocked on the
# sentinel exactly while this function's caller is also waiting on the drain
# gate.
#
# Args: sentinel_path compose_project compose_file result_file [budget_seconds=90]
ifa_fault_watch_restart_sentinel() {
	local sentinel_path="$1" compose_project="$2" compose_file="$3" result_file="$4"
	local budget="${5:-90}"
	local ticks=$((budget * 5))
	local i
	rm -f "${result_file}"
	for i in $(seq 1 "${ticks}"); do
		if [[ -f "${sentinel_path}" ]]; then
			printf 'fired\n' >"${result_file}"
			printf 'ifa_fault_watch_restart_sentinel: sentinel seen (%s); restarting nornicdb\n' "${sentinel_path}" >&2
			docker compose -p "${compose_project}" -f "${compose_file}" restart nornicdb >/dev/null 2>&1
			ifa_det_wait_for_backends "${compose_project}" "${compose_file}" >/dev/null 2>&1 \
				|| echo "ifa_fault_watch_restart_sentinel: backends did not report ready after restart" >&2
			rm -f "${sentinel_path}"
			return 0
		fi
		sleep 0.2
	done
	printf 'not-fired\n' >"${result_file}"
	echo "ifa_fault_watch_restart_sentinel: sentinel ${sentinel_path} never appeared within ${budget}s" >&2
	return 1
}

# ifa_fault_dead_letter_count returns the current durable dead_letter row
# count for fact_work_items. Every fault-injection cell except the (not built
# in this slice -- see verify-ifa-fault-injection.sh's header) fail-terminal
# leg expects this to be 0 after a completed drain: recovery converging means
# no scripted fault here ever produces a durable dead letter.
#
# Args: compose_project use_compose dsn compose_file
ifa_fault_dead_letter_count() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*) FROM fact_work_items WHERE status = 'dead_letter';" \
		"${compose_file}" | tr -d '[:space:]'
}

# ifa_fault_redeliver_succeeded forces every succeeded reducer work item back
# to a claimable pending state and prints how many rows it reset. This is the
# duplicate-delivery fault (#5544 cell 6): a queue redelivering an item whose
# handler already committed, which is the ordinary at-least-once case rather
# than an exotic one -- a lease that expired after the write landed but before
# the ack did produces exactly this.
#
# Resetting status alone is not enough to make a row claimable again. The claim
# path filters on visible_at and takes the lease through lease_owner/
# claim_until (see 005_fact_work_items.sql and its status/visible_at index), so
# a row left with a stale lease owner or a future visible_at would sit
# untouched and the caller's "redelivered" count would be a lie about work that
# never re-ran. All four are cleared together.
#
# The count comes back from the UPDATE itself via a CTE rather than a separate
# SELECT, so it cannot drift from what was actually written by a concurrent
# claim landing between the two statements.
#
# Callers MUST assert the printed count is > 0. A redelivery that matched no
# rows makes the follow-up drain a no-op, and every downstream digest
# comparison then passes vacuously.
#
# Args: compose_project use_compose dsn compose_file [domain=<all reducer domains>]
ifa_fault_redeliver_succeeded() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local domain="${5:-}"
	local domain_clause=""
	if [[ -n "${domain}" ]]; then
		# Same guard as ifa_fault_wait_for_claimed and ifa_fault_count_retried:
		# domain is interpolated into a SQL literal below.
		if [[ ! "${domain}" =~ ^[a-z0-9_]+$ ]]; then
			echo "ifa_fault_redeliver_succeeded: domain must match ^[a-z0-9_]+$, got '${domain}'" >&2
			return 1
		fi
		domain_clause=" AND domain = '${domain}'"
	fi
	ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"WITH redelivered AS (
		   UPDATE fact_work_items
		      SET status = 'pending',
		          lease_owner = NULL,
		          claim_until = NULL,
		          visible_at = now(),
		          updated_at = now()
		    WHERE stage = 'reducer'
		      AND status = 'succeeded'${domain_clause}
		RETURNING 1
		 )
		 SELECT count(*) FROM redelivered;" \
		"${compose_file}" | tr -d '[:space:]'
}
