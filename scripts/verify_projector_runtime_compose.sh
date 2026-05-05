#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
TMP_DIR="$(mktemp -d)"
PROJECTOR_LOG="$TMP_DIR/projector.log"
STATUS_FILE="$TMP_DIR/projector-status.json"
METRICS_FILE="$TMP_DIR/projector-metrics.txt"
PROJECTOR_HTTP_PORT_BASE="${ESHU_PROJECTOR_HTTP_PORT:-28081}"
POSTGRES_PORT_BASE="${ESHU_POSTGRES_PORT:-25432}"
NEO4J_HTTP_PORT_BASE="${NEO4J_HTTP_PORT:-27474}"
NEO4J_BOLT_PORT_BASE="${NEO4J_BOLT_PORT:-27687}"
JAEGER_UI_PORT_BASE="${JAEGER_UI_PORT:-26686}"
OTEL_COLLECTOR_OTLP_GRPC_PORT_BASE="${OTEL_COLLECTOR_OTLP_GRPC_PORT:-24317}"
OTEL_COLLECTOR_OTLP_HTTP_PORT_BASE="${OTEL_COLLECTOR_OTLP_HTTP_PORT:-24318}"
OTEL_COLLECTOR_PROMETHEUS_PORT_BASE="${OTEL_COLLECTOR_PROMETHEUS_PORT:-29464}"
COMPOSE_CMD=()
COMPOSE_DISPLAY=""
POSTGRES_DSN=""
PROJECTOR_PID=""

