#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #10881 removal measurement on one rehearsal table shape.
# Usage: bash removal_bench.sh BASE_DB ARM
#   ARM=drain_asis    shipped retention drain (fact_id-only delete), committed pages
#   ARM=drain_scoped  the divided-table form (scope_id AND fact_id = ANY), committed pages
#   ARM=cascade       DELETE FROM scope_generations (FK ON DELETE CASCADE path)
#   ARM=scope_delete  delete every row of the scopes in $SCOPE_FILE (one per line)
#   ARM=truncate      TRUNCATE the partition(s) holding $SCOPE_FILE's rows (divided only)
# The removal set is the superseded generations, in generation_id order, until
# about REMOVE_FRACTION (default 0.25) of the superseded rows are covered, so
# both shapes remove the same rows. Each arm runs in ONE session: the delete, then (unless
# NO_VACUUM=1) VACUUM (VERBOSE) in the same session. Seconds come from
# clock_timestamp and WAL bytes from pg_stat_get_backend_wal for that backend
# alone, so a concurrent load elsewhere in the cluster does not pollute them.
# CLONE=1 (default) runs in a throwaway CREATE DATABASE ... TEMPLATE clone
# (KEEP=1 keeps it); CLONE=0 runs in place, SCHEMA=public|d picking the table.
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
  db="$base"
  export LP_PGOPTIONS="-c search_path=${SCHEMA:-public},public"
fi

lp "$db" -q -f - <"$here/removal_procs.sql"
lp "$db" -q -c "CHECKPOINT"
gens=$(lp "$db" -At -c "WITH gc AS (SELECT f.generation_id, count(*) n FROM fact_records f JOIN scope_generations g ON g.generation_id = f.generation_id WHERE g.status = 'superseded' GROUP BY 1), ordered AS (SELECT generation_id, n, sum(n) OVER (ORDER BY generation_id) cum FROM gc) SELECT string_agg(quote_literal(generation_id), ',' ORDER BY generation_id) FROM ordered WHERE cum - n < (SELECT sum(n) FROM gc) * ${REMOVE_FRACTION:-0.25}")
arr="ARRAY[$gens]::text[]"
case "$arm" in
  drain_asis) stmt="CALL drain_generations($arr, $page, false)" ;;
  drain_scoped) stmt="CALL drain_generations($arr, $page, true)" ;;
  cascade) stmt="DELETE FROM scope_generations WHERE generation_id = ANY($arr)" ;;
  scope_delete | truncate)
    scopes="ARRAY[$(awk '{printf "%s'"'"'%s'"'"'", (NR>1?",":""), $0}' "${SCOPE_FILE:?}")]::text[]"
    if [ "$arm" = scope_delete ]; then
      stmt="DELETE FROM fact_records WHERE scope_id = ANY($scopes)"
    else
      parts=$(lp "$db" -At -c "SELECT string_agg(DISTINCT tableoid::regclass::text, ',') FROM fact_records WHERE scope_id = ANY($scopes)")
      echo "partitions: $parts"
      stmt="TRUNCATE $parts"
    fi ;;
  *) echo "unknown arm $arm" >&2; exit 2 ;;
esac
vac="\\echo vacuum skipped"
if [ "${NO_VACUUM:-0}" != 1 ]; then
  vac="SELECT wal_bytes AS w1 FROM pg_stat_get_backend_wal(pg_backend_pid()) \\gset
SELECT clock_timestamp() AS t1 \\gset
VACUUM (VERBOSE, INDEX_CLEANUP on, PARALLEL 0) fact_records;
SELECT round(extract(epoch FROM clock_timestamp() - :'t1'::timestamptz)::numeric, 2) AS vacuum_seconds, (SELECT wal_bytes FROM pg_stat_get_backend_wal(pg_backend_pid())) - :w1 AS vacuum_wal_bytes, pg_total_relation_size('fact_records') AS size_after \\gset
\\echo arm=$arm db=$db vacuum_seconds=:vacuum_seconds vacuum_wal_bytes=:vacuum_wal_bytes size_after=:size_after"
fi
echo "# load=$(cut -d' ' -f1-3 /proc/loadavg) io_psi=$(awk '/some/{print $2}' /proc/pressure/io) removal_set_generations=$(echo "$gens" | tr -cd ',' | wc -c)+1"
lp "$db" -q 2>"${WORKDIR:-/tmp}/vacuum_${db}_${arm}.log" -f - <<SQL
\\set ON_ERROR_STOP on
SELECT pg_total_relation_size('fact_records') AS size_before \\gset
SELECT wal_bytes AS w0 FROM pg_stat_get_backend_wal(pg_backend_pid()) \\gset
SELECT clock_timestamp() AS t0 \\gset
$stmt;
SELECT round(extract(epoch FROM clock_timestamp() - :'t0'::timestamptz)::numeric, 2) AS delete_seconds, (SELECT wal_bytes FROM pg_stat_get_backend_wal(pg_backend_pid())) - :w0 AS wal_bytes \\gset
\\echo arm=$arm db=$db delete_seconds=:delete_seconds wal_bytes=:wal_bytes size_before=:size_before
$vac
SQL
