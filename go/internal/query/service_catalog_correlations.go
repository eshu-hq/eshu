// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

const serviceCatalogCorrelationFactKind = "reducer_service_catalog_correlation"

// errServiceCatalogOutsideGrantNeedsAGrant refuses an outside-grant read that
// carries no grant at all. The ordinary grant clause is permissive on two empty
// arrays, so its negation matches every row: a caller that lost its grant on
// the way here would silently see every service as contested rather than as
// its own. That failure reads exactly like ordinary tenant isolation, so the
// store fails loudly instead of answering.
var errServiceCatalogOutsideGrantNeedsAGrant = errors.New(
	"outside-grant reads require an allowed repository or scope grant",
)

// ServiceCatalogCorrelationStore reads reducer-owned service catalog correlations.
type ServiceCatalogCorrelationStore interface {
	ListServiceCatalogCorrelations(context.Context, ServiceCatalogCorrelationFilter) ([]ServiceCatalogCorrelationRow, error)
}

// ServiceCatalogCorrelationFilter bounds catalog reads to a concrete catalog
// entity, repository, service, workload, owner, or ingestion scope.
type ServiceCatalogCorrelationFilter struct {
	ScopeID              string
	Provider             string
	EntityRef            string
	RepositoryID         string
	ServiceID            string
	WorkloadID           string
	OwnerRef             string
	Outcome              string
	DriftStatus          string
	AfterCorrelationID   string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
	// OutsideGrant inverts the grant clause: the read returns the rows the
	// caller's grant does NOT admit, rather than the rows it does. It answers
	// "does anything outside my grant also claim this selector", which is what
	// a caller needs before it may act on a shared identifier whose downstream
	// tables carry no scope column of their own. The two grant arrays are
	// required in this mode -- see errServiceCatalogOutsideGrantNeedsAGrant.
	OutsideGrant bool
	Limit        int
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
	RequiredAnchorKeys     []string
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
	if filter.OutsideGrant && len(filter.AllowedRepositoryIDs) == 0 && len(filter.AllowedScopeIDs) == 0 {
		return nil, errServiceCatalogOutsideGrantNeedsAGrant
	}

	statement := listServiceCatalogCorrelationsQuery
	if filter.OutsideGrant {
		statement = listServiceCatalogCorrelationsOutsideGrantQuery
	}

	rows, err := s.DB.QueryContext(
		ctx,
		statement,
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
		pgarray.Array(filter.AllowedRepositoryIDs),
		pgarray.Array(filter.AllowedScopeIDs),
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

// ListServiceCatalogLocalDescriptorEvidence returns active repo-local
// service-catalog source facts for a canonical repository id.
func (s PostgresServiceCatalogCorrelationStore) ListServiceCatalogLocalDescriptorEvidence(
	ctx context.Context,
	repositoryID string,
	limit int,
) ([]ServiceCatalogLocalDescriptorEvidenceRow, error) {
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
		listServiceCatalogLocalDescriptorEvidenceQuery,
		serviceCatalogGitRepositoryScopeID(repositoryID),
		pgarray.Array(facts.ServiceCatalogFactKinds()),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list service catalog local descriptor evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ServiceCatalogLocalDescriptorEvidenceRow, 0, limit)
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
  AND (
    (COALESCE(cardinality($13::text[]), 0) = 0 AND COALESCE(cardinality($14::text[]), 0) = 0)
    OR fact.payload->>'repository_id' = ANY($13::text[])
    OR fact.payload->'candidate_repository_ids' ?| $13::text[]
    OR fact.scope_id = ANY($14::text[])
  )
ORDER BY fact.fact_id ASC
LIMIT $12
`

// listServiceCatalogCorrelationsOutsideGrantQuery is
// listServiceCatalogCorrelationsQuery with the grant disjunction negated, so it
// returns the correlations the caller's grant does not admit. It is a separate
// literal rather than a built string so the ordinary statement's text -- and
// therefore its plan cache entry, shared with every other caller of this store
// -- is untouched;
// TestServiceCatalogCorrelationsOutsideGrantQueryInvertsOnlyTheGrantClause pins
// the two in lockstep.
//
// Two of the three disjuncts are COALESCEd to FALSE, because a payload without
// a repository_id, or without a candidate_repository_ids array, compares to
// NULL: NOT NULL is NULL, which would drop exactly the rows whose ownership
// cannot be read -- the ones this statement most needs to report. The third,
// fact.scope_id = ANY($14), needs no guard for one specific reason: the
// statement INNER JOINs ingestion_scopes ON scope.scope_id = fact.scope_id, so
// no row with a NULL scope_id reaches this WHERE and that comparison cannot
// evaluate to NULL. A disjunct added here on a nullable column would need the
// COALESCE; do not read the third one as evidence that the pattern is
// optional. The empty-arrays arm of the ordinary clause is absent on purpose;
// its negation would match every row, and the store refuses that filter before
// it gets here.
const listServiceCatalogCorrelationsOutsideGrantQuery = `
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
  AND NOT (
    COALESCE(fact.payload->>'repository_id' = ANY($13::text[]), FALSE)
    OR COALESCE(fact.payload->'candidate_repository_ids' ?| $13::text[], FALSE)
    OR fact.scope_id = ANY($14::text[])
  )
ORDER BY fact.fact_id ASC
LIMIT $12
`

const listServiceCatalogLocalDescriptorEvidenceQuery = `
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

func (f ServiceCatalogCorrelationFilter) hasScope() bool {
	return f.ScopeID != "" ||
		f.EntityRef != "" ||
		f.RepositoryID != "" ||
		f.ServiceID != "" ||
		f.WorkloadID != "" ||
		f.OwnerRef != "" ||
		len(f.AllowedRepositoryIDs) > 0
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
		RequiredAnchorKeys:     StringSliceVal(payload, "required_anchor_keys"),
	}, nil
}

func decodeServiceCatalogLocalDescriptorEvidenceRow(
	factID string,
	factKind string,
	sourceURI string,
	payloadBytes []byte,
) (ServiceCatalogLocalDescriptorEvidenceRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return ServiceCatalogLocalDescriptorEvidenceRow{}, fmt.Errorf("decode service catalog local descriptor evidence: %w", err)
	}
	return ServiceCatalogLocalDescriptorEvidenceRow{
		FactID:    factID,
		FactKind:  factKind,
		Provider:  StringVal(payload, "provider"),
		EntityRef: StringVal(payload, "entity_ref"),
		SourceURI: sourceURI,
	}, nil
}
