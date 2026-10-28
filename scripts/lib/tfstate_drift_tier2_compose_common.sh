#!/usr/bin/env bash
#
# scripts/lib/tfstate_drift_tier2_compose_common.sh
#
# Helpers for scripts/verify_tfstate_drift_compose_tier2.sh (issue #187).
# Re-uses the Tier-1 assertion helpers from
# scripts/lib/tfstate_drift_compose_common.sh and adds the orchestration
# steps that Tier-1 does not need: waiting for terraform_state work items
# to drain, asserting that backend candidates and snapshot facts landed
# before kicking Phase 3.5, and dumping the extra services for diagnostics.

# TODO(#188): once feature/promote-compose-wait-helper-188 lands on main,
# delete the duplicated eshu_compose_wait_for_named_exit_tier2 helper below
# and call the shared helper from compose_verification_runtime_common.sh
# instead. PR #193 is still open as of this writing, so this file carries
# the duplicate so Tier-2 can land without serializing on #188.

# Wait for a one-shot compose service to exit cleanly. Pattern-copy of the
# Tier-1 helper so this lib is self-contained while #188 is in flight.
eshu_compose_wait_for_named_exit_tier2() {
	local service="$1" timeout_seconds="$2"
	local deadline=$((SECONDS + timeout_seconds))
	while ((SECONDS < deadline)); do
		local container_id state exit_code
		container_id="$("${COMPOSE_CMD[@]}" ps -a -q "$service")"
		if [[ -z "$container_id" ]]; then
			sleep 2
			continue
		fi
		state="$(docker inspect --format='{{.State.Status}}' "$container_id" 2>/dev/null || true)"
		if [[ "$state" == "exited" ]]; then
			exit_code="$(docker inspect --format='{{.State.ExitCode}}' "$container_id" 2>/dev/null || true)"
			if [[ "$exit_code" != "0" ]]; then
				echo "$service exited with code $exit_code" >&2
				return 1
			fi
			return 0
		fi
		sleep 2
	done
	echo "Timed out waiting for $service to exit" >&2
	return 1
}

# Run a single psql query against the compose Postgres and echo the trimmed
# scalar result. Used by the wait-and-assert helpers below.
tier2_psql_scalar() {
	local query="$1"
	"${COMPOSE_CMD[@]}" exec -T -e PGPASSWORD="${ESHU_POSTGRES_PASSWORD:-change-me}" postgres \
		psql -U eshu -d eshu -t -A -c "$query" \
		2>/dev/null \
		| tr -d '[:space:]' \
		|| true
}

# Assert that the Git collector landed at least one terraform_backends row for
# every fixture repo we expect to drive a backend candidate. Tier-2 has four
# distinct s3 buckets (A, B, D, E) declared across five repos (D uses two
# repos). The exact_attributes filter in terraformBackendCandidate keeps only
# rows with literal bucket/key/region, so the count below also confirms the
# parser emitted the *_is_literal flags correctly.
tier2_assert_terraform_backend_facts() {
	local minimum="$1" deadline=$((SECONDS + ${2:-60}))
	while ((SECONDS < deadline)); do
		local count
		count="$(tier2_psql_scalar "
			SELECT COUNT(*) FROM fact_records
			WHERE payload -> 'parsed_file_data' -> 'terraform_backends' IS NOT NULL
			  AND jsonb_array_length(payload -> 'parsed_file_data' -> 'terraform_backends') > 0;
		")"
		if [[ -n "$count" && "$count" -ge "$minimum" ]]; then
			echo "  terraform_backends fact_records count=$count (want >=$minimum)"
			return 0
		fi
		sleep 2
	done
	echo "Timed out waiting for >=$minimum terraform_backends fact_records (last count=${count:-<missing>})" >&2
	return 1
}

# Wait until the workflow-coordinator has planned at least $1 terraform_state
# work items. The coordinator runs a reconcile loop; planning is idempotent
# per (instance, plan_key) so a steady-state count is reachable quickly.
tier2_wait_for_terraform_state_work_items() {
	local minimum="$1" deadline=$((SECONDS + ${2:-120}))
	local count=""
	while ((SECONDS < deadline)); do
		count="$(tier2_psql_scalar "
			SELECT COUNT(*) FROM workflow_work_items
			WHERE collector_kind='terraform_state';
		")"
		if [[ -n "$count" && "$count" -ge "$minimum" ]]; then
			echo "  workflow_work_items collector_kind=terraform_state count=$count (want >=$minimum)"
			return 0
		fi
		sleep 2
	done
	echo "Timed out waiting for >=$minimum terraform_state work items (last count=${count:-<missing>})" >&2
	"${COMPOSE_CMD[@]}" logs --tail=120 workflow-coordinator >&2 || true
	return 1
}

