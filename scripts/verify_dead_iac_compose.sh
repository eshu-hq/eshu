#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNTIME_LIB="$REPO_ROOT/scripts/lib/compose_verification_runtime_common.sh"
ASSERT_LIB="$REPO_ROOT/scripts/lib/compose_verification_assertions.sh"
SOURCE_FIXTURE_ROOT="$REPO_ROOT/tests/fixtures/product_truth/dead_iac"
RUN_ROOT_BASE="${ESHU_DEAD_IAC_RUN_ROOT:-$REPO_ROOT/.eshu-compose-runs}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-dead-iac-$$}"
RUN_ROOT="$RUN_ROOT_BASE/$COMPOSE_PROJECT_NAME"
FIXTURE_ROOT="$RUN_ROOT/repos"
TMP_DIR="$RUN_ROOT/tmp"
INDEX_STATUS_FILE="$TMP_DIR/index-status.json"
API_RESPONSE_FILE="$TMP_DIR/dead-iac-api.json"
MCP_RESPONSE_FILE="$TMP_DIR/dead-iac-mcp.json"
ROW_COUNTS_FILE="$TMP_DIR/iac-row-counts.json"
POSTGRES_ROWS_FILE="$TMP_DIR/iac-reachability-rows.json"
GRAPH_NODES_FILE="$TMP_DIR/graph-repository-nodes.json"
GRAPH_RELATIONSHIPS_FILE="$TMP_DIR/graph-repository-relationships.json"
GRAPH_EVIDENCE_FILE="$TMP_DIR/graph-deployment-evidence.json"
BOOTSTRAP_LOG_FILE="$TMP_DIR/bootstrap.log"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
GRAPH_BACKEND="${ESHU_DEAD_IAC_GRAPH_BACKEND:-nornicdb}"
API_PORT="${ESHU_HTTP_PORT:-8080}"
MCP_PORT="${ESHU_MCP_PORT:-8081}"
API_BASE_URL="http://localhost:${API_PORT}/api/v0"
API_KEY=""
COMPOSE_CMD=()
COMPOSE_DISPLAY=""

NEO4J_HTTP_PORT_BASE="${NEO4J_HTTP_PORT:-27474}"
NEO4J_BOLT_PORT_BASE="${NEO4J_BOLT_PORT:-27687}"
ESHU_POSTGRES_PORT_BASE="${ESHU_POSTGRES_PORT:-35432}"
ESHU_HTTP_PORT_BASE="${ESHU_HTTP_PORT:-28080}"
JAEGER_UI_PORT_BASE="${JAEGER_UI_PORT:-36686}"
OTEL_COLLECTOR_OTLP_GRPC_PORT_BASE="${OTEL_COLLECTOR_OTLP_GRPC_PORT:-34317}"
OTEL_COLLECTOR_OTLP_HTTP_PORT_BASE="${OTEL_COLLECTOR_OTLP_HTTP_PORT:-34318}"
OTEL_COLLECTOR_PROMETHEUS_PORT_BASE="${OTEL_COLLECTOR_PROMETHEUS_PORT:-39464}"
ESHU_API_METRICS_PORT_BASE="${ESHU_API_METRICS_PORT:-29464}"
ESHU_BOOTSTRAP_METRICS_PORT_BASE="${ESHU_BOOTSTRAP_METRICS_PORT:-29467}"
ESHU_MCP_PORT_BASE="${ESHU_MCP_PORT:-28081}"
ESHU_MCP_METRICS_PORT_BASE="${ESHU_MCP_METRICS_PORT:-29468}"
ESHU_INGESTER_METRICS_PORT_BASE="${ESHU_INGESTER_METRICS_PORT:-29465}"
ESHU_RESOLUTION_ENGINE_METRICS_PORT_BASE="${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-29466}"

source "$RUNTIME_LIB"
source "$ASSERT_LIB"

