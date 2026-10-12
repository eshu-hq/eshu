#!/usr/bin/env bash
# Claim-bound handshake for cell 3. The tagged reducer holds one handler after
# a committed graph write; its queue row stays unlocked for competing claims.
# This library inherits strict mode and ifa_det_pg from its caller.

# Validate the held record before interpolating its identity into bounded SQL
# or a log filter. Prints work_item_id, claimed_at, and attempt_count as TSV.
ifa_fault_parse_held_record() {
	jq -er 'select(.domain == "gcp_resource_materialization") |
		select(.work_item_id | type == "string" and test("^[A-Za-z0-9_:./-]+$")) |
		select(.claimed_at | type == "string" and test("^[0-9TZ:.+-]+$")) |
		select(.attempt_count | type == "number" and . >= 1 and floor == .) |
		[.work_item_id, .claimed_at, .attempt_count] | @tsv' "$1"
}

# Read the claim the reducer still holds; a finished handler has no held claim.
ifa_fault_read_held_claim() {
	local barrier="$1"
	[[ ! -e "${barrier}.finished.json" ]] || return 1
	ifa_fault_parse_held_record "${barrier}.held.json"
}

# Wait for a live held handler, failing promptly on process death/cancellation.
ifa_fault_wait_for_held_claim() {
	local barrier="$1" reducer_pid="$2" deadline=$((SECONDS + $3))
	while :; do
		kill -0 "${reducer_pid}" 2>/dev/null || return 1
		[[ ! -e "${barrier}.finished.json" ]] || return 1
		if [[ -f "${barrier}.held.json" ]]; then
			ifa_fault_read_held_claim "${barrier}" >/dev/null
			return $?
		fi
		(( SECONDS < deadline )) || return 1
		sleep 0.1
	done
}

# The mutation itself returns the exact old identity. A prior claimed-row poll
# never substitutes for this committed, nonzero expiry result.
ifa_fault_expire_held_claim() {
	local project="$1" compose="$2" dsn="$3" file="$4" barrier="$5"
	local held id epoch attempt result
	held="$(ifa_fault_read_held_claim "${barrier}")" || return 1
	IFS=$'\t' read -r id epoch attempt <<<"${held}"
	result="$(ifa_det_pg "${project}" "${compose}" "${dsn}" \
		"WITH expiring AS MATERIALIZED (SELECT work_item_id FROM fact_work_items WHERE work_item_id = '${id}' AND stage = 'reducer' AND status IN ('claimed', 'running') AND last_attempt_at = '${epoch}'::timestamptz AND attempt_count = ${attempt} AND claim_until > clock_timestamp() ORDER BY work_item_id COLLATE \"C\" FOR NO KEY UPDATE), expired AS (UPDATE fact_work_items SET claim_until = now() WHERE work_item_id IN (SELECT work_item_id FROM expiring) RETURNING work_item_id, last_attempt_at, attempt_count) SELECT json_build_object('work_item_id', work_item_id, 'claimed_at', '${epoch}', 'attempt_count', attempt_count) FROM expired;" \
		"${file}")" || return 1
	[[ ! -e "${barrier}.finished.json" ]] || return 1
	jq -e --arg id "${id}" --arg epoch "${epoch}" --argjson attempt "${attempt}" \
		'.work_item_id == $id and .claimed_at == $epoch and .attempt_count == $attempt' \
		<<<"${result}" >/dev/null || return 1
	[[ -n "${result}" ]] || return 1
	printf '%s\n' "${result}" >"${barrier}.expired.json"
	printf 'expire-lease-mid-handler: committed exact expiry: %s\n' "${result}"
}

# Require the SAME item's new claim epoch while the original handler remains
# active. Terminal success is allowed: the replacement may ACK before polling.
ifa_fault_wait_for_held_replacement() {
	local project="$1" compose="$2" dsn="$3" file="$4" barrier="$5"
	local held id epoch attempt result deadline=$((SECONDS + $6))
	held="$(ifa_fault_read_held_claim "${barrier}")" || return 1
	IFS=$'\t' read -r id epoch attempt <<<"${held}"
	while :; do
		[[ ! -e "${barrier}.finished.json" ]] || return 1
		result="$(ifa_det_pg "${project}" "${compose}" "${dsn}" \
			"SELECT json_build_object('work_item_id', work_item_id, 'claimed_at', last_attempt_at, 'attempt_count', attempt_count, 'status', status) FROM fact_work_items WHERE work_item_id = '${id}' AND stage = 'reducer' AND last_attempt_at <> '${epoch}'::timestamptz AND attempt_count > ${attempt};" \
			"${file}")" || return 1
		[[ ! -e "${barrier}.finished.json" ]] || return 1
		if [[ -n "${result}" ]] && jq -e --arg id "${id}" --arg epoch "${epoch}" --argjson attempt "${attempt}" \
			'.work_item_id == $id and .claimed_at != $epoch and .attempt_count > $attempt' \
			<<<"${result}" >/dev/null; then
			printf '%s\n' "${result}" >"${barrier}.replacement.json"
			printf 'expire-lease-mid-handler: replacement while original held: %s\n' "${result}"
			return 0
		fi
		(( SECONDS < deadline )) || return 1
		sleep 0.1
	done
}

