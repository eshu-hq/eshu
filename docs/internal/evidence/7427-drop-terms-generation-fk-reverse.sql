-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
-- #7427 PR3 reverse procedure: put back the eshu_search_index_terms
-- generation foreign key that migration 191 drops. Not shipped as a
-- migration. Run it only to roll back to an image older than #7427 PR1 (that
-- image's retention relies on the cascade), with psql, one step at a time,
-- following the same lock order as migration 191.
-- Each step is its own transaction; rerunning a finished step is a no-op.
-- Measured on PostgreSQL 18.6 only (it accepts a NOT VALID foreign key on a
-- partitioned table).
--
-- Do not run it while an image with migration 191 can start the local
-- supervisor (eshu local): the supervisor re-applies every migration at each
-- start and drops the key again. The tracked migrator (db-migrate) applies
-- each migration once; step 4 says how to make it drop the key again on a
-- later roll-forward.
--
-- psql sets no lock_timeout. Without one, a lock request in step 2 waits
-- behind every open transaction on scope_generations, and every new reader
-- and writer of that table queues behind the waiting request until it is
-- granted.
SET lock_timeout = '5s';

-- step 1: census precondition. Every terms row must have its generation, or
-- the VALIDATE in step 3 fails with 23503. The reducer's
-- eshu_dp_search_index_orphan_generations{table="eshu_search_index_terms"}
-- gauge reads the same set once per retention poll interval. This statement
-- scans every terms leaf once (the same anti-join VALIDATE runs). Expect zero
-- rows; delete what it reports with
-- DELETE FROM eshu_search_index_terms WHERE scope_id = '<scope>' AND generation_id = '<generation>';
SELECT terms.scope_id, terms.generation_id, count(*) AS orphan_rows
FROM eshu_search_index_terms AS terms
WHERE NOT EXISTS (
    SELECT 1 FROM scope_generations AS generation
    WHERE generation.generation_id = terms.generation_id)
GROUP BY terms.scope_id, terms.generation_id;

-- step 2: add the key NOT VALID. ADD FOREIGN KEY takes SHARE ROW EXCLUSIVE
-- on the terms parent, its 64 leaves and scope_generations (measured on
-- PostgreSQL 18.6). The same lock order rule as migration 191 applies: wait
-- for scope_generations while holding nothing, then take the terms parent
-- and its leaves with NOWAIT. Once granted the locks are held for the catalog
-- change only (13 ms on an 800,000-row fixture). On "canceling statement due
-- to lock timeout" or "could not obtain lock" (55P03), rerun the block. New
-- terms writes are checked from here on.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint AS constraint_row
        WHERE constraint_row.contype = 'f'
          AND constraint_row.conparentid = 0
          AND constraint_row.conrelid = to_regclass('eshu_search_index_terms')
          AND constraint_row.confrelid = to_regclass('scope_generations')) THEN
        LOCK TABLE scope_generations IN SHARE ROW EXCLUSIVE MODE;
        LOCK TABLE eshu_search_index_terms IN SHARE ROW EXCLUSIVE MODE NOWAIT;
        ALTER TABLE eshu_search_index_terms
            ADD CONSTRAINT eshu_search_index_terms_generation_id_fkey
            FOREIGN KEY (generation_id) REFERENCES scope_generations(generation_id) ON DELETE CASCADE NOT VALID;
    END IF;
END $$;

-- step 3: validate the existing rows. VALIDATE takes SHARE UPDATE EXCLUSIVE
-- on the terms parent and its leaves and ROW SHARE on scope_generations:
-- reads and writes continue. It runs one anti-join per leaf (98.8 ms for
-- 800,000 rows on the scratch fixture), so size the run from the table size
-- and cache state. A 23503 here means an orphan appeared after step 1: rerun
-- step 1, delete it, rerun this step.
ALTER TABLE eshu_search_index_terms VALIDATE CONSTRAINT eshu_search_index_terms_generation_id_fkey;

-- step 4: roll forward again later. Migration 191 is recorded in the tracked
-- migrator's ledger, so db-migrate does not re-run it and the key stays.
-- Before deploying an image with 191 again, run this in the schema the
-- migrator uses, so the next db-migrate drops the key again:
DELETE FROM eshu_schema_migrations
WHERE path = 'go/internal/storage/postgres/migrations/191_eshu_search_index_terms_drop_generation_fk.sql';