# Wait for every terraform_state work item to reach a steady-state status.
# Steady states for a Tier-2 happy path are `completed`; we also accept
# `failed_retryable` as transient on the way up (the coordinator/collector
# may retry on initial DNS warm-up) but treat `failed_terminal` and `expired`
# as immediate failures because they will never drain on their own.
tier2_wait_for_terraform_state_work_drained() {
	local timeout_seconds="${1:-180}"
	local minimum_completed="${2:-3}"
	local deadline=$((SECONDS + timeout_seconds))
	local active="" terminal="" completed=""
	while ((SECONDS < deadline)); do
		active="$(tier2_psql_scalar "
			SELECT COUNT(*) FROM workflow_work_items
			WHERE collector_kind='terraform_state'
			  AND status IN ('pending','claimed','failed_retryable');
		")"
		terminal="$(tier2_psql_scalar "
			SELECT COUNT(*) FROM workflow_work_items
			WHERE collector_kind='terraform_state'
			  AND status IN ('failed_terminal','expired');
		")"
		completed="$(tier2_psql_scalar "
			SELECT COUNT(*) FROM workflow_work_items
			WHERE collector_kind='terraform_state'
			  AND status='completed';
		")"
		if [[ -n "$terminal" && "$terminal" -gt 0 ]]; then
			echo "terraform_state work items reached terminal failure: failed_terminal+expired=$terminal" >&2
			"${COMPOSE_CMD[@]}" exec -T -e PGPASSWORD="${ESHU_POSTGRES_PASSWORD:-change-me}" postgres \
				psql -U eshu -d eshu -c \
				"SELECT work_item_id, status, COALESCE(last_failure_class,'') AS failure_class,
				        COALESCE(last_failure_message,'') AS failure_message
				 FROM workflow_work_items
				 WHERE collector_kind='terraform_state'
				 ORDER BY status, work_item_id;" >&2 || true
			"${COMPOSE_CMD[@]}" logs --tail=200 collector-terraform-state >&2 || true
			return 1
		fi
		if [[ "$active" == "0" && -n "$completed" && "$completed" -ge "$minimum_completed" ]]; then
			echo "  workflow_work_items completed=$completed active=$active terminal=$terminal"
			return 0
		fi
		sleep 3
	done
	echo "Timed out waiting for terraform_state work items to drain (active=$active completed=$completed terminal=$terminal)" >&2
	"${COMPOSE_CMD[@]}" logs --tail=200 collector-terraform-state >&2 || true
	return 1
}

# Confirm the collector emitted terraform_state_snapshot facts for each
# bucket whose backend resolved to a single owner repo. The ambiguous bucket
# (D) is deliberately excluded — the coordinator's resolver should reject it
# before any work item gets enqueued for D, OR the collector should bail out
# after attempting and falling back to ambiguous-owner classification. We
# accept any of A/B/E snapshots landing (D may or may not land depending on
# the planner's ambiguity handling).
tier2_assert_terraform_state_snapshot_facts() {
	local minimum="$1"
	local count
	count="$(tier2_psql_scalar "
		SELECT COUNT(*) FROM fact_records
		WHERE fact_kind='terraform_state_snapshot';
	")"
	if [[ -z "$count" || "$count" -lt "$minimum" ]]; then
		echo "terraform_state_snapshot fact count=$count, expected >=$minimum" >&2
		return 1
	fi
	echo "  terraform_state_snapshot facts count=$count (want >=$minimum)"
	return 0
}

# Confirm the collector emitted at least one terraform_state_resource fact for
# the buckets that should have a non-empty state (A: unmanaged, E: logs).
tier2_assert_terraform_state_resource_facts() {
	local minimum="$1"
	local count
	count="$(tier2_psql_scalar "
		SELECT COUNT(*) FROM fact_records
		WHERE fact_kind='terraform_state_resource';
	")"
	if [[ -z "$count" || "$count" -lt "$minimum" ]]; then
		echo "terraform_state_resource fact count=$count, expected >=$minimum" >&2
		return 1
	fi
	echo "  terraform_state_resource facts count=$count (want >=$minimum)"
	return 0
}

# Dump diagnostic info from every Tier-2 service when a failure trap fires.
# Called from the verifier's cleanup function on non-zero exit.
tier2_dump_failure_logs() {
	local services="minio minio-init collector-terraform-state workflow-coordinator bootstrap-index resolution-engine ingester eshu"
	for service in $services; do
		echo "----- logs for $service (tail 80) -----"
		"${COMPOSE_CMD[@]}" logs --tail=80 --no-color "$service" 2>&1 || true
	done
}
