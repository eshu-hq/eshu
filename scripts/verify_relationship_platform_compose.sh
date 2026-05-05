#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNTIME_LIB="$REPO_ROOT/scripts/lib/compose_verification_runtime_common.sh"
ASSERT_LIB="$REPO_ROOT/scripts/lib/compose_verification_assertions.sh"
API_PORT="${ESHU_HTTP_PORT:-8080}"
NEO4J_BOLT_PORT="${NEO4J_BOLT_PORT:-7687}"
JAEGER_PORT="${JAEGER_UI_PORT:-16686}"
NEO4J_HTTP_PORT_BASE="${NEO4J_HTTP_PORT:-17474}"
NEO4J_BOLT_PORT_BASE="${NEO4J_BOLT_PORT:-17687}"
ESHU_POSTGRES_PORT_BASE="${ESHU_POSTGRES_PORT:-25432}"
ESHU_HTTP_PORT_BASE="${ESHU_HTTP_PORT:-18080}"
JAEGER_UI_PORT_BASE="${JAEGER_UI_PORT:-26686}"
OTEL_COLLECTOR_OTLP_GRPC_PORT_BASE="${OTEL_COLLECTOR_OTLP_GRPC_PORT:-24317}"
OTEL_COLLECTOR_OTLP_HTTP_PORT_BASE="${OTEL_COLLECTOR_OTLP_HTTP_PORT:-24318}"
OTEL_COLLECTOR_PROMETHEUS_PORT_BASE="${OTEL_COLLECTOR_PROMETHEUS_PORT:-29464}"
ESHU_API_METRICS_PORT_BASE="${ESHU_API_METRICS_PORT:-19464}"
ESHU_BOOTSTRAP_METRICS_PORT_BASE="${ESHU_BOOTSTRAP_METRICS_PORT:-19467}"
ESHU_MCP_PORT_BASE="${ESHU_MCP_PORT:-8081}"
ESHU_MCP_METRICS_PORT_BASE="${ESHU_MCP_METRICS_PORT:-19468}"
ESHU_INGESTER_METRICS_PORT_BASE="${ESHU_INGESTER_METRICS_PORT:-19465}"
ESHU_RESOLUTION_ENGINE_METRICS_PORT_BASE="${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-19466}"
API_BASE_URL="http://localhost:${API_PORT}/api/v0"
JAEGER_URL="http://localhost:${JAEGER_PORT}"
TMP_DIR="$(mktemp -d)"
RELATIONSHIP_PLATFORM_FIXTURE_ROOT="$REPO_ROOT/tests/fixtures/relationship_platform"
REPOSITORIES_FILE="$TMP_DIR/repositories.json"
CONTEXT_FILE="$TMP_DIR/repository-context.json"
COVERAGE_FILE="$TMP_DIR/repository-coverage.json"
INDEX_STATUS_FILE="$TMP_DIR/index-status.json"
GRAPH_RELATIONSHIP_TYPES_FILE="$TMP_DIR/graph-relationship-types.txt"
SERVICE_CONTEXT_FILE="$TMP_DIR/service-context.json"
TRACE_FILE="$TMP_DIR/trace-deployment.json"
GRAPH_QUERY_FILE="$TMP_DIR/graph-query.txt"
HTTP_STATUS_FILE="$TMP_DIR/http-status.txt"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
API_KEY=""
COMPOSE_CMD=()
source "$RUNTIME_LIB"
source "$ASSERT_LIB"

