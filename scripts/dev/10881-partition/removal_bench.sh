#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #10881 removal measurement on one rehearsal database.
# Usage: bash removal_bench.sh BASE_DB ARM
# The arm runs in a throwaway clone (CREATE DATABASE ... TEMPLATE BASE_DB,
# file copy), so every arm starts from byte-identical data; the clone is
# dropped afterwards unless KEEP=1. Nothing connects to BASE_DB while cloning.
# CLONE=0 runs in place (disk-bound sizes): SCHEMA=public|d picks the table, and
# a destructive arm is then the last one that table sees.
#   ARM=drain_asis    shipped drain SQL (fact_id only), committed pages
#   ARM=drain_scoped  scope-aware drain (divided-table form), committed pages
#   ARM=cascade       DELETE FROM scope_generations (FK ON DELETE CASCADE path)
#   ARM=scope_delete  delete every row of the scopes listed in $SCOPE_FILE
#   ARM=truncate      TRUNCATE the partition(s) holding $SCOPE_FILE (divided only)
# Removal set: superseded generations (about 25% of rows), so both shapes
# remove the same rows. Each arm prints seconds, rows, WAL bytes, then
# runs VACUUM (VERBOSE) and prints its seconds, WAL bytes and size change.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$here/common.sh"
base="$1"
arm="$2"
page="${PAGE_ROWS:-100000}"
if [ "${CLONE:-1}" = 1 ]; then
  db="w_${base}_${arm}"
  lp postgres -q -c "DROP DATABASE IF EXISTS $db" -c "CREATE DATABASE $db TEMPLATE $base STRATEGY file_copy"
  if [ "${KEEP:-0}" != 1 ]; then trap 'lp postgres -q -c "DROP DATABASE IF EXISTS '"$db"'" >/dev/null' EXIT; fi
else
  # In place: SCHEMA picks the table shape (public = undivided, d = divided).
  db="$base"
  export LP_PGOPTIONS="-c search_path=${SCHEMA:-public},public"
fi

lp "$db" -q -f - <"$here/removal_procs.sql"
# The removal set: superseded generations in generation_id order until about
# REMOVE_FRACTION (default 25%) of the rows are covered. The same rule on the
# same data picks the same generations in both shapes.
gens=$(lp "$db" -At -c "WITH gc AS (SELECT f.generation_id, count(*) n FROM fact_records f JOIN scope_generations g ON g.generation_id = f.generation_id WHERE g.status = 'superseded' GROUP BY 1), ordered AS (SELECT generation_id, n, sum(n) OVER (ORDER BY generation_id) cum FROM gc) SELECT string_agg(quote_literal(generation_id), ',' ORDER BY generation_id) FROM ordered WHERE cum - n < (SELECT count(*) FROM fact_records) * ${REMOVE_FRACTION:-0.25}")
arr="ARRAY[$gens]::text[]"
rows_before=$(lp "$db" -At -c "SELECT count(*) FROM fact_records")
wal() { lp "$db" -At -c "SELECT pg_current_wal_lsn()"; }
walb() { lp "$db" -At -c "SELECT pg_wal_lsn_diff('$2','$1')::bigint"; }
size() { lp "$db" -At -c "SELECT pg_total_relation_size('fact_records')"; }

lp "$db" -q -c "CHECKPOINT"
size0=$(size)
lsn0=$(wal)
t0=$(now_s)
case "$arm" in
  drain_asis) lp "$db" -q -c "CALL drain_generations($arr, $page, false)" ;;
  drain_scoped) lp "$db" -q -c "CALL drain_generations($arr, $page, true)" ;;
  cascade) lp "$db" -q -c "DELETE FROM scope_generations WHERE generation_id = ANY($arr)" ;;
  scope_delete)
    scopes=$(awk 'BEGIN{ORS=","} {printf "%s'"'"'%s'"'"'", (NR>1?",":""), $0}' "${SCOPE_FILE:?}")
    lp "$db" -q -c "DELETE FROM fact_records WHERE scope_id = ANY(ARRAY[$scopes]::text[])" ;;
  truncate)
    parts=$(lp "$db" -At -c "SELECT string_agg(DISTINCT tableoid::regclass::text, ',') FROM fact_records WHERE scope_id = ANY(ARRAY[$(awk '{printf "%s'"'"'%s'"'"'", (NR>1?",":""), $0}' "${SCOPE_FILE:?}")]::text[])")
    echo "partitions: $parts"
    lp "$db" -q -c "TRUNCATE $parts" ;;
  *) echo "unknown arm $arm" >&2; exit 2 ;;
esac
t1=$(now_s)
lsn1=$(wal)
rows_after=$(lp "$db" -At -c "SELECT count(*) FROM fact_records")
printf 'arm=%s db=%s delete_seconds=%.2f rows_before=%s rows_after=%s removed=%s wal_bytes=%s\n' \
  "$arm" "$db" "$(echo "$t1 - $t0" | bc -l)" "$rows_before" "$rows_after" "$((rows_before - rows_after))" "$(walb "$lsn0" "$lsn1")"

if [ "${NO_VACUUM:-0}" != 1 ]; then
  lsn2=$(wal)
  t2=$(now_s)
  lp "$db" -q -c "VACUUM (VERBOSE, INDEX_CLEANUP on, PARALLEL 0) fact_records" 2>"${WORKDIR}/vacuum_${db}_${arm}.log" || true
  t3=$(now_s)
  lsn3=$(wal)
  printf 'arm=%s db=%s vacuum_seconds=%.2f vacuum_wal_bytes=%s size_before=%s size_after=%s\n' \
    "$arm" "$db" "$(echo "$t3 - $t2" | bc -l)" "$(walb "$lsn2" "$lsn3")" "$size0" "$(size)"
fi
