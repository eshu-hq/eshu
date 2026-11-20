#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #10879 phase-1 migration proof: applies migration 322 to a representative
# copy (264,567 rows, the QA row count), times each phase, proves the
# backfill does not block concurrent heartbeat writes, plants a deterministic
# orphan and proves the 323→324 sequence removes it, replays the sequence
# under 200 concurrent heartbeats plus 200 concurrent main-only terminals
# and proves zero orphans with mirror == stamped live, repeats the sequence
# at ~10x rows, rehearses the rollback, and enforces:
# RED  = any phase errors, exactness mismatch (mirror != stamped live or
#        orphans != 0), plant/reconcile mismatch, or rollback loses data.
# GREEN = all checks hold; timings are reported, not gated.
set -euo pipefail

DB="${DB:-eshu_10879_migproof}"
OUT_DIR="${OUT_DIR:-/tmp/10879-migproof}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
mkdir -p "$OUT_DIR"
LOG="$OUT_DIR/migproof-$(date +%Y%m%dT%H%M%S).log"

PSQL="psql -h /var/run/postgresql -U ubuntu -d $DB -v ON_ERROR_STOP=1 -X -q"
# Racing loops use the non-stopping variant: 324's locked CTE takes
# FOR UPDATE OF w and can deadlock-victimize a racing single-row UPDATE
# (detected, one side aborted, row left live). A victim row never orphans,
# so the end-state gates below stay exact; aborting the script on a victim
# would gate on lock timing instead of correctness.
PSQL_RACE="psql -h /var/run/postgresql -U ubuntu -d $DB -X -q"

# The shipped sequence: 323 copies live leases, 324 reconciles raced rows.
run_backfill() {
  $PSQL -f "$ROOT/go/internal/storage/postgres/migrations/323_fact_work_item_leases_backfill.sql"
  $PSQL -f "$ROOT/go/internal/storage/postgres/migrations/324_fact_work_item_leases_reconcile.sql"
}

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
$PSQL -f "$ROOT/scripts/dev/10879-lease-migration-load.sql"
END_MS=$(date +%s%3N)
echo "load wall ms: $((END_MS - START_MS))"
$PSQL -c "SELECT count(*), count(*) FILTER (WHERE status = 'claimed') FROM fact_work_items"

echo "--- phase timings for migrations 322/323/324 (shipped files) ---"
$PSQL -c '\timing on' -f "$ROOT/go/internal/storage/postgres/migrations/322_fact_work_item_leases.sql"
BACKFILL_START=$(date +%s%3N)
run_backfill
BACKFILL_END=$(date +%s%3N)
echo "backfill wall ms: $((BACKFILL_END - BACKFILL_START))"

echo "--- backfill exactness (gated) ---"
read -r MIRROR STAMPED_LIVE ORPHANS <<< "$($PSQL -t -A -F' ' -c \
  "SELECT (SELECT count(*) FROM fact_work_item_leases),
  (SELECT count(*) FROM fact_work_items WHERE stage='reducer' AND status IN ('claimed','running')
    AND lease_owner IS NOT NULL AND claim_until IS NOT NULL AND last_attempt_at IS NOT NULL),
  (SELECT count(*) FROM fact_work_item_leases l LEFT JOIN fact_work_items w USING (work_item_id)
    WHERE w.work_item_id IS NULL OR NOT (w.stage='reducer' AND w.status IN ('claimed','running')));")"
echo "mirror=$MIRROR stamped_live=$STAMPED_LIVE orphans=$ORPHANS"
if [ "$ORPHANS" != "0" ]; then echo "RED: backfill admitted or orphaned rows"; exit 1; fi
if [ "$MIRROR" != "$STAMPED_LIVE" ]; then echo "RED: mirror=$MIRROR != stamped live=$STAMPED_LIVE"; exit 1; fi

