#!/usr/bin/env bash

verify_remote_e2e_finite_completion() {
	echo "Checking remote E2E finite completion..."
	if ! api_get "/index-status" "${INDEX_STATUS_FILE}"; then
		echo "remote E2E finite completion check could not read ${API_BASE_URL}/index-status" >&2
		echo "verify the API is reachable and ESHU_REMOTE_E2E_API_KEY is valid when set" >&2
		return 1
	fi
	if jq -e '
		def count_value($section; $name):
			if ((.coordinator[$section] // null) | type) == "array" then
				([.coordinator[$section][]? | select(.name == $name) | (.count // 0)] | add // 0)
			elif ((.coordinator[$section] // null) | type) == "object" then
				(.coordinator[$section][$name] // 0)
			else
				0
			end;
		def active_background:
			(count_value("run_status_counts"; "collection_pending") > 0) or
			(count_value("run_status_counts"; "collection_active") > 0) or
			(count_value("run_status_counts"; "reducer_converging") > 0) or
			(count_value("work_item_status_counts"; "pending") > 0) or
			(count_value("work_item_status_counts"; "claimed") > 0) or
			(count_value("completeness_counts"; "pending") > 0) or
			((.coordinator.active_claims // 0) > 0);
		((.status // "") == "healthy" or (.status // "") == "progressing") and
		(.queue | type == "object") and
		((.queue.retrying // 0) == 0) and
		((.queue.failed // 0) == 0) and
		((.queue.dead_letter // 0) == 0) and
		((.queue.overdue_claims // 0) == 0) and
		(.coordinator | type == "object") and
		((.coordinator.overdue_claims // 0) == 0) and
		(count_value("run_status_counts"; "failed") == 0) and
		(count_value("completeness_counts"; "blocked") == 0) and
		(((.status // "") == "progressing") or (active_background | not))
	' "${INDEX_STATUS_FILE}" >/dev/null; then
		jq -r '
			def count_value($section; $name):
				if ((.coordinator[$section] // null) | type) == "array" then
					([.coordinator[$section][]? | select(.name == $name) | (.count // 0)] | add // 0)
				elif ((.coordinator[$section] // null) | type) == "object" then
					(.coordinator[$section][$name] // 0)
				else
					0
				end;
			"remote E2E finite completion state: status=\(.status // "unknown") retrying=\(.queue.retrying // 0) failed=\(.queue.failed // 0) dead_letter=\(.queue.dead_letter // 0) failed_runs=\(count_value("run_status_counts"; "failed")) blocked_completeness=\(count_value("completeness_counts"; "blocked"))",
			"remote E2E continuous collector polling: outstanding=\(.queue.outstanding // 0) in_flight=\(.queue.in_flight // 0) pending=\(.queue.pending // 0) collection_pending=\(count_value("run_status_counts"; "collection_pending")) collection_active=\(count_value("run_status_counts"; "collection_active")) reducer_converging=\(count_value("run_status_counts"; "reducer_converging")) work_items_pending=\(count_value("work_item_status_counts"; "pending")) work_items_claimed=\(count_value("work_item_status_counts"; "claimed")) pending_completeness=\(count_value("completeness_counts"; "pending")) active_claims=\(.coordinator.active_claims // 0)"
		' "${INDEX_STATUS_FILE}"
		echo "remote E2E finite completion verified"
		return 0
	fi

	echo "remote E2E finite completion not reached" >&2
	cat "${INDEX_STATUS_FILE}" >&2
	return 1
}
