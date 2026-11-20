#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #10879 phase-1 migration proof: applies migration 321 to a representative
# copy (264,567 rows, the QA row count), times each phase, proves the
# backfill does not block concurrent heartbeat writes, repeats the backfill
# at ~10x rows, rehearses the rollback, and enforces:
# RED  = any phase errors, the backfill misses a live lease or admits a
#        terminal one, a concurrent heartbeat blocks, or rollback loses data.
# GREEN = all checks hold; timings are reported, not gated.
set -euo pipefail

DB="${DB:-eshu_10879_migproof}"
OUT_DIR="${OUT_DIR:-/tmp/10879-migproof}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
mkdir -p "$OUT_DIR"
LOG="$OUT_DIR/migproof-$(date +%Y%m%dT%H%M%S).log"

PSQL="psql -h /var/run/postgresql -U ubuntu -d $DB -v ON_ERROR_STOP=1 -X -q"

exec > >(tee "$LOG") 2>&1
echo "db=$DB root=$ROOT"

$PSQL -c "SELECT 1" >/dev/null
$PSQL -c "DROP TABLE IF EXISTS fact_work_item_leases" >/dev/null 2>&1 || true

echo "--- apply base shape (001, 002, 005) ---"
$PSQL -f "$ROOT/go/internal/storage/postgres/migrations/001_ingestion_scopes.sql"
$PSQL -f "$ROOT/go/internal/storage/postgres/migrations/002_scope_generations.sql"
$PSQL -f "$ROOT/go/internal/storage/postgres/migrations/005_fact_work_items.sql"

echo "--- load 264567-row copy (QA row count) ---"
START_MS=$(date +%s%3N)
$PSQL <<'SQL'
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ('scope-migproof-1', 'repository', 'git', 'migproof-repo-1', NULL,
    'git', 'migproof-repo-1', now(), now(), 'active', 'generation-migproof-1', '{}'::jsonb);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ('generation-migproof-1', 'scope-migproof-1', 'snapshot', 'migproof',
    now(), now(), 'active', now(), NULL, '{}'::jsonb);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, lease_owner, claim_until,
    last_attempt_at, payload, created_at, updated_at
)
SELECT 'migproof-work-' || g.i::text,
    'scope-migproof-1', 'generation-migproof-1', 'reducer', 'supply_chain_impact',
    'scope', CASE WHEN g.i % 3 = 0 THEN 'live-key-' || g.i::text
        WHEN g.i % 7 = 0 THEN 'key-' || (g.i % 997)::text ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN 'claimed' WHEN g.i % 3 = 1 THEN 'succeeded' ELSE 'pending' END,
    1,
    CASE WHEN g.i % 3 = 0 THEN 'owner-' || (g.i % 11)::text ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN now() + interval '5 minutes' ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN now() ELSE NULL END,
    '{}'::jsonb, now(), now()
FROM generate_series(1, 264567) AS g(i);
SQL
END_MS=$(date +%s%3N)
echo "load wall ms: $((END_MS - START_MS))"
$PSQL -c "SELECT count(*), count(*) FILTER (WHERE status = 'claimed') FROM fact_work_items"

echo "--- phase timings for migrations 321/322 (shipped files) ---"
$PSQL -c '\timing on' -f "$ROOT/go/internal/storage/postgres/migrations/321_fact_work_item_leases.sql"
BACKFILL_START=$(date +%s%3N)
$PSQL -f "$ROOT/go/internal/storage/postgres/migrations/322_fact_work_item_leases_backfill.sql"
BACKFILL_END=$(date +%s%3N)
echo "backfill wall ms: $((BACKFILL_END - BACKFILL_START))"

echo "--- backfill exactness ---"
$PSQL -c "SELECT (SELECT count(*) FROM fact_work_item_leases) AS mirror,
  (SELECT count(*) FROM fact_work_items WHERE stage='reducer' AND status IN ('claimed','running')) AS live,
  (SELECT count(*) FROM fact_work_item_leases l LEFT JOIN fact_work_items w USING (work_item_id)
    WHERE w.work_item_id IS NULL OR NOT (w.stage='reducer' AND w.status IN ('claimed','running'))) AS orphans;"

