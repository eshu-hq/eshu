#!/usr/bin/env bash
#
# Runs the disaster-recovery procedure in
# docs/public/operate/graph-rebuild-from-facts.md end to end against a local
# Compose stack, and times it.
#
# The claim this script exists to test is that Eshu's graph is a projection:
# throw it away, keep Postgres, and the graph can be rebuilt from the facts
# Postgres still holds. That is easy to assert and was never executed, so this
# executes it:
#
#   index a corpus -> snapshot graph counts -> wipe the graph volume
#   -> reapply graph schema -> rebuild every scope from facts -> drain
#   -> compare counts against the snapshot
#
# Pass 2 repeats the rebuild with the workers killed partway through, to show an
# interrupted rebuild converges to the same counts after a restart rather than
# needing a wipe and a fresh start.
#
# Set ESHU_DR_SKIP_INTERRUPT=true to run pass 1 only.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_PROJECT="${ESHU_DR_COMPOSE_PROJECT:-eshu-dr-rebuild}"
KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
SKIP_INTERRUPT="${ESHU_DR_SKIP_INTERRUPT:-false}"
BOOTSTRAP_TIMEOUT="${ESHU_DR_BOOTSTRAP_TIMEOUT:-1800}"
DRAIN_TIMEOUT="${ESHU_DR_DRAIN_TIMEOUT:-1800}"

TMP_DIR="$(mktemp -d)"
COMPOSE_CMD=()

# Services that hold a graph write path. They stop before a wipe so nothing
# writes into a volume that is about to be removed, or into a graph that has no
# schema yet.
GRAPH_WRITERS=(eshu mcp-server ingester resolution-engine projector
	workflow-coordinator component-extension-collector webhook-listener)

