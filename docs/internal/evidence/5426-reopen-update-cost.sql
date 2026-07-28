-- SUPERSEDED. Kept for history only.
--
-- This script times a SERVER-SIDE plpgsql UPDATE loop, which excludes exactly
-- the client round-trips that dominate the real cost: production reopens rows
-- one at a time through queue.ReopenSucceeded, one round-trip each. The numbers
-- below therefore understate the per-drain cost by roughly two orders of
-- magnitude, and they compare against a hypothetical unbounded variant rather
-- than the real pre-change ingester baseline, which was ZERO.
--
-- Use TestCorrelationReopenPerDrainCostProof
-- (go/internal/storage/postgres/ingestion_reopen_correlation_cost_proof_test.go,
-- gated on ESHU_CORRELATION_REOPEN_COST_PROOF_DSN) instead: it drives the real
-- ReopenSucceededReducerWorkItems against Postgres and reports the round-trip
-- cost, and its doc comment names what still is not measured.

DROP SCHEMA IF EXISTS reopen_update_cost CASCADE;
CREATE SCHEMA reopen_update_cost;
SET search_path TO reopen_update_cost;
CREATE TABLE fact_work_items (
    work_item_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    lease_owner TEXT NULL,
    claim_until TIMESTAMPTZ NULL,
    visible_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
INSERT INTO fact_work_items (work_item_id, status, updated_at)
SELECT 'wi-' || g, 'succeeded', now() FROM generate_series(1, 5001) AS g;
ANALYZE;
DO $$
DECLARE t0 timestamptz; n int; ids text[];
BEGIN
  SELECT array_agg(work_item_id) INTO ids FROM fact_work_items LIMIT 1;
  t0 := clock_timestamp();
  FOR n IN 1..5001 LOOP
    UPDATE fact_work_items
       SET status='pending', lease_owner=NULL, claim_until=NULL,
           visible_at=NULL, next_attempt_at=NULL, updated_at=now()
     WHERE work_item_id = 'wi-' || n AND status='succeeded';
  END LOOP;
  RAISE NOTICE 'OLD shape: 5001 single-row reopen UPDATEs took % ms (server-side, no client round-trip)',
    round(extract(epoch from (clock_timestamp()-t0))*1000);
END $$;
UPDATE fact_work_items SET status='succeeded';
DO $$
DECLARE t0 timestamptz; n int;
BEGIN
  t0 := clock_timestamp();
  FOR n IN 1..201 LOOP
    UPDATE fact_work_items
       SET status='pending', lease_owner=NULL, claim_until=NULL,
           visible_at=NULL, next_attempt_at=NULL, updated_at=now()
     WHERE work_item_id = 'wi-' || n AND status='succeeded';
  END LOOP;
  RAISE NOTICE 'NEW shape: 201 single-row reopen UPDATEs took % ms (server-side, no client round-trip)',
    round(extract(epoch from (clock_timestamp()-t0))*1000);
END $$;
DROP SCHEMA reopen_update_cost CASCADE;
