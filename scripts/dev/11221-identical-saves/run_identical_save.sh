#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #11221: per-row WAL of an identical re-save, a guarded skip, a changed re-save and a
# fresh insert on a rehearsal database that already holds the production schema (see
# scripts/dev/10881-partition/make_index_profile.py for the retained profile).
# Usage: CONTAINER=pg-x ROWS=50000 bash run_identical_save.sh DB
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
db="$1"
c="${CONTAINER:?}"
rows="${ROWS:-50000}"
run() { docker exec -i "$c" psql -U postgres -d "$db" -X -q "$@"; }
run -v rows="$rows" -v payload_chunks="${CHUNKS:-40}" -f - <"$here/fixture_load.sql"
echo "# db=$db rows=$rows payload_bytes_avg=$(run -At -c "SELECT round(avg(pg_column_size(payload))) FROM fact_records WHERE fact_id LIKE 'fx-%'")"
# Cold: a CHECKPOINT before each arm, so the first touch of every page writes a full-page image.
echo "# cold (CHECKPOINT before each arm)"
for arm in shipped_new_ts shipped_same_ts guarded_new_ts changed_guarded fresh_insert; do
  run -c "VACUUM fact_records" -c "CHECKPOINT"
  run -v arm="$arm" -f - <"$here/identical_save.sql" 2>&1 | rg '^arm='
done
# Steady: one CHECKPOINT, then a priming pass of every arm, then the measured pass with no
# checkpoint between arms, so full-page images are already spent for the pages the arm touches.
echo "# steady (no CHECKPOINT between arms; second pass)"
run -c "VACUUM fact_records" -c "CHECKPOINT"
for pass in 1 2; do
  for arm in guarded_new_ts shipped_same_ts shipped_new_ts changed_guarded; do
    out=$(run -v arm="$arm" -f - <"$here/identical_save.sql" 2>&1 | rg '^arm=')
    if [ "$pass" = 2 ]; then echo "$out"; fi
  done
done
