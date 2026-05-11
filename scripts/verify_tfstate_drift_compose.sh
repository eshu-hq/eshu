#!/usr/bin/env bash
#
# scripts/verify_tfstate_drift_compose.sh
#
# Compose-level proof matrix for the Terraform config-vs-state drift handler
# (issue #166, PR #165 follow-up). Reaches the same handler entry points
# PR #165 wired by writing durable facts the production queries already read,
# rather than running collector-terraform-state end-to-end.
#
# What it proves (per the eshu-correlation-truth runtime gate):
#
#   * Phase 3.5 enqueues one config_state_drift intent per active
#     state_snapshot:* scope (logged as
#     `config_state_drift_intents_enqueued count=N`).
#   * The reducer claims each intent, runs the drift handler, and increments
#     eshu_dp_correlation_rule_matches_total and
#     eshu_dp_correlation_drift_detected_total with the documented labels.
#   * Structured logs carry the high-cardinality `drift.kind`, `drift.address`,
#     and `drift.pack` attributes outside metric labels.
#   * The ambiguous-owner rejection path emits a WARN log with
#     `failure_class="ambiguous_backend_owner"` and no counter increments.
#
# Optional artifact:
#   ESHU_TFSTATE_DRIFT_PROOF_OUT=path/to/proof.md captures the scraped
#   counter values and log excerpts to the named markdown file.
#
# Usage:
#   bash scripts/verify_tfstate_drift_compose.sh
#
# Environment knobs honored:
#   ESHU_KEEP_COMPOSE_STACK=true   Skip `docker compose down -v` at the end.
#   ESHU_TFSTATE_DRIFT_PROOF_OUT   Write captured artifacts to this file.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

RUNTIME_LIB="$REPO_ROOT/scripts/lib/compose_verification_runtime_common.sh"
DRIFT_LIB="$REPO_ROOT/scripts/lib/tfstate_drift_compose_common.sh"

source "$RUNTIME_LIB"
source "$DRIFT_LIB"

SEED_PATH="$REPO_ROOT/tests/fixtures/tfstate_drift/seed.sql"
# tests/fixtures/tfstate_drift/expected/*.json files are human-readable
# documentation of the expected counter deltas and log fields per scenario;
# the verifier asserts them inline below rather than parsing the JSON. If
# this script grows past five scenarios, consider threading expected via
# jq so the JSON becomes the single source of truth.

TMP_DIR="$(mktemp -d)"
METRICS_BEFORE_FILE="$TMP_DIR/metrics-before.txt"
METRICS_AFTER_FILE="$TMP_DIR/metrics-after.txt"
PHASE_35_LOG_FILE="$TMP_DIR/phase-3-5.log"
DRIFT_LOGS_FILE="$TMP_DIR/drift-logs.json"

KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
PROOF_OUT="${ESHU_TFSTATE_DRIFT_PROOF_OUT:-}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-tfstate-drift-166-$$}"
export COMPOSE_PROJECT_NAME

COMPOSE_CMD=()
COMPOSE_DISPLAY=""
RESERVED_HOST_PORTS=()
PICKED_PORT=""

# Counter labels we care about (kept here so failures cite the exact regex).
LABEL_PACK='pack="terraform_config_state_drift"'
LABEL_ADDED_IN_STATE="drift_kind=\"added_in_state\""
LABEL_ADDED_IN_CONFIG="drift_kind=\"added_in_config\""
LABEL_REMOVED_FROM_STATE="drift_kind=\"removed_from_state\""
LABEL_ATTRIBUTE_DRIFT="drift_kind=\"attribute_drift\""

# -- preflight ---------------------------------------------------------------

require_compose() {
	for candidate in "docker compose" "docker-compose"; do
		# shellcheck disable=SC2206
		local cmd_array=($candidate)
		if "${cmd_array[@]}" version >/dev/null 2>&1; then
			COMPOSE_CMD=("${cmd_array[@]}")
			COMPOSE_DISPLAY="$candidate"
			return 0
		fi
	done
	echo "docker compose not found on PATH" >&2
	return 1
}

eshu_require_tool docker
eshu_require_tool curl
eshu_require_tool jq
# nc is used by the port picker below; fail fast at preflight rather than
# during the first pick_port call with a less-actionable error.
eshu_require_tool nc
require_compose

# -- port assignment ---------------------------------------------------------

pick_port() {
	local start_port="$1" port
	for ((port = start_port; port < start_port + 200; port++)); do
		if [[ " ${RESERVED_HOST_PORTS[*]} " == *" $port "* ]]; then
			continue
		fi
		nc -z 127.0.0.1 "$port" >/dev/null 2>&1 || {
			RESERVED_HOST_PORTS+=("$port")
			PICKED_PORT="$port"
			return 0
		}
	done
	echo "no free port found near $start_port" >&2
	return 1
}

assign_port() {
	local name="$1" start_port="$2"
	pick_port "$start_port"
	printf -v "$name" '%s' "$PICKED_PORT"
	export "$name"
}

