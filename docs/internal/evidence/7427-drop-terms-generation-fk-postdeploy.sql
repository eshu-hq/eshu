-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
-- #7427 PR3 post-deploy checks for migration 191. Read-only; run in psql in
-- the schema the migrator uses, after db-migrate finished and the reducers
-- run the image with #7427 PR1 and PR2. Record each result on #7427.

-- check 1: no foreign key from the terms parent or any leaf references
-- scope_generations. Expect zero rows.
SELECT constraint_row.conrelid::regclass AS table_name, constraint_row.conname
FROM pg_constraint AS constraint_row
WHERE constraint_row.contype = 'f'
  AND constraint_row.confrelid = 'scope_generations'::regclass
  AND (constraint_row.conrelid = 'eshu_search_index_terms'::regclass
       OR constraint_row.conrelid IN (
           SELECT inhrelid FROM pg_inherits
           WHERE inhparent = 'eshu_search_index_terms'::regclass));

-- check 2: the tracked migrator recorded 191. Expect one row.
SELECT path, variant, applied_at
FROM eshu_schema_migrations
WHERE path = 'go/internal/storage/postgres/migrations/191_eshu_search_index_terms_drop_generation_fk.sql';

-- check 3: the orphan census, the same loose index scan per terms leaf that
-- the reducer's census runs (search/index/retention censusQuery), summed.
-- Expect orphan_pairs 0, or a value the next retention run sweeps to 0. It
-- reads each leaf's primary-key prefix, one index descent per pair; on the
-- 31,021-pair PR2 fixture the whole census took 0.8-3.6 s.
DO $$
DECLARE
    leaf regclass;
    leaf_pairs bigint;
    leaf_orphans bigint;
    total_pairs bigint := 0;
    total_orphans bigint := 0;
BEGIN
    FOR leaf IN
        SELECT inheritance.inhrelid::regclass
        FROM pg_inherits AS inheritance
        WHERE inheritance.inhparent = 'eshu_search_index_terms'::regclass
        ORDER BY 1
    LOOP
        EXECUTE format($census$
WITH RECURSIVE pairs AS (
    (
        SELECT relation.scope_id, relation.generation_id
        FROM %1$s AS relation
        ORDER BY relation.scope_id, relation.generation_id
        LIMIT 1
    )
    UNION ALL
    SELECT following.scope_id, following.generation_id
    FROM pairs
    CROSS JOIN LATERAL (
        SELECT relation.scope_id, relation.generation_id
        FROM %1$s AS relation
        WHERE (relation.scope_id, relation.generation_id) > (pairs.scope_id, pairs.generation_id)
        ORDER BY relation.scope_id, relation.generation_id
        LIMIT 1
    ) AS following
)
SELECT count(*),
    count(*) FILTER (WHERE NOT EXISTS (
        SELECT 1 FROM scope_generations AS generation
        WHERE generation.generation_id = pairs.generation_id
          AND generation.scope_id = pairs.scope_id))
FROM pairs
$census$, leaf) INTO leaf_pairs, leaf_orphans;
        total_pairs := total_pairs + leaf_pairs;
        total_orphans := total_orphans + leaf_orphans;
    END LOOP;
    RAISE NOTICE 'eshu_search_index_terms census: pairs=% orphan_pairs=%', total_pairs, total_orphans;
END $$;
