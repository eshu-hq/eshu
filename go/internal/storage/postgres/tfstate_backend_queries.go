package postgres

const listTerraformBackendFactsQuery = `
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
    fact.payload->'parsed_file_data'->'terraform_backends' AS terraform_backends
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
  AND generation.status = 'active'
  AND jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
ORDER BY repo_id ASC, fact.observed_at ASC, fact.fact_id ASC
`

const listTerragruntRemoteStateFactsQuery = `
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
    COALESCE(repository.payload->>'local_path', '') AS repo_local_path,
    fact.payload->'parsed_file_data'->'terragrunt_remote_states' AS terragrunt_remote_states
FROM active_repositories
JOIN fact_records AS fact
  ON fact.payload->>'repo_id' = active_repositories.canonical_repo_id
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
LEFT JOIN fact_records AS repository
  ON repository.scope_id = fact.scope_id
 AND repository.generation_id = fact.generation_id
 AND repository.fact_kind = 'repository'
 AND repository.source_system = 'git'
WHERE fact.fact_kind = 'file'
  AND fact.source_system = 'git'
  AND generation.status = 'active'
  AND jsonb_typeof(fact.payload->'parsed_file_data'->'terragrunt_remote_states') = 'array'
ORDER BY repo_id ASC, fact.observed_at ASC, fact.fact_id ASC
`

const listTerraformStateLocalCandidateFactsQuery = `
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
    candidate.payload->>'relative_path' AS relative_path,
    COALESCE(repository.payload->>'local_path', repository.source_uri, '') AS source_uri
FROM active_repositories
JOIN fact_records AS candidate
  ON candidate.payload->>'repo_id' = active_repositories.canonical_repo_id
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidate.scope_id
 AND scope.active_generation_id = candidate.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = candidate.scope_id
 AND generation.generation_id = candidate.generation_id
JOIN fact_records AS repository
  ON repository.scope_id = candidate.scope_id
 AND repository.generation_id = candidate.generation_id
 AND repository.fact_kind = 'repository'
 AND repository.source_system = 'git'
WHERE candidate.fact_kind = 'terraform_state_candidate'
  AND candidate.source_system = 'git'
  AND generation.status = 'active'
ORDER BY repo_id ASC, relative_path ASC, candidate.observed_at ASC, candidate.fact_id ASC
`

const terraformStateGitReadinessQuery = `
SELECT EXISTS (
    SELECT 1
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.generation_id = fact.generation_id
     AND generation.scope_id = fact.scope_id
    WHERE fact.fact_kind = 'repository'
      AND fact.source_system = 'git'
      AND generation.status = 'active'
      AND (
          fact.payload->>'repo_id' = $1
       OR fact.payload->>'graph_id' = $1
       OR fact.payload->>'name' = $1
       OR fact.payload->>'repo_slug' = $1
      )
    LIMIT 1
)
`

const listTerraformStatePriorSnapshotMetadataQuery = `
SELECT
    fact.payload->>'locator_hash' AS locator_hash,
    fact.payload->>'etag' AS etag,
    generation.generation_id
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'terraform_state_snapshot'
  AND generation.status = 'active'
  AND fact.payload->>'locator_hash' = ANY($1::text[])
  AND COALESCE(fact.payload->>'etag', '') <> ''
ORDER BY fact.observed_at DESC, fact.fact_id DESC
`
