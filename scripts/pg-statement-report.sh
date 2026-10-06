#!/usr/bin/env bash
# pg-statement-report.sh - read-only Postgres latency report for operators
# (#7596). Lists the slowest normalized statements from pg_stat_statements and,
# when pg_buffercache is installed, how much of the hot Eshu relations sits in
# shared buffers.
#
# Usage:
#   pg-statement-report.sh [--dsn <conninfo-or-URI>] [--install]
#
#   --dsn      Postgres connection string. Without it psql reads the standard
#              PGHOST, PGPORT, PGUSER, PGDATABASE, and PGPASSWORD variables.
#              A DSN with a password is visible in `ps`; prefer the PG*
#              variables or a ~/.pgpass entry on shared hosts.
#   --install  Opt in to DDL: run CREATE EXTENSION IF NOT EXISTS for
#              pg_stat_statements and pg_buffercache before the report. The
#              role needs the rights to create them. Without this flag the
#              script runs no DDL and writes nothing.
#
# Every report query runs in a READ ONLY transaction with a short
# statement_timeout. The report prints normalized statement text only, cut to
# 200 characters. It never prints parameter values.
#
# Exit codes:
#   0 - report printed
#   1 - psql failed (connection, privilege, or SQL error)
#   2 - bad usage
#   3 - pg_stat_statements is not preloaded, or its extension is not created
#
# Server setup and reading guide: docs/public/reference/postgres-diagnostics.md

set -euo pipefail

STATEMENT_TIMEOUT="15s"
TOP_N=20
MIN_CALLS=20
QUERY_CHARS=200

dsn=""
install_ext=false

usage() {
  sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//' >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dsn)
      [ "$#" -ge 2 ] || { echo "pg-statement-report: --dsn needs a value" >&2; exit 2; }
      dsn="$2"
      shift 2
      ;;
    --install)
      install_ext=true
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "pg-statement-report: unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

command -v psql >/dev/null 2>&1 || {
  echo "pg-statement-report: psql is required on PATH" >&2
  exit 2
}

# run_sql reads one SQL script on stdin and prints bare values or the aligned
# report. Passing the script on stdin keeps it out of the process list.
run_sql() {
  local mode="$1"
  local args=(-X -q -v ON_ERROR_STOP=1 -P pager=off)
  if [ "$mode" = "value" ]; then
    args+=(-A -t)
  else
    args+=(-P footer=off)
  fi
  if [ -n "$dsn" ]; then
    args+=(-d "$dsn")
  fi
  psql "${args[@]}" -f -
}

# read_only wraps one statement in a READ ONLY transaction with a short timeout.
read_only() {
  printf 'BEGIN READ ONLY;\nSET LOCAL statement_timeout = '"'"'%s'"'"';\n%s\nCOMMIT;\n' \
    "$STATEMENT_TIMEOUT" "$1"
}

# 1. The module must be preloaded. This needs a server restart, so say so.
preload="$(read_only "SHOW shared_preload_libraries;" | run_sql value | tail -n 1)" || exit 1
case "$preload" in
  *pg_stat_statements*) ;;
  *)
    echo "pg-statement-report: pg_stat_statements is not in shared_preload_libraries, so add it to shared_preload_libraries on the primary and the replica and restart Postgres before this report can work." >&2
    exit 3
    ;;
esac

# 2. Optional, opt-in DDL.
if [ "$install_ext" = true ]; then
  printf 'CREATE EXTENSION IF NOT EXISTS pg_stat_statements;\nCREATE EXTENSION IF NOT EXISTS pg_buffercache;\n' |
    run_sql value >/dev/null || exit 1
fi

# 3. The extension must exist in the connected database.
has_pgss="$(read_only "SELECT count(*) FROM pg_extension WHERE extname = 'pg_stat_statements';" | run_sql value | tail -n 1)" || exit 1
if [ "$has_pgss" != "1" ]; then
  echo "pg-statement-report: pg_stat_statements is preloaded but its extension is not created in this database, so run CREATE EXTENSION pg_stat_statements as a privileged role or rerun this script with --install." >&2
  exit 3
fi

