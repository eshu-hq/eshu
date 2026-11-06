-- Fence EXPLAIN probe (#10055): exact shipped statements, representative seed.
-- (a) holder-reopen probe = fenceFindHoldersQuery verbatim (fence.go).
PREPARE holder_probe(text, text) AS
SELECT DISTINCT finding.scope_id
        FROM fact_records AS finding
        JOIN ingestion_scopes AS scope
          ON scope.scope_id = finding.scope_id
         AND scope.active_generation_id = finding.generation_id
        WHERE finding.fact_kind = 'reducer_supply_chain_impact_finding'
          AND finding.is_tombstone = FALSE
          AND finding.payload->>'repository_id' = $1
          AND finding.scope_id <> $2
        ORDER BY finding.scope_id;

EXPLAIN (ANALYZE, BUFFERS)
EXECUTE holder_probe('repository:r_probe01', 'go-reachability://repository:r_probe01');

-- (b) skewed-row probe = the evidence_left_at CASE subquery verbatim (store.go),
-- run for one skewed candidate the way the page's CASE guard invokes it.
PREPARE skew_probe(text, text) AS
SELECT max(history.superseded_at)
                FROM scope_generations AS history
                WHERE history.scope_id = $1
                  AND history.status = 'superseded'
                  AND history.source_commit_sha = $2;

EXPLAIN (ANALYZE, BUFFERS)
EXECUTE skew_probe('repository:r_probe01', 'E-commit');

-- (c) full page query verbatim (store.go pageQuery), bucket 0 of 256-wide pages
-- over the 20 seeded repository scopes (all 20 skewed: C <> E everywhere).
PREPARE page_probe(bigint, bigint) AS
WITH candidates AS (
    SELECT scope.scope_id,
           scope.source_key AS repository_id,
           scope.active_generation_id,
           COALESCE(scope.payload->>'remote_url', '') AS remote_url,
           ROW_NUMBER() OVER (ORDER BY scope.scope_id) - 1 AS candidate_rank,
           COUNT(*) OVER () AS total_candidates
    FROM ingestion_scopes AS scope
    WHERE scope.source_system = 'git'
      AND scope.scope_kind = 'repository'
      AND scope.active_generation_id IS NOT NULL
)
SELECT candidate.repository_id,
       candidate.remote_url,
       COALESCE(generation.source_commit_sha, '') AS source_commit_sha,
       candidate.total_candidates,
       EXISTS (
           SELECT 1
           FROM content_files AS file
           WHERE file.repo_id = candidate.repository_id
             AND (file.relative_path = 'go.mod' OR file.relative_path LIKE '%/go.mod')
       ) AS has_go_module,
       EXISTS (
           SELECT 1
           FROM ingestion_scopes AS reach
           JOIN fact_records AS fact
             ON fact.scope_id = reach.scope_id
            AND fact.generation_id = reach.active_generation_id
           WHERE reach.scope_id = 'go-reachability://' || candidate.repository_id
             AND reach.active_generation_id IS NOT NULL
             AND fact.fact_kind IN ('vulnerability.go_module_evidence', 'vulnerability.go_call_reachability')
             AND fact.is_tombstone = FALSE
       ) AS has_go_evidence,
       COALESCE(reach_generation.generation_id, '') AS evidence_generation_id,
       COALESCE(reach_generation.source_commit_sha, '') AS evidence_commit_sha,
       CASE WHEN COALESCE(reach_generation.source_commit_sha, '') <> ''
             AND COALESCE(generation.source_commit_sha, '') <> ''
             AND reach_generation.source_commit_sha <> generation.source_commit_sha
            THEN (
                SELECT max(history.superseded_at)
                FROM scope_generations AS history
                WHERE history.scope_id = candidate.scope_id
                  AND history.status = 'superseded'
                  AND history.source_commit_sha = reach_generation.source_commit_sha
            )
       END AS evidence_left_at
FROM candidates AS candidate
JOIN scope_generations AS generation
  ON generation.generation_id = candidate.active_generation_id
 AND generation.status = 'active'
LEFT JOIN ingestion_scopes AS reach_scope
  ON reach_scope.scope_id = 'go-reachability://' || candidate.repository_id
LEFT JOIN scope_generations AS reach_generation
  ON reach_generation.generation_id = reach_scope.active_generation_id
 AND reach_generation.status = 'active'
WHERE candidate.candidate_rank / $2::bigint =
      $1::bigint % CEIL(candidate.total_candidates::numeric / $2::bigint)::bigint
ORDER BY candidate.scope_id;

EXPLAIN (ANALYZE, BUFFERS)
EXECUTE page_probe(0, 256);
