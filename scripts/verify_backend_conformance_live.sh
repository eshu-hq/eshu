#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

GRAPH_BACKEND="${ESHU_GRAPH_BACKEND:-nornicdb}"
case "$GRAPH_BACKEND" in
    nornicdb)
        DEFAULT_GRAPH_DATABASE="nornic"
        ;;
    neo4j)
        DEFAULT_GRAPH_DATABASE="neo4j"
        ;;
    *)
        echo "Unsupported ESHU_GRAPH_BACKEND for backend conformance: $GRAPH_BACKEND" >&2
        exit 1
        ;;
esac

export ESHU_GRAPH_BACKEND="$GRAPH_BACKEND"
export ESHU_BACKEND_CONFORMANCE_LIVE=1
export ESHU_NEO4J_URI="${ESHU_NEO4J_URI:-${NEO4J_URI:-bolt://localhost:7687}}"
export ESHU_NEO4J_USERNAME="${ESHU_NEO4J_USERNAME:-${NEO4J_USERNAME:-neo4j}}"
export ESHU_NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD:-${NEO4J_PASSWORD:-change-me}}"
export ESHU_NEO4J_DATABASE="${ESHU_NEO4J_DATABASE:-${NEO4J_DATABASE:-${DEFAULT_DATABASE:-$DEFAULT_GRAPH_DATABASE}}}"
export NEO4J_URI="$ESHU_NEO4J_URI"
export NEO4J_USERNAME="$ESHU_NEO4J_USERNAME"
export NEO4J_PASSWORD="$ESHU_NEO4J_PASSWORD"
export NEO4J_DATABASE="$ESHU_NEO4J_DATABASE"
export DEFAULT_DATABASE="$ESHU_NEO4J_DATABASE"

echo "Running live backend conformance for $ESHU_GRAPH_BACKEND on $ESHU_NEO4J_URI database $ESHU_NEO4J_DATABASE"
cd "$REPO_ROOT/go"
go test ./internal/backendconformance -run '^TestLiveBackendConformance$' -count=1
