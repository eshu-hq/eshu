#!/usr/bin/env bash
#
# scripts/verify_tfstate_drift_compose_tier2.sh
#
# Tier-2 E2E compose proof for the Terraform config-vs-state drift handler
# (issue #187). Where the Tier-1 verifier seeds Postgres with handcrafted facts
# to exercise the reducer, Tier-2 originates every fact from the real Eshu
# collector chain:
#
#   bootstrap-index -> Git collector parses fixture .tf files into
#     terraform_backends facts
#   workflow-coordinator (active mode) -> plans terraform_state work items
#   collector-terraform-state -> claims items, reads .tfstate from minio over
#     the AWS SDK, emits terraform_state_snapshot/_resource facts
#   bootstrap-index (rerun) -> Phase 3.5 enqueues config_state_drift intents
#   resolution-engine -> drift handler increments counters and logs
#
# The script then asserts non-zero counter deltas for the buckets a single
# collector pass can produce:
#
#   A (added_in_state)  - state has aws_s3_bucket.unmanaged, config does not
#   B (added_in_config) - config declares aws_s3_bucket.declared, state empty
#   D (ambiguous owner) - two repos claim the same backend; WARN log
#   E (attribute_drift) - both sides declare aws_s3_bucket.logs, SSE differs
#
# Buckets C (removed_from_state) and F (removed_from_config) are deferred to
# a v2.5 follow-up because they need a two-pass collector run.
#
# Tier-2 must coexist with the Tier-1 verifier running concurrently. The
# COMPOSE_PROJECT_NAME, host port pool, and fixture paths are all distinct.
#
# Usage:
#   bash scripts/verify_tfstate_drift_compose_tier2.sh
#
# Environment knobs:
#   ESHU_KEEP_COMPOSE_STACK=true   Skip `docker compose down -v` at the end.
#   ESHU_TFSTATE_DRIFT_PROOF_OUT   Write captured artifacts to this file.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

RUNTIME_LIB="$REPO_ROOT/scripts/lib/compose_verification_runtime_common.sh"
DRIFT_LIB="$REPO_ROOT/scripts/lib/tfstate_drift_compose_common.sh"
TIER2_LIB="$REPO_ROOT/scripts/lib/tfstate_drift_tier2_compose_common.sh"

source "$RUNTIME_LIB"
source "$DRIFT_LIB"
source "$TIER2_LIB"

TMP_DIR="$(mktemp -d)"
METRICS_BEFORE_FILE="$TMP_DIR/metrics-before.txt"
METRICS_AFTER_FILE="$TMP_DIR/metrics-after.txt"
PHASE_35_LOG_FILE="$TMP_DIR/phase-3-5.log"
DRIFT_LOGS_FILE="$TMP_DIR/drift-logs.json"

KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
PROOF_OUT="${ESHU_TFSTATE_DRIFT_PROOF_OUT:-}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-tfstate-drift-tier2-187-$$}"
export COMPOSE_PROJECT_NAME

COMPOSE_CMD=()
COMPOSE_DISPLAY=""
RESERVED_HOST_PORTS=()
PICKED_PORT=""

# Counter labels we care about (asserted as substrings, label order is not
# guaranteed by the Prometheus exporter).
LABEL_PACK='pack="terraform_config_state_drift"'
LABEL_ADDED_IN_STATE="drift_kind=\"added_in_state\""
LABEL_ADDED_IN_CONFIG="drift_kind=\"added_in_config\""
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
eshu_require_tool nc
require_compose

