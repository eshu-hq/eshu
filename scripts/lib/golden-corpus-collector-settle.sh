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
# GATE_MIN_COLLECTOR_SOURCES, GATE_EXPECTED_TOTAL_SCOPES,
# GATE_COLLECTOR_SETTLE_SECONDS, GATE_COLLECTOR_SETTLE_POLL_SECONDS, and the
# pg() and die() functions. Bodies resolve these lazily at call time, so
# sourcing this file before those globals are set is safe as long as they
# exist by the time wait_for_collector_settle actually runs.

# wait_for_collector_settle polls Postgres until every credentialed collector's
# cassette has fully replayed, or until GATE_COLLECTOR_SETTLE_SECONDS elapses —
# whichever comes first. It replaces a fixed
# `sleep "${GATE_COLLECTOR_SETTLE_SECONDS}"`: a fixed sleep sized for an idle
# machine has zero margin once host load or Docker I/O contention slows down
# fact commit (observed failing under host load ~6-13 with 36 concurrent
# containers: only a fraction of the credentialed collector sources had landed
# facts by the time the fixed sleep gave up).
#
# The stop condition requires BOTH:
#   - collector_sources >= GATE_MIN_COLLECTOR_SOURCES (derived from
#     len(collector_specs), never restated here as a literal — see that
#     array's own comment for why a hand-maintained count drifts): every
#     credentialed collector landed at least one scope.
#   - landed_scopes >= GATE_EXPECTED_TOTAL_SCOPES (the sum of every cassette's
#     scope count, computed by the orchestrator via jq): every scope of every
#     cassette actually landed, not just the first one per collector.
#
# The second condition is load-bearing on its own, not a nice-to-have: a
# cassette.Source.Next call (go/internal/replay/cassette/source.go) emits one
# scope per call, and collector.Service's Run loop (go/internal/collector/
# service.go) commits every scope of a cassette back-to-back with no sleep
# between them -- only the drained, empty-batch case waits for the poll
# interval. A cassette carries 1-6 scopes. The distinct-source count alone
# reaches GATE_MIN_COLLECTOR_SOURCES the moment every collector has committed
# its FIRST scope, which can happen well before any of them have committed
# their remaining scopes, especially under the host load this fix exists to
# tolerate (slower commits stretch the gap between successive scopes within
# one collector). Breaking on the distinct-source count alone and then killing
# every collector pid would truncate the corpus for whichever collectors were
# still mid-replay -- silently, since the gate would still report success. That
# is a worse failure than the fixed sleep this lib replaced: the old sleep was
# too short and failed LOUDLY (the count came up short and the gate went red);
# a premature break here would pass GREEN against less data than the rest of
# the gate (B-7 graph/query truth) is supposed to assert on.
#
# On success it prints the observed settle duration — the margin data that made
# the original bug invisible before, since nobody could see how close to the
# fixed 20s edge a "normal" run actually ran. On timeout it dies (non-zero
# exit) reporting both counts actually reached, both thresholds, and how long
# it waited; it does NOT raise either threshold, soften the assertion, or
# retry past the deadline quietly.
wait_for_collector_settle() {
	local settle_start settle_elapsed i pid
	local remaining_seconds sleep_seconds
	local settle_probe_line raw_collector_sources raw_landed_scopes
	local collector_sources landed_scopes
	settle_start="$(date +%s)"
	collector_sources=0
	landed_scopes=0
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

		# One combined query for both counts (fewer round-trips than two
		# separate probes every poll tick). A bare `settle_probe_line="$(pg
		# ...)"` assignment aborts the WHOLE gate under this script's
		# `set -euo pipefail` the instant a transient docker-exec/psql hiccup
		# makes pg() exit non-zero -- before the non-numeric fallback below or
		# the deadline ever get a chance to run. The `|| settle_probe_line=""`
		# on the same line is what actually protects it: an assignment tested
		# on the &&/|| side of a command is exempt from set -e, a bare one is
		# not (https://mywiki.wooledge.org/BashFAQ/105; the same class of bug
		# is documented in golden-corpus-local-backend.sh's header for its own
		# jq call). Polling queries Postgres far more often than a one-shot
		# check would, so both this guard and the numeric hardening below
		# matter more here than they would for a single check.
		settle_probe_line="$(pg "
SELECT
  (SELECT count(DISTINCT source_system) FROM ingestion_scopes WHERE source_system <> 'git')
  || ' ' ||
  (SELECT count(*) FROM ingestion_scopes WHERE source_system <> 'git');
")" || settle_probe_line=""
		read -r raw_collector_sources raw_landed_scopes <<<"${settle_probe_line}"
		if [[ "${raw_collector_sources}" =~ ^[0-9]+$ ]]; then
			collector_sources="${raw_collector_sources}"
		else
			collector_sources=0
		fi
		if [[ "${raw_landed_scopes}" =~ ^[0-9]+$ ]]; then
			landed_scopes="${raw_landed_scopes}"
		else
			landed_scopes=0
		fi
		if (( collector_sources >= GATE_MIN_COLLECTOR_SOURCES && landed_scopes >= GATE_EXPECTED_TOTAL_SCOPES )); then
			break
		fi

		settle_elapsed=$(( $(date +%s) - settle_start ))
		if (( settle_elapsed >= GATE_COLLECTOR_SETTLE_SECONDS )); then
			die "collector settle poll timed out after ${settle_elapsed}s (deadline ${GATE_COLLECTOR_SETTLE_SECONDS}s): ${collector_sources} credentialed collector source(s) landed facts (want >= ${GATE_MIN_COLLECTOR_SOURCES}), ${landed_scopes} total scopes landed (want >= ${GATE_EXPECTED_TOTAL_SCOPES}) (cassette replay did not fully commit)"
		fi
		# Sleep min(poll interval, remaining time), never the full interval
		# unconditionally: sleeping the full GATE_COLLECTOR_SETTLE_POLL_SECONDS
		# regardless of how much deadline is left let the loop overshoot
		# GATE_COLLECTOR_SETTLE_SECONDS by up to one interval, which made the
		# "timed out after Ns (deadline Ns)" message inaccurate in exactly the
		# way the fixed sleep it replaced was inaccurate. remaining_seconds is
		# always >= 1 here since the die() above already handled >= deadline.
		remaining_seconds=$(( GATE_COLLECTOR_SETTLE_SECONDS - settle_elapsed ))
		sleep_seconds="${GATE_COLLECTOR_SETTLE_POLL_SECONDS}"
		if (( remaining_seconds < sleep_seconds )); then
			sleep_seconds="${remaining_seconds}"
		fi
		sleep "${sleep_seconds}"
	done
	settle_elapsed=$(( $(date +%s) - settle_start ))
	for pid in "${collector_pids[@]}"; do kill "${pid}" >/dev/null 2>&1 || true; done
	printf 'cassette facts settled in %ss: %s credentialed collector sources (want >= %s), %s total scopes landed (want >= %s)\n' \
		"${settle_elapsed}" "${collector_sources}" "${GATE_MIN_COLLECTOR_SOURCES}" \
		"${landed_scopes}" "${GATE_EXPECTED_TOTAL_SCOPES}"
}
