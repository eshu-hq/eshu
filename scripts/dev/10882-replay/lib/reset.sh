#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Resets the replay venue to an empty, schema-applied state: drops and recreates
# the PostgreSQL public schema, wipes the Neo4j graph, then applies the real
# bootstrap schemas with the built eshu-bootstrap-data-plane. Touches only the
# replay-* containers named in env.sh.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "$here/env.sh"

replay_reset() {
  PGPASSWORD="$REPLAY_PG_PASSWORD" psql -h 127.0.0.1 -p "$REPLAY_PG_PORT" -U postgres -X -q \
    -v ON_ERROR_STOP=1 -c 'SET client_min_messages = warning; DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null
  docker exec "$REPLAY_NEO4J_NAME" cypher-shell -u neo4j -p "$REPLAY_NEO4J_PASSWORD" \
    'MATCH (n) DETACH DELETE n' >/dev/null
  ESHU_POSTGRES_DSN="$REPLAY_DSN" ESHU_GRAPH_BACKEND=neo4j \
    NEO4J_URI="bolt://127.0.0.1:$REPLAY_NEO4J_BOLT_PORT" NEO4J_USERNAME=neo4j \
    NEO4J_PASSWORD="$REPLAY_NEO4J_PASSWORD" NEO4J_DATABASE=neo4j \
    "$REPLAY_BIN/eshu-bootstrap-data-plane" >"$REPLAY_SCRATCH/bootstrap.log" 2>&1
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  mkdir -p "$REPLAY_SCRATCH"
  replay_reset
  echo "replay venue reset"
fi
