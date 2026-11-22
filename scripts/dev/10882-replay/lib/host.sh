#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Host rules for the #10882 replay: the shared host is not ours alone.
#   - an arm starts only when the 1-minute load is below REPLAY_LOAD_START_MAX;
#   - a held arm resumes below REPLAY_LOAD_RESUME;
#   - the whole replay stops at REPLAY_WALL_CAP_SECONDS;
#   - only replay-* containers are ever started, stopped or removed.
# Source env.sh first.

replay_load1() {
  awk '{print $1}' /proc/loadavg
}

# replay_wait_for_load blocks until the load allows an arm to start, or the wall
# cap would be crossed. Returns 1 when the cap is reached.
replay_wait_for_load() {
  local started="$1" load
  load="$(replay_load1)"
  if awk -v l="$load" -v m="$REPLAY_LOAD_START_MAX" 'BEGIN{exit !(l < m)}'; then
    return 0
  fi
  echo "HOLD: load1=$load >= $REPLAY_LOAD_START_MAX; waiting for < $REPLAY_LOAD_RESUME" >&2
  while :; do
    sleep 20
    if (($(date +%s) - started >= REPLAY_WALL_CAP_SECONDS)); then
      echo "wall cap reached while holding for load" >&2
      return 1
    fi
    load="$(replay_load1)"
    if awk -v l="$load" -v m="$REPLAY_LOAD_RESUME" 'BEGIN{exit !(l < m)}'; then
      return 0
    fi
  done
}

# replay_check_wall_cap fails when the 4 hour replay budget is spent.
replay_check_wall_cap() {
  local started="$1"
  if (($(date +%s) - started >= REPLAY_WALL_CAP_SECONDS)); then
    echo "wall cap reached ($REPLAY_WALL_CAP_SECONDS s)" >&2
    return 1
  fi
}

# replay_require_venue fails unless both replay containers are running.
replay_require_venue() {
  local name
  for name in "$REPLAY_PG_NAME" "$REPLAY_NEO4J_NAME"; do
    if [[ "$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null)" != "true" ]]; then
      echo "replay container $name is not running" >&2
      return 1
    fi
  done
}

# replay_psql runs psql against the replay database.
replay_psql() {
  PGPASSWORD="$REPLAY_PG_PASSWORD" psql -h 127.0.0.1 -p "$REPLAY_PG_PORT" -U postgres -X -At -v ON_ERROR_STOP=1 "$@"
}

# replay_cypher runs one statement against the replay Neo4j and prints plain rows.
replay_cypher() {
  docker exec "$REPLAY_NEO4J_NAME" cypher-shell -u neo4j -p "$REPLAY_NEO4J_PASSWORD" --format plain "$1"
}
