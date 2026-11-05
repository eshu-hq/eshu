// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstate

// Delta-overlay query shapes and thin exports for the Terraform drift
// readers (issue #10006).
//
// The active-generation readers in this package (the canonical backend
// rows, the backend facts, the config resources, and the module calls)
// miss every row that only exists on a delta-active scope's base full
// generation. The overlay-capable entry points in the parent postgres
// package (drift_overlay_tfstate.go) run these texts through the shared
// delta-overlay helper (deriveDeltaOverlayBaseQuery) and merge the base
// rows back in, so drift findings match what a full generation would
// produce. This package cannot call that helper itself: the parent
// package imports this one (compat_tfstate.go, issue #8361), so an import
// back would be a cycle. This file therefore holds static query text plus
// pure decode helpers only — no overlay orchestration.
//
// SHAPE CONTRACT (enforced fail-closed by deriveDeltaOverlayBaseQuery):
// every `fact_records AS fact` branch carries exactly one
// `AND scope.active_generation_id = fact.generation_id` and one
// `AND generation.status = 'active'`; the delta-active scope ids bind at
// the documented scope placeholder, which the active text leaves unused;
// the text never uses the active_delta alias. Branches that follow another
// branch's resolved generations (the canonical outer file branch, keyed
// off the backend_generations CTE) deliberately use a different alias so
// the helper derives the resolving branch only — see each constant.
//
// The pre-existing active-only constants and methods are byte-identical to
// before: unit fakes and the AWS runtime-drift consumer keep today's
// single-generation behavior. Keep each overlay text in lockstep with the
// twin named in its comment.

// ActiveTerraformBackendCanonicalQuery is the overlay-capable twin of
// listTerraformBackendCanonicalRowsQuery (backend_canonical.go). Changes
// versus the twin, all load-bearing for the overlay merge in the parent
// package:
//
//   - the outer file branch reads `fact_records AS backend_fact`, not
//     `AS fact`, so the shared helper counts exactly one fact branch (the
//     backend_generations CTE) and derives the base rule there. The outer
//     branch joins the CTE on (scope_id, generation_id) equality, so it
//     reads exactly the generations the CTE resolved — active triples
//     from the active text, base triples from the derived text — with no
//     generation predicate of its own. The repo_local_paths CTE is keyed
//     off the same triples, so base generations resolve their own
//     repository fact for repo_local_path.
//   - the outer SELECT carries the row-identity columns the merge keys
//     on (fact_id, stable_fact_key, is_tombstone, relative_path) after
//     the twin's six decode columns; the parent package scans all ten.
//   - the outer branch filters `backend_fact.is_tombstone = FALSE` like
//     the manifest reader: a delta tombstone removes its base row by
//     stable key through the overlay inputs, never by decoding the
//     tombstone row itself.
//
// The delta-active scope ids bind at $1, unused by the active text.
const ActiveTerraformBackendCanonicalQuery = `
WITH backend_generations AS (
    SELECT DISTINCT
        fact.payload->>'repo_id' AS repo_id,
        fact.scope_id,
        fact.generation_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'file'
      AND fact.source_system = 'git'
      AND generation.status = 'active'
      AND CASE
            WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
            THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_backends')
            ELSE 0
          END > 0
),
repo_local_paths AS MATERIALIZED (
    SELECT DISTINCT ON (repository.scope_id, repository.generation_id, repository.payload->>'repo_id')
        repository.scope_id,
        repository.generation_id,
        repository.payload->>'repo_id' AS repo_id,
        COALESCE(repository.payload->>'local_path', repository.source_uri, '') AS repo_local_path
    FROM fact_records AS repository
    WHERE repository.fact_kind = 'repository'
      AND repository.source_system = 'git'
      AND repository.scope_id IN (SELECT scope_id FROM backend_generations)
    ORDER BY
        repository.scope_id,
        repository.generation_id,
        repository.payload->>'repo_id',
        repository.observed_at DESC,
        repository.fact_id DESC
)
SELECT
    backend.repo_id                                           AS repo_id,
    backend_fact.scope_id                                     AS scope_id,
    backend_fact.generation_id                                AS generation_id,
    backend_fact.observed_at                                  AS observed_at,
    COALESCE(repo_local_paths.repo_local_path, '')             AS repo_local_path,
    jsonb_build_object(
        'terraform_backends', COALESCE(backend_fact.payload->'parsed_file_data'->'terraform_backends', '[]'::jsonb),
        'terraform_variables', COALESCE(backend_fact.payload->'parsed_file_data'->'terraform_variables', '[]'::jsonb),
        'terraform_locals', COALESCE(backend_fact.payload->'parsed_file_data'->'terraform_locals', '[]'::jsonb)
    )                                                         AS terraform_backend_context,
    backend_fact.fact_id                                      AS fact_id,
    backend_fact.stable_fact_key                              AS stable_fact_key,
    backend_fact.is_tombstone                                 AS is_tombstone,
    backend_fact.payload->>'relative_path'                   AS relative_path
FROM backend_generations AS backend
JOIN fact_records AS backend_fact
  ON backend_fact.scope_id = backend.scope_id
 AND backend_fact.generation_id = backend.generation_id
 AND backend_fact.payload->>'repo_id' = backend.repo_id
LEFT JOIN repo_local_paths
  ON repo_local_paths.scope_id = backend.scope_id
 AND repo_local_paths.generation_id = backend.generation_id
 AND repo_local_paths.repo_id = backend.repo_id
WHERE backend_fact.fact_kind = 'file'
  AND backend_fact.source_system = 'git'
  AND backend_fact.is_tombstone = FALSE
  AND (
      CASE
        WHEN jsonb_typeof(backend_fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
        THEN jsonb_array_length(backend_fact.payload->'parsed_file_data'->'terraform_backends')
        ELSE 0
      END
    + CASE
        WHEN jsonb_typeof(backend_fact.payload->'parsed_file_data'->'terraform_variables') = 'array'
        THEN jsonb_array_length(backend_fact.payload->'parsed_file_data'->'terraform_variables')
        ELSE 0
      END
    + CASE
        WHEN jsonb_typeof(backend_fact.payload->'parsed_file_data'->'terraform_locals') = 'array'
        THEN jsonb_array_length(backend_fact.payload->'parsed_file_data'->'terraform_locals')
        ELSE 0
      END
  ) > 0
ORDER BY backend.repo_id ASC, backend_fact.scope_id ASC, backend_fact.generation_id ASC, backend_fact.observed_at ASC, backend_fact.fact_id ASC
`

