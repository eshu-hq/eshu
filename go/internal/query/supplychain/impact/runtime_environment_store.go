// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// The candidate CTE is deliberately materialized and the aggregate remains
// correlated inside LATERAL. PostgreSQL otherwise flattens the 200 candidates
// behind the environment index, multiplying a hot fact set by every candidate.
// The measured shape performs one artifact-digest index search per candidate.
const selectSupplyChainImpactRuntimeEnvironmentEvidenceQueryTemplate = `
WITH candidate_pairs AS MATERIALIZED (
  SELECT DISTINCT BTRIM(candidate.digest) AS digest,
                  BTRIM(candidate.environment) AS environment
  FROM UNNEST($1::text[], $2::text[]) AS candidate(digest, environment)
  WHERE BTRIM(candidate.digest) <> ''
    AND BTRIM(candidate.environment) <> ''
)
SELECT candidate.digest,
       candidate.environment,
       confirmation.environment_evidence
FROM candidate_pairs AS candidate
CROSS JOIN LATERAL (
  SELECT CASE
           WHEN BOOL_OR(BTRIM(COALESCE(fact.payload->>'environment_evidence', '')) = 'deploy_event')
             THEN 'deploy_event'
           ELSE 'declared'
         END AS environment_evidence
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
%s
  WHERE fact.fact_kind = 'reducer_ci_cd_run_correlation'
    AND fact.is_tombstone = FALSE
    AND fact.payload->>'artifact_digest' = candidate.digest
    AND BTRIM(COALESCE(fact.payload->>'environment', '')) = candidate.environment
    AND generation.status = 'active'
    AND BTRIM(COALESCE(fact.payload->>'outcome', '')) IN ('', 'exact', 'derived')
    AND LOWER(BTRIM(COALESCE(fact.payload->>'provenance_only', ''))) <> 'true'
    AND (
          (
            COALESCE(cardinality($3::text[]), 0) = 0
            AND COALESCE(cardinality($4::text[]), 0) = 0
          )
          OR runtime_repository.repository_id = ANY($3::text[])
          OR fact.scope_id = ANY($4::text[])
        )
  HAVING COUNT(*) > 0
) AS confirmation
ORDER BY candidate.digest, candidate.environment`

var selectSupplyChainImpactRuntimeEnvironmentEvidenceQuery = fmt.Sprintf(
	selectSupplyChainImpactRuntimeEnvironmentEvidenceQueryTemplate,
	supplyChainRuntimeRepositoryDecoderJoin(
		"fact.payload",
		"fact.scope_id",
		"runtime_repository",
	),
)

// ListSupplyChainImpactRuntimeEnvironmentEvidence confirms finding-bound
// digest/environment candidates against current, active, authorized, accepted
// CI/CD correlation facts. Candidate names select the read only; baked evidence
// values are never trusted. The grouped result contains at most one row per
// input pair and deploy_event wins over declared across matching facts.
func (s PostgresSupplyChainImpactFindingStore) ListSupplyChainImpactRuntimeEnvironmentEvidence(
	ctx context.Context,
	candidates []SupplyChainRuntimeEnvironmentCandidate,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (map[string]map[string]string, error) {
	out := make(map[string]map[string]string)
	if len(candidates) == 0 {
		return out, nil
	}
	if len(candidates) > maxSupplyChainRuntimeEnvironmentCandidates {
		return nil, fmt.Errorf(
			"supply chain runtime environment candidates exceed limit %d",
			maxSupplyChainRuntimeEnvironmentCandidates,
		)
	}
	if s.DB == nil {
		return nil, fmt.Errorf("supply chain runtime environment evidence database is required")
	}
	digests := make([]string, 0, len(candidates))
	environments := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		digest := strings.TrimSpace(candidate.SubjectDigest)
		environment := strings.TrimSpace(candidate.Environment)
		if digest == "" || environment == "" {
			continue
		}
		digests = append(digests, digest)
		environments = append(environments, environment)
	}
	if len(digests) == 0 {
		return out, nil
	}
	rows, err := s.DB.QueryContext(
		ctx,
		selectSupplyChainImpactRuntimeEnvironmentEvidenceQuery,
		pgarray.Array(digests),
		pgarray.Array(environments),
		pgarray.Array(allowedRepositoryIDs),
		pgarray.Array(allowedScopeIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list supply chain impact runtime environment evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var digest, environment, evidence string
		if err := rows.Scan(&digest, &environment, &evidence); err != nil {
			return nil, fmt.Errorf("scan supply chain impact runtime environment evidence: %w", err)
		}
		digest = strings.TrimSpace(digest)
		environment = strings.TrimSpace(environment)
		if digest == "" || environment == "" {
			continue
		}
		out[digest] = RecordSupplyChainRuntimeEnvironmentEvidence(
			out[digest],
			environment,
			evidence,
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read supply chain impact runtime environment evidence: %w", err)
	}
	return out, nil
}
