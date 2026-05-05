#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNTIME_LIB="$REPO_ROOT/scripts/lib/compose_verification_runtime_common.sh"
ASSERT_LIB="$REPO_ROOT/scripts/lib/compose_verification_assertions.sh"
FIXTURE_ROOT="$REPO_ROOT/tests/fixtures/graph_analysis_compose"
REPO_NAME="graph-analysis-go"
TMP_DIR="$(mktemp -d)"
REPOSITORIES_FILE="$TMP_DIR/repositories.json"
DIRECT_CALLERS_FILE="$TMP_DIR/direct-callers.json"
TRANSITIVE_CALLERS_FILE="$TMP_DIR/transitive-callers.json"
CALL_CHAIN_FILE="$TMP_DIR/call-chain.json"
DEAD_CODE_FILE="$TMP_DIR/dead-code.json"
INDEX_STATUS_FILE="$TMP_DIR/index-status.json"
GRAPH_QUERY_FILE="$TMP_DIR/graph-query.txt"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-graph-analysis-$$}"
# These *_BASE candidates are the preferred host ports for this wrapper. The
# compose verification runtime remaps them to free ports when the preferred
# values are already taken, then writes the final assignments back into the
# exported ESHU_* variables used below.
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
API_KEY=""
COMPOSE_CMD=()
COMPOSE_DISPLAY=""
source "$RUNTIME_LIB"
source "$ASSERT_LIB"

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

require_real_directory() {
	local path="$1"
	local resolved
	[[ -d "$path" ]] || {
		echo "Fixture root is not a directory: $path" >&2
		return 1
	}
	resolved="$(cd "$path" && pwd -P)"
	[[ "$resolved" == "$path" ]] || {
		echo "Fixture root must be a real absolute directory, not a symlink: $path -> $resolved" >&2
		return 1
	}
	printf '%s\n' "$resolved"
}

build_repo_rules_json() {
	jq -cn --arg repo "$REPO_NAME" '{exact: [$repo]}'
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
		curl -fsS "$API_BASE_URL$path" >"$output_file"
	fi
}

api_post_envelope_json() {
	local path="$1"
	local payload="$2"
	local output_file="$3"
	local -a curl_args=(
		-fsS
		-X POST
		-H "Accept: application/eshu.envelope+json"
		-H "Content-Type: application/json"
		-d "$payload"
		"$API_BASE_URL$path"
	)
	if [[ -n "$API_KEY" ]]; then
		curl_args=(-fsS -X POST -H "Authorization: Bearer $API_KEY" -H "Accept: application/eshu.envelope+json" -H "Content-Type: application/json" -d "$payload" "$API_BASE_URL$path")
	fi
	curl "${curl_args[@]}" >"$output_file"
}

