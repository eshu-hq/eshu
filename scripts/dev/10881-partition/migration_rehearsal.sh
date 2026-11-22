#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #10881 AC5 migration rehearsal on one rehearsal database that already holds
# an undivided public.fact_records. Shape: expand / backfill / verify /
# cutover / way back, in the new table's own schema `d` so the old table is
# never touched until the cutover stage. Every stage prints seconds, WAL
# bytes and, where it matters, the strongest lock the stage held.
#
# Usage: bash migration_rehearsal.sh DB STAGE
#   expand     CREATE UNIQUE INDEX CONCURRENTLY (scope_id, fact_id) on the old
#              table: lets new writers use ON CONFLICT (scope_id, fact_id)
#              against the OLD table, so writers can be upgraded before the swap
#   build      d.fact_records: HASH(scope_id) x PARTS, PK (scope_id, fact_id),
#              the retained indexes, FKs, triggers (pg_get_*def of the old table)
#   backfill   INSERT ... SELECT old -> d in scope batches
#   verify     id-set count and hash for both tables, plus a payload hash over a 1% sample
#   cutover    one transaction: ACCESS EXCLUSIVE old, catch-up delta, rename swap;
#              prints the seconds the exclusive lock was held
#   wayback    reverse catch-up of rows written since the cutover, rename back
#   mixed      the mixed-version writer proof: old/new conflict target against
#              old/new table shapes (SQLSTATE 42P10 where it fails)
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$here/common.sh"
db="$1"
stage="$2"
wal() { lp "$db" -At -c "SELECT pg_current_wal_lsn()"; }
walb() { lp "$db" -At -c "SELECT pg_wal_lsn_diff('$2','$1')::bigint"; }
# A background sampler records every relation lock mode held on the fact table
# (and its expand index) while the stage runs; it is printed when the stage ends,
# so the lock levels in the record are observed, not read from documentation.
lockfile="$WORKDIR/locks_${db}_${stage}.txt"
mkdir -p "$WORKDIR"
: >"$lockfile"
(while :; do
  lp "$db" -At -c "SELECT DISTINCT n.nspname || '.' || c.relname || ' ' || l.mode FROM pg_locks l JOIN pg_class c ON c.oid = l.relation JOIN pg_namespace n ON n.oid = c.relnamespace WHERE l.pid <> pg_backend_pid() AND l.granted AND c.relname IN ('fact_records', 'fact_records_old', 'fact_records_scope_fact_uniq', 'scope_generations', 'ingestion_scopes')" >>"$lockfile" 2>/dev/null
  sleep 0.3
done) &
watcher=$!
trap 'kill $watcher 2>/dev/null || true; echo "locks seen during $stage:"; sort -u "$lockfile"' EXIT
report() { printf 'stage=%s %s seconds=%.2f wal_bytes=%s\n' "$stage" "$1" "$(echo "$3 - $2" | bc -l)" "$4"; }

case "$stage" in
  expand)
    lsn0=$(wal); t0=$(now_s)
    lp "$db" -q -c "SET maintenance_work_mem = '1GB'" -c "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS fact_records_scope_fact_uniq ON public.fact_records (scope_id, fact_id)"
    t1=$(now_s)
    report "index_bytes=$(lp "$db" -At -c "SELECT pg_relation_size('fact_records_scope_fact_uniq')")" "$t0" "$t1" "$(walb "$lsn0" "$(wal)")"
    echo "lock: CREATE INDEX CONCURRENTLY holds SHARE UPDATE EXCLUSIVE (writers continue); it waits out transactions older than each of its two scans" ;;
  build)
    t0=$(now_s)
    lp "$db" -q <<SQL
