// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// resolveChangedSinceScopeQuery resolves one repository-kind (or any) scope and
// its current active generation for a changed-since diff. It accepts a scope_id
// or a repository source_key selector ($1 scope_id, $2 repository; an empty
// string bypasses that predicate). It returns the resolved scope identity, the
// current active generation id (empty when the scope has no active generation),
// the current active generation observed_at, and whether the scope currently has
// a pending generation in flight.
//
// Parameter order:
//
//	$1 scope_id     (empty bypasses)
//	$2 repository   (empty bypasses; matches source_key for repository scopes)
const resolveChangedSinceScopeQuery = `
SELECT
    scope.scope_id,
    scope.scope_kind,
    COALESCE(scope.active_generation_id, '') AS current_active_generation_id,
    active_generation.observed_at AS current_observed_at,
    EXISTS (
        SELECT 1
        FROM scope_generations AS pending
        WHERE pending.scope_id = scope.scope_id
          AND pending.status = 'pending'
    ) AS has_pending
FROM ingestion_scopes AS scope
LEFT JOIN scope_generations AS active_generation
    ON active_generation.generation_id = scope.active_generation_id
WHERE ($1 = '' OR scope.scope_id = $1)
  AND ($2 = '' OR (scope.scope_kind = 'repository' AND scope.source_key = $2))
ORDER BY scope.observed_at DESC, scope.scope_id ASC
LIMIT 1
`

// resolveChangedSinceGenerationQuery resolves the prior generation a
// changed-since diff compares against. When $2 (since_generation_id) is
// supplied, it returns that exact generation if it belongs to the scope.
// Otherwise it returns the generation that was observed at or before $3
// (since_observed_at) for the scope, preferring the most recent such generation.
// It returns the generation id and its observed_at, or no rows when nothing
// matches (an explicit not-found signal).
//
// Parameter order:
//
//	$1 scope_id               (required, exact)
//	$2 since_generation_id    (empty falls through to observed-at resolution)
//	$3 since_observed_at      (used only when $2 is empty; the diff baseline)
const resolveChangedSinceGenerationQuery = `
SELECT
    generation.generation_id,
    generation.observed_at
FROM scope_generations AS generation
WHERE generation.scope_id = $1
  AND (
        ($2 <> '' AND generation.generation_id = $2)
        OR ($2 = '' AND generation.observed_at <= $3)
      )
ORDER BY
    (CASE WHEN $2 <> '' THEN 0 ELSE 1 END) ASC,
    generation.observed_at DESC,
    generation.generation_id ASC
LIMIT 1
`

// resolveChangedSinceRetentionExpiredQuery distinguishes a pruned prior
// generation from a generation id or timestamp that never belonged to the
// scope. It looks up safe hashes recorded by generation retention cleanup and
// returns the pruned generation's observed_at for the unavailable response.
//
// Parameter order:
//
//	$1 scope_id_hash
//	$2 generation_id_hash  (empty falls through to observed-at resolution)
//	$3 since_observed_at   (used only when $2 is empty)
const resolveChangedSinceRetentionExpiredQuery = `
SELECT TRUE AS retention_expired, generation_observed_at
FROM generation_retention_events
WHERE scope_id_hash = $1
  AND (
        ($2 <> '' AND generation_id_hash = $2)
        OR ($2 = '' AND generation_observed_at <= $3)
      )
ORDER BY generation_observed_at DESC
LIMIT 1
`

// changedSinceCountsQuery computes the exact per-category, per-classification
// stable-fact-key counts for a changed-since diff between a prior generation and
// the current active generation of one scope. It FULL OUTER JOINs the prior
// generation's non-tombstone key set against the current generation's key set
// (including tombstones) on (fact_category, stable_fact_key), then classifies
// each key:
//
//   - added:      present in current (non-tombstone), absent in prior.
//   - updated:    present in both, payload hash differs.
//   - unchanged:  present in both, payload hash matches.
//   - retired:    present in prior, tombstoned in the current generation.
//   - superseded: present in prior, absent entirely from the current generation.
//
// The category bucket is files (fact_kind = 'file'), content_entities
// (fact_kind = 'content_entity'), or facts (everything else). Payload identity
// uses md5(payload::text) so a changed payload is detected without a stored hash
// column. The diff is keyed by (scope_id, generation_id, stable_fact_key),
// matching the fact_records primary access path.
//
// Parameter order:
//
//	$1 scope_id
//	$2 prior_generation_id
//	$3 current_generation_id
const changedSinceCountsQuery = `
WITH prior_keys AS (
    SELECT
        CASE
            WHEN fact_kind = 'file' THEN 'files'
            WHEN fact_kind = 'content_entity' THEN 'content_entities'
            ELSE 'facts'
        END AS fact_category,
        stable_fact_key,
        MIN(md5(payload::text)) AS payload_hash
    FROM fact_records
    WHERE scope_id = $1
      AND generation_id = $2
      AND is_tombstone = FALSE
    GROUP BY fact_category, stable_fact_key
),
current_active_keys AS (
    SELECT
        CASE
            WHEN fact_kind = 'file' THEN 'files'
            WHEN fact_kind = 'content_entity' THEN 'content_entities'
            ELSE 'facts'
        END AS fact_category,
        stable_fact_key,
        MIN(md5(payload::text)) AS payload_hash
    FROM fact_records
    WHERE scope_id = $1
      AND generation_id = $3
      AND is_tombstone = FALSE
    GROUP BY fact_category, stable_fact_key
),
current_tombstones AS (
    SELECT DISTINCT
        CASE
            WHEN fact_kind = 'file' THEN 'files'
            WHEN fact_kind = 'content_entity' THEN 'content_entities'
            ELSE 'facts'
        END AS fact_category,
        stable_fact_key
    FROM fact_records
    WHERE scope_id = $1
      AND generation_id = $3
      AND is_tombstone = TRUE
),
classified AS (
    SELECT
        COALESCE(prior.fact_category, current.fact_category) AS fact_category,
        CASE
            WHEN prior.stable_fact_key IS NULL THEN 'added'
            WHEN current.stable_fact_key IS NOT NULL
                 AND prior.payload_hash IS DISTINCT FROM current.payload_hash THEN 'updated'
            WHEN current.stable_fact_key IS NOT NULL THEN 'unchanged'
            WHEN tombstone.stable_fact_key IS NOT NULL THEN 'retired'
            ELSE 'superseded'
        END AS classification
    FROM prior_keys AS prior
    FULL OUTER JOIN current_active_keys AS current
        ON current.fact_category = prior.fact_category
       AND current.stable_fact_key = prior.stable_fact_key
    LEFT JOIN current_tombstones AS tombstone
        ON tombstone.fact_category = COALESCE(prior.fact_category, current.fact_category)
       AND tombstone.stable_fact_key = COALESCE(prior.stable_fact_key, current.stable_fact_key)
)
SELECT fact_category, classification, COUNT(*) AS key_count
FROM classified
GROUP BY fact_category, classification
ORDER BY fact_category ASC, classification ASC
`

