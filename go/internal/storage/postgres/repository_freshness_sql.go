// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// repositoryFreshnessResolveQuery resolves a canonical repository id to the
// (scope_id, generation_id) pair backing GET
// /api/v0/repositories/{id}/freshness (#5143). It reuses
// activeRepositoryGenerationsQuery's exact join shape
// (ingestion_queries.go) -- the shared latestGenerationCTE resolves each
// scope's currently active (or, absent one, newest) generation, and the same
// COALESCE(repo_id, graph_id, name) derivation reads the repository fact's
// payload -- but bounds it to a single repository via a WHERE predicate
// instead of activeRepositoryGenerationsQuery's DISTINCT ON over every repo.
//
// The repo_id filter runs as a Filter over the existing partial index
// fact_records_active_repository_idx (fact_kind = 'repository' AND
// source_system = 'git'): that index already bounds the scan to one fact row
// per repo per generation -- a set sized by repo count, not file count -- so
// no new index is warranted here (Index Doctrine: add an index only when
// evidence shows a hot, unbounded scan; this query reuses a proven bound).
//
// Keying downstream reads on the returned (scope_id, generation_id) pair,
// never repo_id alone, avoids the documented COALESCE-collapse caveat
// (ingestion_queries.go's activeRepositoryGenerationsQuery doc comment):
// two distinct scopes whose COALESCE(repo_id, graph_id, name) collapses to
// the same value would otherwise share one row.
const repositoryFreshnessResolveQuery = latestGenerationCTE + `
SELECT resolved.scope_id, resolved.generation_id
FROM (
    SELECT
        COALESCE(fact.payload->>'repo_id', fact.payload->>'graph_id', fact.payload->>'name', '') AS repo_id,
        fact.scope_id,
        fact.generation_id,
        fact.observed_at,
        fact.fact_id
    FROM fact_records AS fact
    JOIN latest_generations AS latest
      ON latest.scope_id = fact.scope_id
     AND latest.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'repository'
) AS resolved
WHERE resolved.repo_id = $1
ORDER BY resolved.observed_at DESC, resolved.fact_id DESC
LIMIT 1
`

// repositoryFreshnessGenerationQuery reads the resolved generation's
// lifecycle row plus its owning scope's kind and a display-name-resolved
// repo slug (for the webhook-trigger lookup below), keyed by (scope_id,
// generation_id) -- the ingestion_scopes primary key and
// scope_generations_scope_generation_idx serve this as a single-row,
// index-backed read (Performance Evidence: see this package's README, "Repo
// freshness single-scope composite read (#5143)").
//
// repo_display mirrors buildLiveActivityQuery's source_display expression
// (status_operations.go): repo_slug, then repo_name, then the opaque
// source_key, so the webhook lookup below always has a best-effort
// "owner/repo" identity to match against even when the scope payload is
// sparse.
const repositoryFreshnessGenerationQuery = `
SELECT g.generation_id, g.status, g.trigger_kind, g.is_delta, g.activated_at,
       COALESCE(g.source_commit_sha, ''), g.observed_at,
       s.scope_kind,
       COALESCE(NULLIF(BTRIM(s.payload->>'repo_slug'), ''), NULLIF(BTRIM(s.payload->>'repo_name'), ''), s.source_key) AS repo_display
FROM scope_generations AS g
JOIN ingestion_scopes AS s ON s.scope_id = g.scope_id
WHERE g.scope_id = $1
  AND g.generation_id = $2
`

