#!/usr/bin/env bash
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
# Args: compose_project use_compose dsn compose_file [budget_seconds=60]
ifa_fault_wait_for_claimed() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local budget="${5:-60}"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		echo "ifa_fault_wait_for_claimed: budget must be a positive integer, got ${budget}" >&2
		return 1
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
		      WHERE stage = 'reducer' AND status IN ('claimed', 'running');
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
	echo "ifa_fault_wait_for_claimed: no claimed/running fact_work_items row appeared within ${budget}s" >&2
	return 1
}

# ifa_fault_count_retried prints the number of succeeded
# gcp_resource_materialization reducer rows with attempt_count > 1 -- the durable
# fingerprint of a CloudResource-write intent that failed a first attempt,
# re-queued, and succeeded on re-claim. graph_write_timeout is a counting failure
# class (not a non-counting readiness class), so the re-claim bumps attempt_count,
# and Ack preserves it on success, so this survives recovery and is committed the
# instant the retry path runs. Used to snapshot the fault-free baseline (cell 1)
# and, via ifa_fault_assert_retried_above, to prove the fault ADDED a retry the
# baseline lacked. This replaced a log grep that raced the reducer's stderr flush
# (the injected-failure line reached the captured file a minute-plus after the
# event in CI, so a bounded log poll flaked).
ifa_fault_count_retried() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*) FROM fact_work_items WHERE stage = 'reducer' AND status = 'succeeded' AND attempt_count > 1 AND domain = 'gcp_resource_materialization';" \
		"${compose_file}" | tr -d '[:space:]'
}

# ifa_fault_assert_retried_above proves the queue-retry fault genuinely fired by
# asserting the retried-CloudResource-intent count STRICTLY EXCEEDS the fault-free
# baseline captured in cell 1. Requiring a strict increase over baseline -- rather
# than merely count > 0 -- makes the signal specific to the injected fault: a
# natural counting-class retry (a real NornicDB deadlock / TransactionExecutionLimit,
# or a transient EntityNotFound under the concurrent projector+reducer this gate
# runs) would also appear in the identical fault-free baseline drive, so it cannot
# green the check on its own while the decorator sits silently inert (the
# "measured inert" hazard). Polls to absorb read latency; returns non-zero (die)
# if the count never exceeds baseline within the budget.
ifa_fault_assert_retried_above() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local baseline="$5" budget="${6:-15}"
	local i count
	for i in $(seq 1 "${budget}"); do
		count="$(ifa_fault_count_retried "${compose_project}" "${use_compose}" "${dsn}" "${compose_file}")"
		if [[ -n "${count}" && "${count}" -gt "${baseline}" ]]; then
			printf '%s' "${count}"
			return 0
		fi
		sleep 1
	done
	return 1
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