cleanup() {
	local exit_code=$?
	if [[ "$exit_code" -ne 0 ]]; then
		echo
		echo "Dead-IaC compose verification failed."
		echo "Useful logs:"
		echo "  $COMPOSE_DISPLAY logs --tail=200 bootstrap-index"
		echo "  $COMPOSE_DISPLAY logs --tail=200 eshu"
		echo "  $COMPOSE_DISPLAY logs --tail=200 mcp-server"
		[[ -f "$INDEX_STATUS_FILE" ]] && { echo "Last index-status payload:"; cat "$INDEX_STATUS_FILE"; }
	fi
	if [[ "$KEEP_STACK" != "true" ]]; then
		"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
		rm -rf "$RUN_ROOT"
	fi
	exit "$exit_code"
}
trap cleanup EXIT

require_real_directory() {
	local path="$1"
	local resolved
	[[ -d "$path" ]] || {
		echo "Not a directory: $path" >&2
		return 1
	}
	resolved="$(cd "$path" && pwd -P)"
	[[ "$resolved" == "$path" ]] || {
		echo "Directory must be real, not a symlink: $path -> $resolved" >&2
		return 1
	}
}

build_fixture_repositories() {
	rm -rf "$RUN_ROOT"
	mkdir -p "$FIXTURE_ROOT" "$TMP_DIR"
	local repo
	for repo_path in "$SOURCE_FIXTURE_ROOT"/*; do
		[[ -d "$repo_path" ]] || continue
		repo="$(basename "$repo_path")"
		mkdir -p "$FIXTURE_ROOT/$repo"
		cp -R "$repo_path/." "$FIXTURE_ROOT/$repo/"
		git -C "$FIXTURE_ROOT/$repo" init -q
		git -C "$FIXTURE_ROOT/$repo" add -A
		git -C "$FIXTURE_ROOT/$repo" \
			-c user.email=eshu-fixture@example.invalid \
			-c user.name="Eshu Fixture" \
			commit --allow-empty -q -m fixture
	done
	require_real_directory "$FIXTURE_ROOT"
}

build_repo_rules_json() {
	local -a repos=()
	local repo_path
	for repo_path in "$FIXTURE_ROOT"/*; do
		[[ -d "$repo_path" ]] || continue
		repos+=("$(basename "$repo_path")")
	done
	jq -cn --args '{exact: $ARGS.positional}' "${repos[@]}"
}

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
	MCP_PORT="$ESHU_MCP_PORT"
	API_BASE_URL="http://localhost:${API_PORT}/api/v0"
}

refresh_compose_ports() {
	local mapped
	mapped="$("${COMPOSE_CMD[@]}" port eshu 8080 | tail -n 1)"
	[[ -n "$mapped" ]] || { echo "Could not determine API port." >&2; return 1; }
	export ESHU_HTTP_PORT="${mapped##*:}"
	mapped="$("${COMPOSE_CMD[@]}" port mcp-server 8080 | tail -n 1)"
	[[ -n "$mapped" ]] || { echo "Could not determine MCP port." >&2; return 1; }
	export ESHU_MCP_PORT="${mapped##*:}"
	API_PORT="$ESHU_HTTP_PORT"
	MCP_PORT="$ESHU_MCP_PORT"
	API_BASE_URL="http://localhost:${API_PORT}/api/v0"
}

api_post_envelope_json() {
	local path="$1" payload="$2" output_file="$3"
	local -a curl_args=(-fsS -X POST -H "Accept: application/eshu.envelope+json" -H "Content-Type: application/json" -d "$payload")
	[[ -z "$API_KEY" ]] || curl_args+=(-H "Authorization: Bearer $API_KEY")
	curl "${curl_args[@]}" "$API_BASE_URL$path" >"$output_file"
}

api_post_json() {
	local path="$1" payload="$2" output_file="$3"
	local -a curl_args=(-fsS -X POST -H "Content-Type: application/json" -d "$payload")
	[[ -z "$API_KEY" ]] || curl_args+=(-H "Authorization: Bearer $API_KEY")
	curl "${curl_args[@]}" "$API_BASE_URL$path" >"$output_file"
}

api_get() {
	local path="$1" output_file="$2"
	local -a curl_args=(-fsS)
	[[ -z "$API_KEY" ]] || curl_args+=(-H "Authorization: Bearer $API_KEY")
	curl "${curl_args[@]}" "$API_BASE_URL$path" >"$output_file"
}

dead_iac_repo_names() {
	printf '%s\n' \
		terraform-stack terraform-modules \
		helm-controller helm-charts \
		ansible-controller ansible-ops \
		kustomize-controller kustomize-config \
		compose-controller compose-app
}

verify_api() {
	local payload
	payload="$(jq -cn --args '{repo_ids: $ARGS.positional, include_ambiguous: true, limit: 100}' \
		$(dead_iac_repo_names))"
	api_post_envelope_json "/iac/dead" "$payload" "$API_RESPONSE_FILE"
	eshu_assert_json_query "$API_RESPONSE_FILE" '
		.data.truth_basis == "materialized_reducer_rows" and
		.data.analysis_status == "materialized_reachability" and
		.data.findings_count == 10 and
		((.data.findings // []) | any(.repo_name == "terraform-modules" and .artifact == "modules/orphan-cache" and .reachability == "unused")) and
		((.data.findings // []) | any(.repo_name == "terraform-modules" and .artifact == "modules/dynamic-target" and .reachability == "ambiguous")) and
		((.data.findings // []) | any(.repo_name == "helm-charts" and .artifact == "charts/orphan-worker" and .reachability == "unused")) and
		((.data.findings // []) | any(.repo_name == "helm-charts" and .artifact == "charts/dynamic-target" and .reachability == "ambiguous")) and
		((.data.findings // []) | any(.repo_name == "ansible-ops" and .artifact == "roles/orphan_maintenance" and .reachability == "unused")) and
		((.data.findings // []) | any(.repo_name == "ansible-ops" and .artifact == "roles/dynamic_role" and .reachability == "ambiguous")) and
		((.data.findings // []) | any(.repo_name == "kustomize-config" and .artifact == "base/orphan-api" and .reachability == "unused")) and
		((.data.findings // []) | any(.repo_name == "kustomize-config" and .artifact == "base/dynamic-target" and .reachability == "ambiguous")) and
		((.data.findings // []) | any(.repo_name == "compose-app" and .artifact == "services/orphan-cache" and .reachability == "unused")) and
		((.data.findings // []) | any(.repo_name == "compose-app" and .artifact == "services/dynamic-target" and .reachability == "ambiguous")) and
		((.data.findings // []) | all(.artifact != "modules/checkout-service" and .artifact != "charts/checkout-service" and .artifact != "charts/worker-service" and .artifact != "roles/checkout_deploy" and .artifact != "base/checkout-service" and .artifact != "overlays/prod" and .artifact != "services/api" and .artifact != "services/worker"))
	' "dead-IaC API response did not match materialized product truth"
}

verify_postgres_rows() {
	"${COMPOSE_CMD[@]}" exec -T postgres psql -U eshu -d eshu -t -A -F '|' -c \
		"SELECT family, reachability, count(*) FROM iac_reachability_rows GROUP BY family, reachability ORDER BY family, reachability;" \
		| jq -R -s 'split("\n") | map(select(length > 0) | split("|")) | map({family:.[0], reachability:.[1], count:(.[2]|tonumber)})' \
		>"$ROW_COUNTS_FILE"
	eshu_assert_json_query "$ROW_COUNTS_FILE" '
		(map(select(.reachability == "used") | .count) | add) == 8 and
		(map(select(.reachability == "unused") | .count) | add) == 5 and
		(map(select(.reachability == "ambiguous") | .count) | add) == 5 and
		(any(.family == "kustomize" and .reachability == "used" and .count == 2)) and
		(any(.family == "kustomize" and .reachability == "unused" and .count == 1)) and
		(any(.family == "kustomize" and .reachability == "ambiguous" and .count == 1)) and
		(any(.family == "compose" and .reachability == "used" and .count == 2)) and
		(any(.family == "compose" and .reachability == "unused" and .count == 1)) and
		(any(.family == "compose" and .reachability == "ambiguous" and .count == 1))
	' "materialized IaC reachability row counts did not match expected truth"
	"${COMPOSE_CMD[@]}" exec -T postgres psql -U eshu -d eshu -t -A -c \
		"SELECT jsonb_pretty(jsonb_agg(jsonb_build_object('repo_id', repo_id, 'family', family, 'artifact_path', artifact_path, 'reachability', reachability, 'finding', finding, 'confidence', confidence, 'evidence', evidence, 'limitations', limitations) ORDER BY family, artifact_path)) FROM iac_reachability_rows;" \
		>"$POSTGRES_ROWS_FILE"
}

verify_mcp() {
	local payload
	payload="$(jq -cn --args '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_dead_iac","arguments":{"repo_ids": $ARGS.positional, "include_ambiguous": true, "limit": 100}}}' \
		$(dead_iac_repo_names))"
	local -a curl_args=(-fsS -X POST -H "Content-Type: application/json" -d "$payload")
	[[ -z "$API_KEY" ]] || curl_args+=(-H "Authorization: Bearer $API_KEY")
	curl "${curl_args[@]}" "http://localhost:${MCP_PORT}/mcp/message" >"$MCP_RESPONSE_FILE"
	eshu_assert_json_query "$MCP_RESPONSE_FILE" '
		((.result.content // []) | map(select(.type == "resource" and .resource.uri == "eshu://tool-result/envelope") | .resource.text | fromjson) | first) as $env |
		.result.isError != true and
		$env.data.truth_basis == "materialized_reducer_rows" and
		$env.data.analysis_status == "materialized_reachability" and
		$env.data.findings_count == 10
	' "dead-IaC MCP response did not mirror the materialized API truth"
}

verify_graph() {
	local repo_list cypher payload
	repo_list="$(dead_iac_repo_names | jq -R . | jq -r -s 'join(", ")')"
	cypher="MATCH (r:Repository) WHERE r.name IN [$repo_list] RETURN r.id AS id, r.name AS name ORDER BY r.name"
	payload="$(jq -cn --arg cypher "$cypher" '{cypher_query: $cypher}')"
	api_post_json "/code/cypher" "$payload" "$GRAPH_NODES_FILE"
	eshu_assert_json_query "$GRAPH_NODES_FILE" '
		(.results // []) as $rows |
		($rows | length == 10) and
		($rows | any(.name == "terraform-stack")) and
		($rows | any(.name == "helm-controller")) and
		($rows | any(.name == "kustomize-controller")) and
		($rows | any(.name == "compose-app"))
	' "graph repository nodes did not include every dead-IaC fixture repository"

	cypher="MATCH (r:Repository)-[rel]->(n) WHERE r.name IN [$repo_list] RETURN r.name AS source, type(rel) AS relationship_type, n.id AS target_id, n.name AS target_name ORDER BY source, relationship_type, target_name LIMIT 200"
	payload="$(jq -cn --arg cypher "$cypher" '{cypher_query: $cypher}')"
	api_post_json "/code/cypher" "$payload" "$GRAPH_RELATIONSHIPS_FILE"
	eshu_assert_json_query "$GRAPH_RELATIONSHIPS_FILE" '
		(.results // []) as $rows |
		($rows | any(.source == "terraform-stack" and .relationship_type == "USES_MODULE" and .target_name == "terraform-modules")) and
		($rows | any(.source == "helm-controller" and .relationship_type == "DEPLOYS_FROM" and .target_name == "helm-charts")) and
		($rows | any(.source == "kustomize-controller" and .relationship_type == "DEPLOYS_FROM" and .target_name == "kustomize-config")) and
		($rows | any(.source == "compose-app" and .relationship_type == "REPO_CONTAINS" and .target_name == "compose.yaml"))
	' "graph relationships did not expose expected IaC repository/evidence edges"

	cypher="MATCH (r:Repository)-[:HAS_DEPLOYMENT_EVIDENCE]->(e) WHERE r.name IN ['terraform-stack', 'helm-controller', 'kustomize-controller'] RETURN r.name AS repo, e.id AS evidence_id, e.path AS path, e.name AS name, e.evidence_kind AS evidence_kind ORDER BY repo, path LIMIT 100"
	payload="$(jq -cn --arg cypher "$cypher" '{cypher_query: $cypher}')"
	api_post_json "/code/cypher" "$payload" "$GRAPH_EVIDENCE_FILE"
	eshu_assert_json_query "$GRAPH_EVIDENCE_FILE" '
		(.results // []) as $rows |
		($rows | any(.repo == "terraform-stack" and .evidence_kind == "TERRAFORM_MODULE_SOURCE")) and
		($rows | any(.repo == "helm-controller" and .evidence_kind == "ARGOCD_APPLICATION_SOURCE")) and
		($rows | any(.repo == "kustomize-controller" and .evidence_kind == "ARGOCD_APPLICATION_SOURCE"))
	' "graph deployment evidence nodes did not expose expected evidence kinds"
}

eshu_require_tool curl
eshu_require_tool docker
eshu_require_tool git
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

if [[ "$GRAPH_BACKEND" == "nornicdb" ]]; then
	COMPOSE_CMD+=(-f docker-compose.yaml)
	COMPOSE_DISPLAY+=" -f docker-compose.yaml"
else
	COMPOSE_CMD+=(-f docker-compose.neo4j.yml)
	COMPOSE_DISPLAY+=" -f docker-compose.neo4j.yml"
fi
COMPOSE_CMD+=(-f docker-compose.telemetry.yml)
COMPOSE_DISPLAY+=" -f docker-compose.telemetry.yml"

cd "$REPO_ROOT"
build_fixture_repositories
export COMPOSE_PROJECT_NAME
export ESHU_FILESYSTEM_HOST_ROOT="$FIXTURE_ROOT"
export ESHU_REPOSITORY_RULES_JSON
ESHU_REPOSITORY_RULES_JSON="$(build_repo_rules_json)"
export ESHU_QUERY_PROFILE="${ESHU_QUERY_PROFILE:-local_full_stack}"
export ESHU_PG_SHARED_BUFFERS="${ESHU_PG_SHARED_BUFFERS:-512MB}"
export ESHU_PG_EFFECTIVE_CACHE_SIZE="${ESHU_PG_EFFECTIVE_CACHE_SIZE:-2GB}"
export ESHU_PG_MAINTENANCE_WORK_MEM="${ESHU_PG_MAINTENANCE_WORK_MEM:-128MB}"
export GOMEMLIMIT="${GOMEMLIMIT:-2GiB}"

"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
compose_started=false
for attempt in 1 2; do
	configure_ports "$(((attempt - 1) * 25))"
	echo "Starting dead-IaC compose stack..."
	echo "Using compose project: $COMPOSE_PROJECT_NAME"
	echo "Using graph backend: $GRAPH_BACKEND"
	echo "Using fixture root: $ESHU_FILESYSTEM_HOST_ROOT"
	echo "Using repository rules: $ESHU_REPOSITORY_RULES_JSON"
	if "${COMPOSE_CMD[@]}" up -d --build; then
		compose_started=true
		break
	fi
	"${COMPOSE_CMD[@]}" down -v >/dev/null 2>&1 || true
	sleep 2
done
[[ "$compose_started" == "true" ]] || { echo "Could not start compose stack." >&2; exit 1; }

refresh_compose_ports
echo "Waiting for bootstrap indexing to finish..."
eshu_compose_wait_for_bootstrap_exit 1800
echo "Waiting for API and MCP health..."
eshu_compose_wait_for_http "http://localhost:${API_PORT}/health" 120 2
eshu_compose_wait_for_http "http://localhost:${MCP_PORT}/healthz" 120 2
API_KEY="$(eshu_compose_read_api_key)"
echo "Waiting for queue completion..."
eshu_compose_wait_for_index_completion 240 5 "$INDEX_STATUS_FILE"

verify_postgres_rows
verify_api
verify_mcp
verify_graph
"${COMPOSE_CMD[@]}" logs --no-color bootstrap-index >"$BOOTSTRAP_LOG_FILE"
rg -n "iac reachability materialized|iac_reachability_materialized" "$BOOTSTRAP_LOG_FILE" >/dev/null

echo
echo "Dead-IaC compose verification passed."
echo "API: $API_BASE_URL"
echo "MCP: http://localhost:${MCP_PORT}/mcp/message"
echo "Stack teardown: COMPOSE_PROJECT_NAME=$COMPOSE_PROJECT_NAME $COMPOSE_DISPLAY down -v"