# Always include the overlay. Authoring the overlay path once at the top keeps
# the rest of the script from repeating the -f arguments on every invocation.
COMPOSE_CMD+=(-f docker-compose.yaml -f docker-compose.tier2-tfstate.yaml)

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
	# Tier-2 deliberately starts these in a different range than Tier-1 to
	# minimize collision when both run concurrently. assign_port walks +200
	# from the start so a parallel Tier-1 just picks the next free slot.
	assign_port NEO4J_HTTP_PORT "${NEO4J_HTTP_PORT:-47474}"
	assign_port NEO4J_BOLT_PORT "${NEO4J_BOLT_PORT:-47687}"
	assign_port ESHU_POSTGRES_PORT "${ESHU_POSTGRES_PORT:-45432}"
	assign_port ESHU_HTTP_PORT "${ESHU_HTTP_PORT:-48080}"
	assign_port ESHU_MCP_PORT "${ESHU_MCP_PORT:-48081}"
	assign_port ESHU_API_METRICS_PORT "${ESHU_API_METRICS_PORT:-41464}"
	assign_port ESHU_BOOTSTRAP_METRICS_PORT "${ESHU_BOOTSTRAP_METRICS_PORT:-41467}"
	assign_port ESHU_MCP_METRICS_PORT "${ESHU_MCP_METRICS_PORT:-41468}"
	assign_port ESHU_INGESTER_METRICS_PORT "${ESHU_INGESTER_METRICS_PORT:-41465}"
	assign_port ESHU_RESOLUTION_ENGINE_METRICS_PORT "${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-41466}"
	assign_port ESHU_WORKFLOW_COORDINATOR_HTTP_PORT "${ESHU_WORKFLOW_COORDINATOR_HTTP_PORT:-48082}"
	assign_port ESHU_WORKFLOW_COORDINATOR_METRICS_PORT "${ESHU_WORKFLOW_COORDINATOR_METRICS_PORT:-41469}"
	assign_port ESHU_COLLECTOR_TFSTATE_METRICS_PORT "${ESHU_COLLECTOR_TFSTATE_METRICS_PORT:-41470}"
	assign_port MINIO_PORT "${MINIO_PORT:-49000}"
	assign_port MINIO_CONSOLE_PORT "${MINIO_CONSOLE_PORT:-49001}"
}

# -- cleanup -----------------------------------------------------------------

cleanup() {
	local exit_code=$?
	if [[ "$exit_code" -ne 0 ]]; then
		echo
		echo "tier-2 tfstate drift compose verification failed (exit=$exit_code)."
		echo "Compose project: $COMPOSE_PROJECT_NAME"
		"${COMPOSE_CMD[@]}" ps || true
		tier2_dump_failure_logs
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

echo "==> Bringing up Tier-2 compose stack (minio + collector + coordinator + base)"
"${COMPOSE_CMD[@]}" up --build -d \
	postgres nornicdb db-migrate workspace-setup \
	minio minio-init \
	bootstrap-index \
	resolution-engine \
	workflow-coordinator \
	collector-terraform-state \
	eshu

echo "==> Waiting for db-migrate to complete"
eshu_compose_wait_for_named_exit_tier2 "db-migrate" 180

echo "==> Waiting for minio-init to upload .tfstate objects"
eshu_compose_wait_for_named_exit_tier2 "minio-init" 90

echo "==> Waiting for first bootstrap-index pass to complete"
eshu_compose_wait_for_named_exit_tier2 "bootstrap-index" 240

echo "==> Asserting Git collector emitted terraform_backends facts (>=4 expected: A, B, D-a, D-b, E)"
tier2_assert_terraform_backend_facts 4 60

echo "==> Asserting workflow-coordinator planned terraform_state work items (>=3 for distinct backends A, B, E)"
tier2_wait_for_terraform_state_work_items 3 180

echo "==> Waiting for collector-terraform-state to drain work items"
tier2_wait_for_terraform_state_work_drained 240

echo "==> Asserting terraform_state_snapshot facts landed (A, B, E)"
tier2_assert_terraform_state_snapshot_facts 3

echo "==> Asserting terraform_state_resource facts landed (A: unmanaged, E: logs)"
tier2_assert_terraform_state_resource_facts 2

# Capture metrics BEFORE Phase 3.5 so the asserted deltas are caused by this
# rerun and not by any earlier drift activity.
echo "==> Capturing metrics baseline"
tfstate_drift_scrape_counters \
	"localhost:${ESHU_RESOLUTION_ENGINE_METRICS_PORT}" \
	"$METRICS_BEFORE_FILE" \
	|| true  # series may not exist yet before any drift intent fires

echo "==> Rerunning bootstrap-index so Phase 3.5 picks up the new state_snapshot scopes"
"${COMPOSE_CMD[@]}" run --rm bootstrap-index >"$PHASE_35_LOG_FILE" 2>&1 \
	|| {
		echo "bootstrap-index rerun failed; tail of output:" >&2
		tail -n 60 "$PHASE_35_LOG_FILE" >&2
		exit 1
	}

echo "==> Confirming Phase 3.5 enqueued the collector-produced state_snapshot scopes"
grep -E 'config_state_drift_intents_enqueued count=[0-9]+' "$PHASE_35_LOG_FILE" \
	| tail -n 1 \
	| tee -a "$PHASE_35_LOG_FILE.summary"
phase35_count="$(
	grep -E 'config_state_drift_intents_enqueued count=[0-9]+' "$PHASE_35_LOG_FILE" \
	| tail -n 1 \
	| sed -E 's/.*count=([0-9]+).*/\1/'
)"
if [[ -z "$phase35_count" || "$phase35_count" -lt 3 ]]; then
	echo "Phase 3.5 enqueued count=${phase35_count:-<missing>}, expected >=3 (A, B, E)" >&2
	tail -n 60 "$PHASE_35_LOG_FILE" >&2
	exit 1