build_relationship_platform_repo_rules_json() {
    local root="$1"
    local -a repo_ids=()

    for entry in "$root"/*; do
        if [[ ! -d "$entry" ]]; then
            continue
        fi
        repo_ids+=("$(basename "$entry")")
    done

    if [[ ${#repo_ids[@]} -eq 0 ]]; then
        echo "Relationship platform fixture root contains no repository directories: $root" >&2
        return 1
    fi

    jq -cn --args '{exact: $ARGS.positional}' "${repo_ids[@]}"
}
COMPOSE_DISPLAY=""

cleanup() {
    local exit_code=$?
    if [[ "$exit_code" -ne 0 ]]; then
        echo
        echo "Relationship-platform compose verification failed."
        echo "Useful logs:"
        echo "  $COMPOSE_DISPLAY logs --tail=200 eshu"
        echo "  $COMPOSE_DISPLAY logs --tail=200 bootstrap-index"
        echo "  $COMPOSE_DISPLAY logs --tail=200 neo4j"
        if [[ -f "$INDEX_STATUS_FILE" ]]; then
            echo "Last index-status payload:"
            cat "$INDEX_STATUS_FILE"
        fi
        echo "Jaeger UI: $JAEGER_URL"
    fi

    if [[ "$KEEP_STACK" != "true" ]]; then
        "${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
    fi
    rm -rf "$TMP_DIR"
    exit "$exit_code"
}
trap cleanup EXIT

configure_ports() {
    local retry_offset="${1:-0}"

    eshu_reset_reserved_ports
    eshu_assign_reserved_port NEO4J_HTTP_PORT "$((NEO4J_HTTP_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port NEO4J_BOLT_PORT "$((NEO4J_BOLT_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port ESHU_POSTGRES_PORT "$((ESHU_POSTGRES_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port ESHU_HTTP_PORT "$((ESHU_HTTP_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port JAEGER_UI_PORT "$((JAEGER_UI_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port OTEL_COLLECTOR_OTLP_GRPC_PORT "$((OTEL_COLLECTOR_OTLP_GRPC_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port OTEL_COLLECTOR_OTLP_HTTP_PORT "$((OTEL_COLLECTOR_OTLP_HTTP_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port OTEL_COLLECTOR_PROMETHEUS_PORT "$((OTEL_COLLECTOR_PROMETHEUS_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port ESHU_API_METRICS_PORT "$((ESHU_API_METRICS_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port ESHU_BOOTSTRAP_METRICS_PORT "$((ESHU_BOOTSTRAP_METRICS_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port ESHU_MCP_PORT "$((ESHU_MCP_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port ESHU_MCP_METRICS_PORT "$((ESHU_MCP_METRICS_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port ESHU_INGESTER_METRICS_PORT "$((ESHU_INGESTER_METRICS_PORT_BASE + retry_offset))"
    eshu_assign_reserved_port ESHU_RESOLUTION_ENGINE_METRICS_PORT "$((ESHU_RESOLUTION_ENGINE_METRICS_PORT_BASE + retry_offset))"

    API_PORT="$ESHU_HTTP_PORT"
    NEO4J_BOLT_PORT="$NEO4J_BOLT_PORT"
    JAEGER_PORT="$JAEGER_UI_PORT"
    API_BASE_URL="http://localhost:${API_PORT}/api/v0"
    JAEGER_URL="http://localhost:${JAEGER_PORT}"
}

refresh_compose_ports() {
    local mapped

    mapped="$("${COMPOSE_CMD[@]}" port eshu 8080 | tail -n 1)"
    if [[ -z "$mapped" ]]; then
        echo "Could not determine the published API port from compose." >&2
        return 1
    fi
    export ESHU_HTTP_PORT="${mapped##*:}"

    mapped="$("${COMPOSE_CMD[@]}" port neo4j 7687 | tail -n 1)"
    if [[ -z "$mapped" ]]; then
        echo "Could not determine the published Neo4j Bolt port from compose." >&2
        return 1
    fi
    export NEO4J_BOLT_PORT="${mapped##*:}"

    mapped="$("${COMPOSE_CMD[@]}" port jaeger 16686 | tail -n 1)"
    if [[ -z "$mapped" ]]; then
        echo "Could not determine the published Jaeger port from compose." >&2
        return 1
    fi
    export JAEGER_PORT="${mapped##*:}"

    API_PORT="$ESHU_HTTP_PORT"
    API_BASE_URL="http://localhost:${API_PORT}/api/v0"
    JAEGER_URL="http://localhost:${JAEGER_PORT}"
}

api_get() {
    local path="$1"
    local output_file="$2"
    if [[ -n "$API_KEY" ]]; then
        curl -fsS \
            -H "Authorization: Bearer $API_KEY" \
            "$API_BASE_URL$path" \
            >"$output_file"
    else
        curl -fsS \
            "$API_BASE_URL$path" \
            >"$output_file"
    fi
}

repository_id_by_name() {
    local repo_name="$1"
    jq -r --arg repo_name "$repo_name" '
        def repo_rows:
            if type == "array" then
                .[]
            elif ((.repositories? // []) | length > 0) then
                .repositories[]
            elif ((.items? // []) | length > 0) then
                .items[]
            else
                empty
            end;
        first(repo_rows | select((.name // "") == $repo_name) | (.repo_id // .id // .repository_id // empty))
    ' "$REPOSITORIES_FILE"
}

verify_service_edge_api_context() {
    local repo_id
    repo_id="$(repository_id_by_name "service-edge-api")"
    if [[ -z "$repo_id" || "$repo_id" == "null" ]]; then
        echo "Could not resolve service-edge-api repository id from /repositories payload." >&2
        return 1
    fi

    api_get "/repositories/${repo_id}/context" "$CONTEXT_FILE"
    jq -e '
        (.repository.name // "") == "service-edge-api" and
        ((.relationship_overview.workflow_driven // []) | any(
            (.type // "") == "DEPLOYS_FROM" and
            (.target_name // "") == "delivery-legacy-automation" and
            (.evidence_type // "") == "github_actions_reusable_workflow_ref"
        )) and
        ((.relationship_overview.iac_driven // []) | any(
            (.type // "") == "DEPLOYS_FROM" and
            (.target_name // "") == "service-worker-jobs" and
            (
                (.evidence_type // "") == "docker_compose_image" or
                (.evidence_type // "") == "docker_compose_build_context"
            )
        )) and
        ((.relationship_overview.iac_driven // []) | any(
            (.type // "") == "DEPENDS_ON" and
            (.target_name // "") == "service-worker-jobs" and
            (.evidence_type // "") == "docker_compose_depends_on"
        )) and
        ((.infrastructure_overview.artifact_family_counts.github_actions // 0) >= 1) and
        ((.infrastructure_overview.artifact_family_counts.docker // 0) >= 1) and
        ((.deployment_artifacts.deployment_artifacts // []) | any(
            (.artifact_type // "") == "docker_compose" and
            (.relative_path // "") == "docker-compose.yaml" and
            (.service_name // "") == "edge-api"
        )) and
        ((.deployment_artifacts.deployment_artifacts // []) | any(
            (.artifact_type // "") == "docker_compose" and
            (.relative_path // "") == "docker-compose.yaml" and
            (.service_name // "") == "service-worker-jobs"
        ))
    ' "$CONTEXT_FILE" >/dev/null
}

verify_service_worker_jobs_context() {
    local repo_id
    repo_id="$(repository_id_by_name "service-worker-jobs")"
    if [[ -z "$repo_id" || "$repo_id" == "null" ]]; then
        echo "Could not resolve service-worker-jobs repository id from /repositories payload." >&2
        return 1
    fi

    api_get "/repositories/${repo_id}/context" "$CONTEXT_FILE"
    jq -e '
        (.repository.name // "") == "service-worker-jobs" and
        ((.deployment_artifacts.workflow_artifacts // []) | any(
            (.relative_path // "") == ".github/workflows/deploy-gated.yml" and
            (.workflow_name // "") == "deploy-gated" and
            (.command_count // 0) == 2 and
            ((.gating_conditions // []) | length) == 2 and
            ((.needs_dependencies // []) | length) == 1
        ))
    ' "$CONTEXT_FILE" >/dev/null
}

verify_trace_deployment_chain() {
    eshu_api_post_json "/impact/trace-deployment-chain" '{"service_name":"service-edge-api"}' "$TRACE_FILE"
    jq -e '
        (.service_name // "") == "service-edge-api" and
        ((.story // "") | length) > 0 and
        ((.deployment_overview.platform_count // 0) >= 1) and
        ((.deployment_sources // []) | any((.repo_name // "") == "deployment-kustomize")) and
        ((.deployment_sources // []) | any((.repo_name // "") == "deployment-helm")) and
        ((.k8s_resources // []) | any((.relative_path // "") == "k8s/deployment.yaml")) and
        ((.controller_overview.controller_count // 0) >= 1) and
        ((.delivery_paths // []) | any((.type // "") == "deployment_source" and (.target // "") == "deployment-kustomize")) and
        ((.delivery_paths // []) | any((.type // "") == "deployment_source" and (.target // "") == "deployment-helm")) and
        ((.deployment_overview.provenance_families // []) | index("github_actions")) and
        ((.deployment_overview.provenance_families // []) | index("jenkins"))
    ' "$TRACE_FILE" >/dev/null
}

verify_service_contexts() {
    api_get "/services/service-worker-jobs/context" "$SERVICE_CONTEXT_FILE"
    eshu_assert_json_query "$SERVICE_CONTEXT_FILE" '
        (.id // "") == "workload:service-worker-jobs" and
        (.name // "") == "service-worker-jobs" and
        (.repo_name // "") == "service-worker-jobs" and
        ((.instances // []) | any(
            (.environment // "") == "modern" and
            ((.platform_kind // "") | ascii_downcase) == "kubernetes"
        )) and
        ((.deployment_overview.environment_count // 0) >= 1)
    ' "service-worker-jobs service context did not prove its materialized modern kubernetes instance"

    api_get "/services/service-edge-api/context" "$SERVICE_CONTEXT_FILE"
    eshu_assert_json_query "$SERVICE_CONTEXT_FILE" '
        (.id // "") == "workload:service-edge-api" and
        (.name // "") == "service-edge-api" and
        (.repo_name // "") == "service-edge-api" and
        ((.instances // []) | any(
            (.environment // "") == "modern" and
            ((.platform_kind // "") | ascii_downcase) == "kubernetes"
        )) and
        ((.dependencies // []) | any(
            (.type // "") == "DEPENDS_ON" and
            (.target_name // "") == "service-worker-jobs"
        ))
    ' "service-edge-api service context did not prove workload identity, modern instance, and service dependency truth"
}

verify_api_surface() {
    local repo_id
    local relationship_repo_id

    api_get "/repositories" "$REPOSITORIES_FILE"
    jq -e '
        (type == "array" and length > 0) or
        (
            type == "object" and
            (
                ((.repositories? // []) | length > 0) or
                ((.items? // []) | length > 0)
            )
        )
    ' "$REPOSITORIES_FILE" >/dev/null

    repo_id="$(jq -r '
        if type == "array" then
            (.[0].repo_id // .[0].id // .[0].repository_id // empty)
        elif ((.repositories? // []) | length > 0) then
            (.repositories[0].repo_id // .repositories[0].id // .repositories[0].repository_id // empty)
        elif ((.items? // []) | length > 0) then
            (.items[0].repo_id // .items[0].id // .items[0].repository_id // empty)
        else
            empty
        end
    ' "$REPOSITORIES_FILE")"
    if [[ -z "$repo_id" ]]; then
        echo "Could not determine repository id from /repositories payload" >&2
        return 1
    fi

    api_get "/repositories/${repo_id}/context" "$CONTEXT_FILE"
    api_get "/repositories/${repo_id}/coverage" "$COVERAGE_FILE"
    api_get "/index-status" "$INDEX_STATUS_FILE"

    jq -e 'type == "object" and length > 0' "$CONTEXT_FILE" >/dev/null
    jq -e 'type == "object" and length > 0' "$COVERAGE_FILE" >/dev/null
    jq -e 'type == "object" and length > 0' "$INDEX_STATUS_FILE" >/dev/null

    relationship_repo_id="$(
        jq -r '
            def repo_ids:
                if type == "array" then
                    .[]
                elif ((.repositories? // []) | length > 0) then
                    .repositories[]
                elif ((.items? // []) | length > 0) then
                    .items[]
                else
                    empty
                end;
            [repo_ids | (.repo_id // .id // .repository_id // empty)] | .[]
        ' "$REPOSITORIES_FILE" | while read -r candidate_repo_id; do
            if [[ -z "$candidate_repo_id" ]]; then
                continue
            fi
            api_get "/repositories/${candidate_repo_id}/context" "$CONTEXT_FILE"
            if jq -e '
                (.relationship_overview.relationship_count // 0) > 0 and
                (
                    ((.relationship_overview.controller_driven // []) | length) > 0 or
                    ((.relationship_overview.workflow_driven // []) | length) > 0 or
                    ((.relationship_overview.iac_driven // []) | length) > 0
                )
            ' "$CONTEXT_FILE" >/dev/null; then
                printf "%s" "$candidate_repo_id"
                break
            fi
        done
    )"
    if [[ -z "$relationship_repo_id" ]]; then
        echo "Could not find a repository context with compose-backed typed relationship evidence." >&2
        return 1
    fi

    api_get "/repositories/${relationship_repo_id}/context" "$CONTEXT_FILE"
    jq -e '
        (.relationship_overview.relationship_count // 0) > 0 and
        ((.relationship_overview.story // "") | length) > 0 and
        (
            ((.relationship_overview.controller_driven // []) | length) > 0 or
            ((.relationship_overview.workflow_driven // []) | length) > 0 or
            ((.relationship_overview.iac_driven // []) | length) > 0
        )
    ' "$CONTEXT_FILE" >/dev/null

    verify_service_edge_api_context
    verify_service_worker_jobs_context
    verify_service_contexts
    verify_trace_deployment_chain
}

verify_graph_state() {
    eshu_neo4j_query_to_file "MATCH (n:Repository) RETURN count(n) AS count" "$GRAPH_QUERY_FILE"
    if ! tail -n 1 "$GRAPH_QUERY_FILE" | rg -q '^[1-9][0-9]*$'; then
        echo "Expected Repository nodes in Neo4j, got:" >&2
        cat "$GRAPH_QUERY_FILE" >&2
        return 1
    fi

    eshu_neo4j_query_to_file \
        "MATCH ()-[rel:DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|RUNS_ON]->() RETURN DISTINCT type(rel) ORDER BY type(rel)" \
        "$GRAPH_RELATIONSHIP_TYPES_FILE"
    if ! rg -q 'DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|RUNS_ON' "$GRAPH_RELATIONSHIP_TYPES_FILE"; then
        echo "Expected at least one typed relationship family in Neo4j, got:" >&2
        cat "$GRAPH_RELATIONSHIP_TYPES_FILE" >&2
        return 1
    fi

    eshu_neo4j_count_equals \
        "MATCH (:Repository {name:'service-edge-api'})-[:DEFINES]->(:Workload {id:'workload:service-edge-api'}) RETURN count(*)" \
        "1" \
        "service-edge-api should materialize exactly one workload node" \
        "$GRAPH_QUERY_FILE"
    eshu_neo4j_count_equals \
        "MATCH (:Workload {id:'workload:service-edge-api'})<-[:INSTANCE_OF]-(:WorkloadInstance {id:'workload-instance:service-edge-api:modern'}) RETURN count(*)" \
        "1" \
        "service-edge-api should materialize a modern workload instance" \
        "$GRAPH_QUERY_FILE"
    eshu_neo4j_count_equals \
        "MATCH (:WorkloadInstance {id:'workload-instance:service-edge-api:modern'})-[:DEPLOYMENT_SOURCE]->(:Repository {name:'deployment-kustomize'}) RETURN count(*)" \
        "1" \
        "service-edge-api modern instance should point at deployment-kustomize as a deployment source" \
        "$GRAPH_QUERY_FILE"
    eshu_neo4j_count_equals \
        "MATCH (:WorkloadInstance {id:'workload-instance:service-edge-api:modern'})-[:RUNS_ON]->(:Platform {kind:'kubernetes', name:'modern'}) RETURN count(*)" \
        "1" \
        "service-edge-api modern instance should run on the modern kubernetes platform" \
        "$GRAPH_QUERY_FILE"
    eshu_neo4j_count_equals \
        "MATCH (:Workload {id:'workload:service-edge-api'})-[:DEPENDS_ON]->(:Workload {id:'workload:service-worker-jobs'}) RETURN count(*)" \
        "1" \
        "service-edge-api workload should depend on service-worker-jobs" \
        "$GRAPH_QUERY_FILE"
    eshu_neo4j_count_equals \
        "MATCH (:Repository {name:'deployment-helm'})-[:DEPLOYS_FROM]->(:Repository {name:'service-edge-api'}) RETURN count(*)" \
        "1" \
        "deployment-helm repository should have a DEPLOYS_FROM edge to service-edge-api" \
        "$GRAPH_QUERY_FILE"
    eshu_neo4j_count_equals \
        "MATCH (:Repository {name:'deployment-kustomize'})-[:DEPLOYS_FROM]->(:Repository {name:'service-edge-api'}) RETURN count(*)" \
        "1" \
        "deployment-kustomize repository should have a DEPLOYS_FROM edge to service-edge-api" \
        "$GRAPH_QUERY_FILE"
    eshu_neo4j_count_equals \
        "MATCH (:Repository {name:'service-edge-api'})-[:DEPLOYS_FROM]->(:Repository {name:'delivery-legacy-automation'}) RETURN count(*)" \
        "1" \
        "service-edge-api repository should retain reusable-workflow DEPLOYS_FROM evidence to delivery-legacy-automation" \
        "$GRAPH_QUERY_FILE"
}

eshu_require_tool curl
eshu_require_tool docker
eshu_require_tool jq
eshu_require_tool nc
eshu_require_tool rg

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

if [[ -z "${ESHU_FILESYSTEM_HOST_ROOT:-}" ]]; then
    export ESHU_FILESYSTEM_HOST_ROOT="$RELATIONSHIP_PLATFORM_FIXTURE_ROOT"
fi
if [[ -z "${ESHU_REPOSITORY_RULES_JSON:-}" ]]; then
    ESHU_REPOSITORY_RULES_JSON="$(build_relationship_platform_repo_rules_json "$ESHU_FILESYSTEM_HOST_ROOT")"
    export ESHU_REPOSITORY_RULES_JSON
fi

"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
compose_started=false
for attempt in 1 2; do
    configure_ports "$(((attempt - 1) * 10))"
    echo "Starting local compose stack..."
    echo "Using host ports: api=$ESHU_HTTP_PORT postgres=$ESHU_POSTGRES_PORT neo4j_bolt=$NEO4J_BOLT_PORT jaeger=$JAEGER_UI_PORT otel_grpc=$OTEL_COLLECTOR_OTLP_GRPC_PORT otel_http=$OTEL_COLLECTOR_OTLP_HTTP_PORT otel_prom=$OTEL_COLLECTOR_PROMETHEUS_PORT"
    echo "Using runtime ports: mcp=$ESHU_MCP_PORT api_metrics=$ESHU_API_METRICS_PORT bootstrap_metrics=$ESHU_BOOTSTRAP_METRICS_PORT ingester_metrics=$ESHU_INGESTER_METRICS_PORT reducer_metrics=$ESHU_RESOLUTION_ENGINE_METRICS_PORT mcp_metrics=$ESHU_MCP_METRICS_PORT"
    echo "Using fixture root: $ESHU_FILESYSTEM_HOST_ROOT"
    echo "Using repository rules: $ESHU_REPOSITORY_RULES_JSON"
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

refresh_compose_ports
echo "Waiting for bootstrap indexing to finish..."
eshu_compose_wait_for_bootstrap_exit 600

echo "Waiting for API health..."
eshu_compose_wait_for_http "http://localhost:${API_PORT}/health" 60 2

echo "Reading API bearer token from the running API container..."
API_KEY="$(eshu_compose_read_api_key)"
if [[ -n "$API_KEY" ]]; then
    echo "Found ESHU_API_KEY in the API container environment."
else
    echo "No ESHU_API_KEY is set in the API container; using unauthenticated local API access."
fi

echo "Waiting for /index-status queue completion..."
eshu_compose_wait_for_index_completion 180 5 "$INDEX_STATUS_FILE"

echo "Verifying relationship platform API and graph state..."
verify_api_surface
verify_graph_state

echo
echo "Relationship-platform compose verification passed."
echo "API: $API_BASE_URL"
echo "Jaeger UI: $JAEGER_URL"
echo "Stack teardown: $COMPOSE_DISPLAY down -v"
