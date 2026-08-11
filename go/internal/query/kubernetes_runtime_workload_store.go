// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
)

// PostgresKubernetesRuntimeWorkloadStore owns current-inventory and
// authorization reads for exact digest-bound Kubernetes runtime candidates.
type PostgresKubernetesRuntimeWorkloadStore struct {
	db *sql.DB
}

// NewPostgresKubernetesRuntimeWorkloadStore returns the production Kubernetes
// runtime candidate gate.
func NewPostgresKubernetesRuntimeWorkloadStore(db *sql.DB) *PostgresKubernetesRuntimeWorkloadStore {
	return &PostgresKubernetesRuntimeWorkloadStore{db: db}
}

// CurrentAuthorizedKubernetesRuntimeWorkloads returns graph candidates whose
// owner winner and RUNS_IMAGE edge generation are each current and authorized.
// Owner and edge provenance are deliberately independent: a canonical workload
// may be owned by a different current scope from the current correlation edge.
func (s *PostgresKubernetesRuntimeWorkloadStore) CurrentAuthorizedKubernetesRuntimeWorkloads(
	ctx context.Context,
	candidates []KubernetesRuntimeCandidate,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) ([]KubernetesRuntimeWorkloadMatch, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("kubernetes runtime workload database is required")
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	query, args := buildKubernetesRuntimeWorkloadQuery(
		candidates, allScopes, allowedRepositoryIDs, allowedScopeIDs,
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select current authorized kubernetes runtime workloads: %w", err)
	}
	defer func() { _ = rows.Close() }()

	matches := make([]KubernetesRuntimeWorkloadMatch, 0, len(candidates))
	for rows.Next() {
		var match KubernetesRuntimeWorkloadMatch
		if err := rows.Scan(
			&match.Digest,
			&match.WorkloadRef.UID,
			&match.WorkloadRef.ClusterID,
			&match.WorkloadRef.Namespace,
			&match.WorkloadRef.Name,
		); err != nil {
			return nil, fmt.Errorf("scan current authorized kubernetes runtime workload: %w", err)
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current authorized kubernetes runtime workloads: %w", err)
	}
	return matches, nil
}

func buildKubernetesRuntimeWorkloadQuery(
	candidates []KubernetesRuntimeCandidate,
	allScopes bool,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (string, []any) {
	workloadUIDs := make([]string, 0, len(candidates))
	digests := make([]string, 0, len(candidates))
	edgeScopeIDs := make([]string, 0, len(candidates))
	edgeGenerationIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		workloadUIDs = append(workloadUIDs, candidate.WorkloadUID)
		digests = append(digests, candidate.Digest)
		edgeScopeIDs = append(edgeScopeIDs, candidate.EdgeScopeID)
		edgeGenerationIDs = append(edgeGenerationIDs, candidate.EdgeGenerationID)
	}

	args := make([]any, 0, 7)
	bind := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	uidSet := bind(workloadUIDs)
	digestSet := bind(digests)
	edgeScopeSet := bind(edgeScopeIDs)
	edgeGenerationSet := bind(edgeGenerationIDs)
	candidateLimit := supplyChainKubernetesRuntimeProbeMaxResults
	if allScopes {
		candidateLimit = supplyChainKubernetesRuntimeProbeMaxAllScopesCandidates
	}
	limit := bind(candidateLimit)

	ownerAuthorization := ""
	edgeAuthorization := ""
	if !allScopes {
		repositories := bind(allowedRepositoryIDs)
		scopes := bind(allowedScopeIDs)
		ownerAuthorization = "\n  AND ((owner_scope.scope_kind = 'repository' AND owner_scope.source_key = ANY(" + repositories + "::text[]))" +
			" OR owner_scope.scope_id = ANY(" + scopes + "::text[]))"
		edgeAuthorization = "\n  AND ((edge_scope.scope_kind = 'repository' AND edge_scope.source_key = ANY(" + repositories + "::text[]))" +
			" OR edge_scope.scope_id = ANY(" + scopes + "::text[]))"
	}

	return `
WITH candidates AS MATERIALIZED (
  SELECT DISTINCT input.workload_uid,
                  input.digest,
                  input.edge_scope_id,
                  input.edge_generation_id
  FROM UNNEST(
    ` + uidSet + `::text[],
    ` + digestSet + `::text[],
    ` + edgeScopeSet + `::text[],
    ` + edgeGenerationSet + `::text[]
  ) AS input(workload_uid, digest, edge_scope_id, edge_generation_id)
  WHERE NULLIF(BTRIM(input.workload_uid), '') IS NOT NULL
    AND NULLIF(BTRIM(input.digest), '') IS NOT NULL
    AND NULLIF(BTRIM(input.edge_scope_id), '') IS NOT NULL
    AND NULLIF(BTRIM(input.edge_generation_id), '') IS NOT NULL
  ORDER BY input.digest, input.workload_uid, input.edge_scope_id, input.edge_generation_id
  LIMIT ` + limit + `
)
SELECT candidate.digest,
       owner.uid,
       COALESCE(owner.winning_row->>'cluster_id', ''),
       COALESCE(owner.winning_row->>'namespace', ''),
       COALESCE(owner.winning_row->>'name', '')
FROM candidates AS candidate
JOIN graph_node_owner AS owner ON owner.uid = candidate.workload_uid
JOIN fact_records AS owner_fact
  ON owner_fact.fact_id = owner.winning_row->>'source_fact_id'
JOIN ingestion_scopes AS owner_scope ON owner_scope.scope_id = owner_fact.scope_id
JOIN scope_generations AS owner_generation ON owner_generation.generation_id = owner_fact.generation_id
JOIN ingestion_scopes AS edge_scope ON edge_scope.scope_id = candidate.edge_scope_id
JOIN scope_generations AS edge_generation ON edge_generation.generation_id = candidate.edge_generation_id
WHERE owner_scope.active_generation_id = owner_fact.generation_id
  AND owner_generation.scope_id = owner_scope.scope_id
  AND owner_generation.status = 'active'
  AND owner_fact.is_tombstone = FALSE` + ownerAuthorization + `
  AND edge_scope.active_generation_id = candidate.edge_generation_id
  AND edge_generation.scope_id = edge_scope.scope_id
  AND edge_generation.status = 'active'` + edgeAuthorization + `
ORDER BY candidate.digest, owner.uid, edge_scope.scope_id, edge_generation.generation_id`, args
}
