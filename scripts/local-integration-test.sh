#!/usr/bin/env bash
# Local integration test runner for Eshu.
# Copies real repos into a flat workspace, runs Compose ingestion against the
# Neo4j compatibility stack, waits for completion, and validates the results.
#
# Usage:
#   ./scripts/local-integration-test.sh                    # Tier 1 (10 repos)
#   ./scripts/local-integration-test.sh --tier2            # Tier 2 (10 + legacy portal)
#   ./scripts/local-integration-test.sh --workspace-only   # Just prepare workspace, don't start compose
#   ./scripts/local-integration-test.sh --validate-only    # Just run validation against running stack
#
# Environment overrides:
#   ESHU_WORKSPACE=/path/to/workspace    # Override workspace location
#   ESHU_GRAPH_WRITE_TX_FILE_BATCH_SIZE=25  # Pass through to docker-compose
#   ESHU_REPO_FILE_PARSE_MULTIPROCESS=true  # Pass through to docker-compose

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Defaults
TIER="tier1"
WORKSPACE_ONLY=false
VALIDATE_ONLY=false
WORKSPACE="${ESHU_WORKSPACE:-"$REPO_ROOT/.eshu-test-workspace"}"
COMPOSE_CMD=()

# Parse args
for arg in "$@"; do
    case "$arg" in
        --tier2) TIER="tier2" ;;
        --workspace-only) WORKSPACE_ONLY=true ;;
        --validate-only) VALIDATE_ONLY=true ;;
        --help|-h)
            echo "Usage: $0 [--tier2] [--workspace-only] [--validate-only]"
            exit 0
            ;;
    esac
done

if docker compose version >/dev/null 2>&1; then
    COMPOSE_CMD=(docker compose -f "$REPO_ROOT/docker-compose.neo4j.yml")
elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE_CMD=(docker-compose -f "$REPO_ROOT/docker-compose.neo4j.yml")
else
    echo "Missing required compose command: docker compose or docker-compose" >&2
    exit 1
fi

# Tier 1 repos (10 repos, ~2,060 parseable files)
TIER1_REPOS=(
    "$HOME/repos/examples/gitops-runtime-config"
    "$HOME/repos/examples/deployment-charts"
    "$HOME/repos/examples/cluster-addons"
    "$HOME/repos/examples/platform-compositions"
    "$HOME/repos/examples/observability-config"
    "$HOME/repos/examples/crossplane-xrd-runtime-role"
    "$HOME/repos/terraform-modules/terraform-module-runtime-identity"
    "$HOME/repos/services/sample-service-api"
    "$HOME/repos/services/sample-home-api"
    "$HOME/repos/terraform-stacks/terraform-stack-marketplace"
)

# Tier 2 adds stress test repos
TIER2_REPOS=(
    "$HOME/repos/services/legacy-portal-php"
    "$HOME/repos/terraform-stacks/terraform-stack-legacy-portal"
)

log() { echo "[$(date +%H:%M:%S)] $*"; }

prepare_workspace() {
    log "Preparing $TIER workspace at $WORKSPACE"
    rm -rf "$WORKSPACE"
    mkdir -p "$WORKSPACE"

    local repos=("${TIER1_REPOS[@]}")
    if [ "$TIER" = "tier2" ]; then
        repos+=("${TIER2_REPOS[@]}")
    fi

    for repo in "${repos[@]}"; do
        local name
        name=$(basename "$repo")
        if [ ! -d "$repo" ]; then
            log "WARNING: $repo not found, skipping"
            continue
        fi
        log "  Copying $name..."
        rsync -a --exclude='.git/objects' --exclude='node_modules' --exclude='vendor' \
            "$repo/" "$WORKSPACE/$name/"
        # Ensure .git dir exists for repo identity detection
        mkdir -p "$WORKSPACE/$name/.git"
    done

    local count
    count=$(ls -1 "$WORKSPACE" | wc -l | tr -d ' ')
    log "Workspace ready: $count repos in $WORKSPACE"
}

start_stack() {
    log "Stopping existing stack..."
    cd "$REPO_ROOT"
    "${COMPOSE_CMD[@]}" down -v 2>/dev/null || true
    sleep 2

    log "Starting Neo4j compose stack with $TIER workspace..."
    ESHU_FILESYSTEM_HOST_ROOT="$WORKSPACE" "${COMPOSE_CMD[@]}" up --build --force-recreate -d 2>&1 | tail -5

    log "Waiting for bootstrap indexer to complete..."
    local waited=0
    local max_wait=1800  # 30 min for tier2
    if [ "$TIER" = "tier1" ]; then
        max_wait=300  # 5 min for tier1
    fi

    while [ $waited -lt $max_wait ]; do
        local status
        status=$(docker inspect --format='{{.State.Status}}' eshu-bootstrap-index-1 2>/dev/null || echo "unknown")
        if [ "$status" = "exited" ]; then
            local exit_code
            exit_code=$(docker inspect --format='{{.State.ExitCode}}' eshu-bootstrap-index-1 2>/dev/null || echo "?")
            log "Bootstrap exited with code $exit_code after ${waited}s"
            break
        fi
        sleep 5
        waited=$((waited + 5))
        if [ $((waited % 30)) -eq 0 ]; then
            log "  Still running... ${waited}s"
        fi
    done

    if [ $waited -ge $max_wait ]; then
        log "ERROR: Bootstrap timed out after ${max_wait}s"
        "${COMPOSE_CMD[@]}" logs bootstrap-index 2>&1 | tail -30
        exit 1
    fi

    # Show finalization timing
    log "=== Finalization Timing ==="
    "${COMPOSE_CMD[@]}" logs bootstrap-index 2>&1 \
        | rg "Finalization timings" \
        | tail -1 \
        | sed 's/^.*| //' \
        | jq -r '(.extra_keys // {} | to_entries[]? | select(.key | endswith("_seconds")) | "  \(.key): \(.value)s")' \
        2>/dev/null || true

    # Show memory
    log "=== Peak Memory ==="
    "${COMPOSE_CMD[@]}" logs bootstrap-index 2>&1 \
        | rg "After finalization" \
        | tail -1 \
        | sed 's/^.*| //' \
        | jq -r '"  \(.message // "?")"' \
        2>/dev/null || true
}

