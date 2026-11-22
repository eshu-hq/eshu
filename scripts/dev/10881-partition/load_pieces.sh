#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Load pulled pieces into one rehearsal database (undivided or divided).
# Triggers and FK checks are off for the load only (session_replication_role
# = replica); measurements run with them on. Prints rows and seconds.
# Usage: bash load_pieces.sh DB PIECE [PIECE...]   (WORKDIR holds the pieces)
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$here/common.sh"
db="$1"
shift
for piece in "$@"; do
  dir="$WORKDIR/$piece"
  t0=$(now_s)
  for t in ingestion_scopes scope_generations; do
    zstd -dc "$dir/$t.tsv.zst" |
      lp "$db" -q -c "SET session_replication_role = replica; COPY $t FROM STDIN"
  done
  # Fact pages load in parallel: one COPY stream per page, LOAD_JOBS at a time.
  export REHEARSAL_CONTAINER db FACT_COLUMNS
  printf '%s\0' "$dir"/facts/*.tsv.zst | xargs -0 -P "${LOAD_JOBS:-4}" -n 1 sh -c '
    zstd -dc "$0" | docker exec -i "$REHEARSAL_CONTAINER" psql -U postgres -d "$db" -X -q -v ON_ERROR_STOP=1 \
      -c "SET session_replication_role = replica; COPY fact_records ($FACT_COLUMNS) FROM STDIN"'

  t1=$(now_s)
  n=$(lp "$db" -At -c "SELECT count(*) FROM fact_records WHERE scope_id IN (SELECT scope_id FROM ingestion_scopes)")
  printf 'load %s into %s: rows_in_db=%s seconds=%.1f\n' "$piece" "$db" "$n" "$(echo "$t1 - $t0" | bc -l)"
done