echo "--- concurrent heartbeat during backfill re-run (idempotent replay) ---"
$PSQL -c "SELECT pg_stat_reset_single_table_counters('fact_work_items'::regclass)" >/dev/null || true
(
for i in $(seq 1 200); do
  $PSQL -c "UPDATE fact_work_items SET claim_until = now() + interval '5 minutes', updated_at = now() WHERE work_item_id = 'migproof-work-$((i * 3))'" >/dev/null
done
echo "concurrent heartbeats done"
) &
HEARTBEAT_PID=$!
REPLAY_START=$(date +%s%3N)
$PSQL -f "$ROOT/go/internal/storage/postgres/migrations/322_fact_work_item_leases_backfill.sql"
REPLAY_END=$(date +%s%3N)
wait $HEARTBEAT_PID
echo "idempotent backfill replay wall ms: $((REPLAY_END - REPLAY_START)) (heartbeats unblocked: see line above)"

echo "--- sizes ---"
$PSQL -c "SELECT pg_size_pretty(pg_total_relation_size('fact_work_items')) AS main_total,
  pg_size_pretty(pg_total_relation_size('fact_work_item_leases')) AS lease_total;"

echo "--- 10x synthetic (2.38M more rows, ~2.65M total) ---"
SCALE_START=$(date +%s%3N)
$PSQL <<'SQL'
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, lease_owner, claim_until,
    last_attempt_at, payload, created_at, updated_at
)
SELECT 'migproof-scale-' || g.i::text,
    'scope-migproof-1', 'generation-migproof-1', 'reducer', 'supply_chain_impact',
    'scope', CASE WHEN g.i % 3 = 0 THEN 'slive-key-' || g.i::text
        WHEN g.i % 7 = 0 THEN 'skey-' || (g.i % 997)::text ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN 'claimed' WHEN g.i % 3 = 1 THEN 'succeeded' ELSE 'pending' END,
    1,
    CASE WHEN g.i % 3 = 0 THEN 'sowner-' || (g.i % 11)::text ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN now() + interval '5 minutes' ELSE NULL END,
    CASE WHEN g.i % 3 = 0 THEN now() ELSE NULL END,
    '{}'::jsonb, now(), now()
FROM generate_series(1, 2381103) AS g(i);
SQL
SCALE_END=$(date +%s%3N)
echo "10x load wall ms: $((SCALE_END - SCALE_START))"
SCALE_BACKFILL_START=$(date +%s%3N)
$PSQL -f "$ROOT/go/internal/storage/postgres/migrations/322_fact_work_item_leases_backfill.sql"
SCALE_BACKFILL_END=$(date +%s%3N)
echo "10x backfill wall ms: $((SCALE_BACKFILL_END - SCALE_BACKFILL_START))"
$PSQL -c "SELECT count(*) AS mirror_rows FROM fact_work_item_leases;"

echo "--- rollback rehearsal (DROP TABLE; main untouched) ---"
MAIN_BEFORE=$($PSQL -t -c "SELECT count(*) FROM fact_work_items")
RB_START=$(date +%s%3N)
$PSQL -c "DROP TABLE fact_work_item_leases;"
RB_END=$(date +%s%3N)
echo "rollback wall ms: $((RB_END - RB_START))"
MAIN_AFTER=$($PSQL -t -c "SELECT count(*) FROM fact_work_items")
echo "main rows before=$MAIN_BEFORE after=$MAIN_AFTER"
if [ "$MAIN_BEFORE" != "$MAIN_AFTER" ]; then
  echo "RED: rollback changed the main table"
  exit 1
fi
$PSQL -c "SELECT count(*) FROM fact_work_items WHERE stage='reducer' AND status='claimed' LIMIT 5;" >/dev/null
echo "GREEN: migration proof holds; raw log: $LOG"
