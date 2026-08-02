#!/usr/bin/env bash
#
# golden-corpus-collector-settle.sh — polls for cassette-collector fact commit
# after collectors are launched (scripts/verify-golden-corpus-gate.sh). Extracted
# into a lib chunk so the orchestrator stays under the 500-line cap, and so the
# poll logic can be exercised directly by scripts/test-verify-golden-corpus-gate.sh
# and by a targeted local repro without a live Docker stack.
#
# Requires (set by the caller before wait_for_collector_settle is invoked):
# collector_pids (array), collector_names (array), log_dir,
# GATE_MIN_COLLECTOR_SOURCES, GATE_COLLECTOR_SETTLE_SECONDS,
# GATE_COLLECTOR_SETTLE_POLL_SECONDS, and the pg() and die() functions. Bodies
# resolve these lazily at call time, so sourcing this file before those globals
# are set is safe as long as they exist by the time wait_for_collector_settle
# actually runs.

# wait_for_collector_settle polls the landed credentialed-collector-source count
# in Postgres until it reaches GATE_MIN_COLLECTOR_SOURCES, or until
# GATE_COLLECTOR_SETTLE_SECONDS elapses — whichever comes first. It replaces a
# fixed `sleep "${GATE_COLLECTOR_SETTLE_SECONDS}"`: a fixed sleep sized for an
# idle machine has zero margin once host load or Docker I/O contention slows
# down fact commit (observed failing under host load ~6-13 with 36 concurrent
# containers: "only 5 credentialed collector source(s) landed facts; want >=
# 18"). Polling returns as soon as the threshold is met, so the common case gets
# FASTER, while the deadline still bounds the wait so a genuinely broken
# cassette replay fails loudly instead of hanging, or worse, passing anyway.
#
# On success it prints the observed settle duration — the margin data that made
# this bug invisible before, since nobody could see how close to the fixed 20s
# edge a "normal" run actually ran. On timeout it dies (non-zero exit) reporting
# the actual count reached, the threshold, and how long it waited; it does NOT
# raise the threshold, soften the assertion, or retry past the deadline quietly.
wait_for_collector_settle() {
	local settle_start settle_elapsed raw_collector_sources i pid
	settle_start="$(date +%s)"
	collector_sources=0
	while true; do
		# A collector that crashed on startup (cassette parse, Postgres connect)
		# exited during the settle. Catch that before killing, so a
		# silently-dead collector does not let the gate pass with the cassette
		# half of the pipeline unverified.
		for i in "${!collector_pids[@]}"; do
			if ! kill -0 "${collector_pids[$i]}" >/dev/null 2>&1; then
				tail -20 "${log_dir}/${collector_names[$i]}.log" >&2 || true
				die "collector ${collector_names[$i]} exited during settle (did not stay up to commit)"
			fi
		done

		# A transient docker-exec/psql hiccup can return an empty string or an
		# error line instead of a count; treat anything that is not a plain
		# integer as "not yet landed" rather than let it corrupt the
		# arithmetic comparison below and abort the poll on a false syntax
		# error. Polling queries Postgres far more often than the old one-shot
		# check did, so this hardening matters more here than it did before.
		raw_collector_sources="$(pg "SELECT count(DISTINCT source_system) FROM ingestion_scopes WHERE source_system <> 'git';" | tr -d '[:space:]')"
		if [[ "${raw_collector_sources}" =~ ^[0-9]+$ ]]; then
			collector_sources="${raw_collector_sources}"
		else
			collector_sources=0
		fi
		if (( collector_sources >= GATE_MIN_COLLECTOR_SOURCES )); then
			break
		fi

		settle_elapsed=$(( $(date +%s) - settle_start ))
		if (( settle_elapsed >= GATE_COLLECTOR_SETTLE_SECONDS )); then
			die "collector settle poll timed out after ${settle_elapsed}s (deadline ${GATE_COLLECTOR_SETTLE_SECONDS}s): only ${collector_sources} credentialed collector source(s) landed facts; want >= ${GATE_MIN_COLLECTOR_SOURCES} (cassette replay did not commit)"
		fi
		sleep "${GATE_COLLECTOR_SETTLE_POLL_SECONDS}"
	done
	settle_elapsed=$(( $(date +%s) - settle_start ))
	for pid in "${collector_pids[@]}"; do kill "${pid}" >/dev/null 2>&1 || true; done
	printf 'cassette facts settled in %ss: %s credentialed collector sources (want >= %s)\n' \
		"${settle_elapsed}" "${collector_sources}" "${GATE_MIN_COLLECTOR_SOURCES}"
}
