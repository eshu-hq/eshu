#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Shared settings for the #10881 fact_records HASH(scope_id) rehearsal scripts.
# Source this file; it sets nothing global except defaults and helper functions.
#
# Secrets are never stored here: REPLICA_DSN (a read-replica DSN, reached
# through `kubectl port-forward`) is read from the environment only.
# shellcheck disable=SC2034 # these defaults are read by the scripts that source this file
set -euo pipefail

: "${REHEARSAL_CONTAINER:=pg-10881-rehearsal}"
: "${REHEARSAL_VOLUME:=pg-10881-rehearsal-vol}"
: "${REHEARSAL_IMAGE:=postgres:18.6}"
: "${REHEARSAL_PORT:=25882}"
# One database per table shape so both shapes hold byte-identical rows.
: "${DB_UNDIVIDED:=und}"
: "${DB_DIVIDED:=div}"
: "${PARTS:=32}"
: "${WORKDIR:=${TMPDIR:-/tmp}/10881-rehearsal}"

# FACT_COLUMNS is the fact_records column list in table order; COPY streams
# use it so a pulled file loads into either shape unchanged.
FACT_COLUMNS="fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind, fencing_token, source_confidence, source_system, source_fact_key, source_uri, source_record_id, observed_at, ingested_at, is_tombstone, payload"

# rq runs psql against the read replica. It forces a read-only session so a
# mistake in a script cannot write, and it is the ONLY entry point to the
# replica in this directory.
rq() {
  : "${REPLICA_DSN:?set REPLICA_DSN to a read-replica DSN}"
  PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=${RQ_TIMEOUT:-120000}" \
    psql "$REPLICA_DSN" -X -A "$@"
}

# lp runs psql inside the rehearsal container against database $1.
# LP_PGOPTIONS (for example "-c search_path=d,public") reaches the session.
lp() {
  local db="$1"
  shift
  docker exec -i -e PGOPTIONS="${LP_PGOPTIONS:-}" "$REHEARSAL_CONTAINER" psql -U postgres -d "$db" -X -v ON_ERROR_STOP=1 "$@"
}

now_s() { date +%s.%N; }