cleanup() {
	local exit_code=$?
	if [[ "$exit_code" -ne 0 ]]; then
		echo
		echo "Graph rebuild-from-facts verification FAILED (exit $exit_code)."
		echo "Logs:"
		echo "  docker compose -p $COMPOSE_PROJECT logs --tail=200 db-migrate"
		echo "  docker compose -p $COMPOSE_PROJECT logs --tail=200 ingester"
		echo "  docker compose -p $COMPOSE_PROJECT logs --tail=200 resolution-engine"
	fi
	if [[ "$KEEP_STACK" != "true" && ${#COMPOSE_CMD[@]} -gt 0 ]]; then
		"${COMPOSE_CMD[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
	fi
	rm -rf "$TMP_DIR"
	exit "$exit_code"
}
trap cleanup EXIT

require_tool() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Missing required tool: $1" >&2
		exit 1
	}
}

require_tool docker
require_tool curl
require_tool jq
require_tool rg

if docker compose version >/dev/null 2>&1; then
	COMPOSE_CMD=(docker compose -p "$COMPOSE_PROJECT" -f docker-compose.yaml)
else
	echo "docker compose is required" >&2
	exit 1
fi

# Private host ports. The shared 7474/7687/15432 set and the 1577x/1877x set the
# promotion gate uses are both avoided so this can run beside them.
export NEO4J_HTTP_PORT="${NEO4J_HTTP_PORT:-17694}"
export NEO4J_BOLT_PORT="${NEO4J_BOLT_PORT:-17695}"
export ESHU_POSTGRES_PORT="${ESHU_POSTGRES_PORT:-15694}"
export ESHU_HTTP_PORT="${ESHU_HTTP_PORT:-18694}"
export ESHU_MCP_PORT="${ESHU_MCP_PORT:-18695}"
export ESHU_WORKFLOW_COORDINATOR_HTTP_PORT="${ESHU_WORKFLOW_COORDINATOR_HTTP_PORT:-18696}"
export ESHU_COMPONENT_EXTENSION_COLLECTOR_HTTP_PORT="${ESHU_COMPONENT_EXTENSION_COLLECTOR_HTTP_PORT:-18697}"
export ESHU_BOOTSTRAP_METRICS_PORT="${ESHU_BOOTSTRAP_METRICS_PORT:-19694}"
export ESHU_API_METRICS_PORT="${ESHU_API_METRICS_PORT:-19695}"
export ESHU_MCP_METRICS_PORT="${ESHU_MCP_METRICS_PORT:-19696}"
export ESHU_INGESTER_METRICS_PORT="${ESHU_INGESTER_METRICS_PORT:-19697}"
export ESHU_RESOLUTION_ENGINE_METRICS_PORT="${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-19698}"
export ESHU_PROJECTOR_METRICS_PORT="${ESHU_PROJECTOR_METRICS_PORT:-19699}"
export ESHU_WORKFLOW_COORDINATOR_METRICS_PORT="${ESHU_WORKFLOW_COORDINATOR_METRICS_PORT:-19700}"
export ESHU_COMPONENT_EXTENSION_COLLECTOR_METRICS_PORT="${ESHU_COMPONENT_EXTENSION_COLLECTOR_METRICS_PORT:-19701}"

API_BASE="http://localhost:${ESHU_HTTP_PORT}"
GRAPH_BASE="http://localhost:${NEO4J_HTTP_PORT}"

psql_scalar() {
	"${COMPOSE_CMD[@]}" exec -T postgres \
		psql -U eshu -d eshu -Atc "$1" | tr -d '\r'
}

# graph_scalar runs one read query over NornicDB's transactional HTTP endpoint
# and returns the single scalar it produced.
graph_scalar() {
	local statement="$1"
	curl -fsS -H 'Content-Type: application/json' \
		-d "$(jq -nc --arg s "$statement" '{statements:[{statement:$s}]}')" \
		"${GRAPH_BASE}/db/nornic/tx/commit" \
		| jq -r '.results[0].data[0].row[0] // 0'
}

wait_for_http() {
	local url="$1" attempts="$2"
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		if curl -fsS "$url" >/dev/null 2>&1; then return 0; fi
		sleep 2
	done
	echo "Timed out waiting for $url" >&2
	return 1
}

# wait_for_service_exit blocks until a one-shot Compose service exits, and fails
# on a nonzero exit code so a silent schema-bootstrap failure cannot be mistaken
# for a completed step.
wait_for_service_exit() {
	local service="$1" timeout_seconds="$2"
	local deadline=$((SECONDS + timeout_seconds))
	while ((SECONDS < deadline)); do
		local container_id state exit_code
		container_id="$("${COMPOSE_CMD[@]}" ps -a -q "$service")"
		if [[ -z "$container_id" ]]; then
			sleep 2
			continue
		fi
		state="$(docker inspect --format='{{.State.Status}}' "$container_id")"
		if [[ "$state" == "exited" ]]; then
			exit_code="$(docker inspect --format='{{.State.ExitCode}}' "$container_id")"
			if [[ "$exit_code" != "0" ]]; then
				echo "$service exited with code $exit_code" >&2
				"${COMPOSE_CMD[@]}" logs --tail=100 "$service" >&2 || true
				return 1
			fi
			return 0
		fi
		sleep 2
	done
	echo "Timed out waiting for $service to exit" >&2
	return 1
}

# wait_for_queue_terminal blocks until the rebuild has no work left anywhere,
# then fails if anything ended in a non-terminal state. Both halves matter: an
# empty in-flight set with dead-letter rows is a failed rebuild, not a finished
# one.
#
# "Anywhere" means two queues, not one. `fact_work_items` holds projector and
# reducer work; `shared_projection_intents` holds the shared edge backlog that
# the reducer domains feed, and it drains on its own worker cycle. Watching only
# the first one snapshots the graph mid-rebuild. Measured on a rebuild of this
# corpus: `fact_work_items` was terminal for four minutes while 21 shared
# intents were still open, and the relationship count rose from 3,277 to 3,286
# when they finally drained. Every one of those edges would have been reported
# as missing.
queue_active_count() {
	psql_scalar "SELECT
	    (SELECT count(*) FROM fact_work_items WHERE status IN ('pending','claimed','running'))
	  + (SELECT count(*) FROM shared_projection_intents WHERE completed_at IS NULL);"
}

wait_for_queue_terminal() {
	local timeout_seconds="$1"
	local deadline=$((SECONDS + timeout_seconds))
	local active residual
	while ((SECONDS < deadline)); do
		active="$(queue_active_count)"
		if [[ "$active" == "0" ]]; then
			sleep 5
			active="$(queue_active_count)"
			if [[ "$active" == "0" ]]; then
				residual="$(psql_scalar "SELECT count(*) FROM fact_work_items WHERE status NOT IN ('succeeded','superseded');")"
				if [[ "$residual" != "0" ]]; then
					echo "Queue drained but $residual work items are not in a clean terminal state:" >&2
					psql_scalar "SELECT status, failure_class, count(*) FROM fact_work_items WHERE status NOT IN ('succeeded','superseded') GROUP BY 1,2;" >&2
					return 1
				fi
				return 0
			fi
		fi
		sleep 5
	done
	echo "Timed out waiting for the queue to reach a terminal state" >&2
	return 1
}

# graph_pairs runs a two-column key/count query and emits `key=count` lines.
graph_pairs() {
	local statement="$1" prefix="$2"
	curl -fsS -H 'Content-Type: application/json' \
		-d "$(jq -nc --arg s "$statement" '{statements:[{statement:$s}]}')" \
		"${GRAPH_BASE}/db/nornic/tx/commit" \
		| jq -r --arg p "$prefix" '.results[0].data[] | "\($p)\(.row[0])=\(.row[1])"'
}

# snapshot_counts records totals plus a count for every node label and every
# relationship type. The per-label breakdown is what makes a mismatch
# actionable: bare totals tell you the rebuild is short without telling you
# which part of the graph did not come back.
#
# This is count equivalence, not the canonical byte-identity the issue's
# acceptance criterion ultimately wants. It catches a missing label or edge
# family; it would not catch a property that changed value while cardinality
# stayed the same.
snapshot_counts() {
	local out_file="$1"
	{
		echo "total_nodes=$(graph_scalar 'MATCH (n) RETURN count(n) AS c')"
		echo "total_rels=$(graph_scalar 'MATCH ()-[r]->() RETURN count(r) AS c')"
		graph_pairs 'MATCH (n) UNWIND labels(n) AS l RETURN l AS k, count(*) AS c ORDER BY l' 'label:'
		graph_pairs 'MATCH ()-[r]->() RETURN type(r) AS k, count(*) AS c ORDER BY k' 'rel:'
	} >"$out_file"
}

compare_counts() {
	local before="$1" after="$2" label="$3"
	if diff -u "$before" "$after" >"$TMP_DIR/counts.diff"; then
		echo "$label: graph counts match the pre-wipe snapshot."
		return 0
	fi
	echo "$label: rebuilt graph does not match the pre-wipe snapshot:" >&2
	cat "$TMP_DIR/counts.diff" >&2
	return 1
}

wipe_graph() {
	echo "Stopping graph writers..."
	"${COMPOSE_CMD[@]}" stop "${GRAPH_WRITERS[@]}" >/dev/null

	echo "Wiping the graph volume (Postgres is preserved)..."
	"${COMPOSE_CMD[@]}" rm -sf nornicdb >/dev/null
	docker volume rm "${COMPOSE_PROJECT}_nornicdb_data" >/dev/null
	"${COMPOSE_CMD[@]}" up -d nornicdb >/dev/null
	wait_for_http "${GRAPH_BASE}/health" 60

	local remaining
	remaining="$(graph_scalar 'MATCH (n) RETURN count(n) AS c')"
	if [[ "$remaining" != "0" ]]; then
		echo "Graph still holds $remaining nodes after the wipe" >&2
		return 1
	fi
	echo "Graph is empty."
}

# reapply_graph_schema runs schema bootstrap with the marker override. Without
# it the run is a no-op: the "applied" marker is a Postgres row, Postgres was
# preserved, and bootstrap would return before opening a graph connection,
# leaving the rebuild to write into a backend with no indexes or constraints.
reapply_graph_schema() {
	echo "Reapplying graph schema with ESHU_GRAPH_SCHEMA_FORCE_REAPPLY=true..."
	"${COMPOSE_CMD[@]}" rm -sf db-migrate >/dev/null 2>&1 || true
	ESHU_GRAPH_SCHEMA_FORCE_REAPPLY=true "${COMPOSE_CMD[@]}" up -d db-migrate >/dev/null
	wait_for_service_exit db-migrate 600

	if ! "${COMPOSE_CMD[@]}" logs db-migrate 2>&1 | rg -q 'bootstrap.graph.applied'; then
		echo "Schema bootstrap did not apply graph schema. The marker override did not take effect:" >&2
		"${COMPOSE_CMD[@]}" logs --tail=50 db-migrate >&2
		return 1
	fi
	echo "Graph schema applied."
}

# start_services restarts the stopped writers with `start`, not `up -d`.
# `ingester` declares depends_on bootstrap-index: service_completed_successfully,
# and `up` is entitled to recreate that one-shot. If it did, bootstrap-index
# would re-index the corpus and re-project it, which both contaminates the
# rebuild timing and means the graph was not rebuilt from facts by the command
# under test. `start` restarts existing containers and resolves no dependencies.
start_services() {
	"${COMPOSE_CMD[@]}" start "${GRAPH_WRITERS[@]}" >/dev/null
	wait_for_http "${API_BASE}/health" 120

	local bootstrap_state
	bootstrap_state="$(docker inspect --format='{{.State.Status}}' \
		"$("${COMPOSE_CMD[@]}" ps -a -q bootstrap-index)")"
	if [[ "$bootstrap_state" != "exited" ]]; then
		echo "bootstrap-index is $bootstrap_state, expected exited: it restarted and would re-index the corpus, so the rebuild would not be measuring rebuild-from-facts" >&2
		return 1
	fi
}

API_KEY=""

# resolve_api_key reads the shared admin key the API persisted under ESHU_HOME.
# The Compose stack runs with ESHU_AUTO_GENERATE_API_KEY=true and no configured
# key, so the key does not exist until the API has started once. Without it the
# rebuild request gets a 401, because recover-generations requires an admin
# (all-scopes) token.
resolve_api_key() {
	API_KEY="$("${COMPOSE_CMD[@]}" exec -T eshu \
		sh -lc 'sed -n "s/^ESHU_API_KEY=//p" /data/.eshu/.env' | tr -d '\r\n')"
	if [[ -z "$API_KEY" ]]; then
		echo "Could not read the generated admin API key from the eshu container" >&2
		return 1
	fi
}

# request_rebuild issues the one operator command this whole procedure reduces
# to and echoes how many scopes it queued.
request_rebuild() {
	local idempotency_key="$1" response_file="$TMP_DIR/rebuild-$1.json"
	curl -fsS -X POST "${API_BASE}/api/v0/admin/recover-generations" \
		-H 'content-type: application/json' \
		-H "authorization: Bearer ${API_KEY}" \
		-d "$(jq -nc --arg k "$idempotency_key" '{
			all_scopes: true,
			reason: "graph rebuild-from-facts verification",
			idempotency_key: $k
		}')" >"$response_file"

	jq -e '.status == "recovered"' "$response_file" >/dev/null || {
		echo "Rebuild request was not accepted:" >&2
		cat "$response_file" >&2
		return 1
	}
	jq -r '.enqueued' "$response_file"
}