CREATE SCHEMA IF NOT EXISTS d;
CREATE TABLE d.fact_records (LIKE public.fact_records INCLUDING DEFAULTS INCLUDING STORAGE INCLUDING CONSTRAINTS) PARTITION BY HASH (scope_id);
$(for i in $(seq 0 $((PARTS - 1))); do echo "CREATE TABLE d.fact_records_p$i PARTITION OF d.fact_records FOR VALUES WITH (MODULUS $PARTS, REMAINDER $i) WITH (autovacuum_vacuum_scale_factor='0.02', autovacuum_vacuum_insert_scale_factor='0.05');"; done)
ALTER TABLE d.fact_records ADD CONSTRAINT fact_records_pkey_new PRIMARY KEY (scope_id, fact_id);
SQL
    # The retained secondary indexes, rewritten onto d.fact_records under new names.
    lp "$db" -At -c "SELECT replace(replace(pg_get_indexdef(indexrelid), ' ON public.fact_records', ' ON d.fact_records'), 'INDEX ' || indexrelid::regclass::text, 'INDEX ' || indexrelid::regclass::text || '_new') || ';' FROM pg_index WHERE indrelid = 'public.fact_records'::regclass AND NOT indisprimary AND indexrelid::regclass::text <> 'fact_records_scope_fact_uniq'" | lp "$db" -q -f -
    # The six production triggers move with the table; the removal arms must pay them on both shapes.
    lp "$db" -At -c "SELECT replace(pg_get_triggerdef(oid), ' ON public.fact_records ', ' ON d.fact_records ') || ';' FROM pg_trigger WHERE tgrelid = 'public.fact_records'::regclass AND NOT tgisinternal" | lp "$db" -q -f -
    lp "$db" -q -c "ALTER TABLE d.fact_records ADD CONSTRAINT fact_records_generation_id_fkey_new FOREIGN KEY (generation_id) REFERENCES public.scope_generations(generation_id) ON DELETE CASCADE NOT VALID, ADD CONSTRAINT fact_records_scope_id_fkey_new FOREIGN KEY (scope_id) REFERENCES public.ingestion_scopes(scope_id) ON DELETE CASCADE NOT VALID"
    t1=$(now_s)
    printf 'stage=build seconds=%.2f indexes_on_parent=%s partitions=%s\n' "$(echo "$t1 - $t0" | bc -l)" \
      "$(lp "$db" -At -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='d' AND c.relkind IN ('i','I')")" "$PARTS" ;;
  backfill)
    lsn0=$(wal); t0=$(now_s)
    lp "$db" -q -c "SET maintenance_work_mem = '1GB'; SET synchronous_commit = off; SET session_replication_role = replica; INSERT INTO d.fact_records SELECT * FROM public.fact_records"
    t1=$(now_s)
    report "rows=$(lp "$db" -At -c 'SELECT count(*) FROM d.fact_records') new_bytes=$(lp "$db" -At -c "SELECT pg_total_relation_size('d.fact_records')") old_bytes=$(lp "$db" -At -c "SELECT pg_total_relation_size('public.fact_records')")" "$t0" "$t1" "$(walb "$lsn0" "$(wal)")"
    lp "$db" -q -c "ALTER TABLE d.fact_records VALIDATE CONSTRAINT fact_records_generation_id_fkey_new" -c "ALTER TABLE d.fact_records VALIDATE CONSTRAINT fact_records_scope_id_fkey_new"
    printf 'stage=backfill fk_validate_done_seconds=%.2f (VALIDATE holds SHARE UPDATE EXCLUSIVE on the new table and ROW SHARE on the referenced tables)\n' "$(echo "$(now_s) - $t1" | bc -l)"
    lp "$db" -q -c "ANALYZE d.fact_records" ;;
  verify)
    # Index-only scans for the id sets (small IO), plus a payload hash over a
    # 1% sample of ids: a full payload scan of both tables would read the whole
    # heap twice and the copy is INSERT ... SELECT *, so ids and a sample are
    # the cheap proof the row sets match.
    t0=$(now_s)
    lp "$db" -At -c "SELECT 'old', count(*), sum(hashtextextended(scope_id || fact_id, 0))::numeric FROM public.fact_records UNION ALL SELECT 'new', count(*), sum(hashtextextended(scope_id || fact_id, 0))::numeric FROM d.fact_records"
    lp "$db" -At -c "SELECT 'old_sample', count(*), sum(hashtextextended(payload::text, 0))::numeric FROM public.fact_records WHERE hashtext(fact_id) % 100 = 0 UNION ALL SELECT 'new_sample', count(*), sum(hashtextextended(payload::text, 0))::numeric FROM d.fact_records WHERE hashtext(fact_id) % 100 = 0"
    printf 'stage=verify seconds=%.2f\n' "$(echo "$(now_s) - $t0" | bc -l)" ;;
  cutover)
    # Delta rows (ingested after the backfill) are simulated by the caller.
    t0=$(now_s)
    lp "$db" -q <<'SQL'
