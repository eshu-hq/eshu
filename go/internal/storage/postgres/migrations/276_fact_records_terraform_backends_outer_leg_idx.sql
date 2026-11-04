-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8875 round 2 S1 (terraform state reads): the canonical
-- terraform-backends reader's outer leg joins fact_records to
-- backend_generations on (scope_id, generation_id, payload->>'repo_id')
-- and re-evaluates the three-way (backends + variables + locals)
-- JSONB array-length CASE sum per fact heap row in memory. Live
-- read-only EXPLAIN (ANALYZE, BUFFERS) against the production-scale
-- primary attributes ~55k filtered rows to this arm; the lane-1 partial
-- index (migration 273) serves only the single-CASE backend_generations
-- CTE, so the outer leg's three-way sum has no usable index.
--
-- Fixture A/B (disposable PostgreSQL, parent lane round-2 proof): serving
-- the outer leg from this index cuts buffers 59% and statement ms 45%
-- with byte-identical row counts and the two-owner ambiguity contract
-- held. S2/S3 candidates rejected. Fixture timings, not a deployed
-- claim; the deployed magnitude follows the live attribution above.
--
-- One statement per file is required for a concurrent build; manifest
-- writers continue during it. The partial predicate is byte-aligned with
-- the served query's load-bearing filter (modulo table alias); keep the
-- query CASE, the Go DDL entry
-- (terraformBackendsOuterLegFactRecordReadIndexesSQL), and this predicate
-- in lockstep. No is_tombstone term: the served query does not filter
-- tombstones. Write cost is one predicate evaluation per ingested file
-- fact with a B-tree entry only for rows the outer leg can return.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_terraform_backends_outer_leg_idx
    ON fact_records (scope_id, generation_id, (payload->>'repo_id'))
    WHERE fact_kind = 'file'
      AND source_system = 'git'
      AND (
          CASE
            WHEN jsonb_typeof(payload->'parsed_file_data'->'terraform_backends') = 'array'
            THEN jsonb_array_length(payload->'parsed_file_data'->'terraform_backends')
            ELSE 0
          END
        + CASE
            WHEN jsonb_typeof(payload->'parsed_file_data'->'terraform_variables') = 'array'
            THEN jsonb_array_length(payload->'parsed_file_data'->'terraform_variables')
            ELSE 0
          END
        + CASE
            WHEN jsonb_typeof(payload->'parsed_file_data'->'terraform_locals') = 'array'
            THEN jsonb_array_length(payload->'parsed_file_data'->'terraform_locals')
            ELSE 0
          END
      ) > 0;
