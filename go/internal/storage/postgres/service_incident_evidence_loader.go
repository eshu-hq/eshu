// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// serviceIncidentEvidenceQuery loads active PagerDuty routing evidence for the
// requested Eshu catalog services. The provider service id is joined through two
// reducer-owned durable correlations:
//
//   - reducer_service_catalog_correlation: catalog service id -> repository id
//   - reducer_incident_repository_correlation: provider service id -> repository id
//
// Only exact/derived, non-provenance rows are admissible, and a repository must
// map to exactly one active catalog service before incident evidence is attached.
// That fail-closed repository uniqueness gate prevents attributing provider
// service evidence to multiple catalog services when a repository has ambiguous
// catalog ownership. Evidence rows return StableFactKey, never FactID, so the
// incidents family keeps generation-stable identity.
const serviceIncidentEvidenceQuery = `
WITH requested_services AS (
    SELECT unnest($1::text[]) AS service_id
),
requested_service_correlations AS (
    SELECT
        fact.payload->>'service_id' AS service_id,
        fact.payload->>'repository_id' AS repository_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    JOIN requested_services AS requested
      ON requested.service_id = fact.payload->>'service_id'
    WHERE fact.fact_kind = 'reducer_service_catalog_correlation'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'provenance_only' = 'false'
      AND fact.payload->>'outcome' IN ('exact', 'derived')
      AND NULLIF(fact.payload->>'repository_id', '') IS NOT NULL
),
candidate_repositories AS (
    SELECT DISTINCT repository_id
    FROM requested_service_correlations
),
unambiguous_repositories AS (
    SELECT
        fact.payload->>'repository_id' AS repository_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    JOIN candidate_repositories AS candidate
      ON candidate.repository_id = fact.payload->>'repository_id'
    WHERE fact.fact_kind = 'reducer_service_catalog_correlation'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'provenance_only' = 'false'
      AND fact.payload->>'outcome' IN ('exact', 'derived')
      AND NULLIF(fact.payload->>'service_id', '') IS NOT NULL
    GROUP BY fact.payload->>'repository_id'
    HAVING COUNT(DISTINCT fact.payload->>'service_id') = 1
),
requested_service_repositories AS (
    SELECT
        requested.service_id,
        requested.repository_id
    FROM requested_service_correlations AS requested
    JOIN unambiguous_repositories AS unambiguous
      ON unambiguous.repository_id = requested.repository_id
),
active_incident_correlations AS (
    SELECT DISTINCT
        requested.service_id,
        COALESCE(NULLIF(fact.payload->>'provider', ''), 'pagerduty') AS provider,
        fact.payload->>'provider_service_id' AS provider_service_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    JOIN requested_service_repositories AS requested
      ON requested.repository_id = fact.payload->>'repository_id'
    WHERE fact.fact_kind = 'reducer_incident_repository_correlation'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'provenance_only' = 'false'
      AND fact.payload->>'outcome' IN ('exact', 'derived')
      AND NULLIF(fact.payload->>'provider_service_id', '') IS NOT NULL
),
provider_service_bindings AS (
    SELECT DISTINCT
        service_id,
        provider,
        provider_service_id
    FROM active_incident_correlations
),
incident_anchors AS (
    SELECT DISTINCT
        binding.service_id,
        binding.provider,
        COALESCE(NULLIF(fact.payload->>'provider_incident_id', ''), fact.source_record_id) AS provider_incident_id,
        binding.provider_service_id
    FROM provider_service_bindings AS binding
    JOIN fact_records AS fact
      ON fact.fact_kind = 'incident.record'
     AND COALESCE(NULLIF(fact.payload->>'provider', ''), 'pagerduty') = binding.provider
     AND COALESCE(fact.payload->'service'->>'id', fact.payload->>'service_id', '') = binding.provider_service_id
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND NULLIF(COALESCE(NULLIF(fact.payload->>'provider_incident_id', ''), fact.source_record_id), '') IS NOT NULL
),
applied_routing AS (
    SELECT
        anchor.service_id,
        anchor.provider,
        anchor.provider_incident_id,
        'applied_routing' AS slot,
        fact.fact_kind AS evidence_kind,
        fact.stable_fact_key AS evidence_id,
        'exact' AS truth_label,
        COALESCE(fact.payload->>'provider_object_id', '') AS provider_object_id,
        COALESCE(fact.payload->>'declared_match_state', '') AS declared_match_state,
        COALESCE(fact.payload->>'redaction_state', '') AS redaction_state
    FROM incident_anchors AS anchor
    JOIN fact_records AS fact
      ON fact.fact_kind = 'incident_routing.applied_pagerduty_resource'
     AND fact.payload->>'resource_class' = 'service'
     AND fact.payload->>'provider_object_id' = anchor.provider_service_id
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND NULLIF(fact.stable_fact_key, '') IS NOT NULL
),
live_routing AS (
    SELECT
        anchor.service_id,
        anchor.provider,
        anchor.provider_incident_id,
        'live_routing' AS slot,
        fact.fact_kind AS evidence_kind,
        fact.stable_fact_key AS evidence_id,
        'exact' AS truth_label,
        COALESCE(NULLIF(fact.payload->>'provider_object_id', ''), fact.payload->>'service_id', '') AS provider_object_id,
        COALESCE(fact.payload->>'declared_match_state', '') AS declared_match_state,
        COALESCE(fact.payload->>'redaction_state', '') AS redaction_state
    FROM incident_anchors AS anchor
    JOIN fact_records AS fact
      ON fact.fact_kind = 'incident_routing.observed_pagerduty_service'
     AND COALESCE(NULLIF(fact.payload->>'provider_object_id', ''), fact.payload->>'service_id', '') = anchor.provider_service_id
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND NULLIF(fact.stable_fact_key, '') IS NOT NULL
)
SELECT
    service_id,
    provider,
    provider_incident_id,
    slot,
    evidence_kind,
    evidence_id,
    truth_label,
    provider_object_id,
    declared_match_state,
    redaction_state
FROM applied_routing
UNION ALL
SELECT
    service_id,
    provider,
    provider_incident_id,
    slot,
    evidence_kind,
    evidence_id,
    truth_label,
    provider_object_id,
    declared_match_state,
    redaction_state
FROM live_routing
ORDER BY service_id ASC, provider_incident_id ASC, slot ASC, evidence_kind ASC, evidence_id ASC
`

