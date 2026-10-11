#!/usr/bin/env bash
# Claim-bound handshake for cell 3. The tagged reducer holds one handler after
# a committed graph write; its queue row stays unlocked for competing claims.
# This library inherits strict mode and ifa_det_pg from its caller.

# Validate the record before interpolating its identity into bounded SQL.
ifa_fault_read_held_claim() {
	local barrier="$1"
	[[ ! -e "${barrier}.finished.json" ]] || return 1
	jq -er 'select(.domain == "gcp_resource_materialization") |
		select(.work_item_id | type == "string" and test("^[A-Za-z0-9_:./-]+$")) |
		select(.claimed_at | type == "string" and test("^[0-9TZ:.+-]+$")) |
		select(.attempt_count | type == "number" and . >= 1 and floor == .) |
		[.work_item_id, .claimed_at, .attempt_count] | @tsv' "${barrier}.held.json"
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
