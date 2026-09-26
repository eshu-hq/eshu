// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// CatalogCorrelationFactKind is the fact kind carrying durable
// service-catalog correlation rows. Exported for the staying root
// supply-chain anchor test, which pins the cross-family evidence path;
// production readers keep the package-local spelling.
const CatalogCorrelationFactKind = serviceCatalogCorrelationFactKind

const serviceCatalogCorrelationFactKind = "reducer_service_catalog_correlation"

// CatalogCorrelationStore reads reducer-owned service catalog
// correlations. See querycontract.ServiceCatalogCorrelationStore: the port
// moved there for #6060 lane A L2 because the moved codeowners family needs
// it without importing root, and this alias keeps every staying caller
// compiling unchanged.
type CatalogCorrelationStore = querycontract.ServiceCatalogCorrelationStore

// CatalogCorrelationFilter bounds catalog reads to a concrete catalog
// entity, repository, service, workload, owner, or ingestion scope. See
// querycontract.ServiceCatalogCorrelationFilter.
type CatalogCorrelationFilter = querycontract.ServiceCatalogCorrelationFilter

// CatalogCorrelationRow is one durable service-catalog correlation
// fact. See querycontract.ServiceCatalogCorrelationRow.
type CatalogCorrelationRow = querycontract.ServiceCatalogCorrelationRow

type serviceCatalogCorrelationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresServiceCatalogCorrelationStore reads active service-catalog
// correlation facts from Postgres using bounded payload predicates.
type PostgresServiceCatalogCorrelationStore struct {
	DB serviceCatalogCorrelationQueryer
}

// NewPostgresServiceCatalogCorrelationStore creates the Postgres-backed
// service-catalog correlation read model.
func NewPostgresServiceCatalogCorrelationStore(
	db serviceCatalogCorrelationQueryer,
) PostgresServiceCatalogCorrelationStore {
	return PostgresServiceCatalogCorrelationStore{DB: db}
}

// ListServiceCatalogCorrelations returns one bounded page of active reducer
// service-catalog correlation facts.
func (s PostgresServiceCatalogCorrelationStore) ListServiceCatalogCorrelations(
	ctx context.Context,
	filter CatalogCorrelationFilter,
) ([]CatalogCorrelationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("service catalog correlation database is required")
	}
	if !filter.HasScope() {
		return nil, fmt.Errorf("scope_id, entity_ref, repository_id, service_id, workload_id, or owner_ref is required")
	}
	if filter.Limit <= 0 || filter.Limit > querycontract.ServiceCatalogCorrelationMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", querycontract.ServiceCatalogCorrelationMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		ListServiceCatalogCorrelationsQuery,
		serviceCatalogCorrelationFactKind,
		filter.ScopeID,
		filter.Provider,
		filter.EntityRef,
		filter.RepositoryID,
		filter.ServiceID,
		filter.WorkloadID,
		filter.OwnerRef,
		filter.Outcome,
		filter.DriftStatus,
		filter.AfterCorrelationID,
		filter.Limit,
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list service catalog correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]CatalogCorrelationRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list service catalog correlations: %w", err)
		}
		row, err := decodeServiceCatalogCorrelationRow(factID, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list service catalog correlations: %w", err)
	}
	return out, nil
}

// ListServiceCatalogLocalDescriptorEvidence returns active repo-local
// service-catalog source facts for a canonical repository id.
func (s PostgresServiceCatalogCorrelationStore) ListServiceCatalogLocalDescriptorEvidence(
	ctx context.Context,
	repositoryID string,
	limit int,
) ([]CatalogLocalDescriptorEvidenceRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("service catalog correlation database is required")
	}
	if repositoryID == "" {
		return nil, fmt.Errorf("repository_id is required")
	}
	if limit <= 0 || limit > serviceCatalogLocalDescriptorEvidenceLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", serviceCatalogLocalDescriptorEvidenceLimit+1)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		ListServiceCatalogLocalDescriptorEvidenceQuery,
		serviceCatalogGitRepositoryScopeID(repositoryID),
		array.Of(facts.ServiceCatalogFactKinds()),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list service catalog local descriptor evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]CatalogLocalDescriptorEvidenceRow, 0, limit)
	for rows.Next() {
		var factID string
		var factKind string
		var sourceURI string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &factKind, &sourceURI, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list service catalog local descriptor evidence: %w", err)
		}
		row, err := decodeServiceCatalogLocalDescriptorEvidenceRow(factID, factKind, sourceURI, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list service catalog local descriptor evidence: %w", err)
	}
	return out, nil
}

