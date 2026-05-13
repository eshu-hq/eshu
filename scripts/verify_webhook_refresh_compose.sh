#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNTIME_LIB="$REPO_ROOT/scripts/lib/compose_verification_runtime_common.sh"
ASSERT_LIB="$REPO_ROOT/scripts/lib/compose_verification_assertions.sh"

TMP_DIR="$(mktemp -d)"
TMP_DIR="$(cd "$TMP_DIR" && pwd -P)"
FIXTURE_ROOT="$TMP_DIR/fixtures"
SOURCE_REPO="$TMP_DIR/source-repo"
REMOTE_REPO="$FIXTURE_ROOT/remotes/eshu.git"
INITIAL_CONTENT_FILE="$TMP_DIR/initial-content.json"
REFRESH_CONTENT_FILE="$TMP_DIR/refresh-content.json"
INDEX_STATUS_FILE="$TMP_DIR/index-status.json"
WEBHOOK_RESPONSE_FILE="$TMP_DIR/webhook-response.json"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-webhook-refresh-$$}"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"

NEO4J_HTTP_PORT_BASE="${NEO4J_HTTP_PORT:-27474}"
NEO4J_BOLT_PORT_BASE="${NEO4J_BOLT_PORT:-27687}"
ESHU_POSTGRES_PORT_BASE="${ESHU_POSTGRES_PORT:-25432}"
ESHU_HTTP_PORT_BASE="${ESHU_HTTP_PORT:-18080}"
ESHU_API_METRICS_PORT_BASE="${ESHU_API_METRICS_PORT:-19464}"
ESHU_INGESTER_METRICS_PORT_BASE="${ESHU_INGESTER_METRICS_PORT:-19465}"
ESHU_RESOLUTION_ENGINE_METRICS_PORT_BASE="${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-19466}"
ESHU_WEBHOOK_LISTENER_HTTP_PORT_BASE="${ESHU_WEBHOOK_LISTENER_HTTP_PORT:-18083}"

API_BASE_URL=""
API_KEY=""
WEBHOOK_SECRET="webhook-refresh-compose-secret"
BEFORE_SHA=""
AFTER_SHA=""
INITIAL_GENERATION_COUNT="0"
COMPOSE_CMD=()
COMPOSE_DISPLAY=""

# shellcheck source=scripts/lib/compose_verification_runtime_common.sh disable=SC1091
source "$RUNTIME_LIB"
# shellcheck source=scripts/lib/compose_verification_assertions.sh disable=SC1091
source "$ASSERT_LIB"