cleanup() {
    local exit_code=$?
    if [[ -n "$PROJECTOR_PID" ]] && kill -0 "$PROJECTOR_PID" >/dev/null 2>&1; then
        kill "$PROJECTOR_PID" >/dev/null 2>&1 || true
        wait "$PROJECTOR_PID" >/dev/null 2>&1 || true
    fi

    if [[ "$exit_code" -ne 0 ]]; then
        echo
        echo "projector runtime compose verification failed."
        echo "Useful logs:"
        echo "  projector log: $PROJECTOR_LOG"
        if [[ -s "$PROJECTOR_LOG" ]]; then
            echo "  projector log tail:"
            tail -n 200 "$PROJECTOR_LOG" || true
        fi
        if [[ -s "$STATUS_FILE" ]]; then
            echo "  projector status:"
            cat "$STATUS_FILE"
        fi
        if [[ -s "$METRICS_FILE" ]]; then
            echo "  projector metrics tail:"
            tail -n 40 "$METRICS_FILE" || true
        fi
        echo "  $COMPOSE_DISPLAY logs --tail=200 postgres"
        echo "  $COMPOSE_DISPLAY logs --tail=200 neo4j"
        echo "  $COMPOSE_DISPLAY logs --tail=200 otel-collector"
        echo "  $COMPOSE_DISPLAY logs --tail=200 jaeger"
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

configure_ports() {
    export ESHU_POSTGRES_PORT="$(pick_port "$POSTGRES_PORT_BASE")"
    export NEO4J_HTTP_PORT="$(pick_port "$NEO4J_HTTP_PORT_BASE")"
    export NEO4J_BOLT_PORT="$(pick_port "$NEO4J_BOLT_PORT_BASE")"
    export JAEGER_UI_PORT="$(pick_port "$JAEGER_UI_PORT_BASE")"
    export OTEL_COLLECTOR_OTLP_GRPC_PORT="$(pick_port "$OTEL_COLLECTOR_OTLP_GRPC_PORT_BASE")"
    export OTEL_COLLECTOR_OTLP_HTTP_PORT="$(pick_port "$OTEL_COLLECTOR_OTLP_HTTP_PORT_BASE")"
    export OTEL_COLLECTOR_PROMETHEUS_PORT="$(pick_port "$OTEL_COLLECTOR_PROMETHEUS_PORT_BASE")"
    export ESHU_PROJECTOR_HTTP_PORT="$(pick_port "$PROJECTOR_HTTP_PORT_BASE")"
    POSTGRES_DSN="postgresql://eshu:${ESHU_POSTGRES_PASSWORD:-change-me}@localhost:${ESHU_POSTGRES_PORT}/eshu"
}

refresh_compose_ports() {
    local mapped

    mapped="$("${COMPOSE_CMD[@]}" port postgres 5432 | tail -n 1)"
    if [[ -z "$mapped" ]]; then
        echo "Could not determine the published Postgres port from compose." >&2
        return 1
    fi
    export ESHU_POSTGRES_PORT="${mapped##*:}"

    mapped="$("${COMPOSE_CMD[@]}" port neo4j 7474 | tail -n 1)"
    if [[ -z "$mapped" ]]; then
        echo "Could not determine the published Neo4j HTTP port from compose." >&2
        return 1
    fi
    export NEO4J_HTTP_PORT="${mapped##*:}"

    mapped="$("${COMPOSE_CMD[@]}" port neo4j 7687 | tail -n 1)"
    if [[ -z "$mapped" ]]; then
        echo "Could not determine the published Neo4j Bolt port from compose." >&2
        return 1
    fi
    export NEO4J_BOLT_PORT="${mapped##*:}"

    mapped="$("${COMPOSE_CMD[@]}" port otel-collector 4317 | tail -n 1)"
    if [[ -z "$mapped" ]]; then
        echo "Could not determine the published OTEL collector gRPC port from compose." >&2
        return 1
    fi
    export OTEL_COLLECTOR_OTLP_GRPC_PORT="${mapped##*:}"

    mapped="$("${COMPOSE_CMD[@]}" port otel-collector 4318 | tail -n 1)"
    if [[ -z "$mapped" ]]; then
        echo "Could not determine the published OTEL collector HTTP port from compose." >&2
        return 1
    fi
    export OTEL_COLLECTOR_OTLP_HTTP_PORT="${mapped##*:}"

    mapped="$("${COMPOSE_CMD[@]}" port otel-collector 9464 | tail -n 1)"
    if [[ -z "$mapped" ]]; then
        echo "Could not determine the published OTEL collector metrics port from compose." >&2
        return 1
    fi
    export OTEL_COLLECTOR_PROMETHEUS_PORT="${mapped##*:}"

    POSTGRES_DSN="postgresql://eshu:${ESHU_POSTGRES_PASSWORD:-change-me}@localhost:${ESHU_POSTGRES_PORT}/eshu"
}

wait_for_http() {
    local url="$1"
    local attempts="$2"
    local sleep_seconds="$3"

    for ((attempt = 1; attempt <= attempts; attempt++)); do
        if curl -fsS "$url" >/dev/null 2>&1; then
            return 0
        fi
        if [[ -n "$PROJECTOR_PID" ]] && ! kill -0 "$PROJECTOR_PID" >/dev/null 2>&1; then
            echo "projector exited before $url became ready" >&2
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

wait_for_tcp_port() {
    local host="$1"
    local port="$2"
    local attempts="$3"
    local sleep_seconds="$4"

    for ((attempt = 1; attempt <= attempts; attempt++)); do
        if nc -z "$host" "$port" >/dev/null 2>&1; then
            return 0
        fi
        sleep "$sleep_seconds"
    done

    echo "Timed out waiting for ${host}:${port}" >&2
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

bootstrap_data_plane_schema() {
    echo "Applying Go data-plane Postgres schema bootstrap..."
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

seed_projector_state() {
    echo "Seeding projector proof state..."
    cat <<'SQL' | psql_exec >/dev/null
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES (
    'scope-projector-proof', 'repository', 'git', 'repo-projector-proof', NULL,
    'git', 'repo-projector-proof', TIMESTAMPTZ '2026-04-16T00:00:00Z',
    TIMESTAMPTZ '2026-04-16T00:05:00Z', 'pending', NULL,
    $json${"repo_id":"repository:r_projector_proof","source_key":"repo-projector-proof"}$json$::jsonb
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
    'generation-projector-proof', 'scope-projector-proof', 'snapshot', '',
    TIMESTAMPTZ '2026-04-16T00:00:00Z', TIMESTAMPTZ '2026-04-16T00:05:00Z',
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

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, source_uri, source_record_id,
    observed_at, ingested_at, is_tombstone, payload
) VALUES
(
    'fact-projector-graph', 'scope-projector-proof', 'generation-projector-proof',
    'repository', 'repository:repo-projector-proof', 'git', 'repo-projector-proof',
    NULL, NULL, TIMESTAMPTZ '2026-04-16T00:00:00Z', TIMESTAMPTZ '2026-04-16T00:05:00Z',
    FALSE, $json${"graph_id":"repo-projector-proof","graph_kind":"repository","name":"projector-proof-repo"}$json$::jsonb
),
(
    'fact-projector-content', 'scope-projector-proof', 'generation-projector-proof',
    'content_entity', 'content-entity:projector-proof', 'git', 'repo-projector-proof-content',
    NULL, NULL, TIMESTAMPTZ '2026-04-16T00:00:00Z', TIMESTAMPTZ '2026-04-16T00:05:00Z',
    FALSE, $json${"content_path":"README.md","content_body":"# Projector proof\n","content_digest":"digest-projector-proof","entity_type":"SqlTable","entity_name":"public.projector_proof","start_line":1,"end_line":3,"language":"sql","source_cache":"create table public.projector_proof (id bigint);","reducer_domain":"shared_identity","entity_key":"repository:r_projector_proof","reason":"projector live proof follow-up"}$json$::jsonb
)
ON CONFLICT (fact_id) DO UPDATE SET
    fact_kind = EXCLUDED.fact_kind,
    stable_fact_key = EXCLUDED.stable_fact_key,
    source_system = EXCLUDED.source_system,
    source_fact_key = EXCLUDED.source_fact_key,
    source_uri = EXCLUDED.source_uri,
    source_record_id = EXCLUDED.source_record_id,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    is_tombstone = EXCLUDED.is_tombstone,
    payload = EXCLUDED.payload;

INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at,
    last_attempt_at, next_attempt_at, failure_class, failure_message,
    failure_details, payload, created_at, updated_at
) VALUES (
    'projector_scope-projector-proof_generation-projector-proof',
    'scope-projector-proof', 'generation-projector-proof', 'projector',
    'source_local', 'pending', 0, NULL, NULL, NULL, NULL, NULL, NULL, NULL,
    NULL, '{}'::jsonb, TIMESTAMPTZ '2026-04-16T00:05:00Z', TIMESTAMPTZ '2026-04-16T00:05:00Z'
)
ON CONFLICT (work_item_id) DO NOTHING;
SQL
}

start_projector() {
    echo "Launching projector runtime..."
    (
        cd "$REPO_ROOT/go"
        ESHU_LISTEN_ADDR="127.0.0.1:${ESHU_PROJECTOR_HTTP_PORT}" \
        ESHU_POSTGRES_DSN="$POSTGRES_DSN" \
        ESHU_CONTENT_STORE_DSN="$POSTGRES_DSN" \
        DEFAULT_DATABASE="neo4j" \
        NEO4J_URI="bolt://localhost:${NEO4J_BOLT_PORT}" \
        NEO4J_USERNAME="neo4j" \
        NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD:-change-me}" \
        OTEL_EXPORTER_OTLP_ENDPOINT="http://localhost:${OTEL_COLLECTOR_OTLP_GRPC_PORT}" \
        OTEL_EXPORTER_OTLP_PROTOCOL="grpc" \
        OTEL_EXPORTER_OTLP_INSECURE="true" \
        OTEL_TRACES_EXPORTER="otlp" \
        OTEL_METRICS_EXPORTER="none" \
        OTEL_LOGS_EXPORTER="none" \
        go run ./cmd/projector >"$PROJECTOR_LOG" 2>&1
    ) &
    PROJECTOR_PID="$!"
}

verify_projector_outputs() {
    wait_for_sql_value "SELECT status FROM scope_generations WHERE generation_id = 'generation-projector-proof'" "active" 120 1
    wait_for_sql_value "SELECT status FROM fact_work_items WHERE generation_id = 'generation-projector-proof' AND stage = 'projector'" "succeeded" 120 1
    wait_for_sql_value "SELECT COUNT(*) FROM fact_work_items WHERE generation_id = 'generation-projector-proof' AND stage = 'projector' AND lease_owner IS NULL AND claim_until IS NULL AND visible_at IS NULL AND failure_class IS NULL AND failure_message IS NULL AND failure_details IS NULL" "1" 120 1

    curl -fsS "http://127.0.0.1:${ESHU_PROJECTOR_HTTP_PORT}/admin/status?format=json" >"$STATUS_FILE"
    curl -fsS "http://127.0.0.1:${ESHU_PROJECTOR_HTTP_PORT}/metrics" >"$METRICS_FILE"

    jq -e '
        (.health.state | type) == "string" and
        ((.flow // []) | map(select(.lane == "projector")) | length) == 1 and
        ((.stages // []) | map(select(.stage == "projector")) | length) == 1
    ' "$STATUS_FILE" >/dev/null
    rg -q 'eshu_runtime_info\{service_name="projector"' "$METRICS_FILE"
    rg -q 'eshu_runtime_queue_outstanding\{service_name="projector"' "$METRICS_FILE"
}

verify_neo4j_projection() {
    local result
    local count
    result="$("${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell \
        -u neo4j \
        -p "${ESHU_NEO4J_PASSWORD:-change-me}" \
        --format plain \
        "MATCH (n:Repository {scope_id: 'scope-projector-proof', generation_id: 'generation-projector-proof'}) RETURN count(n) AS count")"
    count="$(printf '%s\n' "$result" | tail -n 1 | tr -d '[:space:]')"
    if [[ ! "$count" =~ ^[1-9][0-9]*$ ]]; then
        echo "Expected at least one canonical Repository node in Neo4j, got: $result" >&2
        return 1
    fi
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
COMPOSE_CMD+=(-f docker-compose.neo4j.yml -f docker-compose.telemetry.yml)
COMPOSE_DISPLAY+=" -f docker-compose.neo4j.yml -f docker-compose.telemetry.yml"

cd "$REPO_ROOT"

configure_ports

echo "Starting compose-backed infrastructure..."
echo "Using host ports: postgres=$ESHU_POSTGRES_PORT neo4j_http=$NEO4J_HTTP_PORT neo4j_bolt=$NEO4J_BOLT_PORT jaeger=$JAEGER_UI_PORT projector_http=$ESHU_PROJECTOR_HTTP_PORT"
for attempt in 1 2; do
    "${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
    if "${COMPOSE_CMD[@]}" up -d postgres neo4j jaeger otel-collector; then
        break
    fi
    if [[ "$attempt" -eq 2 ]]; then
        echo "Could not start compose-backed infrastructure after retrying." >&2
        exit 1
    fi
    echo "Compose startup failed; retrying with a clean stack..."
    configure_ports
    sleep 2
done

refresh_compose_ports
wait_for_postgres 60
wait_for_neo4j 60
wait_for_tcp_port 127.0.0.1 "$ESHU_POSTGRES_PORT" 60 1
wait_for_tcp_port 127.0.0.1 "$NEO4J_BOLT_PORT" 60 1
bootstrap_data_plane_schema
seed_projector_state
start_projector
wait_for_http "http://127.0.0.1:${ESHU_PROJECTOR_HTTP_PORT}/healthz" 60 1
wait_for_http "http://127.0.0.1:${ESHU_PROJECTOR_HTTP_PORT}/readyz" 60 1
wait_for_http "http://127.0.0.1:${ESHU_PROJECTOR_HTTP_PORT}/metrics" 60 1
verify_projector_outputs
verify_neo4j_projection

echo
echo "projector runtime compose verification passed."
echo "Projector: http://127.0.0.1:${ESHU_PROJECTOR_HTTP_PORT}"
echo "Stack teardown: $COMPOSE_DISPLAY down -v"