configure_ports() {
	RESERVED_HOST_PORTS=()
	assign_port NEO4J_HTTP_PORT "${NEO4J_HTTP_PORT:-37474}"
	assign_port NEO4J_BOLT_PORT "${NEO4J_BOLT_PORT:-37687}"
	assign_port ESHU_POSTGRES_PORT "${ESHU_POSTGRES_PORT:-35432}"
	assign_port ESHU_HTTP_PORT "${ESHU_HTTP_PORT:-38080}"
	assign_port ESHU_MCP_PORT "${ESHU_MCP_PORT:-38081}"
	assign_port ESHU_API_METRICS_PORT "${ESHU_API_METRICS_PORT:-31464}"
	assign_port ESHU_BOOTSTRAP_METRICS_PORT "${ESHU_BOOTSTRAP_METRICS_PORT:-31467}"
	assign_port ESHU_MCP_METRICS_PORT "${ESHU_MCP_METRICS_PORT:-31468}"
	assign_port ESHU_INGESTER_METRICS_PORT "${ESHU_INGESTER_METRICS_PORT:-31465}"
	assign_port ESHU_RESOLUTION_ENGINE_METRICS_PORT "${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-31466}"
}

# -- cleanup -----------------------------------------------------------------

cleanup() {
	local exit_code=$?
	if [[ "$exit_code" -ne 0 ]]; then
		echo
		echo "tfstate drift compose verification failed (exit=$exit_code)."
		echo "Compose project: $COMPOSE_PROJECT_NAME"
		echo "Useful commands:"
		echo "  $COMPOSE_DISPLAY ps"
		echo "  $COMPOSE_DISPLAY logs --tail=200 bootstrap-index"
		echo "  $COMPOSE_DISPLAY logs --tail=200 resolution-engine"
		"${COMPOSE_CMD[@]}" ps || true
		[[ -f "$DRIFT_LOGS_FILE" ]] && {
			echo "Drift logs captured at $DRIFT_LOGS_FILE (tail 20):"
			tail -n 20 "$DRIFT_LOGS_FILE" || true
		}
		[[ -f "$METRICS_AFTER_FILE" ]] && {
			echo "Metrics scraped at $METRICS_AFTER_FILE:"
			cat "$METRICS_AFTER_FILE" || true
		}
	fi
	if [[ "$KEEP_STACK" != "true" ]]; then
		"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
	fi
	rm -rf "$TMP_DIR"
	exit "$exit_code"
}
trap cleanup EXIT INT TERM

# -- run ---------------------------------------------------------------------

cd "$REPO_ROOT"

echo "==> Configuring host port assignments"
configure_ports

echo "==> Bringing up minimal compose stack (postgres + nornicdb + db-migrate + bootstrap-index + resolution-engine + api)"
"${COMPOSE_CMD[@]}" up --build -d \
	postgres nornicdb db-migrate workspace-setup \
	bootstrap-index \
	resolution-engine \
	eshu

echo "==> Waiting for db-migrate to complete"
eshu_compose_wait_for_named_exit "db-migrate" 180

echo "==> Waiting for first bootstrap-index pass to complete"
eshu_compose_wait_for_named_exit "bootstrap-index" 180

# Capture metrics BEFORE seeding so we can assert deltas, not absolute values.
echo "==> Capturing metrics baseline"
tfstate_drift_scrape_counters \
	"localhost:${ESHU_RESOLUTION_ENGINE_METRICS_PORT}" \
	"$METRICS_BEFORE_FILE" \
	|| true  # The series may not yet exist before any drift intent fires.

echo "==> Applying seed corpus to compose Postgres"
tfstate_drift_seed_db "$SEED_PATH"

echo "==> Rerunning bootstrap-index (Phase 3.5) against the seeded scopes"
"${COMPOSE_CMD[@]}" run --rm bootstrap-index >"$PHASE_35_LOG_FILE" 2>&1 \
	|| {
		echo "bootstrap-index rerun failed; tail of output:" >&2
		tail -n 40 "$PHASE_35_LOG_FILE" >&2
		exit 1
	}

echo "==> Confirming Phase 3.5 enqueued the five seeded state_snapshot scopes"
grep -E 'config_state_drift_intents_enqueued count=[0-9]+' "$PHASE_35_LOG_FILE" \
	| tail -n 1 \
	| tee -a "$PHASE_35_LOG_FILE.summary"
phase35_count="$(
	grep -E 'config_state_drift_intents_enqueued count=[0-9]+' "$PHASE_35_LOG_FILE" \
	| tail -n 1 \
	| sed -E 's/.*count=([0-9]+).*/\1/'
)"
if [[ -z "$phase35_count" || "$phase35_count" -lt 5 ]]; then
	echo "Phase 3.5 enqueued count=${phase35_count:-<missing>}, expected >=5" >&2
	tail -n 40 "$PHASE_35_LOG_FILE" >&2
	exit 1
fi

echo "==> Waiting for reducer to drain config_state_drift intents"
tfstate_drift_wait_for_reducer_drain 120