// ActiveTerraformBackendFactsQuery is the overlay-capable twin of
// listTerraformBackendFactsQuery (backend_queries.go): the same join
// shape, plus the row-identity columns the overlay merge keys on
// (fact_id, scope_id, generation_id, stable_fact_key, is_tombstone,
// relative_path, observed_at) after the twin's two decode columns, plus
// the `fact.is_tombstone = FALSE` predicate. The active_repositories CTE
// keeps the `AS repository` alias (uncounted by the helper) and its
// active-generation semantics: a delta carries its own repository fact,
// so resolution works at the delta, while a delta without one resolves
// nothing — the same as today.
//
// Requested repo ids bind at $1; the delta-active scope ids bind at $2,
// unused by the active text.
const ActiveTerraformBackendFactsQuery = `
WITH requested_repos AS (
    SELECT DISTINCT btrim(value) AS requested_repo_id
    FROM unnest($1::text[]) AS value
    WHERE btrim(value) <> ''
),
active_repositories AS (
    SELECT DISTINCT ON (requested.requested_repo_id)
        requested.requested_repo_id,
        repository.payload->>'repo_id' AS canonical_repo_id
    FROM requested_repos AS requested
    JOIN fact_records AS repository
      ON repository.fact_kind = 'repository'
     AND repository.source_system = 'git'
    JOIN ingestion_scopes AS repository_scope
      ON repository_scope.scope_id = repository.scope_id
     AND repository_scope.active_generation_id = repository.generation_id
    JOIN scope_generations AS repository_generation
      ON repository_generation.scope_id = repository.scope_id
     AND repository_generation.generation_id = repository.generation_id
    WHERE repository_generation.status = 'active'
      AND (
          repository.payload->>'repo_id' = requested.requested_repo_id
       OR repository.payload->>'graph_id' = requested.requested_repo_id
       OR repository.payload->>'name' = requested.requested_repo_id
       OR repository.payload->>'repo_slug' = requested.requested_repo_id
      )
    ORDER BY requested.requested_repo_id, repository.observed_at DESC, repository.fact_id DESC
)
SELECT
    active_repositories.requested_repo_id AS repo_id,
    jsonb_build_object(
        'terraform_backends', COALESCE(fact.payload->'parsed_file_data'->'terraform_backends', '[]'::jsonb),
        'terraform_variables', COALESCE(fact.payload->'parsed_file_data'->'terraform_variables', '[]'::jsonb),
        'terraform_locals', COALESCE(fact.payload->'parsed_file_data'->'terraform_locals', '[]'::jsonb)
    ) AS terraform_backend_context,
    fact.fact_id AS fact_id,
    fact.scope_id AS scope_id,
    fact.generation_id AS generation_id,
    fact.stable_fact_key AS stable_fact_key,
    fact.is_tombstone AS is_tombstone,
    fact.payload->>'relative_path' AS relative_path,
    fact.observed_at AS observed_at
FROM active_repositories
JOIN fact_records AS fact
  ON fact.payload->>'repo_id' = active_repositories.canonical_repo_id
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'file'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      CASE
        WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
        THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_backends')
        ELSE 0
      END
    + CASE
        WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_variables') = 'array'
        THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_variables')
        ELSE 0
      END
    + CASE
        WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_locals') = 'array'
        THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_locals')
        ELSE 0
      END
  ) > 0
ORDER BY repo_id ASC, fact.observed_at ASC, fact.fact_id ASC
`

