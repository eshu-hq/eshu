#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Shared settings for the #10882 replay driver. Source this file; it sets only
# variables and defines no behavior, so every lib file agrees on one venue.
# Venue: containers named replay-*, scratch under /tmp/claude-1000/replay,
# large artifacts under ~/.cache/replay. Override any value from the caller.

REPLAY_HOME="${REPLAY_HOME:-$HOME/.cache/replay}"
REPLAY_SCRATCH="${REPLAY_SCRATCH:-/tmp/claude-1000/replay}"
REPLAY_BIN="${REPLAY_BIN:-$REPLAY_HOME/bin}"
REPLAY_PG_NAME="${REPLAY_PG_NAME:-replay-pg}"
REPLAY_PG_PORT="${REPLAY_PG_PORT:-15882}"
REPLAY_PG_PASSWORD="${REPLAY_PG_PASSWORD:-replay}"
REPLAY_NEO4J_NAME="${REPLAY_NEO4J_NAME:-replay-neo4j}"
REPLAY_NEO4J_BOLT_PORT="${REPLAY_NEO4J_BOLT_PORT:-15887}"
REPLAY_NEO4J_PASSWORD="${REPLAY_NEO4J_PASSWORD:-replaypass123}"
# Same pin as scripts/docker-compose.go-reachability-proof-deps.yaml.
REPLAY_NEO4J_IMAGE="${REPLAY_NEO4J_IMAGE:-ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f}"
REPLAY_PG_IMAGE="${REPLAY_PG_IMAGE:-postgres:18.6}"

REPLAY_DSN="postgres://postgres:${REPLAY_PG_PASSWORD}@127.0.0.1:${REPLAY_PG_PORT}/postgres?sslmode=disable"

# Host rules: start an arm only when the 1-minute load is below this; resume a
# held arm below the resume value; the whole replay may not run past the cap.
REPLAY_LOAD_START_MAX="${REPLAY_LOAD_START_MAX:-60}"
REPLAY_LOAD_RESUME="${REPLAY_LOAD_RESUME:-45}"
REPLAY_WALL_CAP_SECONDS="${REPLAY_WALL_CAP_SECONDS:-14400}"

# Per-process worker counts and batch sizes. Identical in every arm; each arm's
# manifest records them.
REPLAY_PROJECTOR_WORKERS="${REPLAY_PROJECTOR_WORKERS:-4}"
REPLAY_INGESTER_PROJECTOR_WORKERS="${REPLAY_INGESTER_PROJECTOR_WORKERS:-4}"

export REPLAY_HOME REPLAY_SCRATCH REPLAY_BIN REPLAY_PG_NAME REPLAY_PG_PORT REPLAY_PG_PASSWORD \
  REPLAY_NEO4J_NAME REPLAY_NEO4J_BOLT_PORT REPLAY_NEO4J_PASSWORD REPLAY_NEO4J_IMAGE REPLAY_PG_IMAGE \
  REPLAY_DSN REPLAY_LOAD_START_MAX REPLAY_LOAD_RESUME REPLAY_WALL_CAP_SECONDS \
  REPLAY_PROJECTOR_WORKERS REPLAY_INGESTER_PROJECTOR_WORKERS
