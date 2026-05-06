#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
TMP_DIR="$(mktemp -d)"
REDUCER_LOG="$TMP_DIR/reducer.log"
STATUS_FILE="$TMP_DIR/reducer-status.json"
METRICS_FILE="$TMP_DIR/reducer-metrics.txt"
COMPOSE_CMD=()
COMPOSE_DISPLAY=""
REDUCER_PID=""
POSTGRES_DSN=""
REDUCER_MAX_ATTEMPTS=1

POSTGRES_PORT_BASE="${ESHU_POSTGRES_PORT:-25432}"
ESHU_REDUCER_HTTP_PORT_BASE="${ESHU_REDUCER_HTTP_PORT:-28082}"
NEO4J_HTTP_PORT_BASE="${NEO4J_HTTP_PORT:-27474}"
NEO4J_BOLT_PORT_BASE="${NEO4J_BOLT_PORT:-27687}"

cleanup() {
    local exit_code=$?
    if [[ -n "$REDUCER_PID" ]] && kill -0 "$REDUCER_PID" >/dev/null 2>&1; then
        kill "$REDUCER_PID" >/dev/null 2>&1 || true
        wait "$REDUCER_PID" >/dev/null 2>&1 || true
    fi

    if [[ "$exit_code" -ne 0 ]]; then
        echo
        echo "reducer runtime compose verification failed."
        echo "Useful logs:"
        echo "  reducer log: $REDUCER_LOG"
        if [[ -s "$REDUCER_LOG" ]]; then
            echo "  reducer log tail:"
            tail -n 200 "$REDUCER_LOG" || true
        fi
        if [[ -s "$STATUS_FILE" ]]; then
            echo "  reducer status:"
            cat "$STATUS_FILE"
        fi
        if [[ -s "$METRICS_FILE" ]]; then
            echo "  reducer metrics tail:"
            tail -n 40 "$METRICS_FILE" || true
        fi
        echo "  $COMPOSE_DISPLAY logs --tail=200 postgres"
        echo "  $COMPOSE_DISPLAY logs --tail=200 neo4j"
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
        if [[ -n "$REDUCER_PID" ]] && ! kill -0 "$REDUCER_PID" >/dev/null 2>&1; then
            echo "reducer exited before $url became ready" >&2
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

psql_exec() {
    "${COMPOSE_CMD[@]}" exec -T postgres sh -lc \
        "psql -U eshu -d eshu -v ON_ERROR_STOP=1"
}

psql_query() {
    local query="$1"
    "${COMPOSE_CMD[@]}" exec -T postgres sh -lc \
        "psql -U eshu -d eshu -Atc \"$query\""
}

wait_for_sql_value() {
    local query="$1"
    local expected="$2"
    local attempts="$3"
    local sleep_seconds="$4"
    local result=""

    for ((attempt = 1; attempt <= attempts; attempt++)); do
        result="$(psql_query "$query" | tr -d '[:space:]')"
        if [[ "$result" == "$expected" ]]; then
            return 0
        fi
        sleep "$sleep_seconds"
    done

    echo "Timed out waiting for SQL query to return $expected: $query" >&2
    echo "Last result: ${result:-<empty>}" >&2
    return 1
}

configure_runtime_addresses() {
    export ESHU_POSTGRES_PORT="$(pick_port "$POSTGRES_PORT_BASE")"
    export ESHU_REDUCER_HTTP_PORT="$(pick_port "$ESHU_REDUCER_HTTP_PORT_BASE")"
    export NEO4J_HTTP_PORT="$(pick_port "$NEO4J_HTTP_PORT_BASE")"
    export NEO4J_BOLT_PORT="$(pick_port "$NEO4J_BOLT_PORT_BASE")"
    POSTGRES_DSN="postgresql://eshu:${ESHU_POSTGRES_PASSWORD:-change-me}@localhost:${ESHU_POSTGRES_PORT}/eshu"
}

bootstrap_data_plane_schema() {
    echo "Applying Go data-plane schema bootstrap..."
    (
        cd "$REPO_ROOT/go"
        ESHU_POSTGRES_DSN="$POSTGRES_DSN" \
        ESHU_CONTENT_STORE_DSN="$POSTGRES_DSN" \
        ESHU_NEO4J_URI="bolt://localhost:${NEO4J_BOLT_PORT}" \
        ESHU_NEO4J_USERNAME="neo4j" \
        ESHU_NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD:-change-me}" \
        NEO4J_URI="bolt://localhost:${NEO4J_BOLT_PORT}" \
        NEO4J_USERNAME="neo4j" \
        NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD:-change-me}" \
        go run ./cmd/bootstrap-data-plane
    )
}

