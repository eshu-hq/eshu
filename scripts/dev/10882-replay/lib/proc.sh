#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Process control for the built product binaries. Every process gets its own
# pid file, log, listen address and metrics address under the run directory.
# Source env.sh first. REPLAY_RUN_DIR names the current arm's directory.

# replay_graph_env prints the graph and Postgres environment every product
# binary shares, one NAME=value per line.
replay_graph_env() {
  cat <<ENV
ESHU_POSTGRES_DSN=$REPLAY_DSN
ESHU_GRAPH_BACKEND=neo4j
NEO4J_URI=bolt://127.0.0.1:$REPLAY_NEO4J_BOLT_PORT
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=$REPLAY_NEO4J_PASSWORD
NEO4J_DATABASE=neo4j
ENV
}

# replay_start <name> <binary> <metrics-port> [NAME=value ...] starts one
# binary at nice 5 with the shared environment plus the extra values and
# records its pid. The metrics port also names the /metrics scrape target.
replay_start() {
  local name="$1" binary="$2" metrics_port="$3"
  shift 3
  local -a envs
  mapfile -t envs < <(replay_graph_env)
  nohup env "${envs[@]}" "ESHU_METRICS_ADDR=127.0.0.1:$metrics_port" \
    "ESHU_LISTEN_ADDR=127.0.0.1:$((metrics_port + 100))" "$@" \
    nice -n 5 "$REPLAY_BIN/$binary" >"$REPLAY_RUN_DIR/$name.log" 2>&1 &
  echo $! >"$REPLAY_RUN_DIR/$name.pid"
  echo "$metrics_port" >"$REPLAY_RUN_DIR/$name.metrics_port"
}

# replay_stop stops one named process if it is running.
replay_stop() {
  local name="$1" pid_file="$REPLAY_RUN_DIR/$1.pid"
  [[ -f "$pid_file" ]] || return 0
  local pid
  pid="$(cat "$pid_file")"
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 30); do
      kill -0 "$pid" 2>/dev/null || return 0
      sleep 1
    done
    kill -9 "$pid" 2>/dev/null || true
  fi
}

# replay_scrape <name> <tag> saves one /metrics scrape of a running process.
replay_scrape() {
  local name="$1" tag="$2" port
  port="$(cat "$REPLAY_RUN_DIR/$name.metrics_port")"
  mkdir -p "$REPLAY_RUN_DIR/metrics"
  curl -s --max-time 10 "http://127.0.0.1:$port/metrics" >"$REPLAY_RUN_DIR/metrics/$name-$tag.prom" || true
}
