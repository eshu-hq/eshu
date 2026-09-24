-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7007: the supply-chain impact readiness query's package_manifest_active
-- CTE (go/internal/query/supply/chain/impact/readiness_postgres_query.go)
-- reads content_entity facts tagged entity_type='Variable' AND
-- entity_metadata->>'config_kind'='dependency', now bounded by
-- payload->>'repo_id' after the #7007 query pushdown. content_entity is the
-- single largest fact_kind in the corpus and carries no index matching this
-- predicate, so without this index the planner has only
-- fact_records_collector_status_active_idx (scope_id, generation_id,
-- source_system, fact_kind) to work with: it walks every content_entity fact
-- per active scope and filters entity_type/config_kind/repo_id out of the
-- heap afterward, which on the corpus this predicate was measured against
-- (EXPLAIN (ANALYZE, BUFFERS), impact/findings ops-qa readiness proof,
-- go/cmd/api/supply-chain/impact/findings — see the #7007 PR evidence) cost
-- the majority of that endpoint's reported 13-25s.
--
-- Partial and repo_id-leading (not full-expression): the predicate matches
-- package_manifest_active's WHERE clause exactly, so unrelated content_entity
-- rows (the other ~99% of that fact_kind: files, sections, non-dependency
-- entity types) pay no index-maintenance cost, and repo_id leads so the
-- single-repository read the query issues resolves as a direct index probe
-- instead of a per-scope scan-and-filter. scope_id/generation_id follow so
-- the active-generation join in the CTE is index-served too.
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching 073/086/118 precedent. This is a NEW migration file per
-- issue #7002 (never edit an applied migration).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_content_entity_dependency_variable_repo_idx
    ON fact_records (
        (payload->>'repo_id'),
        scope_id,
        generation_id
    )
    WHERE fact_kind = 'content_entity'
      AND source_system = 'git'
      AND is_tombstone = FALSE
      AND payload->>'entity_type' = 'Variable'
      AND (payload->'entity_metadata'->>'config_kind') = 'dependency';
