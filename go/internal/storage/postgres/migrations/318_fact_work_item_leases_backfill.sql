-- 318_fact_work_item_leases_backfill.sql: idempotent live-lease backfill
-- for #10879 phase 1.
--
-- Split from migration 317 per #7961: table-locking DDL and bulk DML must
-- not share a file. 317 creates the table and its indexes; this file only
-- copies fully-stamped live reducer leases. It is idempotent
-- (ON CONFLICT DO NOTHING), so a retried bootstrap replays safely.
INSERT INTO fact_work_item_leases (
    work_item_id, stage, scope_id, conflict_domain, conflict_key,
    lease_owner, claim_until, last_attempt_at
)
SELECT work_item_id, stage, scope_id, conflict_domain, conflict_key,
    lease_owner, claim_until, last_attempt_at
FROM fact_work_items
WHERE stage = 'reducer'
  AND status IN ('claimed', 'running')
  AND lease_owner IS NOT NULL
  AND claim_until IS NOT NULL
  AND last_attempt_at IS NOT NULL
ON CONFLICT (work_item_id) DO NOTHING;

-- At most one live lease per conflict key per stage (#10879, completing the
-- #4137 rule on the mirror): the claim path translates a violation here into
-- the same deferred no-op as the main-table unique index.
