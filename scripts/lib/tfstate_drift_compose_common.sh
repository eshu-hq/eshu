#!/usr/bin/env bash
#
# scripts/lib/tfstate_drift_compose_common.sh
#
# Helper library for scripts/verify_tfstate_drift_compose.sh. Owns the
# seed-the-DB, wait-for-Phase-3.5, wait-for-reducer-drain, scrape-counter,
# and extract-log-line operations the verifier composes. Split out so the
# main script stays under the 500-line repo limit.

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

# Wait for the reducer queue to drain every config_state_drift intent. Uses
# psql against fact_work_items (the table name behind NewReducerQueue) since
# /admin/status does not expose per-domain counts today.
#
# Returns 0 when every drift intent reaches `succeeded`. Returns 1 with a
# concrete failure summary when one or more intents land in `failed` or
# `dead_letter` — those are terminal states that will never drain on their
# own, so polling further is wasted time and the proof has clearly failed.
tfstate_drift_wait_for_reducer_drain() {
	local timeout_seconds="$1"
	local deadline=$((SECONDS + timeout_seconds))
	local active dead_letter failed
	while ((SECONDS < deadline)); do
		active="$(
			"${COMPOSE_CMD[@]}" exec -T -e PGPASSWORD="${ESHU_POSTGRES_PASSWORD:-change-me}" postgres \
				psql -U eshu -d eshu -t -A -c \
				"SELECT count(*) FROM fact_work_items WHERE domain='config_state_drift' AND status IN ('pending','claimed','in_flight','retrying');" \
				2>/dev/null \
				| tr -d '[:space:]' \
				|| true
		)"
		dead_letter="$(
			"${COMPOSE_CMD[@]}" exec -T -e PGPASSWORD="${ESHU_POSTGRES_PASSWORD:-change-me}" postgres \
				psql -U eshu -d eshu -t -A -c \
				"SELECT count(*) FROM fact_work_items WHERE domain='config_state_drift' AND status='dead_letter';" \
				2>/dev/null \
				| tr -d '[:space:]' \
				|| true
		)"
		failed="$(
			"${COMPOSE_CMD[@]}" exec -T -e PGPASSWORD="${ESHU_POSTGRES_PASSWORD:-change-me}" postgres \
				psql -U eshu -d eshu -t -A -c \
				"SELECT count(*) FROM fact_work_items WHERE domain='config_state_drift' AND status='failed';" \
				2>/dev/null \
				| tr -d '[:space:]' \
				|| true
		)"
		if [[ -n "$dead_letter" && "$dead_letter" -gt 0 ]] || [[ -n "$failed" && "$failed" -gt 0 ]]; then
			echo "drift intents reached terminal failure state: dead_letter=$dead_letter failed=$failed" >&2
			"${COMPOSE_CMD[@]}" exec -T -e PGPASSWORD="${ESHU_POSTGRES_PASSWORD:-change-me}" postgres \
				psql -U eshu -d eshu -c \
				"SELECT scope_id, status, COALESCE(failure_class,'') AS failure_class, COALESCE(failure_message,'') AS failure_message FROM fact_work_items WHERE domain='config_state_drift' AND status IN ('dead_letter','failed') ORDER BY status, scope_id;" \
				>&2 || true
			return 1
		fi
		if [[ "$active" == "0" ]]; then
			return 0
		fi
		sleep 2
	done
	echo "Timed out waiting for fact_work_items to drain config_state_drift intents (active=$active dead_letter=$dead_letter failed=$failed)" >&2
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

# Read a counter value from the scraped metrics text. Returns 0 when no
# line in the metrics file matches every filter (counters that have not
# fired yet) so callers can treat absence as zero.
#
# The Prometheus text exposition format emits one cumulative value per
# unique label set. For a counter with a fixed label set (the case today:
# one series per drift_kind) `tail -n 1` picks the single cumulative
# reading. If the exporter ever fans the counter out across multiple
# scope_name or service_name variants for the same drift_kind, this
# helper picks one arbitrary line and the delta math becomes ambiguous;
# remediate by summing across matching lines with `awk` instead.
#
# Args:
#   $1 metrics_file
#   $2..$N one or more grep -E patterns; the line must match ALL of them.
#          Pass each label as a separate pattern so callers do not have
#          to assume any particular alphabetical order from the exporter
#          (e.g. pass both `pack="terraform_config_state_drift"` and
#          `drift_kind="added_in_state"` instead of trying to encode both
#          in a single brittle regex).
tfstate_drift_counter_value() {
	local metrics_file="$1"
	shift
	if [[ ! -f "$metrics_file" ]]; then
		echo "0"
		return 0
	fi
	local lines
	lines="$(cat "$metrics_file")"
	local pattern
	for pattern in "$@"; do
		lines="$(echo "$lines" | grep -E -- "$pattern" || true)"
		if [[ -z "$lines" ]]; then
			echo "0"
			return 0
		fi
	done
	# Prometheus text-format value is the last whitespace-delimited token.
	echo "$lines" | tail -n 1 | awk '{print $NF}'
}

# Extract resolution-engine log lines specific to the drift handler, dump
# them to $2. Slog JSON output; the filter matches the two message bodies
# the handler emits (`drift candidate admitted` for admissions and
# `drift candidate rejected` for every non-fatal rejection class —
# scope_not_state_snapshot, resolver_unavailable, no_config_repo_owns_backend,
# ambiguous_backend_owner, evidence_loader_unavailable). Unrelated reducer
# errors (e.g. semantic_entity_materialization Neo4j constraint violations
# from bootstrap-index processing the ecosystem fixture corpus) do not
# share these message bodies, so the filter keeps the proof artifact
# focused on the drift handler's emissions.
tfstate_drift_extract_drift_logs() {
	local output_file="$1"
	"${COMPOSE_CMD[@]}" logs --no-color resolution-engine 2>/dev/null \
		| grep -E '"drift candidate (admitted|rejected)"' \
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