// ActiveConfigResourcesForOverlayQuery reads the terraform_resources
// arrays for one scope's ACTIVE generation — the overlay-capable
// replacement for listConfigResourcesForCommitQuery
// (drift_evidence_sql.go), which pins (scope_id, generation_id)
// directly and therefore cannot see a delta-active scope's base rows.
// The scope id binds at $1; the delta-active scope ids bind at $2,
// unused by the active text. The parent package pages the whole result
// (one repo's files, today's unbounded behavior) and merges base rows
// through the shared overlay helper before decoding.
const ActiveConfigResourcesForOverlayQuery = `
SELECT
    fact.fact_id AS fact_id,
    fact.scope_id AS scope_id,
    fact.generation_id AS generation_id,
    fact.stable_fact_key AS stable_fact_key,
    fact.is_tombstone AS is_tombstone,
    fact.payload->>'relative_path' AS relative_path,
    fact.payload->'parsed_file_data'->'terraform_resources' AS terraform_resources,
    fact.observed_at AS observed_at
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.scope_id = $1
  AND fact.fact_kind = 'file'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND CASE
        WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_resources') = 'array'
        THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_resources')
        ELSE 0
      END > 0
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

// ActiveModuleCallsForOverlayQuery reads the terraform_modules arrays
// for one scope's ACTIVE generation — the overlay-capable replacement
// for listModuleCallsForCommitQuery (drift_evidence_sql.go). The drift
// loader builds the current-generation module-prefix map from these
// rows; without the overlay a delta that touches no module file would
// lose every base prefix and misattribute module-nested addresses.
// Same binding as ActiveConfigResourcesForOverlayQuery ($1 scope,
// $2 delta-active scopes for the derived text).
const ActiveModuleCallsForOverlayQuery = `
SELECT
    fact.fact_id AS fact_id,
    fact.scope_id AS scope_id,
    fact.generation_id AS generation_id,
    fact.stable_fact_key AS stable_fact_key,
    fact.is_tombstone AS is_tombstone,
    fact.payload->>'relative_path' AS relative_path,
    fact.payload->'parsed_file_data'->'terraform_modules' AS terraform_modules,
    fact.observed_at AS observed_at
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.scope_id = $1
  AND fact.fact_kind = 'file'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND CASE
        WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_modules') = 'array'
        THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_modules')
        ELSE 0
      END > 0
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`
