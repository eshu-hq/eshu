#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
TMP_DIR="$(mktemp -d)"
FIXTURE_ROOT="$TMP_DIR/fixtures"
PROOF_REPO_ROOT="$FIXTURE_ROOT/proof-repo"
COLLECTOR_LOG="$TMP_DIR/collector-git.log"
POSTGRES_PORT_BASE="${ESHU_POSTGRES_PORT:-25432}"
NEO4J_HTTP_PORT_BASE="${NEO4J_HTTP_PORT:-27474}"
NEO4J_BOLT_PORT_BASE="${NEO4J_BOLT_PORT:-27687}"
JAEGER_UI_PORT_BASE="${JAEGER_UI_PORT:-26686}"
OTEL_COLLECTOR_OTLP_GRPC_PORT_BASE="${OTEL_COLLECTOR_OTLP_GRPC_PORT:-24317}"
OTEL_COLLECTOR_OTLP_HTTP_PORT_BASE="${OTEL_COLLECTOR_OTLP_HTTP_PORT:-24318}"
OTEL_COLLECTOR_PROMETHEUS_PORT_BASE="${OTEL_COLLECTOR_PROMETHEUS_PORT:-29464}"
ESHU_COLLECTOR_GIT_HTTP_PORT_BASE="${ESHU_COLLECTOR_GIT_HTTP_PORT:-28080}"
ESHU_COLLECTOR_GIT_METRICS_PORT_BASE="${ESHU_COLLECTOR_GIT_METRICS_PORT:-29467}"
COMPOSE_CMD=()
COMPOSE_DISPLAY=""
COLLECTOR_PID=""