# statement_sql prints the top-N query. $1 is the ORDER BY column, $2 an extra
# WHERE clause. Only SELECT, INSERT, UPDATE, DELETE, MERGE, and WITH statements
# are listed: Postgres normalizes those, while a utility command can keep
# literal values in its stored text.
statement_sql() {
  cat <<SQL
SELECT s.calls,
       round(s.mean_exec_time::numeric, 2)   AS mean_ms,
       round(s.total_exec_time::numeric, 1)  AS total_ms,
       round(s.max_exec_time::numeric, 2)    AS max_ms,
       s.rows,
       s.shared_blks_hit                     AS shared_hit,
       s.shared_blks_read                    AS shared_read,
       round(100.0 * s.shared_blks_hit
             / nullif(s.shared_blks_hit + s.shared_blks_read, 0), 1) AS hit_pct,
       s.temp_blks_read + s.temp_blks_written AS temp_blks,
       left(regexp_replace(s.query, '\s+', ' ', 'g'), ${QUERY_CHARS}) AS query
  FROM pg_stat_statements s
 WHERE s.dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
   AND s.query ~* '^\s*(select|insert|update|delete|merge|with)\M'
   AND s.query !~* 'pg_stat_statements|pg_buffercache|pg_extension'
   $2
 ORDER BY $1 DESC
 LIMIT ${TOP_N};
SQL
}

printf '== Collection window ==\n'
read_only "SELECT current_setting('server_version') AS server_version,
       i.stats_reset,
       now() - i.stats_reset AS window,
       i.dealloc AS evicted_entries,
       current_setting('pg_stat_statements.max') AS statements_max,
       current_setting('pg_stat_statements.track') AS track,
       current_setting('track_io_timing') AS track_io_timing
  FROM pg_stat_statements_info i;" | run_sql table || exit 1

printf '\n== Top %s statements by total_exec_time ==\n' "$TOP_N"
read_only "$(statement_sql s.total_exec_time '')" | run_sql table || exit 1

printf '\n== Top %s statements by mean_exec_time (at least %s calls) ==\n' "$TOP_N" "$MIN_CALLS"
read_only "$(statement_sql s.mean_exec_time "AND s.calls >= ${MIN_CALLS}")" | run_sql table || exit 1

# 4. Shared-buffer residency, only when pg_buffercache exists.
has_buf="$(read_only "SELECT count(*) FROM pg_extension WHERE extname = 'pg_buffercache';" | run_sql value | tail -n 1)" || exit 1
printf '\n== Shared-buffer residency for the hot relations ==\n'
if [ "$has_buf" != "1" ]; then
  echo "pg_buffercache is not installed in this database, so residency is skipped; rerun with --install or run CREATE EXTENSION pg_buffercache."
else
  read_only "WITH hot AS (
  SELECT c.oid, c.relname, c.relkind
    FROM pg_class c
   WHERE c.relkind IN ('r', 'i')
     AND c.relnamespace NOT IN (SELECT oid FROM pg_namespace WHERE nspname IN ('pg_catalog', 'information_schema', 'pg_toast'))
     AND (c.relname IN ('fact_records', 'content_entities', 'content_files')
          OR c.oid IN (SELECT i.indexrelid FROM pg_index i
                         JOIN pg_class t ON t.oid = i.indrelid
                        WHERE t.relname IN ('fact_records', 'content_entities', 'content_files')))
), resident AS (
  SELECT b.relfilenode, count(*) AS buffers
    FROM pg_buffercache b
   WHERE b.reldatabase = (SELECT oid FROM pg_database WHERE datname = current_database())
     AND b.relforknumber = 0
   GROUP BY b.relfilenode
)
SELECT h.relname AS relation,
       CASE h.relkind WHEN 'r' THEN 'table' ELSE 'index' END AS kind,
       coalesce(r.buffers, 0) AS buffers,
       round(coalesce(r.buffers, 0) * current_setting('block_size')::numeric / 1048576, 1) AS mb,
       round(100.0 * coalesce(r.buffers, 0) * current_setting('block_size')::numeric
             / nullif(pg_relation_size(h.oid), 0), 1) AS pct_of_relation
  FROM hot h
  LEFT JOIN resident r ON r.relfilenode = pg_relation_filenode(h.oid)
 ORDER BY h.relkind, h.relname;" | run_sql table || exit 1
fi