verify_repository_catalog() {
	api_get "/repositories" "$REPOSITORIES_FILE"
	eshu_assert_json_query "$REPOSITORIES_FILE" '
		(.count // 0) == 1 and
		((.repositories // []) | length) == 1 and
		(.repositories[0].name // "") == "graph-analysis-go"
	' "repository catalog did not contain the expected single graph-analysis fixture repository"
}

verify_direct_callers() {
	api_post_envelope_json "/code/relationships" '{"name":"persistGraphProof","repo_id":"graph-analysis-go","direction":"incoming","relationship_type":"CALLS"}' "$DIRECT_CALLERS_FILE"
	eshu_assert_json_query "$DIRECT_CALLERS_FILE" '
		(.truth.capability // "") == "call_graph.direct_callers" and
		(.truth.basis // "") == "authoritative_graph" and
		((.data.incoming // []) | length) == 1 and
		(.data.incoming[0].source_name // "") == "dispatchGraphProof"
	' "direct caller analysis did not return the expected single caller"
}

verify_transitive_callers() {
	api_post_envelope_json "/code/relationships" '{"name":"persistGraphProof","repo_id":"graph-analysis-go","direction":"incoming","relationship_type":"CALLS","transitive":true,"max_depth":5}' "$TRANSITIVE_CALLERS_FILE"
	eshu_assert_json_query "$TRANSITIVE_CALLERS_FILE" '
		(.truth.capability // "") == "call_graph.transitive_callers" and
		(.truth.basis // "") == "authoritative_graph" and
		((.data.incoming // []) | length) == 3 and
		((.data.incoming // []) | any((.source_name // "") == "dispatchGraphProof" and (.depth // 0) == 1)) and
		((.data.incoming // []) | any((.source_name // "") == "entrypointGraphProof" and (.depth // 0) == 2)) and
		((.data.incoming // []) | any((.source_name // "") == "main" and (.depth // 0) == 3))
	' "transitive caller analysis did not return the expected depth-aware caller chain"
}

verify_call_chain() {
	api_post_envelope_json "/code/call-chain" '{"start":"entrypointGraphProof","end":"persistGraphProof","repo_id":"graph-analysis-go","max_depth":5}' "$CALL_CHAIN_FILE"
	eshu_assert_json_query "$CALL_CHAIN_FILE" '
		(.truth.capability // "") == "call_graph.call_chain_path" and
		(.truth.basis // "") == "authoritative_graph" and
		((.data.chains // []) | length) >= 1 and
		((.data.chains[0].chain // []) | map(.name) == ["entrypointGraphProof","dispatchGraphProof","persistGraphProof"])
	' "call-chain analysis did not return the expected three-function path"
}

verify_dead_code() {
	api_post_envelope_json "/code/dead-code" '{"repo_id":"graph-analysis-go","limit":10}' "$DEAD_CODE_FILE"
	eshu_assert_json_query "$DEAD_CODE_FILE" '
		(.truth.capability // "") == "code_quality.dead_code" and
		(.truth.level // "") == "derived" and
		(.truth.basis // "") == "hybrid" and
		(.data.limit // 0) == 10 and
		(.data.truncated == false) and
		((.data.results // []) | map(.name) | sort == ["deadAlphaGraphProof","deadBetaGraphProof"])
	' "dead-code analysis did not return the expected unused functions"
}

verify_graph_state() {
	eshu_neo4j_count_equals \
		"MATCH (:Repository {name:'graph-analysis-go'}) RETURN count(*)" \
		"1" \
		"expected one repository node for graph-analysis-go" \
		"$GRAPH_QUERY_FILE"
	eshu_neo4j_count_equals \
		"MATCH (:Function {name:'entrypointGraphProof'})-[:CALLS]->(:Function {name:'dispatchGraphProof'}) RETURN count(*)" \
		"1" \
		"entrypointGraphProof should call dispatchGraphProof exactly once" \
		"$GRAPH_QUERY_FILE"
	eshu_neo4j_count_equals \
		"MATCH (:Function {name:'dispatchGraphProof'})-[:CALLS]->(:Function {name:'persistGraphProof'}) RETURN count(*)" \
		"1" \
		"dispatchGraphProof should call persistGraphProof exactly once" \
		"$GRAPH_QUERY_FILE"
	eshu_neo4j_count_equals \
		"MATCH (:Function {name:'deadAlphaGraphProof'})<-[:CALLS|IMPORTS|REFERENCES]-() RETURN count(*)" \
		"0" \
		"deadAlphaGraphProof should remain unreferenced in the canonical graph" \
		"$GRAPH_QUERY_FILE"
}

wait_for_graph_analysis_projection() {
	local attempts="$1"
	local sleep_seconds="$2"

	for ((attempt = 1; attempt <= attempts; attempt++)); do
		if verify_direct_callers >/dev/null 2>&1 &&
			verify_graph_state >/dev/null 2>&1; then
			return 0
		fi
		/bin/sleep "$sleep_seconds"
	done

	echo "Timed out waiting for graph-analysis projection visibility" >&2
	return 1
}

cleanup() {
	local exit_code=$?
	if [[ "$exit_code" -ne 0 ]]; then
		echo
		echo "Graph-analysis compose verification failed."
		echo "Useful logs:"
		echo "  $COMPOSE_DISPLAY logs --tail=200 eshu"
		echo "  $COMPOSE_DISPLAY logs --tail=200 bootstrap-index"
		echo "  $COMPOSE_DISPLAY logs --tail=200 resolution-engine"
		echo "  $COMPOSE_DISPLAY logs --tail=200 neo4j"
		[[ -f "$CALL_CHAIN_FILE" ]] && { echo "Last call-chain payload:"; cat "$CALL_CHAIN_FILE"; }
		[[ -f "$DEAD_CODE_FILE" ]] && { echo "Last dead-code payload:"; cat "$DEAD_CODE_FILE"; }
		[[ -f "$INDEX_STATUS_FILE" ]] && { echo "Last index-status payload:"; cat "$INDEX_STATUS_FILE"; }
		echo "Jaeger UI: $JAEGER_URL"
	fi
	[[ "$KEEP_STACK" == "true" ]] || "${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
	rm -rf "$TMP_DIR"
	exit "$exit_code"
}
trap cleanup EXIT

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
export COMPOSE_PROJECT_NAME
if [[ -z "${ESHU_FILESYSTEM_HOST_ROOT:-}" ]]; then
	export ESHU_FILESYSTEM_HOST_ROOT="$FIXTURE_ROOT"
fi
export ESHU_FILESYSTEM_HOST_ROOT="$(require_real_directory "$ESHU_FILESYSTEM_HOST_ROOT")"
if [[ -z "${ESHU_REPOSITORY_RULES_JSON:-}" ]]; then
	export ESHU_REPOSITORY_RULES_JSON="$(build_repo_rules_json)"
fi

"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
compose_started=false
for attempt in 1 2; do
	configure_ports "$(((attempt - 1) * 10))"
	echo "Starting local compose stack..."
	echo "Using host ports: api=$ESHU_HTTP_PORT postgres=$ESHU_POSTGRES_PORT neo4j_bolt=$NEO4J_BOLT_PORT jaeger=$JAEGER_UI_PORT otel_grpc=$OTEL_COLLECTOR_OTLP_GRPC_PORT otel_http=$OTEL_COLLECTOR_OTLP_HTTP_PORT otel_prom=$OTEL_COLLECTOR_PROMETHEUS_PORT"
	echo "Using runtime ports: mcp=$ESHU_MCP_PORT api_metrics=$ESHU_API_METRICS_PORT bootstrap_metrics=$ESHU_BOOTSTRAP_METRICS_PORT ingester_metrics=$ESHU_INGESTER_METRICS_PORT reducer_metrics=$ESHU_RESOLUTION_ENGINE_METRICS_PORT mcp_metrics=$ESHU_MCP_METRICS_PORT"
	echo "Using compose project: $COMPOSE_PROJECT_NAME"
	echo "Using fixture root: $ESHU_FILESYSTEM_HOST_ROOT"
	echo "Using repository rules: $ESHU_REPOSITORY_RULES_JSON"
	if "${COMPOSE_CMD[@]}" up -d --build; then
		compose_started=true
		break
	fi
	"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
	[[ "$attempt" -eq 2 ]] && break
	echo "Compose startup failed; retrying with fresh ports..."
	/bin/sleep 2
done

[[ "$compose_started" == "true" ]] || {
	echo "Could not start the local compose stack after retrying." >&2
	exit 1
}

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

echo "Waiting for graph-analysis projection visibility..."
wait_for_graph_analysis_projection 60 2

echo "Verifying graph-analysis API and canonical graph state..."
verify_repository_catalog
verify_direct_callers
verify_transitive_callers
verify_call_chain
verify_dead_code
verify_graph_state

echo
echo "Graph-analysis compose verification passed."
echo "API: $API_BASE_URL"
echo "Jaeger UI: $JAEGER_URL"
echo "Stack teardown: $COMPOSE_DISPLAY down -v"