seed_reducer_dead_letter_state() {
    echo "Seeding reducer dead-letter proof state..."
    cat <<'SQL' | psql_exec >/dev/null
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES (
    'scope-reducer-proof', 'repository', 'git', 'repo-reducer-proof',
    NULL, 'git', 'repo-reducer-proof', TIMESTAMPTZ '2026-04-16T00:00:00Z',
    TIMESTAMPTZ '2026-04-16T00:05:00Z', 'pending', NULL, '{}'::jsonb
)
ON CONFLICT (scope_id) DO UPDATE SET
    scope_kind = EXCLUDED.scope_kind,
    source_system = EXCLUDED.source_system,
    source_key = EXCLUDED.source_key,
    parent_scope_id = EXCLUDED.parent_scope_id,
    collector_kind = EXCLUDED.collector_kind,
    partition_key = EXCLUDED.partition_key,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    status = EXCLUDED.status,
    active_generation_id = EXCLUDED.active_generation_id,
    payload = EXCLUDED.payload;

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint,
    observed_at, ingested_at, status, activated_at, superseded_at, payload
) VALUES (
    'generation-reducer-dead-letter', 'scope-reducer-proof', 'snapshot',
    'compose-proof', TIMESTAMPTZ '2026-04-16T00:00:00Z', TIMESTAMPTZ '2026-04-16T00:05:00Z',
    'pending', NULL, NULL, '{}'::jsonb
)
ON CONFLICT (generation_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    trigger_kind = EXCLUDED.trigger_kind,
    freshness_hint = EXCLUDED.freshness_hint,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    status = EXCLUDED.status,
    activated_at = EXCLUDED.activated_at,
    superseded_at = EXCLUDED.superseded_at,
    payload = EXCLUDED.payload;

INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, last_attempt_at,
    next_attempt_at, failure_class, failure_message, failure_details,
    payload, created_at, updated_at
) VALUES (
    'reducer_scope-reducer-proof_generation-reducer-dead-letter_workload_identity',
    'scope-reducer-proof', 'generation-reducer-dead-letter', 'reducer',
    'workload_identity', 'pending', 0, NULL, NULL, NULL, NULL, NULL,
    NULL, NULL, NULL, $json${"reason":"compose dead-letter proof"}$json$::jsonb,
    TIMESTAMPTZ '2026-04-16T00:05:00Z', TIMESTAMPTZ '2026-04-16T00:05:00Z'
)
ON CONFLICT (work_item_id) DO NOTHING;
SQL
}

start_reducer() {
    echo "Launching reducer runtime..."
    (
        cd "$REPO_ROOT/go"
        ESHU_LISTEN_ADDR="127.0.0.1:${ESHU_REDUCER_HTTP_PORT}" \
        ESHU_POSTGRES_DSN="$POSTGRES_DSN" \
        ESHU_CONTENT_STORE_DSN="$POSTGRES_DSN" \
        ESHU_REDUCER_MAX_ATTEMPTS="$REDUCER_MAX_ATTEMPTS" \
        DEFAULT_DATABASE="neo4j" \
        NEO4J_URI="bolt://localhost:${NEO4J_BOLT_PORT}" \
        NEO4J_USERNAME="neo4j" \
        NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD:-change-me}" \
        ESHU_DEPLOYMENT_ENVIRONMENT="reducer-compose-smoke" \
        go run ./cmd/reducer >"$REDUCER_LOG" 2>&1
    ) &
    REDUCER_PID="$!"
}

verify_reducer_surfaces() {
    echo "Capturing reducer admin status and metrics..."
    curl -fsS "http://127.0.0.1:${ESHU_REDUCER_HTTP_PORT}/admin/status?format=json" >"$STATUS_FILE"
    curl -fsS "http://127.0.0.1:${ESHU_REDUCER_HTTP_PORT}/metrics" >"$METRICS_FILE"

    jq -e '
        (.health.state | type) == "string" and
        ((.flow // []) | map(select(.lane == "reducer")) | length) == 1
    ' "$STATUS_FILE" >/dev/null
    rg -q 'eshu_runtime_info\{service_name="reducer"' "$METRICS_FILE"
    rg -q 'eshu_runtime_health_state\{service_name="reducer"' "$METRICS_FILE"
}

verify_reducer_dead_letter() {
    wait_for_sql_value "SELECT status FROM fact_work_items WHERE work_item_id = 'reducer_scope-reducer-proof_generation-reducer-dead-letter_workload_identity'" "dead_letter" 180 1
    wait_for_sql_value "SELECT COUNT(*) FROM fact_work_items WHERE work_item_id = 'reducer_scope-reducer-proof_generation-reducer-dead-letter_workload_identity' AND lease_owner IS NULL AND claim_until IS NULL AND visible_at IS NULL AND failure_class = 'reducer_failed'" "1" 180 1
}

require_tool curl
require_tool docker
require_tool go
require_tool jq
require_tool nc
require_tool rg

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
COMPOSE_CMD+=(-f docker-compose.neo4j.yml)
COMPOSE_DISPLAY+=" -f docker-compose.neo4j.yml"

cd "$REPO_ROOT"

configure_runtime_addresses
echo "Starting compose-backed reducer infrastructure..."
echo "Using host ports: postgres=$ESHU_POSTGRES_PORT neo4j_http=$NEO4J_HTTP_PORT neo4j_bolt=$NEO4J_BOLT_PORT reducer_http=$ESHU_REDUCER_HTTP_PORT"
"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
if ! "${COMPOSE_CMD[@]}" up -d postgres neo4j; then
    echo "Could not start compose-backed reducer infrastructure." >&2
    exit 1
fi

wait_for_postgres 60
wait_for_neo4j 60
bootstrap_data_plane_schema
seed_reducer_dead_letter_state
start_reducer
wait_for_http "http://127.0.0.1:${ESHU_REDUCER_HTTP_PORT}/healthz" 60 1
wait_for_http "http://127.0.0.1:${ESHU_REDUCER_HTTP_PORT}/readyz" 60 1
verify_reducer_surfaces
verify_reducer_dead_letter

echo
echo "reducer runtime compose verification passed."
echo "Reducer admin: http://127.0.0.1:${ESHU_REDUCER_HTTP_PORT}"
echo "Reducer log: $REDUCER_LOG"
echo "Stack teardown: $COMPOSE_DISPLAY down -v"
