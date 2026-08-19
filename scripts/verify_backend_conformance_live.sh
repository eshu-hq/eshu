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

# The value-flow cloud sink pair is absent from the corpora unless its own
# opt-in is set, so a run without it proves strictly less than a run with it.
# Say which run this is, at the top, so a green result is never read as full
# coverage. The test logs the same fact -- hence -v below, without which the
# omission would be invisible on a pass.
if [ "${ESHU_BACKEND_CONFORMANCE_VALUE_FLOW:-}" = "1" ] \
    || [ "${ESHU_BACKEND_CONFORMANCE_VALUE_FLOW:-}" = "true" ] \
    || [ "${ESHU_BACKEND_CONFORMANCE_VALUE_FLOW:-}" = "yes" ]; then
    echo "  value-flow cloud sink pair: INCLUDED (ESHU_BACKEND_CONFORMANCE_VALUE_FLOW is set)"
else
    echo "  value-flow cloud sink pair: OMITTED -- ESHU_BACKEND_CONFORMANCE_VALUE_FLOW is not set."
    echo "  This run does NOT prove the value-flow cloud sink query on $ESHU_GRAPH_BACKEND."
    echo "  Set ESHU_BACKEND_CONFORMANCE_VALUE_FLOW=1 to include it."
fi

cd "$REPO_ROOT/go"
go test ./internal/backendconformance -run '^TestLiveBackendConformance$' -count=1 -v

if [ "$ESHU_GRAPH_BACKEND" = "nornicdb" ]; then
    echo "Running live NornicDB retry classification contracts and #5441 stale-attribute-removal regression"
    ESHU_NORNICDB_RETRY_CONTRACT_LIVE=1 \
    ESHU_CYPHER_BOLT_DSN="$ESHU_NEO4J_URI" \
    ESHU_CYPHER_BOLT_DATABASE="$ESHU_NEO4J_DATABASE" \
        go test ./internal/storage/cypher -run '^(TestLiveNornicDBRetryConflictClassificationContract|TestLiveNornicDBRelationshipSnapshotConflictRetryContract|TestTerraformResourceWriterLiveClearsStaleAttributeOnRefresh)$' -count=1 -v

    echo "Running live NornicDB CloudResource heterogeneous-batch row-key-default regression (#5714/#5055)"
    ESHU_CLOUDRESOURCE_NODE_WRITER_LIVE=1 \
        go test ./internal/storage/cypher -run '^TestCloudResourceNodeWriterLiveHeterogeneousBatchNeverPersistsLiteral$' -count=1 -v
fi
