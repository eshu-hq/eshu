-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7420: drop the per-fact foreign key from package_manifest_consumption_keys to
-- fact_records. Each fact removal fired one referential trigger for it.
-- Generation retention now removes the key rows explicitly, by (scope_id,
-- generation_id), before it removes the facts (migrations 182 and 183 hold
-- the partition indexes it uses).
--
-- Lock order: DROP CONSTRAINT takes ACCESS EXCLUSIVE on this table and on
-- fact_records. A plain ALTER locks this table first, then waits for
-- fact_records while it holds that lock. Ingestion writes a fact, then its
-- key, so the plain ALTER deadlocks it (SQLSTATE 40P01, which the migration
-- runner does not retry). This block waits only while it holds nothing: it
-- waits for fact_records (bounded by the runner's lock_timeout), then takes
-- this table with NOWAIT. A busy key table fails at once with 55P03, which
-- the runner retries within its lock retry budget.
--
-- The catalog lookup makes a re-run (the local supervisor re-applies every
-- definition at each start) a no-op that takes no table lock.
DO $$
DECLARE
    foreign_key record;
BEGIN
    FOR foreign_key IN
        SELECT constraint_row.conname
        FROM pg_constraint AS constraint_row
        WHERE constraint_row.contype = 'f'
          AND constraint_row.conrelid = to_regclass('package_manifest_consumption_keys')
          AND constraint_row.confrelid = to_regclass('fact_records')
    LOOP
        LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE;
        LOCK TABLE package_manifest_consumption_keys IN ACCESS EXCLUSIVE MODE NOWAIT;
        EXECUTE format('ALTER TABLE package_manifest_consumption_keys DROP CONSTRAINT IF EXISTS %I', foreign_key.conname);
    END LOOP;
END $$;
