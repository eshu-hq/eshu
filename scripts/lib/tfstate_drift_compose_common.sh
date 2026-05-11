#!/usr/bin/env bash
#
# scripts/lib/tfstate_drift_compose_common.sh
#
# Helper library for scripts/verify_tfstate_drift_compose.sh. Owns the
# seed-the-DB, wait-for-Phase-3.5, wait-for-reducer-drain, scrape-counter,
# and extract-log-line operations the verifier composes. Split out so the
# main script stays under the 500-line repo limit.

# Wait for a one-shot compose service to exit cleanly. The shared helper at
# scripts/lib/compose_verification_runtime_common.sh hardcodes the service
# name to `bootstrap-index`; this version takes the service as an argument
# so we can also wait for db-migrate.
#
# Args:
#   $1 service_name
#   $2 timeout_seconds
eshu_compose_wait_for_named_exit() {
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

# Apply the drift proof seed against the compose Postgres container.
# Expects globals: COMPOSE_CMD, ESHU_POSTGRES_PASSWORD.
tfstate_drift_seed_db() {
	local seed_path="$1"
	if [[ ! -f "$seed_path" ]]; then
		echo "Seed not found at $seed_path" >&2
		return 1
	fi
	"${COMPOSE_CMD[@]}" exec -T -e PGPASSWORD="${ESHU_POSTGRES_PASSWORD:-change-me}" postgres \
		psql -U eshu -d eshu -v ON_ERROR_STOP=1 -q -f - <"$seed_path"
}

# Wait until bootstrap-index has rerun and emitted the
# config_state_drift_intents_enqueued line for the seeded scopes. Polls
# bootstrap-index logs because compose's "exited zero" state can race with
# log flushing.
#
# Args:
#   $1 expected_count — minimum non-zero count to consider the trigger fired
#                       (we seed four state_snapshot scopes, so 4)
#   $2 timeout_seconds
tfstate_drift_wait_for_phase_35() {
	local expected_count="$1" timeout_seconds="$2"
	local deadline=$((SECONDS + timeout_seconds))
	local log_line=""
	while ((SECONDS < deadline)); do
		log_line="$(
			"${COMPOSE_CMD[@]}" logs --no-color bootstrap-index 2>/dev/null \
				| grep -E 'config_state_drift_intents_enqueued count=[0-9]+' \
				| tail -n 1 \
				|| true
		)"
		if [[ -n "$log_line" ]]; then
			local count
			count="$(echo "$log_line" | sed -E 's/.*count=([0-9]+).*/\1/')"
			if [[ -n "$count" && "$count" -ge "$expected_count" ]]; then
				echo "$log_line"
				return 0
			fi
		fi
		sleep 2
	done
	echo "Timed out waiting for Phase 3.5 (config_state_drift_intents_enqueued count>=$expected_count)" >&2
	echo "Last bootstrap-index log line matching the trigger: ${log_line:-<none>}" >&2
	return 1
}

# Wait for the reducer queue to drain every config_state_drift intent (status
# is no longer pending or in-flight). Uses psql instead of /admin/status
# because the API does not expose per-domain counts today; psql is the
# minimum surface area required for the proof.
tfstate_drift_wait_for_reducer_drain() {
	local timeout_seconds="$1"
	local deadline=$((SECONDS + timeout_seconds))
	local depth=""
	while ((SECONDS < deadline)); do
		depth="$(
			"${COMPOSE_CMD[@]}" exec -T -e PGPASSWORD="${ESHU_POSTGRES_PASSWORD:-change-me}" postgres \
				psql -U eshu -d eshu -t -A -c \
				"SELECT count(*) FROM reducer_queue WHERE domain='config_state_drift' AND status IN ('pending','claimed','in_flight','retrying');" \
				2>/dev/null \
				| tr -d '[:space:]' \
				|| true
		)"
		if [[ "$depth" == "0" ]]; then
			return 0
		fi
		sleep 2
	done
	echo "Timed out waiting for reducer_queue to drain config_state_drift intents (current depth=$depth)" >&2
	return 1
}

# Scrape the resolution-engine Prometheus surface for the two correlation
# counters and write the relevant lines to $2. Filters out unrelated counters
# to keep diffs noise-free.
#
# Args:
#   $1 host_port (e.g. localhost:19466)
#   $2 output_file
tfstate_drift_scrape_counters() {
	local endpoint="$1" output_file="$2"
	curl -fsS "http://${endpoint}/metrics" \
		| grep -E '^eshu_dp_correlation_(rule_matches|drift_detected)_total' \
		> "$output_file"
}

# Read a counter value from the scraped metrics text. Returns 0 when the
# series is not present (counters that have not fired yet) so callers can
# treat absence as zero.
#
# Args:
#   $1 metrics_file
#   $2 series_match_regex (e.g. 'eshu_dp_correlation_drift_detected_total\{[^}]*drift_kind="added_in_state"[^}]*\}')
tfstate_drift_counter_value() {
	local metrics_file="$1" pattern="$2"
	local line
	line="$(grep -E "$pattern" "$metrics_file" | tail -n 1 || true)"
	if [[ -z "$line" ]]; then
		echo "0"
		return 0
	fi
	# Prometheus text-format value is the last whitespace-delimited token.
	echo "$line" | awk '{print $NF}'
}

# Extract resolution-engine log lines that mention drift, dump them to $2.
# Slog JSON output: caller filters with jq or grep.
tfstate_drift_extract_drift_logs() {
	local output_file="$1"
	"${COMPOSE_CMD[@]}" logs --no-color resolution-engine 2>/dev/null \
		| grep -E '"drift candidate (admitted|rejected)"|drift\.(pack|kind|address)|failure_class' \
		> "$output_file" \
		|| true
}

# Assert that a log line matching the regex appears in $1. Logs the surrounding
# context to stderr when the assertion fails to make the proof failure mode
# inspectable.
tfstate_drift_assert_log_line() {
	local logs_file="$1" pattern="$2" description="$3"
	if grep -E -q "$pattern" "$logs_file"; then
		return 0
	fi
	echo "Assertion failed: $description" >&2
	echo "  pattern: $pattern" >&2
	echo "  logs sampled (tail 40):" >&2
	tail -n 40 "$logs_file" >&2
	return 1
}