# Publish the release atomically and bind it to this held claim, not a filename
# left over from a prior execution. The reducer independently validates it.
ifa_fault_release_held_claim() {
	local barrier="$1"
	ifa_fault_read_held_claim "${barrier}" >/dev/null || return 1
	jq '{work_item_id, claimed_at}' "${barrier}.held.json" >"${barrier}.release.tmp" || return 1
	mv "${barrier}.release.tmp" "${barrier}.release"
}

# Count the production reducer's log records for one work item by status, as a
# JSON object. Non-JSON lines (panics, plain text) are skipped, never fatal.
ifa_fault_intent_status_histogram() {
	jq -cnR --arg id "$2" '[inputs | fromjson? // empty
		| select(type == "object" and .intent_id == $id) | (.status // "none" | tostring)]
		| group_by(.) | map({key: .[0], value: length}) | from_entries' "$1"
}

# After release, observe the real reducer's log for the held work item. Both
# ACKs of the item (the stale claim's and the successor's) leave one record each.
# Flushed apart they log {succeeded 1, fenced 1}; flushed in one batch the
# Service logs both as ack_outcome_unknown and writes no succeeded record,
# because AckBatch commits the successor yet rejects the whole batch (#6162;
# the Service telemetry for that batch is tracked in #8007).
# Require at least one fenced-outcome record, at least two ACK outcome records,
# and at most one succeeded record. A stale ACK accepted after the successor's
# own ACK would add a second succeeded record. The log carries no claim epoch,
# so no record is attributed to the stale claim or the successor; epoch-level
# rejection against a live successor is proven by
# TestReducerQueueExpiryInterleaveLive (class scheduled). The drain gate counts
# queue rows, and a fenced ACK changes none, so wait for the log, then settle
# and re-read so a late second succeeded record is counted. Runs after the
# barrier wrote finished.json, so it reads the held record directly.
ifa_fault_assert_stale_ack_fenced() {
	local barrier="$1" reducer_log="$2" deadline=$((SECONDS + $3))
	local held id histogram fenced succeeded
	held="$(ifa_fault_parse_held_record "${barrier}.held.json")" || return 1
	IFS=$'\t' read -r id _ _ <<<"${held}"
	[[ -f "${reducer_log}" ]] || return 1
	while :; do
		histogram="$(ifa_fault_intent_status_histogram "${reducer_log}" "${id}")" || return 1
		fenced="$(jq -r '(.ack_claim_rejected // 0) + (.ack_outcome_unknown // 0) + (.lease_lost_during_execution // 0)' <<<"${histogram}")"
		succeeded="$(jq -r '.succeeded // 0' <<<"${histogram}")"
		(( fenced >= 1 && fenced + succeeded >= 2 )) && break
		if (( SECONDS >= deadline )); then
			printf 'expire-lease-mid-handler: fewer than two ACK outcome records, or no fenced-outcome record, for %s by the deadline; status histogram: %s\n' "${id}" "${histogram}" >&2
			return 1
		fi
		sleep 0.1
	done
	sleep 1
	histogram="$(ifa_fault_intent_status_histogram "${reducer_log}" "${id}")" || return 1
	succeeded="$(jq -r '.succeeded // 0' <<<"${histogram}")"
	if (( succeeded > 1 )); then
		printf 'expire-lease-mid-handler: %s succeeded records for %s, want at most 1; status histogram: %s\n' "${succeeded}" "${id}" "${histogram}" >&2
		return 1
	fi
	printf 'expire-lease-mid-handler: held work item %s log status histogram: %s\n' "${id}" "${histogram}"
}

# After the drain the held item must be one terminal succeeded row at the
# replacement claim: attempt_count = held + 1 (a third claim would mean a second
# reclaim the cell does not intend), a new last_attempt_at, and the lease
# cleared. Every inequality is in SQL: Postgres renders +00:00 where held.json
# has Z, so comparing the epoch strings in jq would always differ. One read, no
# poll: the state is terminal once the drain gate returns.
ifa_fault_assert_held_item_terminal() {
	local project="$1" compose="$2" dsn="$3" file="$4" barrier="$5"
	local held id epoch attempt result
	held="$(ifa_fault_parse_held_record "${barrier}.held.json")" || return 1
	IFS=$'\t' read -r id epoch attempt <<<"${held}"
	result="$(ifa_det_pg "${project}" "${compose}" "${dsn}" \
		"SELECT json_build_object('work_item_id', work_item_id, 'status', status, 'attempt_count', attempt_count, 'claimed_at', last_attempt_at) FROM fact_work_items WHERE work_item_id = '${id}' AND stage = 'reducer' AND status = 'succeeded' AND attempt_count = ${attempt} + 1 AND last_attempt_at <> '${epoch}'::timestamptz AND lease_owner IS NULL AND claim_until IS NULL;" \
		"${file}")" || return 1
	[[ -n "${result}" ]] || return 1
	jq -e --arg id "${id}" '.work_item_id == $id' <<<"${result}" >/dev/null || return 1
	printf 'expire-lease-mid-handler: held work item terminal row: %s\n' "${result}"
}
