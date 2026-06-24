// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// resolveServiceChangedSinceScopeQuery resolves one service's current active
// materialization generation for a changed-since diff. It returns the service id
// (echoed so an unknown service yields no row), the current active generation id
// (empty when the service has no active generation), the current generation
// observed_at, and whether the service currently has a pending generation in
// flight. A service that has no generations at all yields no row, which the
// reader maps to not-found.
//
// Parameter order:
//
//	$1 service_id (required, exact)
const resolveServiceChangedSinceScopeQuery = `
SELECT
    gen.service_id,
    COALESCE(active.generation_id, '') AS current_active_generation_id,
    active.observed_at AS current_observed_at,
    EXISTS (
        SELECT 1
        FROM service_materialization_generations AS pending
        WHERE pending.service_id = $1
          AND pending.status = 'pending'
    ) AS has_pending
FROM (
    SELECT DISTINCT service_id
    FROM service_materialization_generations
    WHERE service_id = $1
) AS gen
LEFT JOIN service_materialization_generations AS active
    ON active.service_id = gen.service_id
   AND active.status = 'active'
LIMIT 1
`

// resolveServiceChangedSincePriorGenerationQuery resolves the prior service
// generation a diff compares against. It returns the named generation when it
// belongs to the service, or no row when nothing matches (an explicit not-found
// signal).
//
// Parameter order:
//
//	$1 service_id (required, exact)
//	$2 since_generation_id (required, exact)
const resolveServiceChangedSincePriorGenerationQuery = `
SELECT generation_id, observed_at
FROM service_materialization_generations
WHERE service_id = $1
  AND generation_id = $2
LIMIT 1
`

// serviceChangedSinceCountsQuery computes exact per-family, per-classification
// service_evidence_key counts between a prior service generation and the current
// active service generation. It FULL OUTER JOINs the prior generation's
// non-tombstone key set against the current generation's non-tombstone key set
// on (evidence_family, service_evidence_key), then classifies each key with the
// same logic as the repository-scope changed-since diff:
//
//   - added:      present in current (non-tombstone), absent in prior.
//   - updated:    present in both, payload hash differs.
//   - unchanged:  present in both, payload hash matches.
//   - retired:    present in prior, tombstoned in the current generation.
//   - superseded: present in prior, absent entirely from the current generation.
//
// Payload identity uses the stored payload_hash column (md5 of the canonical
// evidence payload), so an unchanged owner across generations classifies as
// unchanged and a changed owner classifies as updated.
//
// Parameter order:
//
//	$1 prior_generation_id
//	$2 current_generation_id
const serviceChangedSinceCountsQuery = `
WITH prior_keys AS (
    SELECT evidence_family, service_evidence_key, payload_hash
    FROM service_evidence_snapshots
    WHERE generation_id = $1
      AND is_tombstone = FALSE
),
current_active_keys AS (
    SELECT evidence_family, service_evidence_key, payload_hash
    FROM service_evidence_snapshots
    WHERE generation_id = $2
      AND is_tombstone = FALSE
),
current_tombstones AS (
    SELECT DISTINCT evidence_family, service_evidence_key
    FROM service_evidence_snapshots
    WHERE generation_id = $2
      AND is_tombstone = TRUE
),
classified AS (
    SELECT
        COALESCE(prior.evidence_family, current.evidence_family) AS evidence_family,
        CASE
            WHEN prior.service_evidence_key IS NULL THEN 'added'
            WHEN current.service_evidence_key IS NOT NULL
                 AND prior.payload_hash IS DISTINCT FROM current.payload_hash THEN 'updated'
            WHEN current.service_evidence_key IS NOT NULL THEN 'unchanged'
            WHEN tombstone.service_evidence_key IS NOT NULL THEN 'retired'
            ELSE 'superseded'
        END AS classification
    FROM prior_keys AS prior
    FULL OUTER JOIN current_active_keys AS current
        ON current.evidence_family = prior.evidence_family
       AND current.service_evidence_key = prior.service_evidence_key
    LEFT JOIN current_tombstones AS tombstone
        ON tombstone.evidence_family = COALESCE(prior.evidence_family, current.evidence_family)
       AND tombstone.service_evidence_key = COALESCE(prior.service_evidence_key, current.service_evidence_key)
)
SELECT evidence_family, classification, COUNT(*) AS key_count
FROM classified
GROUP BY evidence_family, classification
ORDER BY evidence_family ASC, classification ASC
`

// serviceChangedSinceSamplesQuery returns bounded, deterministic sample handles
// for one (family, classification) bucket of a service-scope changed-since diff.
// It reuses the same classification logic as serviceChangedSinceCountsQuery and
// emits the service_evidence_key ordered by key, capped by LIMIT. The caller
// fetches limit+1 rows to detect truncation and trims back to limit.
//
// Parameter order:
//
//	$1 prior_generation_id
//	$2 current_generation_id
//	$3 evidence_family
//	$4 classification
//	$5 limit (sample cap; caller passes limit + 1)
const serviceChangedSinceSamplesQuery = `
WITH prior_keys AS (
    SELECT evidence_family, service_evidence_key, payload_hash
    FROM service_evidence_snapshots
    WHERE generation_id = $1
      AND is_tombstone = FALSE
),
current_active_keys AS (
    SELECT evidence_family, service_evidence_key, payload_hash
    FROM service_evidence_snapshots
    WHERE generation_id = $2
      AND is_tombstone = FALSE
),
current_tombstones AS (
    SELECT DISTINCT evidence_family, service_evidence_key
    FROM service_evidence_snapshots
    WHERE generation_id = $2
      AND is_tombstone = TRUE
),
classified AS (
    SELECT
        COALESCE(prior.evidence_family, current.evidence_family) AS evidence_family,
        COALESCE(prior.service_evidence_key, current.service_evidence_key) AS service_evidence_key,
        CASE
            WHEN prior.service_evidence_key IS NULL THEN 'added'
            WHEN current.service_evidence_key IS NOT NULL
                 AND prior.payload_hash IS DISTINCT FROM current.payload_hash THEN 'updated'
            WHEN current.service_evidence_key IS NOT NULL THEN 'unchanged'
            WHEN tombstone.service_evidence_key IS NOT NULL THEN 'retired'
            ELSE 'superseded'
        END AS classification
    FROM prior_keys AS prior
    FULL OUTER JOIN current_active_keys AS current
        ON current.evidence_family = prior.evidence_family
       AND current.service_evidence_key = prior.service_evidence_key
    LEFT JOIN current_tombstones AS tombstone
        ON tombstone.evidence_family = COALESCE(prior.evidence_family, current.evidence_family)
       AND tombstone.service_evidence_key = COALESCE(prior.service_evidence_key, current.service_evidence_key)
)
SELECT service_evidence_key
FROM classified
WHERE evidence_family = $3
  AND classification = $4
ORDER BY service_evidence_key ASC
LIMIT $5
`
