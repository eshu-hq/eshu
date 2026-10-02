-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7088: the supply-chain impact readiness query
-- (go/internal/query/supply/chain/impact) reads two kinds of
-- dependency-variable content_entity facts that migration 121's index does
-- not cover, because its predicate requires
-- entity_metadata->>'config_kind' = 'dependency':
--
--   * package_manifest_active's legacy arm (#7301), whose payload carries a
--     top-level config_kind = 'dependency';
--   * package_dependency_gap_active, whose entity_metadata config_kind is
--     one of the five provenance-only gap kinds.
--
-- With no matching index the only path was fact_records_collector_status_
-- active_idx: for a repository-only anchor on ops-qa the legacy arm probed
-- it once per active scope (819 loops, ~2.7k content_entity rows filtered
-- per scope; 9.4 s on the warm primary, cancelled at 30 s on the cold read
-- replica), and the gap read scanned the anchored repository's whole active
-- scope (up to 241,726 rows; 3.5-8.3 s cold for the 43 scopes with 10k+
-- rows). Measured by EXPLAIN (ANALYZE, BUFFERS) on ops-qa PG 18.3; see
-- docs/internal/evidence/7088-readiness-repo-scope.md.
--
-- Partial and repo-leading like migration 121, with scope_id and
-- generation_id following so the readiness query's per-scope LATERAL probe
-- binds the repository, scope and active generation in its Index Cond.
-- Both readers' predicates appear here verbatim (one OR arm each), so
-- Postgres proves the implication for custom and generic plans alike. Both
-- arms return zero or near-zero rows fleet-wide today (17 gap rows across
-- ~810 scopes on ops-qa), so the index is tiny and only the matching rows
-- pay maintenance.
--
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching the 073/086/118/121/152/155 precedent. One statement per
-- file so the migration coordinator can run it outside a transaction
-- (coordination.IsSoleConcurrentIndexStatement). A NEW migration file per
-- issue #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_content_entity_dependency_legacy_gap_repo_idx
    ON fact_records (
        (payload->>'repo_id'),
        scope_id,
        generation_id
    )
    WHERE fact_kind = 'content_entity'
      AND source_system = 'git'
      AND is_tombstone = FALSE
      AND payload->>'entity_type' = 'Variable'
      AND (
          payload->>'config_kind' = 'dependency'
          OR (payload->'entity_metadata'->>'config_kind') IN (
              'vcs_dependency',
              'path_dependency',
              'url_dependency',
              'editable_dependency',
              'unsupported_dependency'
          )
      );
