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
# GATE_EXPECTED_SCOPE_PAIRS (array of "scope_id<TAB>generation_id", filled by
# golden-corpus-cassette-replay.sh), GATE_COLLECTOR_SETTLE_SECONDS,
# GATE_COLLECTOR_SETTLE_POLL_SECONDS, and the pg() and die() functions. Bodies
# resolve these lazily at call time, so sourcing this file before those globals
# are set is safe as long as they exist by the time wait_for_collector_settle
# actually runs.

# wait_for_collector_settle polls Postgres until every (scope_id,
# generation_id) pair the gate launched has committed, or until
# GATE_COLLECTOR_SETTLE_SECONDS elapses, whichever comes first. The fixed
# `sleep "${GATE_COLLECTOR_SETTLE_SECONDS}"` it replaced had zero margin under
# host load (only a fraction of the collector sources had landed facts when it
# gave up).
#
# The stop condition is a subset assertion: every launched pair must be present
# in scope_generations, and rows the gate did not launch are ignored. It carries
# no hand-derived number. Its two count-based predecessors (distinct sources >=
# N, then also generations >= M) were each satisfiable with a collector still
# committing. Bootstrap migration 115 seeds an eshu:global scope and generation
# on every run, so the population is always at least one larger than the
# launched set, and the kill below then landed inside the in-flight collector's
# commit transaction and rolled it back (#6965). A kill that lands before the
# transaction opens leaves no log line at all, so the loss was silent.
#
# Killing the collectors after the break is safe once the assertion holds:
# every collector's last pair is committed, and a cassette collector that has
# drained its scopes is parked in its poll wait, so the kill cancels a sleep.
#
# On success it prints the observed settle duration and the launched-pair
# count. On timeout it dies (non-zero exit) naming each missing pair and how
# long it waited. It never softens the assertion or retries past the deadline.
wait_for_collector_settle() {
	local settle_start settle_elapsed i pid pair scope_id generation_id
	local remaining_seconds sleep_seconds launched_values launched_total sql_quote
	local settle_probe_line raw_present raw_total raw_missing
	local present_pairs probed_total missing_pairs
	settle_start="$(date +%s)"
	launched_total="${#GATE_EXPECTED_SCOPE_PAIRS[@]}"
	# Zero launched pairs would make the subset assertion vacuously true.
	(( launched_total > 0 )) || die "collector settle has no launched (scope_id, generation_id) pairs to wait for"
	launched_values=""
	sql_quote="'"
	for pair in "${GATE_EXPECTED_SCOPE_PAIRS[@]}"; do
		IFS=$'\t' read -r scope_id generation_id <<<"${pair}"
		[[ -n "${scope_id}" && -n "${generation_id}" ]] ||
			die "collector settle launched pair is malformed: '${pair}'"
		if [[ -n "${launched_values}" ]]; then
			launched_values+=$',\n'
		fi
		# SQL literal quoting: double every single quote.
		launched_values+="  ('${scope_id//${sql_quote}/${sql_quote}${sql_quote}}', '${generation_id//${sql_quote}/${sql_quote}${sql_quote}}')"
	done
	present_pairs=0
	probed_total=0
	missing_pairs=""
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

		# One row: "<present> <launched> <missing,...>". The launched count is
		# echoed back so a probe that joined a different row set than the one
		# launched (a duplicated VALUES row covering for a missing pair) cannot
		# read as settled; a short answer already fails present == launched. A
		# non-numeric answer reads as nothing landed. A bare `settle_probe_line="$(pg ...)"`
		# assignment aborts the WHOLE gate under this script's `set -euo
		# pipefail` the instant a transient docker-exec/psql hiccup makes pg()
		# exit non-zero. The `|| settle_probe_line=""` on the same line is what
		# protects it: an assignment tested on the &&/|| side of a command is
		# exempt from set -e, a bare one is not
		# (https://mywiki.wooledge.org/BashFAQ/105).
		settle_probe_line="$(pg "
SELECT count(landed.generation_id)
  || ' ' || count(*)
  || ' ' || coalesce(string_agg(
       CASE WHEN landed.generation_id IS NULL
            THEN launched.scope_id || '/' || launched.generation_id END,
       ',' ORDER BY launched.scope_id, launched.generation_id), '')
  FROM (VALUES
${launched_values}
  ) AS launched(scope_id, generation_id)
  LEFT JOIN scope_generations AS landed
    ON landed.scope_id = launched.scope_id
   AND landed.generation_id = launched.generation_id
   AND landed.status IN ('pending', 'active', 'completed', 'superseded');
")" || settle_probe_line=""
		raw_present="" raw_total="" raw_missing=""
		read -r raw_present raw_total raw_missing <<<"${settle_probe_line}" || true
		if [[ "${raw_present}" =~ ^[0-9]+$ && "${raw_total}" =~ ^[0-9]+$ ]]; then
			present_pairs="${raw_present}"
			probed_total="${raw_total}"
			missing_pairs="${raw_missing}"
		else
			present_pairs=0
			probed_total=0
			missing_pairs="(probe returned no usable answer)"
		fi
		if (( probed_total == launched_total && present_pairs == launched_total )); then
			break
		fi

		settle_elapsed=$(( $(date +%s) - settle_start ))
		if (( settle_elapsed >= GATE_COLLECTOR_SETTLE_SECONDS )); then
			die "collector settle poll timed out after ${settle_elapsed}s (deadline ${GATE_COLLECTOR_SETTLE_SECONDS}s): ${present_pairs} of ${launched_total} launched scope generations landed; missing: ${missing_pairs:-(none reported)} (cassette replay did not fully commit)"
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
	printf 'cassette facts settled in %ss: all %s launched scope generations landed\n' \
		"${settle_elapsed}" "${launched_total}"
}