const ListServiceCatalogCorrelationsQuery = `
SELECT fact.fact_id, fact.payload
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
  AND ($2 = '' OR fact.scope_id = $2)
  AND ($3 = '' OR fact.payload->>'provider' = $3)
  AND ($4 = '' OR fact.payload->>'entity_ref' = $4)
  AND ($5 = '' OR fact.payload->>'repository_id' = $5 OR fact.payload->'candidate_repository_ids' ? $5)
  AND ($6 = '' OR fact.payload->>'service_id' = $6)
  AND ($7 = '' OR fact.payload->>'workload_id' = $7)
  AND ($8 = '' OR fact.payload->>'owner_ref' = $8)
  AND ($9 = '' OR fact.payload->>'outcome' = $9)
  AND ($10 = '' OR fact.payload->>'drift_status' = $10)
  AND ($11 = '' OR fact.fact_id > $11)
  AND (
    (COALESCE(cardinality($13::text[]), 0) = 0 AND COALESCE(cardinality($14::text[]), 0) = 0)
    OR fact.payload->>'repository_id' = ANY($13::text[])
    OR fact.payload->'candidate_repository_ids' ?| $13::text[]
    OR fact.scope_id = ANY($14::text[])
  )
ORDER BY fact.fact_id ASC
LIMIT $12
`

const ListServiceCatalogLocalDescriptorEvidenceQuery = `
SELECT fact.fact_id, fact.fact_kind, COALESCE(fact.source_uri, ''), fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.scope_id = $1
  AND fact.fact_kind = ANY($2::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
ORDER BY COALESCE(fact.source_uri, ''), fact.fact_kind, fact.fact_id
LIMIT $3
`

func decodeServiceCatalogCorrelationRow(
	factID string,
	payloadBytes []byte,
) (CatalogCorrelationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return CatalogCorrelationRow{}, fmt.Errorf("decode service catalog correlation: %w", err)
	}
	return CatalogCorrelationRow{
		CorrelationID:          factID,
		Provider:               querycontract.StringVal(payload, "provider"),
		EntityRef:              querycontract.StringVal(payload, "entity_ref"),
		EntityType:             querycontract.StringVal(payload, "entity_type"),
		DisplayName:            querycontract.StringVal(payload, "display_name"),
		RepositoryID:           querycontract.StringVal(payload, "repository_id"),
		ServiceID:              querycontract.StringVal(payload, "service_id"),
		WorkloadID:             querycontract.StringVal(payload, "workload_id"),
		OwnerRef:               querycontract.StringVal(payload, "owner_ref"),
		Lifecycle:              querycontract.StringVal(payload, "lifecycle"),
		Tier:                   querycontract.StringVal(payload, "tier"),
		Outcome:                querycontract.StringVal(payload, "outcome"),
		Reason:                 querycontract.StringVal(payload, "reason"),
		ProvenanceOnly:         querycontract.BoolVal(payload, "provenance_only"),
		DriftKind:              querycontract.StringVal(payload, "drift_kind"),
		DriftStatus:            querycontract.StringVal(payload, "drift_status"),
		CandidateRepositoryIDs: querycontract.StringSliceVal(payload, "candidate_repository_ids"),
		EvidenceFactIDs:        querycontract.StringSliceVal(payload, "evidence_fact_ids"),
		RequiredAnchorKeys:     querycontract.StringSliceVal(payload, "required_anchor_keys"),
	}, nil
}

func decodeServiceCatalogLocalDescriptorEvidenceRow(
	factID string,
	factKind string,
	sourceURI string,
	payloadBytes []byte,
) (CatalogLocalDescriptorEvidenceRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return CatalogLocalDescriptorEvidenceRow{}, fmt.Errorf("decode service catalog local descriptor evidence: %w", err)
	}
	return CatalogLocalDescriptorEvidenceRow{
		FactID:    factID,
		FactKind:  factKind,
		Provider:  querycontract.StringVal(payload, "provider"),
		EntityRef: querycontract.StringVal(payload, "entity_ref"),
		SourceURI: sourceURI,
	}, nil
}
