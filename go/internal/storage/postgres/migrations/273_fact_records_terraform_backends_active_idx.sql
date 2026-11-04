-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8875 lane 1 (terraform state reads): the canonical terraform-backends
-- reader's backend_generations CTE probes every active generation's file
-- facts and evaluates the JSONB array-length CASE per heap row. Live
-- read-only EXPLAIN (ANALYZE, BUFFERS) against the production-scale
-- primary (99M-row
-- fact_records, 2,254 active generations): the CTE arm alone is 5.9 s /
-- 1.1M shared-buffer hits for 175 returned contexts (planner estimates 1
-- row -- a 175x misestimate with no statistics on the CASE expression),
-- inside a 15.3 s / 3.0M-hit statement that returns 9,188 rows the adapter
-- filters in Go to ~1 matching repo. The same CASE shapes the
-- matching_backend_generations CTE of the backend-filter reader, so one
-- index serves both.
--
-- Fixture proof (disposable PostgreSQL 18.6, 3,407 file facts across two
-- active generations with 3,400 decoys): the new canonical query serves
-- the backend_generations arm from this index in 0.03 ms versus 1.48 ms
-- without it; whole-statement buffers drop 4,409 to 1,947 (-56 %) at 2.79
-- ms versus 3.69 ms with byte-identical 404 output rows. Fixture timings,
-- not a deployed claim; the deployed magnitude follows the live
-- attribution (5.9 s / 1.1M-hit arm inside a 15.3 s statement).
--
-- One statement per file is required for a concurrent build; manifest
-- writers continue during it. The partial predicate is byte-aligned with
-- the served queries' load-bearing filter (modulo table alias); keep the
-- query CASE, the Go DDL entry (terraformBackendsFactRecordReadIndexesSQL),
-- and this predicate in lockstep. No is_tombstone term: the served queries
-- do not
-- filter tombstones. Write cost is one predicate evaluation per ingested
-- file fact with a B-tree entry only for backends-bearing rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_terraform_backends_active_idx
    ON fact_records (scope_id, generation_id, (payload->>'repo_id'))
    WHERE fact_kind = 'file'
      AND source_system = 'git'
      AND CASE
            WHEN jsonb_typeof(payload->'parsed_file_data'->'terraform_backends') = 'array'
            THEN jsonb_array_length(payload->'parsed_file_data'->'terraform_backends')
            ELSE 0
          END > 0;
