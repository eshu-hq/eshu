-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
-- #7427 PR3: why migration 191 is not a plain ALTER TABLE ... DROP
-- CONSTRAINT. Two psql sessions on a scratch database at migration 177 (the
-- terms generation foreign key present), with scope s1 and its superseded
-- generation s1-g3 seeded (PostgreSQL 18.6, database t7427c).
--
-- Lock set of the drop (BEGIN; ALTER ...; SELECT FROM pg_locks; ROLLBACK):
--   eshu_search_index_terms        AccessExclusiveLock  1
--   eshu_search_index_terms_pNN    AccessExclusiveLock  64
--   scope_generations              AccessExclusiveLock  1
-- No index lock. The plain form takes the parent and the 64 leaves first:
-- while a session holds scope_generations ACCESS SHARE, pg_locks shows the
-- plain ALTER granted on the parent and the 64 leaves and waiting on
-- scope_generations.
--
-- Session R (the retention prune order: scope_generations, then terms):
--   BEGIN;
--   SELECT generation_id FROM scope_generations WHERE generation_id = 's1-g3' FOR UPDATE;
--   SELECT pg_sleep(2);
--   DELETE FROM scope_generations WHERE generation_id = 's1-g3';  -- cascades into terms
--   ROLLBACK;
--
-- Session M, started 0.5 s after session R (the plain form):
--   SET lock_timeout = '5s';
--   ALTER TABLE eshu_search_index_terms DROP CONSTRAINT eshu_search_index_terms_generation_id_fkey;
--
-- PostgreSQL 18.6 result:
--   R: ERROR:  deadlock detected
--   DETAIL:  Process R waits for RowExclusiveLock on relation (a terms leaf); blocked by process M.
--            Process M waits for AccessExclusiveLock on relation (scope_generations); blocked by process R.
-- Either session can be the victim. When M is the victim the migrator fails:
-- it retries only 55P03. The search index pre-delete batch (its eligibility
-- CTE reads scope_generations, then it deletes terms) and the orphan sweep
-- batch take the same order.
--
-- Session M with the shipped form (the body of migration 191), same R:
-- M waits for scope_generations while holding nothing, R's cascade proceeds,
-- R ends, and M drops the key 7 ms later. Beside a writer that inserted a
-- term (terms, then scope_generations through its key check) M waits the same
-- way and drops the key 10 ms after the writer ends. Beside a session that
-- holds only terms, M fails at once with 55P03:
--   ERROR:  could not obtain lock on relation "eshu_search_index_terms"
--   CONTEXT:  SQL statement "LOCK TABLE eshu_search_index_terms IN ACCESS EXCLUSIVE MODE NOWAIT"
-- which the migrator retries.
DO $$
DECLARE
    foreign_key record;
BEGIN
    FOR foreign_key IN
        SELECT constraint_row.conrelid::regclass::text AS table_name, constraint_row.conname
        FROM pg_constraint AS constraint_row
        JOIN pg_attribute AS column_row
          ON column_row.attrelid = constraint_row.conrelid
         AND column_row.attnum = ANY (constraint_row.conkey)
        WHERE constraint_row.contype = 'f'
          AND constraint_row.conparentid = 0
          AND constraint_row.confrelid = to_regclass('scope_generations')
          AND column_row.attname = 'generation_id'
          AND (constraint_row.conrelid = to_regclass('eshu_search_index_terms')
               OR constraint_row.conrelid IN (
                   SELECT inheritance.inhrelid
                   FROM pg_inherits AS inheritance
                   WHERE inheritance.inhparent = to_regclass('eshu_search_index_terms')))
        ORDER BY constraint_row.conrelid = to_regclass('eshu_search_index_terms'), constraint_row.conname
    LOOP
        LOCK TABLE scope_generations IN ACCESS EXCLUSIVE MODE;
        LOCK TABLE eshu_search_index_terms IN ACCESS EXCLUSIVE MODE NOWAIT;
        EXECUTE format('ALTER TABLE %s DROP CONSTRAINT IF EXISTS %I', foreign_key.table_name, foreign_key.conname);
    END LOOP;
END $$;