// repositoryFreshnessStageCountsQuery groups OUTSTANDING fact_work_items rows
// by (stage, status) for the resolved (scope, generation), mirroring
// stageCountsQuery's shape (status_queries.go) scoped to a single
// generation. fact_work_items_scope_generation_idx (scope_id, generation_id,
// status, updated_at DESC) makes this an index-only bounded read (Performance
// Evidence: see this package's README).
//
// The status filter is load-bearing, not cosmetic (#5143 live Compose proof
// regression): outstanding_by_stage is contractually outstanding work only.
// Without it, a fully-built repository's succeeded rows leak into the
// response (observed live as {reducer, succeeded, 15}, {projector,
// succeeded, 1} for a repo whose verdict was already "current"), which would
// make a still-building repo's UI copy count already-finished items as
// "items left". The status set mirrors the "not yet succeeded" set
// generation_liveness_sql.go's drain predicates use (pending, claimed,
// running, retrying, failed, dead_letter) -- everything except succeeded.
//
// deriveRepositoryFreshnessStages (repository_freshness.go) also skips a
// 'succeeded' row when deriving the Reduced/Projected booleans; that is
// deliberate defense in depth, not redundant with this filter. It kept the
// derived verdict/stages correct even while this predicate was missing
// (verdict rendered "current" correctly in the live proof despite the
// outstanding_by_stage leak) and continues to guard the booleans if this
// predicate ever regresses again.
const repositoryFreshnessStageCountsQuery = `
SELECT stage, status, COUNT(*) AS count
FROM fact_work_items
WHERE scope_id = $1
  AND generation_id = $2
  AND status IN ('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter')
GROUP BY stage, status
ORDER BY stage, status
`

// repositoryFreshnessSharedPendingQuery groups outstanding
// shared_projection_intents rows by projection_domain for one repository's
// resolved generation.
//
// Performance Evidence (#5143 prove-theory-first, recorded in this
// package's README): keying on repository_id first (the existing
// shared_projection_intents_repo_run_idx (repository_id, source_run_id,
// projection_domain, created_at) index prefix), with generation_id and
// completed_at as residual filters, measured 0.018ms against a 14k-pending
// synthetic flood -- versus 2.3ms for a generation_id-only shape, which has
// no supporting index and degrades linearly with the GLOBAL pending
// backlog rather than this one repository's. Never rewrite this to filter
// on generation_id alone.
const repositoryFreshnessSharedPendingQuery = `
SELECT projection_domain, COUNT(*) AS count
FROM shared_projection_intents
WHERE repository_id = $1
  AND generation_id = $2
  AND completed_at IS NULL
GROUP BY projection_domain
ORDER BY projection_domain
`

// repositoryFreshnessWebhookQuery finds queued or claimed webhook refresh
// triggers for one repository's display identity, most recent first. It is
// bounded (webhook_refresh_triggers_status_idx on (status, updated_at))
// rather than filtered to a single row, because a repository can have more
// than one in-flight trigger (for example a rapid double-push); the caller
// compares each row's target_sha against the resolved generation's observed
// commit to decide whether any represents a push eshu has not started
// building yet.
//
// The LOWER()/LOWER() comparison is load-bearing, not cosmetic (#5148
// under-report nuance): repositoryidentity.RepoSlugFromRemoteURL
// (repositoryidentity/identity.go) lower-cases the owner/repo slug it derives
// from a git remote, and repo_display (repositoryFreshnessGenerationQuery
// above) prefers that lower-cased repo_slug first. Webhook normalizers
// (webhook/normalizer_github.go and its siblings) instead store
// repository_full_name verbatim from the provider payload, which preserves
// the repository owner's actual casing. A case-sensitive equality here would
// silently fail to match any repository whose real name contains uppercase
// characters, under-reporting a genuine unobserved push rather than erring.
// repository_full_name carries no index on this table (only
// (status, updated_at) and (status, received_at, trigger_id) are indexed), so
// folding both sides to the same case changes zero index usage and zero query
// shape.
const repositoryFreshnessWebhookQuery = `
SELECT target_sha, ref, received_at
FROM webhook_refresh_triggers
WHERE status IN ('queued', 'claimed')
  AND LOWER(repository_full_name) = LOWER($1)
ORDER BY received_at DESC, trigger_id DESC
LIMIT 5
`
