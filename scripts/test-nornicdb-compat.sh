#!/usr/bin/env bash
# NornicDB compatibility test for Eshu.
#
# Starts the full Eshu stack with NornicDB replacing Neo4j, verifies Bolt
# connectivity, runs the integration test suite, and reports results.
#
# Usage:
#   ./scripts/test-nornicdb-compat.sh              # Full run
#   ./scripts/test-nornicdb-compat.sh --no-build   # Skip docker build (reuse existing images)
#   ./scripts/test-nornicdb-compat.sh --keep        # Don't tear down on exit
#
# Prerequisites:
#   - Docker plus docker compose or docker-compose
#   - Current Go test toolchain for `./tests/run_tests.sh integration`

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

BUILD_FLAG="--build"
KEEP=false

for arg in "$@"; do
    case "$arg" in
        --no-build) BUILD_FLAG="" ;;
        --keep)     KEEP=true ;;
        --help|-h)
            echo "Usage: $0 [--no-build] [--keep]"
            echo "  --no-build  Skip docker image rebuild"
            echo "  --keep      Don't tear down the stack on exit"
            exit 0
            ;;
    esac
done

if docker compose version >/dev/null 2>&1; then
    COMPOSE_CMD=(docker compose -f "$REPO_ROOT/docker-compose.yaml")
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE_CMD=(docker-compose -f "$REPO_ROOT/docker-compose.yaml")
else
    echo "Missing required compose command: docker compose or docker-compose" >&2
    exit 1
fi

GRAPH_SERVICE=nornicdb
GRAPH_NETWORK=""

log() { echo "[$(date +%H:%M:%S)] $*"; }

discover_graph_network() {
    local cid
    cid=$("${COMPOSE_CMD[@]}" ps -q "$GRAPH_SERVICE" 2>/dev/null || true)
    if [ -z "$cid" ]; then
        echo "Could not determine $GRAPH_SERVICE container id" >&2
        return 1
    fi
    GRAPH_NETWORK="$(
        docker inspect --format '{{range $name, $_ := .NetworkSettings.Networks}}{{println $name}}{{end}}' "$cid" |
            awk 'NF {print; exit}'
    )"
    if [ -z "$GRAPH_NETWORK" ]; then
        echo "Could not determine $GRAPH_SERVICE compose network" >&2
        return 1
    fi
}

run_cypher() {
    docker run --rm --network "$GRAPH_NETWORK" neo4j:2026-community \
        cypher-shell -a "bolt://$GRAPH_SERVICE:7687" \
        -u "$NEO4J_USERNAME" \
        -p "$NEO4J_PASSWORD" \
        "$@"
}

cleanup() {
    if [ "$KEEP" = false ]; then
        log "Tearing down NornicDB stack..."
        cd "$REPO_ROOT"
        "${COMPOSE_CMD[@]}" down -v 2>/dev/null || true
    else
        log "Stack left running (--keep). Tear down with:"
        log "  ${COMPOSE_CMD[*]} down -v"
    fi
}

# ---------------------------------------------------------------------------
# 1. Start the stack
# ---------------------------------------------------------------------------
log "=== Step 1: Starting Eshu stack with NornicDB ==="
cd "$REPO_ROOT"

# Clean up any previous run
"${COMPOSE_CMD[@]}" down -v 2>/dev/null || true

# Register cleanup trap
trap cleanup EXIT

"${COMPOSE_CMD[@]}" up ${BUILD_FLAG:+$BUILD_FLAG} -d 2>&1 | tail -10

# ---------------------------------------------------------------------------
# 2. Wait for NornicDB to become healthy
# ---------------------------------------------------------------------------
log "=== Step 2: Waiting for NornicDB to be healthy ==="

MAX_WAIT=120
WAITED=0
while [ $WAITED -lt $MAX_WAIT ]; do
    CID=$("${COMPOSE_CMD[@]}" ps -q "$GRAPH_SERVICE" 2>/dev/null || true)
    if [ -n "$CID" ]; then
        HEALTH=$(docker inspect --format '{{.State.Health.Status}}' "$CID" 2>/dev/null || echo "unknown")
    else
        HEALTH="starting"
    fi
    if echo "$HEALTH" | rg -qi "healthy"; then
        log "NornicDB is healthy after ${WAITED}s"
        break
    fi
    if [ $WAITED -gt 0 ] && [ $((WAITED % 10)) -eq 0 ]; then
        log "  Still waiting... ${WAITED}s (status: $HEALTH)"
    fi
    sleep 2
    WAITED=$((WAITED + 2))
done

if [ $WAITED -ge $MAX_WAIT ]; then
    log "ERROR: NornicDB did not become healthy within ${MAX_WAIT}s"
    log "Container logs:"
    "${COMPOSE_CMD[@]}" logs "$GRAPH_SERVICE" 2>&1 | tail -30
    exit 1
fi

# ---------------------------------------------------------------------------
# 3. Basic Bolt connectivity test
# ---------------------------------------------------------------------------
log "=== Step 3: Bolt connectivity test ==="

