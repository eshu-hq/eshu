#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Trust-chain stage commit wait for the grants_secret_read fault cells
# (#6228 remainder + #8539 seam). Split out of
# scripts/lib/ifa_fault_injection_grants_secret_read_cells.sh under the
# repo's 500-line cap; sourced by
# scripts/lib/ifa_fault_injection_sources.sh right after that file, and
# driven under stubs by
# scripts/lib/test-ifa-fault-injection-grants-secret-read-cases.sh.
#
# The stage function runs a trust-chain-scoped reducer and then STOPS it
# with SIGTERM. Its read-model wait
# (ifa_grants_secret_read_wait_for_read_models) only proves the handler
# WROTE its read-model facts -- the queue row can still be
# claimed/running while the handler finishes (remaining edges, ack), so a
# SIGTERM at that point kills the worker mid-ack: the row never reaches
# succeeded and a later reclaim pass re-drives it, leaving queue residue
# the follow-up projection stage trips over. This wait closes that race:
# the SIGTERM lands only once the trust-chain row has reached `succeeded`.
# Depends on the driver's strict mode and globals (FAULT_COMPOSE_PROJECT,
# use_compose, compose_file, ESHU_POSTGRES_DSN) like its sibling waits.

# ifa_grants_secret_read_wait_for_trust_chain_succeeded polls the
# trust-chain stage's own queue rows until at least one reached
# `succeeded` with none still open. Bounded (budget seconds, one poll per
# second) and fail-closed throughout: a bad budget fails before any poll;
# a failed query, a failed/dead_letter row, an unknown status, or a
# non-numeric count all fail fast; budget exhaustion fails loudly and the
# caller must never proceed to the kill. Classification mirrors
# ifa_grants_secret_read_wait_for_endpoint_domains: succeeded is done;
# pending/claimed/running/retrying is open (retrying is transient -- the
# reclaim path re-drives stale rows through it, see the claim SQL pinned
# by claim_reclaim_test.go); superseded is ignored (benign-terminal:
# residual is status NOT IN ('succeeded','superseded')); zero succeeded
# rows never passes, so a forgotten or never-enqueued trust-chain row
# fails instead of blessing an empty stage.
ifa_grants_secret_read_wait_for_trust_chain_succeeded() {
	local cell="$1" budget="$2"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: trust-chain wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local i rows row status count ok open bad
	for i in $(seq 1 "${budget}"); do
		if ! rows="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT status, count(*) FROM fact_work_items WHERE stage = 'reducer' AND domain = 'secrets_iam_trust_chain' GROUP BY status;" \
			"${compose_file}")"; then
			printf '%s: trust-chain wait: queue poll query failed\n' "${cell}" >&2
			return 1
		fi
		ok=0
		open=0
		bad=0
		# Statuses carry no spaces, so unquoted word-splitting walks one
		# status|count row at a time; an empty result walks zero times and
		# can never pass below.
		for row in ${rows}; do
			status="${row%%|*}"
			count="${row##*|}"
			[[ "${count}" =~ ^[0-9]+$ ]] \
				|| { printf '%s: trust-chain wait: queue poll returned non-numeric output %q\n' "${cell}" "${rows}" >&2; return 1; }
			case "${status}" in
			succeeded) ok=$((ok + count)) ;;
			pending | claimed | running | retrying) open=$((open + count)) ;;
			superseded) ;; # Benign-terminal: replaced by a newer row.
			failed | dead_letter) bad=$((bad + count)) ;;
			*) printf '%s: trust-chain wait: queue poll returned unknown status %q -- refusing to stop the stage on an unclassified queue state\n' "${cell}" "${status}" >&2; return 1 ;;
			esac
		done
		if [[ "${bad}" -gt 0 ]]; then
			printf '%s: trust-chain wait: %s trust-chain row(s) terminally bad -- refusing to stop the stage on a failed queue\n' "${cell}" "${bad}" >&2
			return 1
		fi
		if [[ "${open}" -eq 0 && "${ok}" -ge 1 ]]; then
			printf '%s: trust-chain stage committed: %s succeeded row(s), none open\n' "${cell}" "${ok}"
			return 0
		fi
		sleep 1
	done
	printf '%s: trust-chain wait: trust-chain row never reached succeeded within %ss -- refusing to SIGTERM a live worker\n' "${cell}" "${budget}" >&2
	return 1
}
