#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
API_PORT="${ESHU_HTTP_PORT:-8080}"
JAEGER_PORT="${JAEGER_UI_PORT:-16686}"
JAEGER_URL="http://localhost:${JAEGER_PORT}"
TMP_DIR="$(mktemp -d)"
STATUS_FILE="$TMP_DIR/status.json"
RESPONSE_FILE="$TMP_DIR/refinalize-response.json"
SCOPE_ID_FILE="$TMP_DIR/scope_id.txt"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
COMPOSE_CMD=()
COMPOSE_DISPLAY=""

cleanup() {
    local exit_code=$?
    if [[ "$exit_code" -ne 0 ]]; then
        local scope_id=""
        if [[ -f "$SCOPE_ID_FILE" ]]; then
            scope_id="$(<"$SCOPE_ID_FILE")"
        fi
        echo
        echo "Admin refinalize compose verification failed."
        if [[ -n "$scope_id" ]]; then
            echo "Selected scope_id: $scope_id"
        fi
        echo "Useful logs:"
        echo "  $COMPOSE_DISPLAY logs --tail=200 eshu"
        echo "  $COMPOSE_DISPLAY logs --tail=200 ingester"
        echo "  $COMPOSE_DISPLAY logs --tail=200 resolution-engine"
        echo "Jaeger UI: $JAEGER_URL"
        if [[ -f "$STATUS_FILE" ]]; then
            echo "Last admin status payload:"
            cat "$STATUS_FILE"
        fi
        if [[ -f "$RESPONSE_FILE" ]]; then
            echo "Refinalize response:"
            cat "$RESPONSE_FILE"
        fi
    fi

    if [[ "$KEEP_STACK" != "true" ]]; then
        "${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
    fi
    rm -rf "$TMP_DIR"
    exit "$exit_code"
}
trap cleanup EXIT

require_tool() {
    local tool_name="$1"
    if ! command -v "$tool_name" >/dev/null 2>&1; then
        echo "Missing required tool: $tool_name" >&2
        exit 1
    fi
}

wait_for_http() {
    local url="$1"
    local attempts="$2"
    local sleep_seconds="$3"

    for ((attempt = 1; attempt <= attempts; attempt++)); do
        if curl -fsS "$url" >/dev/null 2>&1; then
            return 0
        fi
        sleep "$sleep_seconds"
    done
    echo "Timed out waiting for $url" >&2
    return 1
}

wait_for_bootstrap_exit() {
    local timeout_seconds="$1"
    local deadline=$((SECONDS + timeout_seconds))

    while ((SECONDS < deadline)); do
        local container_id
        container_id="$("${COMPOSE_CMD[@]}" ps -a -q bootstrap-index)"
        if [[ -z "$container_id" ]]; then
            sleep 2
            continue
        fi
        local state
        state="$(docker inspect --format='{{.State.Status}}' "$container_id")"
        if [[ "$state" == "exited" ]]; then
            local exit_code
            exit_code="$(docker inspect --format='{{.State.ExitCode}}' "$container_id")"
            if [[ "$exit_code" != "0" ]]; then
                echo "bootstrap-index exited with code $exit_code" >&2
                return 1
            fi
            return 0
        fi
        sleep 2
    done

    echo "Timed out waiting for bootstrap-index to finish" >&2
    return 1
}

pick_port() {
    local start_port="$1"
    local port
    for ((port = start_port; port < start_port + 200; port++)); do
        if ! nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
            echo "$port"
            return 0
        fi
    done
    echo "no free port found near $start_port" >&2
    return 1
}

logs_contain() {
    local pattern="$1"
    local logs_output

    logs_output="$("${COMPOSE_CMD[@]}" logs ingester 2>&1 || true)"
    printf '%s\n' "$logs_output" | rg -q --fixed-strings "$pattern"
}

read_scope_id() {
    "${COMPOSE_CMD[@]}" exec -T postgres sh -lc \
        "psql -U eshu -d eshu -Atc \"SELECT scope_id FROM ingestion_scopes WHERE active_generation_id IS NOT NULL ORDER BY observed_at DESC LIMIT 1\""
}

require_tool docker
require_tool curl
require_tool rg
require_tool nc

if docker compose version >/dev/null 2>&1; then
    COMPOSE_CMD=(docker compose)
    COMPOSE_DISPLAY="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE_CMD=(docker-compose)
    COMPOSE_DISPLAY="docker-compose"
else
    echo "Missing required compose command: docker compose or docker-compose" >&2
    exit 1
