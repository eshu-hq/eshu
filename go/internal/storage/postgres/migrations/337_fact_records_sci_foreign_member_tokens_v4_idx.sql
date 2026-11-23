-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10643 PR 2: the GIN array_ops index on the foreign-member token function
-- (migration 336), partial on the finding kind and live rows. Each of the 16
-- key-range holder statements reads it with && on that statement's lookup
-- tokens, so a hot id's foreign-member rows are split over the statements
-- by bucket instead of read as one unranged statement. The bootstrap
-- fragment lives in schema_fact_records_supply_chain_impact_indexes.go.
--
-- CONCURRENTLY, so the build does not block writers on fact_records. Single
-- statement: a concurrent build cannot run inside a transaction block. A
-- failed build leaves an INVALID index; the schema apply path drops it by
-- name before re-running this file (concurrentIndexNamesForInvalidCleanup).
-- The schema Job returns only after the build is valid, and the reducer
-- that reads the index rolls out after the Job.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_sci_foreign_member_tokens_v4_idx
    ON fact_records USING GIN (eshu_sci_foreign_member_tokens_v4(fact_id, payload))
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;