human_duration() {
	local total="$1"
	printf '%dm%02ds' $((total / 60)) $((total % 60))
}

cd "$REPO_ROOT"

echo "=== Phase 1: index the corpus ==="
# Start from nothing. `down` alone can leave a container behind when a previous
# run was killed hard enough to skip its own cleanup, and the next `up` then
# fails on a name conflict rather than on anything to do with the rebuild.
"${COMPOSE_CMD[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
docker ps -a --format '{{.Names}}' | rg "^${COMPOSE_PROJECT}-" \
	| xargs -r docker rm -f >/dev/null 2>&1 || true
docker volume rm "${COMPOSE_PROJECT}_nornicdb_data" >/dev/null 2>&1 || true
"${COMPOSE_CMD[@]}" up -d --build >/dev/null
wait_for_service_exit bootstrap-index "$BOOTSTRAP_TIMEOUT"
wait_for_http "${API_BASE}/health" 120
resolve_api_key
wait_for_queue_terminal "$DRAIN_TIMEOUT"

snapshot_counts "$TMP_DIR/before.txt"
echo "Pre-wipe graph:"
sed 's/^/  /' "$TMP_DIR/before.txt"

BEFORE_NODES="$(rg -o 'total_nodes=(\d+)' -r '$1' "$TMP_DIR/before.txt")"
if [[ "$BEFORE_NODES" == "0" ]]; then
	echo "The corpus produced an empty graph; there is nothing to prove a rebuild against." >&2
	exit 1
