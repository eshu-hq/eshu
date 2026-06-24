// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// resolveDurableIncidentRepositoriesQuery resolves the durable owning
// repositories of one incident. It joins the active incident.record fact to the
// reducer-owned durable incident→repository correlation edge
// (reducer_incident_repository_correlation) on (provider, provider service id):
// the incident carries its PagerDuty provider service id at payload->'service'->>'id',
// and the correlation edge keys on payload->>'provider_service_id'. Only durable,
// edge-bearing correlations are returned — provenance_only = false AND a
// non-blank repository_id — so ambiguous, unresolved, rejected, and
// name-fingerprint-only routing never authorizes a scoped read. Both facts are
// read at their scope's active generation, matching every other query-surface
// read. The incident anchor and the correlation edge live in different ingestion
// scopes (the incident is in the PagerDuty scope; the edge is in the applied
// Terraform-state scope), so the join is on the provider service id alone and
// the optional $3 scope filter narrows only the incident anchor.
const resolveDurableIncidentRepositoriesQuery = `
WITH incident_service AS (
    SELECT DISTINCT fact.payload->'service'->>'id' AS provider_service_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.source_system = $1
      AND (
          fact.payload->>'provider_incident_id' = $2
          OR (
              NULLIF(fact.payload->>'provider_incident_id', '') IS NULL
              AND fact.source_record_id = $2
          )
      )
      AND ($3 = '' OR fact.scope_id = $3)
      AND fact.fact_kind = 'incident.record'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND NULLIF(fact.payload->'service'->>'id', '') IS NOT NULL
)
SELECT DISTINCT correlation.payload->>'repository_id' AS repository_id
FROM fact_records AS correlation
JOIN ingestion_scopes AS scope
  ON scope.scope_id = correlation.scope_id
 AND scope.active_generation_id = correlation.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = correlation.scope_id
 AND generation.generation_id = correlation.generation_id
JOIN incident_service
  ON incident_service.provider_service_id = correlation.payload->>'provider_service_id'
WHERE correlation.fact_kind = 'reducer_incident_repository_correlation'
  AND correlation.is_tombstone = FALSE
  AND generation.status = 'active'
  AND correlation.payload->>'provider' = $1
  AND correlation.payload->>'provenance_only' = 'false'
  AND NULLIF(correlation.payload->>'repository_id', '') IS NOT NULL
ORDER BY repository_id ASC
`

// PostgresIncidentRepositoryAuthorizer resolves an incident's durable owning
// repositories from the reducer-owned correlation edge. It is the query-side
// read seam over the durable fact the incident-repository correlation reducer
// writes, used to fail closed on scoped-token incident-context reads.
type PostgresIncidentRepositoryAuthorizer struct {
	DB incidentContextQueryer
}

// NewPostgresIncidentRepositoryAuthorizer creates the Postgres incident
// repository authorizer over the shared fact store.
func NewPostgresIncidentRepositoryAuthorizer(db incidentContextQueryer) PostgresIncidentRepositoryAuthorizer {
	return PostgresIncidentRepositoryAuthorizer{DB: db}
}

// ResolveDurableIncidentRepositories implements IncidentRepositoryAuthorizer. It
// returns the distinct durable repository ids correlated to the incident, or an
// empty slice when the incident has no durable owning repository. A blank
// provider or provider incident id yields an empty result without a read.
func (a PostgresIncidentRepositoryAuthorizer) ResolveDurableIncidentRepositories(
	ctx context.Context,
	provider string,
	providerIncidentID string,
	scopeID string,
) ([]string, error) {
	if a.DB == nil {
		return nil, fmt.Errorf("incident repository authorizer database is required")
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	providerIncidentID = strings.TrimSpace(providerIncidentID)
	scopeID = strings.TrimSpace(scopeID)
	if provider == "" || providerIncidentID == "" {
		return nil, nil
	}

	rows, err := a.DB.QueryContext(
		ctx,
		resolveDurableIncidentRepositoriesQuery,
		provider,
		providerIncidentID,
		scopeID,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve durable incident repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]struct{})
	repositories := make([]string, 0)
	for rows.Next() {
		var repositoryID string
		if err := rows.Scan(&repositoryID); err != nil {
			return nil, fmt.Errorf("scan durable incident repository: %w", err)
		}
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			continue
		}
		if _, ok := seen[repositoryID]; ok {
			continue
		}
		seen[repositoryID] = struct{}{}
		repositories = append(repositories, repositoryID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate durable incident repositories: %w", err)
	}
	sort.Strings(repositories)
	return repositories, nil
}
