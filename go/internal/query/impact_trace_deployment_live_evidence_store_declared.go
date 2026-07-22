// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	"github.com/lib/pq"
)

// hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery is the
// declared-object anchor sibling of hasLiveKubernetesPodTemplateIdentityQuery
// (#5639): the same ACTIVE-generation join and is_tombstone predicate, but
// anchored on the object's own declared group_version_resource, namespace,
// and name instead of the ArgoCD tracking-id annotation. $5/$6 apply the
// same optional image-ref intersection the ArgoCD variant uses.
const hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery = `
SELECT 1
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'group_version_resource' = $2
  AND fact.payload->>'namespace' = $3
  AND fact.payload->>'name' = $4
  AND ($5 OR fact.payload->'image_refs' ?| $6)
LIMIT 1
`

// hasLiveKubernetesPodTemplateDeclaredObjectIdentityScopedQuery is
// hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery with the additional
// #5167 access-scoping predicate, mirroring
// hasLiveKubernetesPodTemplateIdentityScopedQuery. Bound only when
// filter.AllScopes is false.
const hasLiveKubernetesPodTemplateDeclaredObjectIdentityScopedQuery = `
SELECT 1
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'group_version_resource' = $2
  AND fact.payload->>'namespace' = $3
  AND fact.payload->>'name' = $4
  AND ($5 OR fact.payload->'image_refs' ?| $6)
  AND (fact.scope_id = ANY($7) OR fact.scope_id = ANY($8))
LIMIT 1
`

// listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery is the
// SELECT-columns declared-object sibling of
// listLiveKubernetesPodTemplateIdentityMatchesQuery: the identical
// ACTIVE-generation join, is_tombstone predicate, and image-refs filter, but
// anchored on group_version_resource/namespace/name instead of the ArgoCD
// annotation, selecting the same four columns
// scanLiveIdentityMatchRows decodes. LIMIT is bound as a parameter ($7)
// rather than a literal, matching the ArgoCD variant's convention.
const listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery = `
SELECT
  fact.payload->>'cluster_id' AS cluster_id,
  fact.payload->>'object_id' AS object_id,
  fact.payload->>'group_version_resource' AS group_version_resource,
  (fact.payload->>'ready_replicas')::int AS ready_replicas
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'group_version_resource' = $2
  AND fact.payload->>'namespace' = $3
  AND fact.payload->>'name' = $4
  AND ($5 OR fact.payload->'image_refs' ?| $6)
ORDER BY fact.payload->>'object_id'
LIMIT $7
`

// listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesScopedQuery is
// listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery with the
// additional #5167 access-scoping predicate. Bound only when
// filter.AllScopes is false; the LIMIT parameter shifts to $9 to make room
// for the two scope-id array parameters ($7, $8).
const listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesScopedQuery = `
SELECT
  fact.payload->>'cluster_id' AS cluster_id,
  fact.payload->>'object_id' AS object_id,
  fact.payload->>'group_version_resource' AS group_version_resource,
  (fact.payload->>'ready_replicas')::int AS ready_replicas
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'group_version_resource' = $2
  AND fact.payload->>'namespace' = $3
  AND fact.payload->>'name' = $4
  AND ($5 OR fact.payload->'image_refs' ?| $6)
  AND (fact.scope_id = ANY($7) OR fact.scope_id = ANY($8))
ORDER BY fact.payload->>'object_id'
LIMIT $9
`

// hasLiveDeclaredObjectIdentityMatch is the declared-object anchor half of
// HasLiveIdentityMatch (#5639). The caller (HasLiveIdentityMatch) has already
// validated filter.hasScope() and the #5167 scoped-empty-grant short-circuit,
// so this method only builds and dispatches the declared-object query.
func (s PostgresKubernetesPodTemplateStore) hasLiveDeclaredObjectIdentityMatch(
	ctx context.Context,
	filter KubernetesPodTemplateFilter,
) (bool, error) {
	query := hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery
	args := []any{
		kubernetesPodTemplateFactKind,
		filter.GroupVersionResource,
		filter.Namespace,
		filter.Name,
		len(filter.ImageRefs) == 0,
		pq.Array(filter.ImageRefs),
	}
	if !filter.AllScopes {
		query = hasLiveKubernetesPodTemplateDeclaredObjectIdentityScopedQuery
		args = append(args, pq.Array(filter.AllowedRepositoryIDs), pq.Array(filter.AllowedScopeIDs))
	}
	return queryLiveIdentityMatchExists(ctx, s.DB, query, args)
}

// listLiveDeclaredObjectIdentityMatches is the declared-object anchor half of
// ListLiveIdentityMatches (#5639). The caller has already validated
// filter.hasScope() and the #5167 scoped-empty-grant short-circuit.
func (s PostgresKubernetesPodTemplateStore) listLiveDeclaredObjectIdentityMatches(
	ctx context.Context,
	filter KubernetesPodTemplateFilter,
) ([]LiveIdentityMatch, error) {
	query := listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery
	args := []any{
		kubernetesPodTemplateFactKind,
		filter.GroupVersionResource,
		filter.Namespace,
		filter.Name,
		len(filter.ImageRefs) == 0,
		pq.Array(filter.ImageRefs),
	}
	if !filter.AllScopes {
		query = listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesScopedQuery
		args = append(args, pq.Array(filter.AllowedRepositoryIDs), pq.Array(filter.AllowedScopeIDs))
	}
	args = append(args, serviceStoryItemLimit)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list live kubernetes pod template identity matches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanLiveIdentityMatchRows(rows)
}
