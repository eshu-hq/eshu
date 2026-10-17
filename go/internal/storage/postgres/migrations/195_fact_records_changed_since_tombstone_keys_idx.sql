-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7098 slice D-read: give the changed-since tombstone CTE its own seek.
-- current_tombstones reads WHERE scope_id = $1 AND generation_id = $3 AND
-- is_tombstone = TRUE AND fact_kind NOT LIKE 'reducer\_%' and returns a few
-- hundred keys per generation. The only prior access path was
-- fact_records_scope_generation_idx, whose bitmap fetched the whole
-- generation's heap (8,750 tuples for 750 tombstones on the fixture below)
-- and discarded the rest in a filter.
--
-- Measured on the same throwaway PostgreSQL 18.6 fixture as migration 194
-- (560,000 rows, 16 scopes x 4 generations, 876 MB heap) under the generic
-- plan services get through database/sql (PREPARE, forced
-- force_generic_plan), warm cache: the tombstone aggregate went from a
-- bitmap over fact_records_scope_generation_idx with 8,690 heap blocks and
-- Rows Removed by Filter 8,000 (91% fetched-then-discarded) to a bitmap
-- over this index with 750 heap blocks and no filter removal; buffers
-- ~8,706 to 755 (-91%), execution ~38 ms to 1.75 ms. The custom
-- (literal-parameter) plan takes the same index. The index is 104 kB on
-- that fixture: tombstones are rare, so this costs one cheap build and
-- negligible write amplification.
--
-- The predicate repeats the diff statement's literal filters
-- (changedSinceExcludeReducerDerivedKinds) so a generic plan can prove the
-- partial index applies.
--
-- CONCURRENTLY, so building it does not block writes to a table under
-- continuous ingest, and the lone statement in this file because the runner
-- Execs each file as one simple-query string and Postgres treats a
-- multi-statement string as an implicit transaction block, which CONCURRENTLY
-- cannot run inside. A failed build leaves an INVALID index which IF NOT
-- EXISTS would then skip forever; the schema apply path drops invalid
-- concurrent indexes by name before executing each definition, and runs each
-- statement outside any transaction on a dedicated bootstrap connection.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_changed_since_tombstone_keys_idx
    ON fact_records (scope_id, generation_id)
    WHERE is_tombstone = TRUE AND fact_kind NOT LIKE 'reducer\_%';
