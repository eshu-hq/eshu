-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7115: per-scope claim fence for the projector claim, on a table that only
-- the claim statement locks and updates (projector_queue_claim_sql.go,
-- claimProjectorWorkQuery). The claim reads the fence in its snapshot, locks
-- the fence row SKIP LOCKED joined on fence = snapshot value, and bumps it, so
-- a claim committed after another claimer's snapshot leaves a new row version
-- and the later claimer's EvalPlanQual recheck drops that scope instead of
-- granting a second live lease in it.
--
-- The fence must not live on ingestion_scopes: every FK child insert takes
-- KEY SHARE on the scope row, so a committed Ack next to a running child
-- insert leaves a multixact on a claimer's snapshot version, and LockRows then
-- follows the update chain with a blocking wait that SKIP LOCKED does not
-- cover (PG16 heap_lock_tuple -> heap_lock_updated_tuple_rec,
-- XLTW_LockUpdated). That deadlocked the claim against Ack. Nothing references
-- this table and only claimers write it, so that wait cannot be reached here.
-- Keep it that way: no FK may reference this table, and no statement other
-- than the claim may lock or update its rows.
CREATE TABLE IF NOT EXISTS projector_scope_claim_fences (
    scope_id TEXT PRIMARY KEY REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    fence BIGINT NOT NULL DEFAULT 0
);

-- Every scope needs a fence row before its projector work is claimable: the
-- claim inner-joins it, so a missing row makes the scope unclaimable. A
-- trigger covers every insert path, including pods that predate this
-- migration during a rollout. AFTER INSERT only, so the ingestion upsert's DO
-- UPDATE branch does not fire it. A brand-new scope cannot have an in-flight
-- bump, so ON CONFLICT DO NOTHING never waits here.
CREATE OR REPLACE FUNCTION projector_scope_claim_fences_on_scope_insert() RETURNS trigger AS $$
BEGIN
    INSERT INTO projector_scope_claim_fences (scope_id, fence) VALUES (NEW.scope_id, 0)
    ON CONFLICT (scope_id) DO NOTHING;
    RETURN NULL;
END $$ LANGUAGE plpgsql;

-- The trigger is created before the backfill, in the same implicit
-- transaction: CREATE TRIGGER holds SHARE ROW EXCLUSIVE on ingestion_scopes
-- until commit, so a concurrent scope insert either committed before the lock
-- (and the backfill below sees it) or waits and then fires the trigger.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgname = 'projector_scope_claim_fences_on_scope_insert'
          AND tgrelid = 'ingestion_scopes'::regclass
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER projector_scope_claim_fences_on_scope_insert
        AFTER INSERT ON ingestion_scopes
        FOR EACH ROW EXECUTE FUNCTION projector_scope_claim_fences_on_scope_insert();
    END IF;
END
$$;

-- Backfill existing scopes. NOT EXISTS keeps a rerun from touching rows a
-- claimer is bumping: INSERT ... ON CONFLICT waits on an in-flight update of
-- the conflicting row, and any insert into this table outside the trigger
-- must carry the same guard.
INSERT INTO projector_scope_claim_fences (scope_id, fence)
SELECT scope.scope_id, 0
FROM ingestion_scopes AS scope
WHERE NOT EXISTS (
    SELECT 1 FROM projector_scope_claim_fences AS fence WHERE fence.scope_id = scope.scope_id
)
ON CONFLICT (scope_id) DO NOTHING;
