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
# shellcheck source=host.sh
source "$here/host.sh"

replay_reset() {
  # Standalone or sourced, never drop a schema on anything but the replay venue: both
  # replay-* containers must be running and the Postgres port must be the one the replay
  # Postgres container publishes.
  replay_require_venue || return 1
  if ! docker port "$REPLAY_PG_NAME" 5432/tcp | rg -q ":$REPLAY_PG_PORT\$"; then
    echo "replay_reset: port $REPLAY_PG_PORT is not published by $REPLAY_PG_NAME; refusing to reset" >&2
    return 1
  fi
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