cleanup() {
	local exit_code=$?
	if [[ "$exit_code" -ne 0 ]]; then
		echo
		echo "webhook refresh compose verification failed."
		echo "Useful files:"
		echo "  initial content response: $INITIAL_CONTENT_FILE"
		echo "  refresh content response: $REFRESH_CONTENT_FILE"
		echo "  webhook response: $WEBHOOK_RESPONSE_FILE"
		echo "  index status: $INDEX_STATUS_FILE"
		echo "Useful logs:"
		echo "  $COMPOSE_DISPLAY logs --tail=200 webhook-listener"
		echo "  $COMPOSE_DISPLAY logs --tail=200 ingester"
		echo "  $COMPOSE_DISPLAY logs --tail=200 resolution-engine"
		echo "  $COMPOSE_DISPLAY logs --tail=200 eshu"
	fi

	if [[ "$KEEP_STACK" != "true" ]]; then
		if ((${#COMPOSE_CMD[@]} > 0)); then
			"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
		fi
		rm -rf "$TMP_DIR"
	else
		echo "Keeping compose stack and fixture root: $TMP_DIR"
	fi
	exit "$exit_code"
}
trap cleanup EXIT

configure_ports() {
	eshu_reset_reserved_ports
	eshu_assign_reserved_port NEO4J_HTTP_PORT "$NEO4J_HTTP_PORT_BASE"
	eshu_assign_reserved_port NEO4J_BOLT_PORT "$NEO4J_BOLT_PORT_BASE"
	eshu_assign_reserved_port ESHU_POSTGRES_PORT "$ESHU_POSTGRES_PORT_BASE"
	eshu_assign_reserved_port ESHU_HTTP_PORT "$ESHU_HTTP_PORT_BASE"
	eshu_assign_reserved_port ESHU_API_METRICS_PORT "$ESHU_API_METRICS_PORT_BASE"
	eshu_assign_reserved_port ESHU_INGESTER_METRICS_PORT "$ESHU_INGESTER_METRICS_PORT_BASE"
	eshu_assign_reserved_port ESHU_RESOLUTION_ENGINE_METRICS_PORT "$ESHU_RESOLUTION_ENGINE_METRICS_PORT_BASE"
	eshu_assign_reserved_port ESHU_WEBHOOK_LISTENER_HTTP_PORT "$ESHU_WEBHOOK_LISTENER_HTTP_PORT_BASE"
	API_BASE_URL="http://127.0.0.1:${ESHU_HTTP_PORT}/api/v0"
}

api_get() {
	local path="$1"
	local output_file="$2"
	local -a curl_args=(-fsS "$API_BASE_URL$path")
	if [[ -n "$API_KEY" ]]; then
		curl_args=(-fsS -H "Authorization: Bearer $API_KEY" "$API_BASE_URL$path")
	fi
	curl "${curl_args[@]}" >"$output_file"
}

psql_scalar() {
	local query="$1"
	"${COMPOSE_CMD[@]}" exec -T postgres psql -U eshu -d eshu -Atqc "$query"
}

wait_for_sql_value() {
	local query="$1" expected="$2" attempts="$3" sleep_seconds="$4"
	local result=""
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		result="$(psql_scalar "$query" | tr -d '[:space:]')"
		if [[ "$result" == "$expected" ]]; then
			return 0
		fi
		/bin/sleep "$sleep_seconds"
	done
	echo "Timed out waiting for SQL value $expected: $query" >&2
	echo "Last result: ${result:-<empty>}" >&2
	return 1
}

wait_for_generation_count_gt() {
	local baseline="$1" attempts="$2" sleep_seconds="$3"
	local result=""
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		result="$(generation_count | tr -d '[:space:]')"
		if [[ "$result" =~ ^[0-9]+$ ]] && ((result > baseline)); then
			return 0
		fi
		/bin/sleep "$sleep_seconds"
	done
	echo "Timed out waiting for generation count to exceed $baseline" >&2
	echo "Last result: ${result:-<empty>}" >&2
	return 1
}

generation_count() {
	psql_scalar "SELECT COUNT(*) FROM scope_generations WHERE scope_id IN (SELECT scope_id FROM ingestion_scopes WHERE collector_kind = 'git')"
}

prepare_fixture_remote() {
	mkdir -p "$FIXTURE_ROOT/remotes" "$SOURCE_REPO"
	git -C "$SOURCE_REPO" -c init.defaultBranch=main init >/dev/null
	git -C "$SOURCE_REPO" config user.email "proof@example.test"
	git -C "$SOURCE_REPO" config user.name "Webhook Proof"
	cat >"$SOURCE_REPO/app.py" <<'PY'
def webhook_refresh_marker_v1():
    return "webhook-refresh-compose-v1"
PY
	git -C "$SOURCE_REPO" add app.py
	git -C "$SOURCE_REPO" commit -m "initial webhook proof fixture" >/dev/null
	BEFORE_SHA="$(git -C "$SOURCE_REPO" rev-parse HEAD)"
	git -c init.defaultBranch=main init --bare "$REMOTE_REPO" >/dev/null
	git -C "$SOURCE_REPO" remote add origin "$REMOTE_REPO"
	git -C "$SOURCE_REPO" push origin main >/dev/null
	git --git-dir "$REMOTE_REPO" symbolic-ref HEAD refs/heads/main
}

advance_fixture_remote() {
	cat >"$SOURCE_REPO/app.py" <<'PY'
def webhook_refresh_marker_v2():
    return "webhook-refresh-compose-v2"
PY
	git -C "$SOURCE_REPO" add app.py
	git -C "$SOURCE_REPO" commit -m "refresh webhook proof fixture" >/dev/null
	AFTER_SHA="$(git -C "$SOURCE_REPO" rev-parse HEAD)"
	git -C "$SOURCE_REPO" push origin main >/dev/null
}

start_compose_data_plane() {
	echo "Starting webhook proof data plane..."
	"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
	"${COMPOSE_CMD[@]}" build db-migrate workspace-setup eshu ingester resolution-engine webhook-listener
	"${COMPOSE_CMD[@]}" up -d postgres nornicdb
	eshu_compose_wait_for_http "http://127.0.0.1:${NEO4J_HTTP_PORT}/health" 120 2
	"${COMPOSE_CMD[@]}" run --rm db-migrate
	"${COMPOSE_CMD[@]}" run --rm workspace-setup
}

seed_managed_checkout() {
	echo "Seeding managed Git checkout in the shared workspace volume..."
	"${COMPOSE_CMD[@]}" run --rm --no-deps --entrypoint /bin/sh eshu -lc '
		set -eu
		rm -rf /data/repos/eshu-hq/eshu
		mkdir -p /data/repos/eshu-hq
		git clone /fixtures/remotes/eshu.git /data/repos/eshu-hq/eshu
	'
}

start_query_and_projection_runtimes() {
	echo "Starting API, resolution-engine, and ingester..."
	"${COMPOSE_CMD[@]}" up -d --no-deps eshu resolution-engine ingester
	eshu_compose_wait_for_http "http://127.0.0.1:${ESHU_HTTP_PORT}/health" 120 2
	API_KEY="$(eshu_compose_read_api_key || true)"
}

start_webhook_listener() {
	echo "Starting webhook listener..."
	"${COMPOSE_CMD[@]}" up -d --no-deps webhook-listener
	eshu_compose_wait_for_http "http://127.0.0.1:${ESHU_WEBHOOK_LISTENER_HTTP_PORT}/healthz" 120 2
}

wait_for_content_marker() {
	local marker="$1" output_file="$2"
	for ((attempt = 1; attempt <= 120; attempt++)); do
		eshu_api_post_json "/content/files/search" "{\"pattern\":\"$marker\",\"limit\":10}" "$output_file" || true
		if jq -e '(.count // 0) >= 1' "$output_file" >/dev/null 2>&1; then
			return 0
		fi
		/bin/sleep 2
	done
	echo "Timed out waiting for content marker $marker" >&2
	cat "$output_file" >&2 || true
	return 1
}

send_signed_github_push() {
	local payload signature_hex signature
	payload="$(jq -cn \
		--arg before "$BEFORE_SHA" \
		--arg after "$AFTER_SHA" \
		'{
			ref: "refs/heads/main",
			before: $before,
			after: $after,
			repository: {
				id: 42,
				full_name: "eshu-hq/eshu",
				default_branch: "main"
			},
			sender: {login: "linuxdynasty"}
		}')"
	signature_hex="$(printf '%s' "$payload" | openssl dgst -sha256 -hmac "$WEBHOOK_SECRET" | awk '{print $NF}')"
	signature="sha256=$signature_hex"
	curl -fsS \
		-X POST \
		-H "Content-Type: application/json" \
		-H "X-GitHub-Event: push" \
		-H "X-GitHub-Delivery: webhook-refresh-compose-1" \
		-H "X-Hub-Signature-256: $signature" \
		-d "$payload" \
		"http://127.0.0.1:${ESHU_WEBHOOK_LISTENER_HTTP_PORT}/webhooks/github" \
		>"$WEBHOOK_RESPONSE_FILE"
}

configure_ports
export COMPOSE_PROJECT_NAME
export ESHU_FILESYSTEM_HOST_ROOT="$FIXTURE_ROOT"
export ESHU_FILESYSTEM_ROOT="/data/repos"
export ESHU_FILESYSTEM_DIRECT="true"
export ESHU_REPO_SOURCE_MODE="filesystem"
export ESHU_REPOSITORY_RULES_JSON='[]'
export ESHU_WEBHOOK_GITHUB_SECRET="$WEBHOOK_SECRET"
export ESHU_WEBHOOK_DEFAULT_BRANCH="main"
export ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED="true"

eshu_require_tool awk
eshu_require_tool curl
eshu_require_tool docker
eshu_require_tool git
eshu_require_tool jq
eshu_require_tool nc
eshu_require_tool openssl

require_compose_profile_support() {
	local -a candidate_cmd=("$@")
	local help_text
	help_text="$("${candidate_cmd[@]}" --help 2>&1 || true)"
	if [[ "$help_text" == *"--profile"* ]]; then
		return 0
	fi
	echo "compose binary '${candidate_cmd[*]}' does not support --profile;" >&2
	echo "the webhook refresh verifier requires Docker Compose v2 or docker-compose >= 1.28" >&2
	return 1
}

if docker compose version >/dev/null 2>&1; then
	require_compose_profile_support docker compose
	COMPOSE_CMD=(docker compose)
	COMPOSE_DISPLAY="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
	require_compose_profile_support docker-compose
	COMPOSE_CMD=(docker-compose)
	COMPOSE_DISPLAY="docker-compose"
else
	echo "Missing required compose command: docker compose or docker-compose" >&2
	exit 1
fi
COMPOSE_CMD+=(--profile webhook-listener)
COMPOSE_DISPLAY="$COMPOSE_DISPLAY --profile webhook-listener"

cd "$REPO_ROOT"

prepare_fixture_remote
start_compose_data_plane
seed_managed_checkout
start_query_and_projection_runtimes
echo "Waiting for initial indexed content..."
eshu_compose_wait_for_index_completion 180 2 "$INDEX_STATUS_FILE"
wait_for_content_marker "webhook-refresh-compose-v1" "$INITIAL_CONTENT_FILE"
INITIAL_GENERATION_COUNT="$(generation_count | tr -d '[:space:]')"

echo "Stopping ingester before advancing the remote so only the webhook path wakes the refresh."
"${COMPOSE_CMD[@]}" stop ingester >/dev/null
advance_fixture_remote
start_webhook_listener
send_signed_github_push
eshu_assert_json_query "$WEBHOOK_RESPONSE_FILE" '
	(.decision // "") == "accepted" and
	(.status // "") == "queued" and
	(.trigger_id // "") != ""
' "webhook listener did not accept and queue the signed GitHub push"

echo "Restarting ingester to claim the queued webhook trigger..."
"${COMPOSE_CMD[@]}" up -d --no-deps ingester
wait_for_sql_value "SELECT status FROM webhook_refresh_triggers WHERE delivery_id = 'webhook-refresh-compose-1'" "handed_off" 120 2
wait_for_generation_count_gt "$INITIAL_GENERATION_COUNT" 120 2
eshu_compose_wait_for_index_completion 180 2 "$INDEX_STATUS_FILE"
wait_for_content_marker "webhook-refresh-compose-v2" "$REFRESH_CONTENT_FILE"

echo
echo "webhook refresh compose verification passed."
echo "API: http://127.0.0.1:${ESHU_HTTP_PORT}"
echo "Webhook listener: http://127.0.0.1:${ESHU_WEBHOOK_LISTENER_HTTP_PORT}"
