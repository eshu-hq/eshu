#!/usr/bin/env bash
# Runtime helpers for scripts/verify-graph-rebuild-from-facts.sh.

# wait_for_interrupt_point waits until a rebuild has produced graph output
# while work remains active, then records that sample in INTERRUPT_NODES and
# REMAINING. graph_scalar and queue_active_count are supplied by the caller.
# The bounded wait fails rather than claiming restart convergence from a kill
# that happened before projection began or after it already finished.
wait_for_interrupt_point() {
	local timeout_seconds="$1" attempt nodes work
	if [[ ! "${timeout_seconds}" =~ ^[1-9][0-9]*$ ]]; then
		echo "Interrupt wait timeout must be a positive integer (got ${timeout_seconds})." >&2
		return 1
	fi

	for ((attempt = 1; attempt <= timeout_seconds; attempt++)); do
		nodes="$(graph_scalar 'MATCH (n) RETURN count(n) AS c')"
		work="$(queue_active_count)"
		if [[ "${nodes}" =~ ^[0-9]+$ && "${work}" =~ ^[0-9]+$ ]] \
			&& ((nodes > 0 && work > 0)); then
			INTERRUPT_NODES="${nodes}"
			REMAINING="${work}"
			return 0
		fi
		sleep 1
	done

	echo "Timed out waiting for an in-progress rebuild checkpoint " \
		"(last sample: nodes=${nodes:-unknown}, active_work=${work:-unknown})." >&2
	return 1
}
