-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7098 slice D-read: trim the changed-since live scan to the rows its
-- statement hashes. prior_keys, current_active_keys, and both duplicate-row
-- CTEs read WHERE scope_id = $1 AND generation_id = $2 AND
-- is_tombstone = FALSE AND fact_kind NOT LIKE 'reducer\_%'. The only prior
-- access path was fact_records_scope_generation_idx, whose bitmap fetched
-- every generation row's TOAST payload and discarded the reducer-derived
-- share in a filter (the 41-47% fetched-then-discarded reducer rows).
--
-- Measured on a throwaway PostgreSQL 18.6 fixture (560,000 rows, 16 scopes x
-- 4 generations, ~43% reducer rows per generation, 876 MB heap) under the
-- generic plan services get through database/sql (PREPARE, forced
-- force_generic_plan), warm cache: the prior_keys aggregate went from a
-- bitmap over fact_records_scope_generation_idx with 8,750 heap blocks and
-- Rows Removed by Filter 3,750 to a bitmap over this index with 5,000 heap
-- blocks and no filter removal; buffers 8,761 to 5,007 (-43%), execution
-- 21.4 ms to 13.9 ms (-35%). The custom (literal-parameter) plan takes the
-- same index. The remaining cost is the per-row sha256 over TOAST the
-- statement still pays; removing the re-hash is the WAIT-GATED stored-digest
-- write-path slice (#7401 BIN1), deliberately not this file.
--
-- The predicate repeats the diff statement's literal filters
-- (changedSinceExcludeReducerDerivedKinds) so a generic plan can prove the
-- partial index applies. Narrow on purpose: the statement hashes every
-- matching payload from the heap, so wider keys only tax ingester and
-- reducer writes without skipping heap visits.
--
-- CONCURRENTLY, so building it does not block writes to a table under
-- continuous ingest, and the lone statement in this file because the runner
-- Execs each file as one simple-query string and Postgres treats a
-- multi-statement string as an implicit transaction block, which CONCURRENTLY
-- cannot run inside. A failed build leaves an INVALID index which IF NOT
-- EXISTS would then skip forever; the schema apply path drops invalid
-- concurrent indexes by name before executing each definition, and runs each
-- statement outside any transaction on a dedicated bootstrap connection.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_changed_since_live_keys_idx
    ON fact_records (scope_id, generation_id)
    WHERE is_tombstone = FALSE AND fact_kind NOT LIKE 'reducer\_%';
