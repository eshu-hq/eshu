package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const serviceCatalogCorrelationFactKind = "reducer_service_catalog_correlation"

// ServiceCatalogCorrelationStore reads reducer-owned service catalog correlations.
type ServiceCatalogCorrelationStore interface {
	ListServiceCatalogCorrelations(context.Context, ServiceCatalogCorrelationFilter) ([]ServiceCatalogCorrelationRow, error)
}

// ServiceCatalogCorrelationFilter bounds catalog reads to a concrete catalog
// entity, repository, service, workload, owner, or ingestion scope.
type ServiceCatalogCorrelationFilter struct {
	ScopeID            string
	Provider           string
	EntityRef          string
	RepositoryID       string
	ServiceID          string
	WorkloadID         string
	OwnerRef           string
	Outcome            string
	DriftStatus        string
	AfterCorrelationID string
	Limit              int
}

// ServiceCatalogCorrelationRow is one durable service-catalog correlation fact.
type ServiceCatalogCorrelationRow struct {
	CorrelationID          string
	Provider               string
	EntityRef              string
	EntityType             string
	DisplayName            string
	RepositoryID           string
	ServiceID              string
	WorkloadID             string
	OwnerRef               string
	Lifecycle              string
	Tier                   string
	Outcome                string
	Reason                 string
	ProvenanceOnly         bool
	DriftKind              string
	DriftStatus            string
	CandidateRepositoryIDs []string
	EvidenceFactIDs        []string
}

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
	filter ServiceCatalogCorrelationFilter,
) ([]ServiceCatalogCorrelationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("service catalog correlation database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id, entity_ref, repository_id, service_id, workload_id, or owner_ref is required")
	}
	if filter.Limit <= 0 || filter.Limit > serviceCatalogCorrelationMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", serviceCatalogCorrelationMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listServiceCatalogCorrelationsQuery,
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
	)
	if err != nil {
		return nil, fmt.Errorf("list service catalog correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ServiceCatalogCorrelationRow, 0, filter.Limit)
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

const listServiceCatalogCorrelationsQuery = `
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
ORDER BY fact.fact_id ASC
LIMIT $12
`

func (f ServiceCatalogCorrelationFilter) hasScope() bool {
	return f.ScopeID != "" ||
		f.EntityRef != "" ||
		f.RepositoryID != "" ||
		f.ServiceID != "" ||
		f.WorkloadID != "" ||
		f.OwnerRef != ""
}

func decodeServiceCatalogCorrelationRow(
	factID string,
	payloadBytes []byte,
) (ServiceCatalogCorrelationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return ServiceCatalogCorrelationRow{}, fmt.Errorf("decode service catalog correlation: %w", err)
	}
	return ServiceCatalogCorrelationRow{
		CorrelationID:          factID,
		Provider:               StringVal(payload, "provider"),
		EntityRef:              StringVal(payload, "entity_ref"),
		EntityType:             StringVal(payload, "entity_type"),
		DisplayName:            StringVal(payload, "display_name"),
		RepositoryID:           StringVal(payload, "repository_id"),
		ServiceID:              StringVal(payload, "service_id"),
		WorkloadID:             StringVal(payload, "workload_id"),
		OwnerRef:               StringVal(payload, "owner_ref"),
		Lifecycle:              StringVal(payload, "lifecycle"),
		Tier:                   StringVal(payload, "tier"),
		Outcome:                StringVal(payload, "outcome"),
		Reason:                 StringVal(payload, "reason"),
		ProvenanceOnly:         BoolVal(payload, "provenance_only"),
		DriftKind:              StringVal(payload, "drift_kind"),
		DriftStatus:            StringVal(payload, "drift_status"),
		CandidateRepositoryIDs: StringSliceVal(payload, "candidate_repository_ids"),
		EvidenceFactIDs:        StringSliceVal(payload, "evidence_fact_ids"),
	}, nil
}