fi
FACT_ROWS="$(psql_scalar 'SELECT count(*) FROM fact_records;')"
ACTIVE_SCOPES="$(psql_scalar "SELECT count(*) FROM ingestion_scopes WHERE status = 'active' AND active_generation_id IS NOT NULL;")"
echo "Postgres holds $FACT_ROWS fact_records across $ACTIVE_SCOPES active scopes."

echo
echo "=== Phase 2: wipe, reapply schema, rebuild (timed) ==="
wipe_graph
reapply_graph_schema
start_services

REBUILD_START="$SECONDS"
ENQUEUED="$(request_rebuild "dr-rebuild-pass1-$$")"
echo "Rebuild enqueued $ENQUEUED scopes."
wait_for_queue_terminal "$DRAIN_TIMEOUT"
REBUILD_SECONDS=$((SECONDS - REBUILD_START))

snapshot_counts "$TMP_DIR/after.txt"
echo "Rebuilt graph:"
sed 's/^/  /' "$TMP_DIR/after.txt"

# Report the timing before comparing. The measurement is valid whether or not
# the counts match, and a mismatch that swallowed the number would mean rerunning
# the whole thing to get it back.
echo
echo "graph_rebuild_seconds=${REBUILD_SECONDS} ($(human_duration "$REBUILD_SECONDS"))"
echo "  scopes_enqueued=${ENQUEUED} fact_records=${FACT_ROWS} nodes=${BEFORE_NODES}"
echo "  load_at_finish=$(uptime | sed 's/.*averages: //')"
echo