// serviceIncidentEvidenceBoundedQuery is the report read path: the same durable
// fail-closed join as serviceIncidentEvidenceQuery, capped with a row LIMIT so a
// single report request cannot scan or load an unbounded incident history. The
// reducer materialization path keeps using the unbounded query because it must
// observe every routed incident; only the bounded report surface caps the read.
const serviceIncidentEvidenceBoundedQuery = serviceIncidentEvidenceQuery + `
LIMIT $2`

// ServiceIncidentEvidenceLoader loads active incident-routing evidence scoped to
// Eshu catalog service ids. It implements reducer.ServiceScopedIncidentEvidenceLoader.
type ServiceIncidentEvidenceLoader struct {
	queryer Queryer
}

// NewServiceIncidentEvidenceLoader constructs a read-only incidents evidence
// loader over the shared query surface.
func NewServiceIncidentEvidenceLoader(queryer Queryer) ServiceIncidentEvidenceLoader {
	return ServiceIncidentEvidenceLoader{queryer: queryer}
}

// GetIncidentEvidenceForServices loads durable active incident-routing evidence
// for the requested catalog services. It is a no-op for an empty service set and
// omits services with no admissible exact routing evidence from the result map.
func (l ServiceIncidentEvidenceLoader) GetIncidentEvidenceForServices(
	ctx context.Context,
	serviceIDs []string,
) (map[string][]reducer.ServiceIncidentRecord, error) {
	if l.queryer == nil {
		return nil, fmt.Errorf("incident evidence queryer is required")
	}
	serviceIDs = cleanStringFilterValues(serviceIDs)
	if len(serviceIDs) == 0 {
		return nil, nil
	}

	rows, err := l.queryer.QueryContext(ctx, serviceIncidentEvidenceQuery, serviceIDs)
	if err != nil {
		return nil, fmt.Errorf("load service incident evidence: %w", err)
	}
	return scanServiceIncidentEvidence(rows, len(serviceIDs))
}

// GetIncidentEvidenceForServicesBounded loads durable active incident-routing
// evidence for the requested catalog services, capped at rowLimit rows so a
// single read-surface request cannot scan an unbounded incident history. It is a
// no-op for an empty service set or a non-positive rowLimit. Callers that need
// every routed incident (the reducer materialization path) use the unbounded
// GetIncidentEvidenceForServices instead; this bounded read is for report and
// query surfaces that only present a capped set.
func (l ServiceIncidentEvidenceLoader) GetIncidentEvidenceForServicesBounded(
	ctx context.Context,
	serviceIDs []string,
	rowLimit int,
) (map[string][]reducer.ServiceIncidentRecord, error) {
	if l.queryer == nil {
		return nil, fmt.Errorf("incident evidence queryer is required")
	}
	serviceIDs = cleanStringFilterValues(serviceIDs)
	if len(serviceIDs) == 0 || rowLimit <= 0 {
		return nil, nil
	}

	rows, err := l.queryer.QueryContext(ctx, serviceIncidentEvidenceBoundedQuery, serviceIDs, rowLimit)
	if err != nil {
		return nil, fmt.Errorf("load service incident evidence: %w", err)
	}
	return scanServiceIncidentEvidence(rows, len(serviceIDs))
}

// scanServiceIncidentEvidence scans incident-routing evidence rows into the
// per-service result map, closing the rows. It is shared by the unbounded and
// bounded loaders so both decode identical row shapes.
func scanServiceIncidentEvidence(rows Rows, capacityHint int) (map[string][]reducer.ServiceIncidentRecord, error) {
	defer func() { _ = rows.Close() }()

	byService := make(map[string][]reducer.ServiceIncidentRecord, capacityHint)
	for rows.Next() {
		var serviceID string
		var record reducer.ServiceIncidentRecord
		if scanErr := rows.Scan(
			&serviceID,
			&record.Provider,
			&record.ProviderIncidentID,
			&record.Slot,
			&record.EvidenceKind,
			&record.EvidenceID,
			&record.TruthLabel,
			&record.ProviderObjectID,
			&record.DeclaredMatchState,
			&record.RedactionState,
		); scanErr != nil {
			return nil, fmt.Errorf("scan service incident evidence: %w", scanErr)
		}
		byService[serviceID] = append(byService[serviceID], record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load service incident evidence: %w", err)
	}
	return byService, nil
}