cleanup() {
    local exit_code=$?
    if [[ -n "$COLLECTOR_PID" ]] && kill -0 "$COLLECTOR_PID" >/dev/null 2>&1; then
        kill "$COLLECTOR_PID" >/dev/null 2>&1 || true
        wait "$COLLECTOR_PID" >/dev/null 2>&1 || true
    fi

    if [[ "$exit_code" -ne 0 ]]; then
        echo
        echo "collector-git runtime compose verification failed."
        echo "Useful logs:"
        echo "  collector log: $COLLECTOR_LOG"
        if [[ -s "$COLLECTOR_LOG" ]]; then
            echo "  collector log tail:"
            tail -n 200 "$COLLECTOR_LOG" || true
        fi
        echo "  $COMPOSE_DISPLAY logs --tail=200 postgres neo4j otel-collector jaeger"
        echo "  http://127.0.0.1:${ESHU_COLLECTOR_GIT_HTTP_PORT}/healthz"
        echo "  http://127.0.0.1:${ESHU_COLLECTOR_GIT_HTTP_PORT}/readyz"
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

wait_for_http() {
    local url="$1"
    local attempts="$2"
    local sleep_seconds="$3"

    for ((attempt = 1; attempt <= attempts; attempt++)); do
        if curl -fsS "$url" >/dev/null 2>&1; then
            return 0
        fi
        if [[ -n "$COLLECTOR_PID" ]] && ! kill -0 "$COLLECTOR_PID" >/dev/null 2>&1; then
            echo "collector-git exited before $url became ready" >&2
            return 1
        fi
        sleep "$sleep_seconds"
    done
    echo "Timed out waiting for $url" >&2
    return 1
}

wait_for_postgres() {
    local attempts="$1"
    for ((attempt = 1; attempt <= attempts; attempt++)); do
        if "${COMPOSE_CMD[@]}" exec -T postgres pg_isready -U eshu -d eshu >/dev/null 2>&1; then
            return 0
        fi
        sleep 2
    done
    echo "Timed out waiting for postgres readiness" >&2
    return 1
}

wait_for_neo4j() {
    local attempts="$1"
    for ((attempt = 1; attempt <= attempts; attempt++)); do
        if "${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell -u neo4j -p "${ESHU_NEO4J_PASSWORD:-change-me}" "RETURN 1" >/dev/null 2>&1; then
            return 0
        fi
        sleep 2
    done
    echo "Timed out waiting for neo4j readiness" >&2
    return 1
}

psql_scalar() {
    local query="$1"
    "${COMPOSE_CMD[@]}" exec -T postgres psql -U eshu -d eshu -Atqc "$query"
}

wait_for_sql_positive() {
    local query="$1"
    local attempts="$2"
    local sleep_seconds="$3"
    local result=""

    for ((attempt = 1; attempt <= attempts; attempt++)); do
        result="$(psql_scalar "$query" | tr -d '[:space:]')"
        if [[ "$result" =~ ^[1-9][0-9]*$ ]]; then
            return 0
        fi
        sleep "$sleep_seconds"
    done

    echo "Timed out waiting for SQL query to return a positive count: $query" >&2
    echo "Last result: ${result:-<empty>}" >&2
    return 1
}

configure_runtime_addresses() {
    export ESHU_POSTGRES_PORT="$(pick_port "$POSTGRES_PORT_BASE")"
    export NEO4J_HTTP_PORT="$(pick_port "$NEO4J_HTTP_PORT_BASE")"
    export NEO4J_BOLT_PORT="$(pick_port "$NEO4J_BOLT_PORT_BASE")"
    export JAEGER_UI_PORT="$(pick_port "$JAEGER_UI_PORT_BASE")"
    export OTEL_COLLECTOR_OTLP_GRPC_PORT="$(pick_port "$OTEL_COLLECTOR_OTLP_GRPC_PORT_BASE")"
    export OTEL_COLLECTOR_OTLP_HTTP_PORT="$(pick_port "$OTEL_COLLECTOR_OTLP_HTTP_PORT_BASE")"
    export OTEL_COLLECTOR_PROMETHEUS_PORT="$(pick_port "$OTEL_COLLECTOR_PROMETHEUS_PORT_BASE")"
    export ESHU_COLLECTOR_GIT_HTTP_PORT="$(pick_port "$ESHU_COLLECTOR_GIT_HTTP_PORT_BASE")"
    export ESHU_COLLECTOR_GIT_METRICS_PORT="$(pick_port "$ESHU_COLLECTOR_GIT_METRICS_PORT_BASE")"
    export ESHU_HTTP_PORT="$ESHU_COLLECTOR_GIT_HTTP_PORT"
    export ESHU_API_METRICS_PORT="$ESHU_COLLECTOR_GIT_METRICS_PORT"
}

start_compose_infra() {
    for attempt in 1 2; do
        configure_runtime_addresses
        echo "Starting compose-backed infrastructure..."
        echo "Using host ports: postgres=$ESHU_POSTGRES_PORT neo4j_http=$NEO4J_HTTP_PORT neo4j_bolt=$NEO4J_BOLT_PORT jaeger=$JAEGER_UI_PORT collector_http=$ESHU_COLLECTOR_GIT_HTTP_PORT"
        "${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
        if "${COMPOSE_CMD[@]}" up -d postgres neo4j jaeger otel-collector; then
            return 0
        fi
        if [[ "$attempt" -eq 2 ]]; then
            break
        fi
        echo "Compose startup failed; retrying with a clean stack..."
        sleep 2
    done
    echo "Could not start compose-backed infrastructure after retrying." >&2
    return 1
}

bootstrap_data_plane_schema() {
    echo "Applying Go data-plane schema bootstrap..."
    (
        cd "$REPO_ROOT"
        "${COMPOSE_CMD[@]}" run --rm --no-deps eshu /usr/local/bin/eshu-bootstrap-data-plane
    )
}

prepare_workspace_volume() {
    echo "Preparing shared workspace volume..."
    (
        cd "$REPO_ROOT"
        "${COMPOSE_CMD[@]}" run --rm --no-deps workspace-setup
    )
}

prepare_fixture_root() {
    mkdir -p "$PROOF_REPO_ROOT"
    cp "$REPO_ROOT/tests/fixtures/ecosystems/code_only_python/app.py" "$PROOF_REPO_ROOT/app.py"
    export ESHU_FILESYSTEM_HOST_ROOT="$FIXTURE_ROOT"
}

start_collector() {
    echo "Launching collector-git runtime..."
    (
        cd "$REPO_ROOT"
        "${COMPOSE_CMD[@]}" run --rm --no-deps --service-ports eshu /usr/local/bin/eshu-collector-git >"$COLLECTOR_LOG" 2>&1
    ) &
    COLLECTOR_PID="$!"
}

verify_collector_outputs() {
    local latest_scope_subquery
    latest_scope_subquery="(SELECT scope_id FROM ingestion_scopes WHERE collector_kind = 'git' ORDER BY ingested_at DESC, observed_at DESC LIMIT 1)"

    echo "Waiting for collector facts and projector work to land in Postgres..."
    wait_for_sql_positive "SELECT COUNT(*) FROM ingestion_scopes WHERE scope_id = $latest_scope_subquery AND collector_kind = 'git'" 120 1
    wait_for_sql_positive "SELECT COUNT(*) FROM scope_generations WHERE scope_id = $latest_scope_subquery" 120 1
    wait_for_sql_positive "SELECT COUNT(*) FROM fact_records WHERE scope_id = $latest_scope_subquery AND fact_kind = 'repository'" 120 1
    wait_for_sql_positive "SELECT COUNT(*) FROM fact_work_items WHERE scope_id = $latest_scope_subquery AND stage = 'projector'" 120 1
}

require_tool curl
require_tool docker
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
COMPOSE_CMD+=(-f docker-compose.neo4j.yml -f docker-compose.telemetry.yml)
COMPOSE_DISPLAY+=" -f docker-compose.neo4j.yml -f docker-compose.telemetry.yml"

cd "$REPO_ROOT"

prepare_fixture_root
start_compose_infra
wait_for_postgres 60
wait_for_neo4j 60
prepare_workspace_volume
bootstrap_data_plane_schema
start_collector
wait_for_http "http://127.0.0.1:${ESHU_COLLECTOR_GIT_HTTP_PORT}/healthz" 60 1
wait_for_http "http://127.0.0.1:${ESHU_COLLECTOR_GIT_HTTP_PORT}/readyz" 60 1
verify_collector_outputs

echo
echo "collector-git runtime compose verification passed."
echo "Collector admin: http://127.0.0.1:${ESHU_COLLECTOR_GIT_HTTP_PORT}"
echo "Collector log: $COLLECTOR_LOG"
echo "Jaeger UI: http://localhost:${JAEGER_UI_PORT}"
