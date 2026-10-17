-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7427 PR3: drop the generation_id foreign key from eshu_search_index_terms
-- to scope_generations. Its ON DELETE CASCADE ran one statement on the
-- hash-partitioned parent for every pruned generation, under the retention
-- scope lock, and that statement walked the whole leaf even when it found no
-- row. Generation retention now deletes a generation's terms in committed
-- batches before it prunes the generation and admits only drained ones; the
-- orphan census and sweep remove rows a late writer leaves, and readers join
-- the scope's active generation. The scope_id key and the documents and stats
-- generation keys stay.
--
-- The key has two names in the field: eshu_search_index_terms_generation_id_fkey
-- (fresh install, 003b) and eshu_search_index_terms_shadow_generation_id_fkey
-- (an install that went through the 039a cutover). The parent and all 64
-- leaves carry the same name; dropping the parent's key drops the leaves'.
-- The block finds the key in pg_constraint, never by name, and also drops a
-- key a leaf carries on its own.
--
-- Lock order: DROP CONSTRAINT takes ACCESS EXCLUSIVE on the terms parent, on
-- each of its 64 leaves and on scope_generations (measured on PostgreSQL
-- 18.6). A plain ALTER locks the parent and the leaves first, then waits for
-- scope_generations while it holds them. Retention holds scope_generations
-- and then deletes terms, so the plain ALTER deadlocks it (SQLSTATE 40P01,
-- which the migration runner does not retry). This block waits only while it
-- holds nothing: it waits for scope_generations (bounded by the runner's
-- lock_timeout), then takes the parent and its leaves with NOWAIT. A terms
-- statement in flight makes it fail at once with 55P03, which the runner
-- retries within its lock retry budget.
--
-- The catalog lookup makes a re-run (the local supervisor re-applies every
-- definition at each start) a no-op that takes no table lock.
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
        -- Leaf keys first, the parent's last. Only a key a leaf carries on its
        -- own qualifies (conparentid = 0); an inherited leaf key listed first
        -- would fail with "cannot drop inherited constraint".
        ORDER BY constraint_row.conrelid = to_regclass('eshu_search_index_terms'), constraint_row.conname
    LOOP
        LOCK TABLE scope_generations IN ACCESS EXCLUSIVE MODE;
        LOCK TABLE eshu_search_index_terms IN ACCESS EXCLUSIVE MODE NOWAIT;
        EXECUTE format('ALTER TABLE %s DROP CONSTRAINT IF EXISTS %I', foreign_key.table_name, foreign_key.conname);
    END LOOP;
END $$;
