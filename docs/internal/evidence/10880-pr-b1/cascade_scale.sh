#!/usr/bin/env bash
# Scale shim for the supports generation-FK cascade (#10880 PR-B1, arbiter
# check 2). For each corpus size it builds a table with the same columns and
# indexes as migration 336 plus a parent generation table with the same
# ON DELETE CASCADE, then measures, with and without the (generation_id) index:
#   1. the cascade probe: DELETE FROM ONLY <supports> WHERE generation_id = $1
#      as a prepared statement under plan_cache_mode = force_generic_plan (what
#      the RI trigger runs), shared buffers and "Index Searches";
#   2. the real cascade: one DELETE FROM <generations> of 50 generations,
#      EXPLAIN (ANALYZE, BUFFERS), execution and trigger time.
# Every measured statement runs inside BEGIN ... ROLLBACK.
# Usage: PSQL_DSN=postgres://user:pw@host:port/db cascade_scale.sh 5000 20000
# (arguments are generation counts; 60 supports per generation, so 5000 is
# 300,000 supports and 20000 is 1,200,000).
set -euo pipefail
dsn=${PSQL_DSN:?set PSQL_DSN to a disposable database}
here=$(cd "$(dirname "$0")" && pwd)
for gens in "$@"; do
  echo "##### generations=$gens supports=$((gens * 60))"
  psql "$dsn" -X -q -v ON_ERROR_STOP=1 -v gens="$gens" -f "$here/cascade_scale_load.sql"
  for variant in without with; do
    echo "===== variant=$variant generation index"
    psql "$dsn" -X -q -v ON_ERROR_STOP=1 -v variant="$variant" -v gens="$gens" -f "$here/cascade_scale_probe.sql"
  done
done
