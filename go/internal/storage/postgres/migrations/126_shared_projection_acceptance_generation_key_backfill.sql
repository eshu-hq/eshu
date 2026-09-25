-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #6679: backfill shared_projection_acceptance.generation_ingested_at (added
-- by 125) from scope_generations.ingested_at.
--
-- Separate from 125 because a bootstrap migration file runs as one implicit
-- transaction: in the same file, 125's ACCESS EXCLUSIVE lock would be held for
-- the whole rewrite (measured 16-19 s per 1M rows locally) and block every
-- acceptance reader and writer. Here only row locks are taken.
--
-- FOR UPDATE SKIP LOCKED: the backfill never waits on a row a live reducer is
-- writing, so it cannot deadlock with a writer's batch (40P01 would fail the
-- bootstrap) and never stalls on a long writer transaction. A skipped row keeps
-- a NULL key, which the upsert guard treats as older than any incoming
-- generation: its next write advances it and fills the key. Rerun-safe: only
-- NULL keys are selected, so a second boot updates nothing.
UPDATE shared_projection_acceptance AS acceptance
SET generation_ingested_at = generation.ingested_at
FROM (
    SELECT candidate.scope_id, candidate.acceptance_unit_id, candidate.source_run_id
    FROM shared_projection_acceptance AS candidate
    WHERE candidate.generation_ingested_at IS NULL
    FOR UPDATE SKIP LOCKED
) AS pending,
    scope_generations AS generation
WHERE acceptance.scope_id = pending.scope_id
  AND acceptance.acceptance_unit_id = pending.acceptance_unit_id
  AND acceptance.source_run_id = pending.source_run_id
  AND generation.generation_id = acceptance.generation_id;
