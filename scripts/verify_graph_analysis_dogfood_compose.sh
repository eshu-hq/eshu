#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNTIME_LIB="$REPO_ROOT/scripts/lib/compose_verification_runtime_common.sh"
ASSERT_LIB="$REPO_ROOT/scripts/lib/compose_verification_assertions.sh"
TMP_DIR="$(mktemp -d)"
REPOSITORIES_FILE="$TMP_DIR/repositories.json"
DIRECT_CALLERS_FILE="$TMP_DIR/direct-callers.json"
DIRECT_CALLEES_FILE="$TMP_DIR/direct-callees.json"
TRANSITIVE_CALLERS_FILE="$TMP_DIR/transitive-callers.json"
CALL_CHAIN_FILE="$TMP_DIR/call-chain.json"
INDEX_STATUS_FILE="$TMP_DIR/index-status.json"
GRAPH_QUERY_FILE="$TMP_DIR/graph-query.txt"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-graph-dogfood-$$}"
# These *_BASE candidates are the preferred host ports for this wrapper. The
# compose verification runtime remaps them to free ports when the preferred
# values are already taken, then writes the final assignments back into the
# exported ESHU_* variables used below.
API_PORT="${ESHU_HTTP_PORT:-8080}"
NEO4J_BOLT_PORT="${NEO4J_BOLT_PORT:-7687}"
JAEGER_PORT="${JAEGER_UI_PORT:-16686}"
NEO4J_HTTP_PORT_BASE="${NEO4J_HTTP_PORT:-17484}"
NEO4J_BOLT_PORT_BASE="${NEO4J_BOLT_PORT:-17697}"
ESHU_POSTGRES_PORT_BASE="${ESHU_POSTGRES_PORT:-25442}"
ESHU_HTTP_PORT_BASE="${ESHU_HTTP_PORT:-18090}"
JAEGER_UI_PORT_BASE="${JAEGER_UI_PORT:-26696}"
OTEL_COLLECTOR_OTLP_GRPC_PORT_BASE="${OTEL_COLLECTOR_OTLP_GRPC_PORT:-24327}"
OTEL_COLLECTOR_OTLP_HTTP_PORT_BASE="${OTEL_COLLECTOR_OTLP_HTTP_PORT:-24328}"
OTEL_COLLECTOR_PROMETHEUS_PORT_BASE="${OTEL_COLLECTOR_PROMETHEUS_PORT:-29474}"
ESHU_API_METRICS_PORT_BASE="${ESHU_API_METRICS_PORT:-19484}"
ESHU_BOOTSTRAP_METRICS_PORT_BASE="${ESHU_BOOTSTRAP_METRICS_PORT:-19487}"
ESHU_MCP_PORT_BASE="${ESHU_MCP_PORT:-8091}"
ESHU_MCP_METRICS_PORT_BASE="${ESHU_MCP_METRICS_PORT:-19488}"
ESHU_INGESTER_METRICS_PORT_BASE="${ESHU_INGESTER_METRICS_PORT:-19485}"
ESHU_RESOLUTION_ENGINE_METRICS_PORT_BASE="${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-19486}"
API_BASE_URL="http://localhost:${API_PORT}/api/v0"
JAEGER_URL="http://localhost:${JAEGER_PORT}"
API_KEY=""
COMPOSE_CMD=()
COMPOSE_DISPLAY=""
REPO_NAME="$(basename "$REPO_ROOT")"
DEFAULT_HOST_ROOT="$(cd "$REPO_ROOT/.." && pwd -P)"
REPO_SELECTOR=""
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
		echo "Host root is not a directory: $path" >&2
		return 1
	}
	resolved="$(cd "$path" && pwd -P)"
	[[ "$resolved" == "$path" ]] || {
		echo "Host root must be a real absolute directory, not a symlink: $path -> $resolved" >&2
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

discover_repo_selector() {
	api_get "/repositories" "$REPOSITORIES_FILE"
	REPO_SELECTOR="$(jq -r --arg repo_name "$REPO_NAME" --arg repo_root "$REPO_ROOT" '
		[
			(.repositories // [])[]
			| select(
				(.name // "") == $repo_name or
				(.repo_slug // "") == $repo_name or
				(.path // "") == $repo_root or
				(.local_path // "") == $repo_root
			)
			| .name
		]
		| first // ""
	' "$REPOSITORIES_FILE")"
	if [[ -z "$REPO_SELECTOR" ]]; then
		echo "Could not discover the dogfood repository selector from /repositories." >&2
		cat "$REPOSITORIES_FILE" >&2
		return 1
	fi
}

verify_repository_catalog() {
	jq -e --arg repo_name "$REPO_NAME" '
		((.repositories // []) | any((.name // "") == $repo_name))
	' "$REPOSITORIES_FILE" >/dev/null || {
		echo "repository catalog did not contain the dogfood repository" >&2
		cat "$REPOSITORIES_FILE" >&2
		return 1
	}
}

verify_direct_callers() {
	api_post_envelope_json "/code/relationships" "$(jq -cn --arg repo "$REPO_SELECTOR" '{name:"buildCallChainCypher", repo_id:$repo, direction:"incoming", relationship_type:"CALLS"}')" "$DIRECT_CALLERS_FILE"
	eshu_assert_json_query "$DIRECT_CALLERS_FILE" '
		(.truth.capability // "") == "call_graph.direct_callers" and
		(.truth.basis // "") == "authoritative_graph" and
		((.data.incoming // []) | length) == 1 and
		(.data.incoming[0].source_name // "") == "handleCallChain"
	' "dogfood direct caller analysis did not return handleCallChain -> buildCallChainCypher"
}

verify_direct_callees() {
	api_post_envelope_json "/code/relationships" "$(jq -cn --arg repo "$REPO_SELECTOR" '{name:"NewHostedWithStatusServer", repo_id:$repo, direction:"outgoing", relationship_type:"CALLS"}')" "$DIRECT_CALLEES_FILE"
	eshu_assert_json_query "$DIRECT_CALLEES_FILE" '
		(.truth.capability // "") == "call_graph.direct_callees" and
		(.truth.basis // "") == "authoritative_graph" and
		((.data.outgoing // []) | map(.target_name) | sort == ["MountStatusServer","NewHosted"])
	' "dogfood direct callee analysis did not return the expected NewHostedWithStatusServer callees"
}

verify_transitive_callers() {
	api_post_envelope_json "/code/relationships" "$(jq -cn --arg repo "$REPO_SELECTOR" '{name:"buildTransitiveRelationshipGraphResponse", repo_id:$repo, direction:"incoming", relationship_type:"CALLS", transitive:true, max_depth:4}')" "$TRANSITIVE_CALLERS_FILE"
	eshu_assert_json_query "$TRANSITIVE_CALLERS_FILE" '
		(.truth.capability // "") == "call_graph.transitive_callers" and
		(.truth.basis // "") == "authoritative_graph" and
		((.data.incoming // []) | any((.source_name // "") == "transitiveRelationshipsGraphRow" and (.depth // 0) == 1)) and
		((.data.incoming // []) | any((.source_name // "") == "handleRelationships" and (.depth // 0) == 2))
	' "dogfood transitive caller analysis did not return the expected query handler chain"
}

verify_call_chain() {
	api_post_envelope_json "/code/call-chain" "$(jq -cn --arg repo "$REPO_SELECTOR" '{start:"NewHostedWithStatusServer", end:"NewAdminMux", repo_id:$repo, max_depth:6}')" "$CALL_CHAIN_FILE"
	eshu_assert_json_query "$CALL_CHAIN_FILE" '
		(.truth.capability // "") == "call_graph.call_chain_path" and
		(.truth.basis // "") == "authoritative_graph" and
		((.data.chains // []) | length) >= 1 and
		((.data.chains[0].chain // []) | map(.name)) as $names |
		($names | length) >= 5 and
		($names[0] == "NewHostedWithStatusServer") and
		($names[1] == "MountStatusServer") and
		($names[2] == "NewStatusAdminServer") and
		($names[3] == "NewStatusAdminMux") and
		($names[4] == "NewAdminMux")
	' "dogfood call-chain analysis did not return the expected hosted status-server path"
}

verify_graph_state() {
	eshu_neo4j_count_equals \
		"MATCH (:Function {name:'handleCallChain'})-[:CALLS]->(:Function {name:'buildCallChainCypher'}) RETURN count(*)" \
		"1" \
		"handleCallChain should call buildCallChainCypher exactly once in the canonical graph" \
		"$GRAPH_QUERY_FILE"
	eshu_neo4j_count_equals \
		"MATCH (:Function {name:'NewHostedWithStatusServer'})-[:CALLS]->(:Function {name:'MountStatusServer'}) RETURN count(*)" \
		"1" \
		"NewHostedWithStatusServer should call MountStatusServer exactly once in the canonical graph" \
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

	echo "Timed out waiting for dogfood graph-analysis projection visibility" >&2
	return 1
}

cleanup() {
	local exit_code=$?
	if [[ "$exit_code" -ne 0 ]]; then
		echo
		echo "Dogfood graph-analysis compose verification failed."
		echo "Useful logs:"
		echo "  $COMPOSE_DISPLAY logs --tail=200 eshu"
		echo "  $COMPOSE_DISPLAY logs --tail=200 bootstrap-index"
		echo "  $COMPOSE_DISPLAY logs --tail=200 resolution-engine"
		echo "  $COMPOSE_DISPLAY logs --tail=200 neo4j"
		[[ -f "$DIRECT_CALLERS_FILE" ]] && { echo "Last direct-callers payload:"; cat "$DIRECT_CALLERS_FILE"; }
		[[ -f "$DIRECT_CALLEES_FILE" ]] && { echo "Last direct-callees payload:"; cat "$DIRECT_CALLEES_FILE"; }
		[[ -f "$TRANSITIVE_CALLERS_FILE" ]] && { echo "Last transitive-callers payload:"; cat "$TRANSITIVE_CALLERS_FILE"; }
		[[ -f "$CALL_CHAIN_FILE" ]] && { echo "Last call-chain payload:"; cat "$CALL_CHAIN_FILE"; }
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
	export ESHU_FILESYSTEM_HOST_ROOT="$DEFAULT_HOST_ROOT"
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
	echo "Using host root: $ESHU_FILESYSTEM_HOST_ROOT"
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
eshu_compose_wait_for_bootstrap_exit 1800
echo "Waiting for API health..."
eshu_compose_wait_for_http "http://localhost:${API_PORT}/health" 120 2
echo "Reading API bearer token from the running API container..."
API_KEY="$(eshu_compose_read_api_key)"
if [[ -n "$API_KEY" ]]; then
	echo "Found ESHU_API_KEY in the API container environment."
else
	echo "No ESHU_API_KEY is set in the API container; using unauthenticated local API access."
fi
echo "Waiting for /index-status queue completion..."
eshu_compose_wait_for_index_completion 240 5 "$INDEX_STATUS_FILE"

echo "Discovering dogfood repository selector..."
discover_repo_selector
verify_repository_catalog
echo "Using dogfood repository selector: $REPO_SELECTOR"

echo "Waiting for dogfood graph-analysis projection visibility..."
wait_for_graph_analysis_projection 90 2

echo "Verifying dogfood graph-analysis API and canonical graph state..."
verify_direct_callers
verify_direct_callees
verify_transitive_callers
verify_call_chain
verify_graph_state

echo
echo "Dogfood graph-analysis compose verification passed."
echo "API: $API_BASE_URL"
echo "Repository selector: $REPO_SELECTOR"
echo "Jaeger UI: $JAEGER_URL"
echo "Stack teardown: $COMPOSE_DISPLAY down -v"