validate() {
    log "=== Validation ==="
    cd "$REPO_ROOT"

    local neo4j_password="${ESHU_NEO4J_PASSWORD:-change-me}"
    local repo_count variable_count function_count relationship_count argocd_count xrd_count terraform_count

    repo_count=$(
        "${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell \
            -u neo4j \
            -p "$neo4j_password" \
            --format plain \
            "MATCH (r:Repository) RETURN count(r) AS count" 2>/dev/null \
            | tail -n 1
    )
    variable_count=$(
        "${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell \
            -u neo4j \
            -p "$neo4j_password" \
            --format plain \
            "MATCH (v:Variable) RETURN count(v) AS count" 2>/dev/null \
            | tail -n 1
    )
    function_count=$(
        "${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell \
            -u neo4j \
            -p "$neo4j_password" \
            --format plain \
            "MATCH (f:Function) RETURN count(f) AS count" 2>/dev/null \
            | tail -n 1
    )
    relationship_count=$(
        "${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell \
            -u neo4j \
            -p "$neo4j_password" \
            --format plain \
            "MATCH (:Repository)-[r]->(:Repository) RETURN count(r) AS count" 2>/dev/null \
            | tail -n 1
    )
    argocd_count=$(
        "${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell \
            -u neo4j \
            -p "$neo4j_password" \
            --format plain \
            "MATCH (a:ArgoCDApplicationSet) RETURN count(a) AS count" 2>/dev/null \
            | tail -n 1
    )
    xrd_count=$(
        "${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell \
            -u neo4j \
            -p "$neo4j_password" \
            --format plain \
            "MATCH (x:CrossplaneXRD) RETURN count(x) AS count" 2>/dev/null \
            | tail -n 1
    )
    terraform_count=$(
        "${COMPOSE_CMD[@]}" exec -T neo4j cypher-shell \
            -u neo4j \
            -p "$neo4j_password" \
            --format plain \
            "MATCH (t:TerraformResource) RETURN count(t) AS count" 2>/dev/null \
            | tail -n 1
    )

    log "  Repositories: ${repo_count:-0}"
    log "  Variable nodes: ${variable_count:-0}"
    if [[ "${variable_count:-0}" != "0" ]]; then
        log "  FAIL: INDEX_VARIABLES=false but Variable nodes exist"
    fi
    log "  Function nodes: ${function_count:-0}"
    log "  Cross-repo relationships: ${relationship_count:-0}"
    log "  ArgoCD ApplicationSets: ${argocd_count:-0}"
    log "  Crossplane XRDs: ${xrd_count:-0}"
    log "  Terraform resources: ${terraform_count:-0}"

    # API health
    log "=== API Check ==="
    local api_key
    api_key=$(
        "${COMPOSE_CMD[@]}" exec -T eshu sh -lc '
            token="${ESHU_API_KEY:-}";
            if [ -n "$token" ]; then
                printf %s "$token";
                exit 0;
            fi
            home="${ESHU_HOME:-/data/.eshu}";
            if [ -f "$home/.env" ]; then
                sed -n "s/^ESHU_API_KEY=//p" "$home/.env" | tail -n 1 | tr -d "\n";
            fi
        ' 2>/dev/null || true
    )
    if [ -n "$api_key" ]; then
        local repo_count
        repo_count=$(curl -s -H "Authorization: Bearer $api_key" http://localhost:8080/api/v0/repositories 2>/dev/null | jq -r 'if type == "array" then length elif ((.repositories? // []) | length > 0) then (.repositories | length) elif ((.items? // []) | length > 0) then (.items | length) else "?" end' 2>/dev/null || echo "?")
        log "  API repos: $repo_count"
    else
        local repo_count
        repo_count=$(curl -s http://localhost:8080/api/v0/repositories 2>/dev/null | jq -r 'if type == "array" then length elif ((.repositories? // []) | length > 0) then (.repositories | length) elif ((.items? // []) | length > 0) then (.items | length) else "?" end' 2>/dev/null || echo "?")
        log "  API repos: $repo_count"
        log "  INFO: no explicit ESHU_API_KEY found; local auth may be disabled or the token may be persisted under ESHU_HOME/.env"
    fi

    log "=== Validation complete ==="
}

# Main
if [ "$VALIDATE_ONLY" = true ]; then
    validate
    exit 0
fi

if [ "$WORKSPACE_ONLY" = true ]; then
    prepare_workspace
    exit 0
fi

prepare_workspace
start_stack
validate