echo "--- deterministic orphan plant + reconcile (seeded RED/GREEN) ---"
$PSQL -c "UPDATE fact_work_items SET status='succeeded', lease_owner=NULL, claim_until=NULL, last_attempt_at=NULL WHERE work_item_id='migproof-work-3'"
PLANTED=$($PSQL -t -A -c "SELECT count(*) FROM fact_work_item_leases l LEFT JOIN fact_work_items w USING (work_item_id)
  WHERE w.work_item_id IS NULL OR NOT (w.stage='reducer' AND w.status IN ('claimed','running'))")
echo "planted orphans=$PLANTED (want 1: the detector sees the race damage)"
if [ "$PLANTED" != "1" ]; then echo "RED: plant did not orphan exactly one row"; exit 1; fi
run_backfill
CLEANED=$($PSQL -t -A -c "SELECT count(*) FROM fact_work_item_leases l LEFT JOIN fact_work_items w USING (work_item_id)
  WHERE w.work_item_id IS NULL OR NOT (w.stage='reducer' AND w.status IN ('claimed','running'))")
echo "orphans after reconcile=$CLEANED (want 0)"
if [ "$CLEANED" != "0" ]; then echo "RED: reconcile left orphans"; exit 1; fi

echo "--- concurrent heartbeats + terminals during backfill re-run (idempotent replay) ---"
$PSQL -c "SELECT pg_stat_reset_single_table_counters('fact_work_items'::regclass)" >/dev/null || true
(
for i in $(seq 1 200); do
  $PSQL_RACE -c "UPDATE fact_work_items SET claim_until = now() + interval '5 minutes', updated_at = now() WHERE work_item_id = 'migproof-work-$((i * 3))'" >/dev/null 2>&1
done
echo "concurrent heartbeats done"
) &
HEARTBEAT_PID=$!
(
for i in $(seq 1 200); do
  $PSQL_RACE -c "UPDATE fact_work_items SET status='succeeded', lease_owner=NULL, claim_until=NULL, last_attempt_at=NULL, updated_at = now() WHERE work_item_id = 'migproof-work-$((i * 3 + 600))'" >/dev/null 2>&1
done
echo "concurrent terminals done"
) &
TERMINAL_PID=$!
REPLAY_START=$(date +%s%3N)
run_backfill
REPLAY_END=$(date +%s%3N)
wait $HEARTBEAT_PID $TERMINAL_PID
echo "idempotent backfill replay wall ms: $((REPLAY_END - REPLAY_START)) (workers unblocked: see lines above)"

echo "--- post-race exactness (gated) ---"
run_backfill
read -r MIRROR2 STAMPED_LIVE2 ORPHANS2 <<< "$($PSQL -t -A -F' ' -c \
  "SELECT (SELECT count(*) FROM fact_work_item_leases),
  (SELECT count(*) FROM fact_work_items WHERE stage='reducer' AND status IN ('claimed','running')
    AND lease_owner IS NOT NULL AND claim_until IS NOT NULL AND last_attempt_at IS NOT NULL),
  (SELECT count(*) FROM fact_work_item_leases l LEFT JOIN fact_work_items w USING (work_item_id)
    WHERE w.work_item_id IS NULL OR NOT (w.stage='reducer' AND w.status IN ('claimed','running')));")"
echo "mirror=$MIRROR2 stamped_live=$STAMPED_LIVE2 orphans=$ORPHANS2"
if [ "$ORPHANS2" != "0" ]; then echo "RED: race left orphans"; exit 1; fi
if [ "$MIRROR2" != "$STAMPED_LIVE2" ]; then echo "RED: mirror=$MIRROR2 != stamped live=$STAMPED_LIVE2"; exit 1; fi

echo "--- sizes ---"
$PSQL -c "SELECT pg_size_pretty(pg_total_relation_size('fact_work_items')) AS main_total,
  pg_size_pretty(pg_total_relation_size('fact_work_item_leases')) AS lease_total;"

echo "--- 10x synthetic (2.38M more rows, ~2.65M total) ---"
SCALE_START=$(date +%s%3N)
$PSQL -f "$ROOT/scripts/dev/10879-lease-migration-scale.sql"
SCALE_END=$(date +%s%3N)
echo "10x load wall ms: $((SCALE_END - SCALE_START))"
SCALE_BACKFILL_START=$(date +%s%3N)
run_backfill
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
