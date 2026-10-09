-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7760: generation-tag stored content so manifest reads bind to the active
-- generation. content_files is latest-wins on (repo_id, relative_path) with
-- no generation column, written before the activating Ack, so a stored
-- manifest can be ahead of the active generation (#7609, repaired by the
-- dirty-scope union in #7776). generation_id records the generation that
-- wrote each row; readers that want active-generation content fail safe on
-- the tag (NULL, dangling, empty, or never-activated tag reads dirty, never
-- dropped) instead of inferring dirt from proxy signals. Readers that want
-- latest-wins keep it: the tag is advisory, with no foreign key by design
-- (retention prunes generation rows; a pruned tag must read dirty, not
-- violate). New writes stamp content.Materialization.GenerationID; deletes
-- need no tag.
--
-- Backfill direction is fail-safe: a row is attributed to the scope's active
-- generation only where ALL hold --
--   (i)   the scope has zero generation rows with activated_at IS NULL
--         (every generation activated at least once; a refused-then-superseded
--         writer still has NULL and excludes its scope);
--   (ii)  the active row exists with status='active' AND activated_at NOT NULL;
--   (iii) the row's indexed_at <= the activation (per-row form of the
--         manifest_max guard: skew only over-excludes, and a scope whose
--         manifest is clean but another path is ahead still tags the clean
--         rows instead of excluding the whole scope);
--   (iv)  exactly one kind='repository' scope maps the repo (source_key is not
--         unique, so a shared key stays NULL rather than tagging
--         nondeterministically).
-- In the ahead corner the writing generation has activated_at NULL, so (i)
-- excludes exactly the dangerous scopes; backfilling them to active would
-- bless ahead content as active truth (the #7609 bug). Post-hole scopes whose
-- signal row was retention-pruned pass (i) but fail (iii): they stay NULL
-- (dirty), no worse than today's RED. Scopes failing any clause converge as
-- generations rewrite their manifests.
--
-- Lock and deploy behavior (Postgres 18): ADD COLUMN ... NULL is catalog-only
-- (no table rewrite, no default to materialize). The backfill UPDATE takes
-- row locks only (ROW EXCLUSIVE on the table): concurrent reads proceed and
-- concurrent writer upserts to the same rows wait briefly, so this is
-- online-safe with no maintenance window. The WHERE generation_id IS NULL
-- guard makes re-runs idempotent and prevents clobbering tags the new writer
-- stamps mid-migration; old-code writes during a mixed deploy land NULL and
-- read dirty (safe). Rollback drops the column (tags lost); re-upgrade
-- re-backfills idempotently. ANALYZE refreshes planner stats for the new
-- column immediately instead of waiting for autovacuum.
--
-- Migration proof (local Postgres 18, 2026-10-09): 1,000,000-row
-- content_files fixture, 3,000 repository scopes, 1% planted never-activated
-- generations. ADD COLUMN: 4.0 ms (catalog-only). Backfill UPDATE: 30.9 s,
-- tagging 890,300 rows and skipping 109,700 (30 pending-scope manifests +
-- 9,970 pending-scope filler rows by clause i + 99,700 late filler rows by
-- clause iii; the skip arithmetic cross-checks the seed exactly). ANALYZE:
-- 1.0 s. Row locks only; concurrent reads proceed. Full plan and read-side
-- EXPLAIN in docs/internal/evidence/7760-generation-tag.md.

ALTER TABLE content_files
    ADD COLUMN IF NOT EXISTS generation_id TEXT NULL;

UPDATE content_files AS f
SET generation_id = one.active_generation_id
FROM (
    SELECT s.source_key, s.scope_id, s.active_generation_id,
           count(*) OVER (PARTITION BY s.source_key) AS scope_count
    FROM ingestion_scopes AS s
    WHERE s.scope_kind = 'repository'
) AS one
JOIN scope_generations AS a
  ON a.scope_id = one.scope_id
 AND a.generation_id = one.active_generation_id
WHERE f.repo_id = one.source_key
  AND one.scope_count = 1
  AND f.generation_id IS NULL
  AND a.status = 'active'
  AND a.activated_at IS NOT NULL
  AND f.indexed_at <= a.activated_at
  AND NOT EXISTS (
      SELECT 1
      FROM scope_generations AS g
      WHERE g.scope_id = one.scope_id
        AND g.activated_at IS NULL
  );

ANALYZE content_files;