// changedSinceSamplesQuery returns bounded, deterministic sample handles for one
// (category, classification) bucket of a changed-since diff. It reuses the same
// classification logic as changedSinceCountsQuery but emits the stable_fact_key
// and an example fact_kind per key, ordered by stable_fact_key, capped by LIMIT.
// The caller fetches limit+1 rows to detect truncation and trims back to limit.
//
// Parameter order:
//
//	$1 scope_id
//	$2 prior_generation_id
//	$3 current_generation_id
//	$4 fact_category        ('files' | 'content_entities' | 'facts')
//	$5 classification       ('added' | 'updated' | 'unchanged' | 'retired' | 'superseded')
//	$6 limit                (sample cap; caller passes limit + 1)
const changedSinceSamplesQuery = `
WITH prior_keys AS (
    SELECT
        CASE
            WHEN fact_kind = 'file' THEN 'files'
            WHEN fact_kind = 'content_entity' THEN 'content_entities'
            ELSE 'facts'
        END AS fact_category,
        stable_fact_key,
        MIN(fact_kind) AS fact_kind,
        MIN(md5(payload::text)) AS payload_hash
    FROM fact_records
    WHERE scope_id = $1
      AND generation_id = $2
      AND is_tombstone = FALSE
    GROUP BY fact_category, stable_fact_key
),
current_active_keys AS (
    SELECT
        CASE
            WHEN fact_kind = 'file' THEN 'files'
            WHEN fact_kind = 'content_entity' THEN 'content_entities'
            ELSE 'facts'
        END AS fact_category,
        stable_fact_key,
        MIN(fact_kind) AS fact_kind,
        MIN(md5(payload::text)) AS payload_hash
    FROM fact_records
    WHERE scope_id = $1
      AND generation_id = $3
      AND is_tombstone = FALSE
    GROUP BY fact_category, stable_fact_key
),
current_tombstones AS (
    SELECT
        CASE
            WHEN fact_kind = 'file' THEN 'files'
            WHEN fact_kind = 'content_entity' THEN 'content_entities'
            ELSE 'facts'
        END AS fact_category,
        stable_fact_key,
        MIN(fact_kind) AS fact_kind
    FROM fact_records
    WHERE scope_id = $1
      AND generation_id = $3
      AND is_tombstone = TRUE
    GROUP BY fact_category, stable_fact_key
),
classified AS (
    SELECT
        COALESCE(prior.fact_category, current.fact_category) AS fact_category,
        COALESCE(prior.stable_fact_key, current.stable_fact_key) AS stable_fact_key,
        COALESCE(current.fact_kind, tombstone.fact_kind, prior.fact_kind) AS fact_kind,
        CASE
            WHEN prior.stable_fact_key IS NULL THEN 'added'
            WHEN current.stable_fact_key IS NOT NULL
                 AND prior.payload_hash IS DISTINCT FROM current.payload_hash THEN 'updated'
            WHEN current.stable_fact_key IS NOT NULL THEN 'unchanged'
            WHEN tombstone.stable_fact_key IS NOT NULL THEN 'retired'
            ELSE 'superseded'
        END AS classification
    FROM prior_keys AS prior
    FULL OUTER JOIN current_active_keys AS current
        ON current.fact_category = prior.fact_category
       AND current.stable_fact_key = prior.stable_fact_key
    LEFT JOIN current_tombstones AS tombstone
        ON tombstone.fact_category = COALESCE(prior.fact_category, current.fact_category)
       AND tombstone.stable_fact_key = COALESCE(prior.stable_fact_key, current.stable_fact_key)
)
SELECT stable_fact_key, fact_kind
FROM classified
WHERE fact_category = $4
  AND classification = $5
ORDER BY stable_fact_key ASC
LIMIT $6
`
