#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# EXPLAIN (ANALYZE, BUFFERS) of the bounded per-source claimable-signal probe on a
# saved replay database (the T3 shape: aged source-A rows all parked in
# reducer_waiting_items). It restores a pg_dump of a finished run into a SCRATCH
# database inside replay-pg, so no arm is reset, ANALYZEs it, and runs the probe
# statement as pgx does (an unprepared statement, so a custom plan each time).
#
# The statement text is the product's own: print it with a throwaway overlay test
# that writes boundedSourceSignalQuery() to a file (go test -overlay, no tree
# change), see the design record. Needs env.sh and host.sh sourced indirectly.
#
# Usage: explain_probe.sh <dump> <bounded_probe.sql> <source_system> [runs]
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/env.sh
source "$here/lib/env.sh"
# shellcheck source=lib/host.sh
source "$here/lib/host.sh"

dump="${1:?usage: explain_probe.sh <dump> <bounded_probe.sql> <source_system> [runs]}"
sql="${2:?bounded probe sql}"
source_system="${3:?source system}"
runs="${4:-3}"
db="${REPLAY_EXPLAIN_DB:-t3explain}"
replay_require_venue

replay_psql -d postgres -c "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db" >/dev/null
docker exec -i -e PGPASSWORD="$REPLAY_PG_PASSWORD" "$REPLAY_PG_NAME" \
  nice -n 19 pg_restore -U postgres -d "$db" --no-owner --exit-on-error <"$dump"
replay_psql -d "$db" -c "ANALYZE" >/dev/null
echo "== rows: reducer $(replay_psql -d "$db" -c "SELECT count(*) FROM fact_work_items WHERE stage='reducer'")," \
  "parked $(replay_psql -d "$db" -c "SELECT count(*) FROM reducer_waiting_items")," \
  "retrying $(replay_psql -d "$db" -c "SELECT count(*) FROM fact_work_items WHERE stage='reducer' AND status='retrying'")"
for run in $(seq 1 "$runs"); do
  echo "== run $run"
  {
    echo "SET plan_cache_mode = force_custom_plan;"
    echo "PREPARE probe(timestamptz, timestamptz, timestamptz, text) AS"
    cat "$sql"
    echo ";"
    echo "EXPLAIN (ANALYZE, BUFFERS, TIMING ON, SETTINGS)"
    echo "EXECUTE probe(now(), now() - interval '1 hour', now() - interval '15 minutes', '$source_system');"
  } | replay_psql -d "$db" -X
done
replay_psql -d postgres -c "DROP DATABASE IF EXISTS $db" >/dev/null