compare_counts "$TMP_DIR/before.txt" "$TMP_DIR/after.txt" "Pass 1 (clean rebuild)"

if [[ "$SKIP_INTERRUPT" == "true" ]]; then
	echo
	echo "Skipping the interrupted-rebuild pass (ESHU_DR_SKIP_INTERRUPT=true)."
	echo "Graph rebuild-from-facts verification passed."
	exit 0
fi

echo
echo "=== Phase 3: interrupted rebuild ==="
wipe_graph
reapply_graph_schema
start_services

request_rebuild "dr-rebuild-pass2a-$$" >/dev/null
echo "Rebuild started; letting it make partial progress..."
sleep 20

INTERRUPT_NODES="$(graph_scalar 'MATCH (n) RETURN count(n) AS c')"
echo "Killing the projection workers mid-drain (graph holds $INTERRUPT_NODES nodes)."
"${COMPOSE_CMD[@]}" kill ingester projector resolution-engine >/dev/null

REMAINING="$(psql_scalar "SELECT count(*) FROM fact_work_items WHERE status IN ('pending','claimed','running');")"
echo "Work left in flight at the kill: $REMAINING items."

echo "Restarting workers and re-issuing the rebuild..."
"${COMPOSE_CMD[@]}" start ingester projector resolution-engine >/dev/null
request_rebuild "dr-rebuild-pass2b-$$" >/dev/null
wait_for_queue_terminal "$DRAIN_TIMEOUT"

snapshot_counts "$TMP_DIR/after-interrupt.txt"
echo "Graph after the interrupted rebuild:"
sed 's/^/  /' "$TMP_DIR/after-interrupt.txt"
compare_counts "$TMP_DIR/before.txt" "$TMP_DIR/after-interrupt.txt" "Pass 2 (interrupted rebuild)"

echo
echo "Graph rebuild-from-facts verification passed."
echo "  clean rebuild:       ${REBUILD_SECONDS}s ($(human_duration "$REBUILD_SECONDS"))"
echo "  interrupted rebuild: converged to the same counts after a restart"
