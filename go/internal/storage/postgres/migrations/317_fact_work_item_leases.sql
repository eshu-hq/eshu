-- 317_fact_work_item_leases.sql: sidecar lease table for the reducer queue
-- (#10879, phase 1 dual-write cutover).
--
-- The fencing triple (lease_owner, claim_until, last_attempt_at) moves here
-- so renewals stop rewriting the main row and its indexes. Phase 1 keeps the
-- main columns authoritative: every reducer writer maintains both, and every
-- reader still reads the main table, so this migration changes no behavior.
-- The read switch and the heartbeat narrowing land in phase 2.
--
-- Shape notes:
-- - PK on work_item_id with ON DELETE CASCADE keeps the mirror exact: a row
--   deleted from fact_work_items cannot orphan a lease row.
-- - fillfactor 70 reserves room for HOT renewal updates: the lease row is
--   updated in place and none of its columns is indexed except through the
--   PK and the conflict unique below, so heartbeat updates stay HOT.
-- - stage is carried so the projector lane (phase 1b) can share this table
--   without cross-stage one-per-group collisions.
-- - The backfill admits only fully-stamped live reducer leases; the INSERT
--   is idempotent (ON CONFLICT DO NOTHING), so a retried bootstrap replays
--   safely, and the unique index build that follows sees a duplicate-free
--   set (concurrent duplicate claims are already fenced by the main-table
--   partial unique index from migration 005).
CREATE TABLE IF NOT EXISTS fact_work_item_leases (
    work_item_id TEXT PRIMARY KEY REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    stage TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    conflict_domain TEXT NOT NULL DEFAULT 'scope',
    conflict_key TEXT NULL,
    lease_owner TEXT NOT NULL,
    claim_until TIMESTAMPTZ NOT NULL,
    last_attempt_at TIMESTAMPTZ NOT NULL
) WITH (fillfactor = 70);

CREATE UNIQUE INDEX IF NOT EXISTS fact_work_item_leases_live_lease_uniq
    ON fact_work_item_leases (stage, conflict_domain, COALESCE(conflict_key, scope_id));