fi

echo "==> Waiting for reducer to drain config_state_drift intents"
tfstate_drift_wait_for_reducer_drain 180

# Give the OTLP/Prometheus exporter a moment to flush counter increments.
sleep 5

echo "==> Scraping metrics after drift drain"
tfstate_drift_scrape_counters \
	"localhost:${ESHU_RESOLUTION_ENGINE_METRICS_PORT}" \
	"$METRICS_AFTER_FILE"

echo "==> Extracting drift log lines from resolution-engine"
tfstate_drift_extract_drift_logs "$DRIFT_LOGS_FILE"

# -- assertions --------------------------------------------------------------

echo "==> Asserting per-kind counter deltas (A, B, E) via compose runtime ${COMPOSE_DISPLAY}"
# Iterate over the (kind, label) pairs so the assertion uses the same label
# literals declared at the top of the script. Keeping the literals out of
# the loop body lets readers grep for which drift_kind values bucket
# assertions actually cover.
for entry in \
	"added_in_state:$LABEL_ADDED_IN_STATE" \
	"added_in_config:$LABEL_ADDED_IN_CONFIG" \
	"attribute_drift:$LABEL_ATTRIBUTE_DRIFT"; do
	kind="${entry%%:*}"
	kind_label="${entry#*:}"
	value_after="$(
		tfstate_drift_counter_value "$METRICS_AFTER_FILE" \
			'^eshu_dp_correlation_drift_detected_total\{' \
			"$LABEL_PACK" \
			"$kind_label"
	)"
	value_before="$(
		tfstate_drift_counter_value "$METRICS_BEFORE_FILE" \
			'^eshu_dp_correlation_drift_detected_total\{' \
			"$LABEL_PACK" \
			"$kind_label"
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

echo "==> Asserting ambiguous-owner rejection log (bucket D)"
tfstate_drift_assert_log_line "$DRIFT_LOGS_FILE" \
	'ambiguous_backend_owner' \
	'ambiguous-owner WARN with failure_class=ambiguous_backend_owner'

echo "==> Asserting each admitted drift kind appears in the structured log"
for kind in added_in_state added_in_config attribute_drift; do
	tfstate_drift_assert_log_line "$DRIFT_LOGS_FILE" \
		"\"drift.kind\":\"$kind\"" \
		"drift candidate admitted log for drift_kind=$kind"
done

echo "==> Asserting drift admission carries high-cardinality address field"
for address in aws_s3_bucket.unmanaged aws_s3_bucket.declared aws_s3_bucket.logs; do
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
		echo "# Tier-2 tfstate drift compose proof matrix"
		echo
		echo "Captured: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
		echo "Compose project: \`$COMPOSE_PROJECT_NAME\`"
		echo "Worktree HEAD: \`$(git rev-parse --short HEAD)\` on $(git rev-parse --abbrev-ref HEAD)"
		echo
		echo "## Phase 3.5 enqueue log (post-collector run)"
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
echo "OK — Tier-2 tfstate drift compose proof matrix passed."
