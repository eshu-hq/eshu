#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #11202 box 2: per-generation census from the READ REPLICA, read-only.
# Writes $WORKDIR/generations.csv: scope_kind,status,observed_at,ingested_at,
# activated_at,superseded_at,rows,scope_hash (a 64-bit hash of the scope id, so the file
# never carries a scope name). The file stays in $WORKDIR; commit only the
# aggregate output of aggregate_census.py. Each chunk is a short query (a slice of
# generations by hash) so a replica recovery conflict costs one retry.
# Usage: REPLICA_DSN=... WORKDIR=... bash census_generations.sh
set -euo pipefail
: "${REPLICA_DSN:?set REPLICA_DSN to a read-replica DSN}"
: "${WORKDIR:?set WORKDIR}"
chunks="${CHUNKS:-24}"
out="$WORKDIR/generations.csv"
: >"$out"
for k in $(seq 0 $((chunks - 1))); do
  for attempt in 1 2 3 4 5; do
    if PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=600000" psql "$REPLICA_DSN" -X -q -A -t -F, -v ON_ERROR_STOP=1 \
      -c "SELECT s.scope_kind, g.status, g.observed_at, g.ingested_at, g.activated_at, g.superseded_at,
                 (SELECT count(*) FROM fact_records f WHERE f.scope_id = g.scope_id AND f.generation_id = g.generation_id) AS n,
                 hashtextextended(g.scope_id, 0)
          FROM scope_generations g JOIN ingestion_scopes s ON s.scope_id = g.scope_id
          WHERE abs(hashtext(g.generation_id)) % $chunks = $k" >"$out.part" 2>"$out.err"; then
      cat "$out.part" >>"$out"
      break
    fi
    sleep $((attempt * 3))
  done
  echo "chunk $k/$chunks done rows=$(wc -l <"$out")" >&2
done