fi
COMPOSE_CMD+=(-f docker-compose.yaml -f docker-compose.telemetry.yml)
COMPOSE_DISPLAY+=" -f docker-compose.yaml -f docker-compose.telemetry.yml"

configure_ports() {
    export NEO4J_HTTP_PORT="$(pick_port "${NEO4J_HTTP_PORT:-17474}")"
    export NEO4J_BOLT_PORT="$(pick_port "${NEO4J_BOLT_PORT:-17687}")"
    export ESHU_POSTGRES_PORT="$(pick_port "${ESHU_POSTGRES_PORT:-25432}")"
    export ESHU_HTTP_PORT="$(pick_port "${ESHU_HTTP_PORT:-18080}")"
    export JAEGER_UI_PORT="$(pick_port "${JAEGER_UI_PORT:-26686}")"
    export OTEL_COLLECTOR_OTLP_GRPC_PORT="$(pick_port "${OTEL_COLLECTOR_OTLP_GRPC_PORT:-24317}")"
    export OTEL_COLLECTOR_OTLP_HTTP_PORT="$(pick_port "${OTEL_COLLECTOR_OTLP_HTTP_PORT:-24318}")"
    export OTEL_COLLECTOR_PROMETHEUS_PORT="$(pick_port "${OTEL_COLLECTOR_PROMETHEUS_PORT:-29464}")"

    API_PORT="$ESHU_HTTP_PORT"
    JAEGER_PORT="$JAEGER_UI_PORT"
    JAEGER_URL="http://localhost:${JAEGER_PORT}"
}

cd "$REPO_ROOT"

"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
compose_started=false
for attempt in 1 2; do
    configure_ports
    echo "Starting local compose stack..."
    echo "Using host ports: api=$ESHU_HTTP_PORT postgres=$ESHU_POSTGRES_PORT neo4j_http=$NEO4J_HTTP_PORT neo4j_bolt=$NEO4J_BOLT_PORT jaeger=$JAEGER_UI_PORT"
    if "${COMPOSE_CMD[@]}" up -d --build; then
        compose_started=true
        break
    fi
    "${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
    if [[ "$attempt" -eq 2 ]]; then
        break
    fi
    echo "Compose startup failed; retrying with fresh ports..."
    sleep 2
done

if [[ "$compose_started" != "true" ]]; then
    echo "Could not start the local compose stack after retrying." >&2
    exit 1
fi

echo "Waiting for bootstrap indexing to finish..."
wait_for_bootstrap_exit 600

echo "Waiting for API health..."
wait_for_http "http://localhost:${API_PORT}/health" 60 2

echo "Waiting for ingester health..."
"${COMPOSE_CMD[@]}" exec -T ingester curl -fsS http://localhost:8080/healthz >/dev/null

echo "Selecting a live scope from the compose Postgres state..."
read_scope_id >"$SCOPE_ID_FILE"
if [[ ! -s "$SCOPE_ID_FILE" ]]; then
    echo "Could not read an active scope_id from Postgres" >&2
    exit 1
fi
SCOPE_ID="$(<"$SCOPE_ID_FILE")"

echo "Capturing ingester admin status before refinalize..."
"${COMPOSE_CMD[@]}" exec -T ingester curl -fsS http://localhost:8080/admin/status >"$STATUS_FILE"

echo "Calling Go-owned ingester /admin/refinalize for scope_id=$SCOPE_ID ..."
"${COMPOSE_CMD[@]}" exec -T ingester sh -lc \
    "curl -fsS -X POST http://localhost:8080/admin/refinalize -H 'content-type: application/json' -d '{\"scope_ids\":[\"$SCOPE_ID\"]}'" \
    >"$RESPONSE_FILE"

rg -q '"status"[[:space:]]*:[[:space:]]*"enqueued"' "$RESPONSE_FILE"
rg -q "\"$SCOPE_ID\"" "$RESPONSE_FILE"

echo "Capturing ingester admin status after refinalize..."
"${COMPOSE_CMD[@]}" exec -T ingester curl -fsS http://localhost:8080/admin/status >"$STATUS_FILE"

echo "Verifying ingester logs mention the refinalized scope..."
logs_contain "$SCOPE_ID"

echo
echo "Admin refinalize compose verification passed."
echo "scope_id: $SCOPE_ID"
echo "Jaeger UI: $JAEGER_URL"
echo "Stack teardown: $COMPOSE_DISPLAY down -v"