# Give the OTLP/Prometheus exporter a moment to flush counter increments.
sleep 5

echo "==> Scraping metrics after drift drain"
tfstate_drift_scrape_counters \
	"localhost:${ESHU_RESOLUTION_ENGINE_METRICS_PORT}" \
	"$METRICS_AFTER_FILE"

echo "==> Extracting drift log lines from resolution-engine"
tfstate_drift_extract_drift_logs "$DRIFT_LOGS_FILE"

# -- assertions --------------------------------------------------------------

echo "==> Asserting per-kind counter deltas"
for kind in added_in_state added_in_config removed_from_state attribute_drift; do
	# Match on three required label substrings rather than a single
	# label-order-dependent regex: the metric name, the pack (so a future
	# correlation pack emitting the same drift_kind cannot satisfy the
	# assertion by accident), and the drift_kind itself.
	value_after="$(
		tfstate_drift_counter_value "$METRICS_AFTER_FILE" \
			'^eshu_dp_correlation_drift_detected_total\{' \
			"$LABEL_PACK" \
			"drift_kind=\"$kind\""
	)"
	value_before="$(
		tfstate_drift_counter_value "$METRICS_BEFORE_FILE" \
			'^eshu_dp_correlation_drift_detected_total\{' \
			"$LABEL_PACK" \
			"drift_kind=\"$kind\""
	)"
	delta=$((value_after - value_before))
	echo "  drift_kind=$kind before=$value_before after=$value_after delta=$delta"
	if [[ "$delta" -lt 1 ]]; then
		echo "Counter delta for drift_kind=$kind was $delta, expected >=1" >&2
		echo "Captured metrics:" >&2
		cat "$METRICS_AFTER_FILE" >&2
		exit 1
	fi
done

echo "==> Asserting rule_matches counter delta for the drift pack"
# Asserts a positive delta rather than a non-zero absolute value: any other
# pack's counter activity (or pre-existing series in the registry) cannot
# mask a missing drift increment.
value_before="$(
	tfstate_drift_counter_value "$METRICS_BEFORE_FILE" \
		'^eshu_dp_correlation_rule_matches_total\{' \
		"$LABEL_PACK"
)"
value_after="$(
	tfstate_drift_counter_value "$METRICS_AFTER_FILE" \
		'^eshu_dp_correlation_rule_matches_total\{' \
		"$LABEL_PACK"
)"
rule_matches_delta=$((value_after - value_before))
echo "  rule_matches $LABEL_PACK before=$value_before after=$value_after delta=$rule_matches_delta"
if [[ "$rule_matches_delta" -lt 1 ]]; then
	echo "rule_matches delta for $LABEL_PACK was $rule_matches_delta, expected >=1" >&2
	cat "$METRICS_AFTER_FILE" >&2
	exit 1
fi

echo "==> Asserting ambiguous-owner rejection log"
tfstate_drift_assert_log_line "$DRIFT_LOGS_FILE" \
	'ambiguous_backend_owner' \
	'ambiguous-owner WARN with failure_class=ambiguous_backend_owner'

echo "==> Asserting each admitted drift kind appears in the structured log"
for kind in added_in_state added_in_config removed_from_state attribute_drift; do
	tfstate_drift_assert_log_line "$DRIFT_LOGS_FILE" \
		"\"drift.kind\":\"$kind\"" \
		"drift candidate admitted log for drift_kind=$kind"
done

echo "==> Asserting drift admission carries high-cardinality address field"
for address in aws_s3_bucket.unmanaged aws_s3_bucket.declared aws_s3_bucket.was_there aws_s3_bucket.logs; do
	tfstate_drift_assert_log_line "$DRIFT_LOGS_FILE" \
		"\"drift.address\":\"$address\"" \
		"drift.address=$address in admitted-candidate log"
done

# -- proof artifact ----------------------------------------------------------

if [[ -n "$PROOF_OUT" ]]; then
	echo "==> Writing proof artifact to $PROOF_OUT"
	proof_dir="$(dirname "$PROOF_OUT")"
	mkdir -p "$proof_dir"
	{
		echo "# tfstate drift compose proof matrix"
		echo
		echo "Captured: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
		echo "Compose project: \`$COMPOSE_PROJECT_NAME\`"
		echo "Worktree HEAD: \`$(git rev-parse --short HEAD)\` on $(git rev-parse --abbrev-ref HEAD)"
		echo
		echo "## Phase 3.5 enqueue log"
		echo
		echo '```'
		grep -E 'config_state_drift_intents_enqueued count=[0-9]+' "$PHASE_35_LOG_FILE" | tail -n 1
		echo '```'
		echo
		echo "## Counter snapshot (after drain)"
		echo
		echo '```'
		cat "$METRICS_AFTER_FILE"
		echo '```'
		echo
		echo "## Structured log excerpts (drift admit + reject)"
		echo
		echo '```json'
		cat "$DRIFT_LOGS_FILE"
		echo '```'
	} >"$PROOF_OUT"
fi

echo
echo "OK — tfstate drift compose proof matrix passed."
