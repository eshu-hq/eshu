// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// resolveServiceChangedSinceScopeQuery lists the lineages the caller may read
// for one service id, one row per lineage key (#6475). A service id holds one
// generation chain per ingestion scope that materialized it (scope_id), plus at
// most one unattributed legacy chain (scope_id IS NULL) whose backfill witness
// aged out and which the writer never supersedes.
//
// The caller's grant binds in the WHERE clause, on the lineage row's own
// scope_id, never on a selector the caller typed:
//
//   - scope grant: g.scope_id = ANY($5);
//   - repository grant: the lineage's ingestion scope is a repository-kind scope
//     whose source_key is granted ($4), the same arm resolveChangedSinceScopeQuery
//     uses for the sibling route.
//
// Both arms are false for a NULL scope_id (NULL = ANY(...) is NULL and the LEFT
// JOIN finds no scope row), so an unattributed lineage is invisible to every
// scoped caller by construction. $3 = false (an unscoped caller) bypasses the
// grant and sees every lineage. An explicit $2 selector narrows to one scope and
// is itself subject to the grant, so selecting an ungranted scope yields no row.
//
// Each row reports the lineage key, whether it is the unattributed chain, the
// chain's current active generation (empty when it has none), that
// generation's observed_at, and whether the chain has a pending generation.
// Attributed rows sort first by scope_id, the unattributed row last; the caller
// passes MaxServiceScopeCandidates + 2 as $6 so it can report truncation of the
// attributed list and still see the unattributed row when there are few
// attributed ones.
//
// has_pending is per lineage. A pending row exists only inside the writer's own
// commit transaction (insert pending, supersede, activate, commit together), so
// no other session observes one in practice.
//
// The lineage CTE is referenced three times, so Postgres materializes it once;
// it reads one service id's rows through service_materialization_generations_
// observed_idx (service_id leading) and joins ingestion_scopes on its primary
// key.
//
// Parameter order:
//
//	$1 service_id                 (required, exact)
//	$2 scope_id selector          (empty bypasses)
//	$3 scoped                     (false bypasses the grant predicate entirely)
//	$4 allowed repository ids     (grant; matched against source_key)
//	$5 allowed scope ids          (grant; matched against scope_id)
//	$6 row limit
const resolveServiceChangedSinceScopeQuery = `
WITH lineage AS (
    SELECT g.scope_id, g.generation_id, g.status, g.observed_at, g.activated_at
    FROM service_materialization_generations AS g
    LEFT JOIN ingestion_scopes AS scope
        ON scope.scope_id = g.scope_id
    WHERE g.service_id = $1
      AND ($2 = '' OR g.scope_id = $2)
      AND ($3::boolean = false
           OR g.scope_id = ANY($5)
           OR (scope.scope_kind = 'repository' AND scope.source_key = ANY($4)))
),
lineage_keys AS (
    SELECT DISTINCT scope_id
    FROM lineage
)
SELECT
    COALESCE(lineage_keys.scope_id, '') AS scope_id,
    lineage_keys.scope_id IS NULL AS unattributed,
    COALESCE(active.generation_id, '') AS current_active_generation_id,
    active.observed_at AS current_observed_at,
    EXISTS (
        SELECT 1
        FROM lineage AS pending
        WHERE pending.scope_id IS NOT DISTINCT FROM lineage_keys.scope_id
          AND pending.status = 'pending'
    ) AS has_pending
FROM lineage_keys
LEFT JOIN LATERAL (
    SELECT candidate.generation_id, candidate.observed_at
    FROM lineage AS candidate
    WHERE candidate.scope_id IS NOT DISTINCT FROM lineage_keys.scope_id
      AND candidate.status = 'active'
    ORDER BY candidate.activated_at DESC NULLS LAST, candidate.generation_id DESC
    LIMIT 1
) AS active ON TRUE
ORDER BY (lineage_keys.scope_id IS NULL), lineage_keys.scope_id
LIMIT $6
`

// serviceChangedSinceLineageExistsQuery reports whether a service id holds any
// lineage row at all, ignoring the grant. It runs only when a SCOPED caller's
// resolve returned no row, and its answer never reaches the caller: it sets
// ServiceSummary.OutsideGrant so the handler span can tell an operator "the
// grant excluded an existing lineage" apart from "no such service", while the
// response body stays the ordinary service-not-found for both.
//
// Parameter order:
//
//	$1 service_id (required, exact)
const serviceChangedSinceLineageExistsQuery = `
SELECT EXISTS (
    SELECT 1
    FROM service_materialization_generations
    WHERE service_id = $1
)
`

// resolveServiceChangedSincePriorGenerationQuery resolves the prior service
// generation a diff compares against. It returns the named generation only when
// it belongs to the same lineage the current generation was resolved from --
// the same service id AND the same scope_id, where the unattributed lineage is
// scope_id IS NULL ($3 NULL). Another lineage's generation id, including
// another tenant's, yields no row, which the caller answers exactly like an
// unknown id.
//
// Parameter order:
//
//	$1 service_id          (required, exact)
//	$2 since_generation_id (required, exact)
//	$3 lineage scope_id    (NULL for the unattributed lineage)
const resolveServiceChangedSincePriorGenerationQuery = `
SELECT generation_id, observed_at
FROM service_materialization_generations
WHERE service_id = $1
  AND generation_id = $2
  AND scope_id IS NOT DISTINCT FROM $3::text
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