export NEO4J_URI=bolt://localhost:${NEO4J_BOLT_PORT:-7687}
export NEO4J_USERNAME=neo4j
export NEO4J_PASSWORD=${ESHU_NEO4J_PASSWORD:-change-me}
export NEO4J_DATABASE=nornic
discover_graph_network
BOLT_RESULT="$(
    {
        echo "Connecting to $NEO4J_URI as $NEO4J_USERNAME..."
        run_cypher --format plain "RETURN 1 AS n" | tail -n 1
        run_cypher "CREATE (t:_NornicDBTest {ts: timestamp()}) RETURN t;" >/dev/null
        COUNT="$(run_cypher --format plain "MATCH (t:_NornicDBTest) RETURN count(t) AS cnt" | tail -n 1)"
        echo "Write/read test: OK (count=${COUNT})"
        run_cypher "MATCH (t:_NornicDBTest) DELETE t;" >/dev/null
        echo "Cleanup: OK"
        echo "All Bolt connectivity checks passed."
    } 2>&1
)" || {
    log "FAIL: Bolt connectivity test failed"
    echo "$BOLT_RESULT"
    exit 1
}

if ! printf '%s\n' "$BOLT_RESULT" | rg -q '^1$'; then
    log "FAIL: Bolt connectivity test did not return the expected sentinel"
    echo "$BOLT_RESULT"
    exit 1
fi

echo "$BOLT_RESULT" | sed '/^1$/d'

# ---------------------------------------------------------------------------
# 4. Wait for bootstrap indexer to complete
# ---------------------------------------------------------------------------
log "=== Step 4: Waiting for bootstrap indexer ==="

BOOTSTRAP_CONTAINER=$("${COMPOSE_CMD[@]}" ps -q bootstrap-index 2>/dev/null || echo "")
if [ -z "$BOOTSTRAP_CONTAINER" ]; then
    log "WARNING: bootstrap-index container not found, skipping wait"
else
    MAX_WAIT=300
    WAITED=0
    while [ $WAITED -lt $MAX_WAIT ]; do
        STATUS=$(docker inspect --format='{{.State.Status}}' "$BOOTSTRAP_CONTAINER" 2>/dev/null || echo "unknown")
        if [ "$STATUS" = "exited" ]; then
            EXIT_CODE=$(docker inspect --format='{{.State.ExitCode}}' "$BOOTSTRAP_CONTAINER" 2>/dev/null || echo "?")
            log "Bootstrap indexer exited with code $EXIT_CODE after ${WAITED}s"
            if [ "$EXIT_CODE" != "0" ]; then
                log "ERROR: Bootstrap indexer failed"
                "${COMPOSE_CMD[@]}" logs bootstrap-index 2>&1 | tail -40
                exit 1
            fi
            break
        fi
        sleep 5
        WAITED=$((WAITED + 5))
        if [ $((WAITED % 30)) -eq 0 ]; then
            log "  Bootstrap still running... ${WAITED}s"
        fi
    done

    if [ $WAITED -ge $MAX_WAIT ]; then
        log "ERROR: Bootstrap indexer timed out after ${MAX_WAIT}s"
        "${COMPOSE_CMD[@]}" logs bootstrap-index 2>&1 | tail -40
        exit 1
    fi
fi

# ---------------------------------------------------------------------------
# 5. Run integration test suite
# ---------------------------------------------------------------------------
log "=== Step 5: Running integration tests ==="

export ESHU_CONTENT_STORE_DSN="postgresql://eshu:${ESHU_POSTGRES_PASSWORD:-change-me}@localhost:${ESHU_POSTGRES_PORT:-15432}/eshu"
export ESHU_POSTGRES_DSN="$ESHU_CONTENT_STORE_DSN"

INTEGRATION_RESULT=0
"$REPO_ROOT/tests/run_tests.sh" integration 2>&1 || INTEGRATION_RESULT=$?

# ---------------------------------------------------------------------------
# 6. Summary
# ---------------------------------------------------------------------------
log ""
log "========================================="
log "  NornicDB Compatibility Test Results"
log "========================================="
log ""
log "  Bolt connectivity:     PASS"

if [ $INTEGRATION_RESULT -eq 0 ]; then
    log "  Integration tests:     PASS"
else
    log "  Integration tests:     FAIL (exit code $INTEGRATION_RESULT)"
fi

log ""
log "  NornicDB image:        ${NORNICDB_IMAGE:-nornicdb-patched:latest}"
log "  Bolt URI:              $NEO4J_URI"
log ""

if [ $INTEGRATION_RESULT -ne 0 ]; then
    log "Some tests failed. Review output above for details."
    log "NornicDB logs:"
    "${COMPOSE_CMD[@]}" logs "$GRAPH_SERVICE" 2>&1 | tail -20
    exit 1
fi

log "All tests passed -- NornicDB is compatible with this Eshu build."
