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
//	$3 scoped       (false bypasses the grant predicate entirely)
//	$4 allowed repository ids (grant; matched against source_key)
//	$5 allowed scope ids      (grant; matched against scope_id)
const resolveChangedSinceScopeQuery = `
	SELECT
	    scope.scope_id,
	    scope.scope_kind,
	    CASE
	        WHEN scope.scope_kind = 'repository' THEN scope.source_key
	        ELSE ''
	    END AS repository,
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
  -- Grant binding (#5167), on the scope row rather than on the selector the
  -- caller typed. A repository grant authorizes a repository-kind scope via
  -- source_key, and the raw scope_id normally differs from that key, so a
  -- handler-side string comparison would deny a repository-granted token its
  -- own scope. Resolving to no row makes an ungranted scope indistinguishable
  -- from a missing one, which is what the caller already sees for a bad
  -- selector.
  AND ($3::boolean = false
       OR (scope.scope_kind = 'repository' AND scope.source_key = ANY($4))
       OR scope.scope_id = ANY($5))
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

// changedSinceClassificationCTEs classifies one scope across two generations.
// A single payload per key uses one SHA-256 digest. Equal minimum digests from
// duplicate-key groups need a sorted multiset comparison: a changed non-minimum
// payload or duplicate multiplicity must not disappear into "unchanged".
// Only those ambiguous keys pay for the second scan and sorted digest arrays.
// Category and tombstone precedence match the original changed-since contract.
//
// Parameter order: $1 scope_id, $2 prior generation, $3 current generation.
const changedSinceClassificationCTEs = `
WITH prior_keys AS (
    SELECT
        CASE
            WHEN fact_kind = 'file' THEN 'files'
            WHEN fact_kind = 'content_entity' THEN 'content_entities'
            ELSE 'facts'
        END AS fact_category,
        stable_fact_key,
        MIN(fact_kind) AS fact_kind,
        COUNT(*) AS row_count,
        MIN(sha256(convert_to(payload::text, 'UTF8'))) AS single_payload_hash
    FROM fact_records
    WHERE scope_id = $1 AND generation_id = $2 AND is_tombstone = FALSE
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
        COUNT(*) AS row_count,
        MIN(sha256(convert_to(payload::text, 'UTF8'))) AS single_payload_hash
    FROM fact_records
    WHERE scope_id = $1 AND generation_id = $3 AND is_tombstone = FALSE
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
    WHERE scope_id = $1 AND generation_id = $3 AND is_tombstone = TRUE
    GROUP BY fact_category, stable_fact_key
),
initial_classified AS MATERIALIZED (
    SELECT
        COALESCE(prior.fact_category, current.fact_category) AS fact_category,
        COALESCE(prior.stable_fact_key, current.stable_fact_key) AS stable_fact_key,
        COALESCE(current.fact_kind, tombstone.fact_kind, prior.fact_kind) AS fact_kind,
        CASE
            WHEN prior.stable_fact_key IS NULL THEN 'added'
            WHEN current.stable_fact_key IS NULL AND tombstone.stable_fact_key IS NOT NULL THEN 'retired'
            WHEN current.stable_fact_key IS NULL THEN 'superseded'
            WHEN prior.row_count IS DISTINCT FROM current.row_count THEN 'updated'
            WHEN prior.single_payload_hash IS DISTINCT FROM current.single_payload_hash THEN 'updated'
            WHEN prior.row_count > 1 THEN 'needs_multiset'
            ELSE 'unchanged'
        END AS classification
    FROM prior_keys AS prior
    FULL OUTER JOIN current_active_keys AS current
        ON current.fact_category = prior.fact_category
       AND current.stable_fact_key = prior.stable_fact_key
    LEFT JOIN current_tombstones AS tombstone
        ON tombstone.fact_category = COALESCE(prior.fact_category, current.fact_category)
       AND tombstone.stable_fact_key = COALESCE(prior.stable_fact_key, current.stable_fact_key)
),
suspect_keys AS MATERIALIZED (
    SELECT fact_category, stable_fact_key
    FROM initial_classified
    WHERE classification = 'needs_multiset'
),
prior_duplicate_rows AS MATERIALIZED (
    SELECT suspect.fact_category, fact.stable_fact_key,
        sha256(convert_to(fact.payload::text, 'UTF8')) AS payload_hash
    FROM fact_records AS fact
    JOIN suspect_keys AS suspect
        ON suspect.stable_fact_key = fact.stable_fact_key
       AND suspect.fact_category = CASE
           WHEN fact.fact_kind = 'file' THEN 'files'
           WHEN fact.fact_kind = 'content_entity' THEN 'content_entities'
           ELSE 'facts'
       END
    WHERE fact.scope_id = $1 AND fact.generation_id = $2 AND fact.is_tombstone = FALSE
),
current_duplicate_rows AS MATERIALIZED (
    SELECT suspect.fact_category, fact.stable_fact_key,
        sha256(convert_to(fact.payload::text, 'UTF8')) AS payload_hash
    FROM fact_records AS fact
    JOIN suspect_keys AS suspect
        ON suspect.stable_fact_key = fact.stable_fact_key
       AND suspect.fact_category = CASE
           WHEN fact.fact_kind = 'file' THEN 'files'
           WHEN fact.fact_kind = 'content_entity' THEN 'content_entities'
           ELSE 'facts'
       END
    WHERE fact.scope_id = $1 AND fact.generation_id = $3 AND fact.is_tombstone = FALSE
),
prior_duplicate_hashes AS (
    SELECT fact_category, stable_fact_key,
        ARRAY_AGG(payload_hash ORDER BY payload_hash) AS payload_hashes
    FROM prior_duplicate_rows
    GROUP BY fact_category, stable_fact_key
),
current_duplicate_hashes AS (
    SELECT fact_category, stable_fact_key,
        ARRAY_AGG(payload_hash ORDER BY payload_hash) AS payload_hashes
    FROM current_duplicate_rows
    GROUP BY fact_category, stable_fact_key
),
classified AS (
    SELECT fact_category, stable_fact_key, fact_kind, classification
    FROM initial_classified
    WHERE classification <> 'needs_multiset'
    UNION ALL
    SELECT suspect.fact_category, suspect.stable_fact_key, suspect.fact_kind,
        CASE
            WHEN prior_hashes.payload_hashes IS NULL
              OR current_hashes.payload_hashes IS NULL
              OR prior_hashes.payload_hashes IS DISTINCT FROM current_hashes.payload_hashes
            THEN 'updated'
            ELSE 'unchanged'
        END AS classification
    FROM initial_classified AS suspect
    LEFT JOIN prior_duplicate_hashes AS prior_hashes
        ON prior_hashes.fact_category = suspect.fact_category
       AND prior_hashes.stable_fact_key = suspect.stable_fact_key
    LEFT JOIN current_duplicate_hashes AS current_hashes
        ON current_hashes.fact_category = suspect.fact_category
       AND current_hashes.stable_fact_key = suspect.stable_fact_key
    WHERE suspect.classification = 'needs_multiset'
)
`

// changedSinceDeltaQuery evaluates the classification diff once and returns,
// for every non-empty (category, classification) bucket, its exact key count and
// its first $4 keys ordered by stable_fact_key. The caller passes
// sample_limit+1 as $4 so it can tell a truncated bucket from a full one.
//
// The whole request is one statement because every statement re-scans and
// re-hashes both generations: the former counts statement plus one samples
// statement per non-empty bucket cost 1+N diffs (#7127). classified is
// referenced twice (bucket counts, lateral samples), so PostgreSQL materializes
// it once. Buckets without a sample key cannot occur because a bucket exists
// only when it has at least one key and $4 is at least 1.
//
// Parameter order: $1 scope_id, $2 prior generation, $3 current generation,
// $4 sample fetch limit.
const changedSinceDeltaQuery = changedSinceClassificationCTEs + `
, buckets AS (
    SELECT fact_category, classification, COUNT(*) AS key_count
    FROM classified
    GROUP BY fact_category, classification
)
SELECT bucket.fact_category, bucket.classification, bucket.key_count,
       sample.stable_fact_key, sample.fact_kind
FROM buckets AS bucket
LEFT JOIN LATERAL (
    SELECT candidate.stable_fact_key, candidate.fact_kind
    FROM classified AS candidate
    WHERE candidate.fact_category = bucket.fact_category
      AND candidate.classification = bucket.classification
    ORDER BY candidate.stable_fact_key ASC
    LIMIT $4
) AS sample ON TRUE
ORDER BY bucket.fact_category ASC, bucket.classification ASC, sample.stable_fact_key ASC
`