\timing off
BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE public.fact_records IN ACCESS EXCLUSIVE MODE;
SELECT clock_timestamp() AS lock_acquired \gset
INSERT INTO d.fact_records SELECT o.* FROM public.fact_records o
  WHERE o.ingested_at >= (SELECT max(ingested_at) - interval '1 hour' FROM d.fact_records)
  ON CONFLICT (scope_id, fact_id) DO UPDATE SET payload = EXCLUDED.payload, ingested_at = EXCLUDED.ingested_at;
ALTER TABLE public.fact_records RENAME TO fact_records_old;
ALTER TABLE d.fact_records SET SCHEMA public;
SELECT round(extract(epoch FROM clock_timestamp() - :'lock_acquired'::timestamptz)::numeric, 3) AS exclusive_lock_held_seconds;
COMMIT;
SQL
    printf 'stage=cutover total_seconds=%.2f\n' "$(echo "$(now_s) - $t0" | bc -l)" ;;
  wayback)
    t0=$(now_s)
    lp "$db" -q <<'SQL'
BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE public.fact_records IN ACCESS EXCLUSIVE MODE;
LOCK TABLE public.fact_records_old IN ACCESS EXCLUSIVE MODE;
SELECT clock_timestamp() AS lock_acquired \gset
INSERT INTO public.fact_records_old SELECT n.* FROM public.fact_records n
  WHERE n.ingested_at >= (SELECT max(ingested_at) - interval '1 hour' FROM public.fact_records_old)
  ON CONFLICT (fact_id) DO UPDATE SET payload = EXCLUDED.payload, ingested_at = EXCLUDED.ingested_at;
ALTER TABLE public.fact_records SET SCHEMA d;
ALTER TABLE public.fact_records_old RENAME TO fact_records;
SELECT round(extract(epoch FROM clock_timestamp() - :'lock_acquired'::timestamptz)::numeric, 3) AS exclusive_lock_held_seconds;
COMMIT;
SQL
    printf 'stage=wayback total_seconds=%.2f\n' "$(echo "$(now_s) - $t0" | bc -l)" ;;
  swap)
    # The catalog-only part of the cutover, with the catch-up already done: what
    # the ACCESS EXCLUSIVE lock costs once the delta is drained outside the lock.
    lp "$db" -q <<'SQL'
BEGIN;
SET LOCAL lock_timeout = '5s';
LOCK TABLE public.fact_records IN ACCESS EXCLUSIVE MODE;
SELECT clock_timestamp() AS lock_acquired \gset
ALTER TABLE public.fact_records RENAME TO fact_records_old;
ALTER TABLE d.fact_records SET SCHEMA public;
SELECT round(extract(epoch FROM clock_timestamp() - :'lock_acquired'::timestamptz)::numeric, 3) AS swap_exclusive_lock_held_seconds;
COMMIT;
BEGIN;
LOCK TABLE public.fact_records IN ACCESS EXCLUSIVE MODE;
ALTER TABLE public.fact_records SET SCHEMA d;
ALTER TABLE public.fact_records_old RENAME TO fact_records;
COMMIT;
SQL
    ;;
  mixed)
    echo "-- old writer (ON CONFLICT (fact_id)) against the divided PK (scope_id, fact_id):"
    lp "$db" -q -c "BEGIN; INSERT INTO d.fact_records SELECT * FROM d.fact_records LIMIT 1 ON CONFLICT (fact_id) DO NOTHING; ROLLBACK;" 2>&1 | head -2 || true
    echo "-- new writer (ON CONFLICT (scope_id, fact_id)) against the old table WITHOUT the expand index:"
    lp "$db" -q -c "BEGIN; DROP INDEX fact_records_scope_fact_uniq; INSERT INTO public.fact_records SELECT * FROM public.fact_records LIMIT 1 ON CONFLICT (scope_id, fact_id) DO NOTHING; ROLLBACK;" 2>&1 | head -2 || true
    echo "-- new writer against the old table WITH the expand index (compatibility window):"
    lp "$db" -q -c "BEGIN; INSERT INTO public.fact_records SELECT * FROM public.fact_records LIMIT 1 ON CONFLICT (scope_id, fact_id) DO NOTHING; ROLLBACK;" 2>&1 | head -2 && echo "ok: accepted"
    echo "-- old writer against the old table with the expand index present:"
    lp "$db" -q -c "BEGIN; INSERT INTO public.fact_records SELECT * FROM public.fact_records LIMIT 1 ON CONFLICT (fact_id) DO NOTHING; ROLLBACK;" 2>&1 | head -2 && echo "ok: accepted" ;;
  *) echo "unknown stage $stage" >&2; exit 2 ;;
esac
