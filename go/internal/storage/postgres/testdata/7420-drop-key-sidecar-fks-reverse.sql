-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
-- #7420 reverse procedure: put back the three per-fact foreign keys that
-- migrations 188-190 drop. Not shipped as a migration. Run it only to roll
-- back to an image older than #7420 PR1 (the PR1 image deletes the key rows
-- itself), with psql, one step at a time, after the census in step 1 reads
-- zero. Each step 2 block and each step 3 statement is its own transaction;
-- rerunning a finished step is a no-op.
-- TestFactKeyForeignKeyReverseProcedureLive runs this file.
--
-- Do not run it while an image with migrations 188-190 can start the local
-- supervisor (eshu local): the supervisor re-applies every migration at each
-- start and drops the keys again. The tracked migrator (db-migrate) applies
-- each migration once and leaves the restored keys alone; step 4 says how to
-- make it drop them again on a later roll-forward.
--
-- psql sets no lock_timeout. Without one, a lock request in step 2 waits
-- behind every open writer, and every new writer of the table queues behind
-- the waiting request until it is granted.
SET lock_timeout = '5s';

-- step 1: census precondition. Every key row must have its fact, or the
-- VALIDATE in step 3 fails. Expect zero rows; remove any it reports with an
-- anti-join delete inside the reported (scope_id, generation_id) first.
SELECT table_name, sum(orphan_rows) AS orphan_rows FROM (
    SELECT 'relationship_reference_candidate_keys' AS table_name, count(*) AS orphan_rows
    FROM relationship_reference_candidate_keys AS keys
    WHERE NOT EXISTS (SELECT 1 FROM fact_records AS fact WHERE fact.fact_id = keys.fact_id)
    UNION ALL
    SELECT 'package_manifest_consumption_keys', count(*)
    FROM package_manifest_consumption_keys AS keys
    WHERE NOT EXISTS (SELECT 1 FROM fact_records AS fact WHERE fact.fact_id = keys.fact_id)
    UNION ALL
    SELECT 'package_registry_identity_keys', count(*)
    FROM package_registry_identity_keys AS keys
    WHERE NOT EXISTS (SELECT 1 FROM fact_records AS fact WHERE fact.fact_id = keys.fact_id)
) AS census
GROUP BY table_name
HAVING sum(orphan_rows) > 0;

-- step 2: add each key NOT VALID. ADD FOREIGN KEY takes SHARE ROW EXCLUSIVE
-- on the key table and on fact_records (measured on PostgreSQL 18.6). Once
-- granted it is held only for the catalog change, but while the request
-- waits it stalls every new writer of fact_records behind it, until it is
-- granted or the 5 s lock_timeout above fires. The same lock order rule as
-- migrations 188-190 applies: wait for fact_records while holding nothing,
-- then take the key table with NOWAIT. On "canceling statement due to lock
-- timeout" or "could not obtain lock" (55P03), rerun the block, preferably
-- when no long ingestion transaction is open. New writes are checked from
-- here on.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE contype = 'f'
        AND conrelid = to_regclass('relationship_reference_candidate_keys')
        AND confrelid = to_regclass('fact_records')) THEN
        LOCK TABLE fact_records IN SHARE ROW EXCLUSIVE MODE;
        LOCK TABLE relationship_reference_candidate_keys IN SHARE ROW EXCLUSIVE MODE NOWAIT;
        ALTER TABLE relationship_reference_candidate_keys
            ADD CONSTRAINT relationship_reference_candidate_keys_fact_id_fkey
            FOREIGN KEY (fact_id) REFERENCES fact_records(fact_id) ON DELETE CASCADE NOT VALID;
    END IF;
END $$;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE contype = 'f'
        AND conrelid = to_regclass('package_manifest_consumption_keys')
        AND confrelid = to_regclass('fact_records')) THEN
        LOCK TABLE fact_records IN SHARE ROW EXCLUSIVE MODE;
        LOCK TABLE package_manifest_consumption_keys IN SHARE ROW EXCLUSIVE MODE NOWAIT;
        ALTER TABLE package_manifest_consumption_keys
            ADD CONSTRAINT package_manifest_consumption_keys_fact_id_fkey
            FOREIGN KEY (fact_id) REFERENCES fact_records(fact_id) ON DELETE CASCADE NOT VALID;
    END IF;
END $$;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE contype = 'f'
        AND conrelid = to_regclass('package_registry_identity_keys')
        AND confrelid = to_regclass('fact_records')) THEN
        LOCK TABLE fact_records IN SHARE ROW EXCLUSIVE MODE;
        LOCK TABLE package_registry_identity_keys IN SHARE ROW EXCLUSIVE MODE NOWAIT;
        ALTER TABLE package_registry_identity_keys
            ADD CONSTRAINT package_registry_identity_keys_fact_id_fkey
            FOREIGN KEY (fact_id) REFERENCES fact_records(fact_id) ON DELETE CASCADE NOT VALID;
    END IF;
END $$;

-- step 3: validate the existing rows. VALIDATE takes SHARE UPDATE EXCLUSIVE
-- on the key table and ROW SHARE on fact_records: reads and writes continue.
-- It runs one anti-join of the key table to fact_records: 1.14 s, 0.98 s and
-- 0.67 s for the three tables on the 3M-fact fixture (3.75M key rows), so
-- size the run from the table sizes and cache state. A failure here means an
-- orphan appeared after step 1: rerun step 1, clean up, rerun this step.
ALTER TABLE relationship_reference_candidate_keys VALIDATE CONSTRAINT relationship_reference_candidate_keys_fact_id_fkey;
ALTER TABLE package_manifest_consumption_keys VALIDATE CONSTRAINT package_manifest_consumption_keys_fact_id_fkey;
ALTER TABLE package_registry_identity_keys VALIDATE CONSTRAINT package_registry_identity_keys_fact_id_fkey;

-- step 4: roll forward again later. Migrations 188-190 are recorded in the
-- tracked migrator's ledger, so db-migrate does not re-run them and the keys
-- stay. Before deploying an image with 188-190 again, run this in the schema
-- the migrator uses, so the next db-migrate drops the keys again:
DELETE FROM eshu_schema_migrations
WHERE path IN (
    'go/internal/storage/postgres/migrations/188_relationship_reference_candidate_keys_drop_fact_fk.sql',
    'go/internal/storage/postgres/migrations/189_package_manifest_consumption_keys_drop_fact_fk.sql',
    'go/internal/storage/postgres/migrations/190_package_registry_identity_keys_drop_fact_fk.sql'
);
