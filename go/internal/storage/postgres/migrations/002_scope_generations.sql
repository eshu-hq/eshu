CREATE TABLE IF NOT EXISTS scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind TEXT NOT NULL,
    freshness_hint TEXT NULL,
    source_commit_sha TEXT NULL,
    is_delta BOOLEAN NOT NULL DEFAULT false,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- Additive migration for installs created before the delta-correctness
-- baseline (epic #2340): source_commit_sha carries the commit a generation was
-- observed from so the next git sync can baseline its delta on the last
-- successfully projected commit instead of the local working-copy HEAD; is_delta
-- marks delta resyncs so the reconciliation sweep can find the last full
-- observation per scope.
ALTER TABLE scope_generations
    ADD COLUMN IF NOT EXISTS source_commit_sha TEXT NULL;

ALTER TABLE scope_generations
    ADD COLUMN IF NOT EXISTS is_delta BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS scope_generations_scope_idx
    ON scope_generations (scope_id, status, ingested_at DESC);

-- Backs the latest-generation DISTINCT ON (issue #3704): the relationship
-- backfill and active-generation lookups pick each scope's newest generation by
-- ORDER BY (scope_id, ingested_at DESC, generation_id DESC). The status-leading
-- scope_generations_scope_idx cannot serve that ordering without a sort because
-- status sits between scope_id and ingested_at; this index leads straight into
-- the DISTINCT ON sort key so the per-scope newest row is an index read.
CREATE INDEX IF NOT EXISTS scope_generations_scope_latest_lookup_idx
    ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);

CREATE INDEX IF NOT EXISTS scope_generations_active_pending_activity_idx
    ON scope_generations (GREATEST(observed_at, ingested_at, COALESCE(activated_at, observed_at)) DESC)
    WHERE status IN ('pending', 'active');

CREATE UNIQUE INDEX IF NOT EXISTS scope_generations_active_scope_idx
    ON scope_generations (scope_id)
    WHERE status = 'active';

-- Backs activeFactWorkItemsCTE's stale_generation/active_generation self-join
-- (issue #4446): the CTE resolves each work item's own generation row by
-- primary key (cheap) but resolves the scope's ACTIVE generation row by
-- (scope_id, active_generation_id) equality
-- ("scope.active_generation_id = active_generation.generation_id"). None of
-- the existing scope_generations indexes lead with generation_id, so that
-- join could only use scope_generations_scope_idx to fetch every generation
-- row for the scope (all re-ingestion history) and filter the one matching
-- generation_id via a post-scan Join Filter — a per-status-query cost that
-- scales with total re-ingestion churn (generations per scope), not with the
-- work item count. This index lets the planner resolve the active-generation
-- side of the join with a single equality index scan instead of a per-scope
-- generation-history fetch; ingested_at is INCLUDEd so the CTE's tiebreak
-- ordering predicate is covered without a heap fetch.
CREATE INDEX IF NOT EXISTS scope_generations_scope_generation_idx
    ON scope_generations (scope_id, generation_id) INCLUDE (ingested_at);

-- The index's benefit to the planner's cost model is sensitive to ANALYZE's
-- sampled selectivity estimate for a (scope_id, generation_id) equality match
-- against a correlated outer column (this CTE's
-- generation_id = scope.active_generation_id join): at repo scale (many
-- generations per scope), Postgres's default statistics target (100 buckets)
-- can under- or over-estimate that match count, making
-- scope_generations_scope_generation_idx and the pre-existing, broader
-- scope_generations_scope_idx a near cost tie in the planner's estimate even
-- though the new index is materially cheaper once chosen (fewer buffer
-- reads, no post-scan Join Filter). Raising the statistics target on
-- generation_id and scope_id improves the odds ANALYZE's sample converges on
-- the correct near-1-row estimate for the new index, but is not a guaranteed
-- override of the planner's cost-based choice — this is Postgres's normal,
-- documented planner behavior, not an index defect. Empirically, once the
-- planner does pick scope_generations_scope_generation_idx, stageCountsQuery
-- drops from ~310ms (no index, forced full generation-history scan per
-- scope) to ~120-140ms (index chosen) at ~100k scope_generations rows; see
-- the PR body for the full before/after EXPLAIN evidence. The #4446 caching
-- layer (status_stage_counts_cache.go) is the change that gives an
-- operator-visible, planner-independent latency bound for repeated status
-- reads within the cache TTL.
ALTER TABLE scope_generations ALTER COLUMN generation_id SET STATISTICS 1000;
ALTER TABLE scope_generations ALTER COLUMN scope_id SET STATISTICS 1000;
