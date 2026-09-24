// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

const kubernetesCorrelationFactKind = "reducer_kubernetes_correlation"

// WorkloadCorrelationStore reads reducer-owned Kubernetes correlations (issue
// #388, PR2). The store is the read half of the PR1 producer
// (reducer_kubernetes_correlation facts); it writes nothing and projects no
// graph edges.
type WorkloadCorrelationStore interface {
	ListKubernetesCorrelations(context.Context, CorrelationFilter) ([]CorrelationRow, error)
}

// CorrelationFilter bounds correlation reads to a concrete cluster,
// workload, namespace, image reference, source digest, outcome, drift kind, or
// ingestion scope. At least one anchor is required so a read never scans the
// whole fact store.
type CorrelationFilter struct {
	ScopeID            string
	ClusterID          string
	WorkloadObjectID   string
	Namespace          string
	ImageRef           string
	SourceDigest       string
	Outcome            string
	DriftKind          string
	AfterCorrelationID string
	Limit              int
	// AllScopes, AllowedRepositoryIDs, and AllowedScopeIDs carry the #5167
	// access-scoping bound. reducer_kubernetes_correlation facts are keyed by
	// ingestion scope_id; hasScope() only requires SOME anchor (cluster_id,
	// namespace, image_ref, ...), so an unscoped filter (e.g. namespace only,
	// no scope_id) would otherwise fan out across every tenant's scope. When
	// AllScopes is false, rows are additionally restricted to
	// fact.scope_id = ANY(AllowedRepositoryIDs) OR
	// fact.scope_id = ANY(AllowedScopeIDs); listCorrelations short-circuits to
	// an empty page without a query when a scoped caller holds no grants,
	// matching the #5137 LiveActivityStore precedent.
	AllScopes            bool
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

// CorrelationRow is one durable Kubernetes correlation fact. The
// fields mirror the reducer payload written by
// PostgresKubernetesCorrelationWriter; IDs, outcomes, and classifications only,
// preserving the metadata-only contract.
type CorrelationRow struct {
	CorrelationID          string
	ClusterID              string
	WorkloadObjectID       string
	Namespace              string
	WorkloadName           string
	WorkloadUID            string
	ImageRef               string
	SourceDigest           string
	JoinMode               string
	IdentityEdgeKey        string
	RelationshipType       string
	Outcome                string
	DriftKind              string
	Reason                 string
	NonPromotion           string
	ProvenanceOnly         bool
	CandidateSourceDigests []string
	Warnings               []string
	EvidenceFactIDs        []string
}

// CorrelationQueryer is the database/sql surface PostgresCorrelationStore
// reads through; *sql.DB satisfies it.
type CorrelationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresCorrelationStore reads active Kubernetes correlation facts
// from Postgres using bounded payload predicates against the shared active-fact
// read model.
type PostgresCorrelationStore struct {
	DB CorrelationQueryer
}

// NewPostgresCorrelationStore creates the Postgres-backed Kubernetes
// correlation read model.
func NewPostgresCorrelationStore(
	db CorrelationQueryer,
) PostgresCorrelationStore {
	return PostgresCorrelationStore{DB: db}
}

// ListKubernetesCorrelations returns one bounded page of active reducer
// Kubernetes correlation facts. It requires a concrete scope anchor and a
// bounded limit, and orders by fact_id so after_correlation_id pagination is
// deterministic.
func (s PostgresCorrelationStore) ListKubernetesCorrelations(
	ctx context.Context,
	filter CorrelationFilter,
) ([]CorrelationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("kubernetes correlation database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id, cluster_id, workload_object_id, namespace, image_ref, or source_digest is required")
	}
	if filter.Limit <= 0 || filter.Limit > kubernetesCorrelationMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", kubernetesCorrelationMaxLimit)
	}
	// Defense in depth (#5167, mirrors #5137 ReadLiveActivity): a scoped
	// caller with no granted repository or ingestion scope gets zero rows
	// without a query, even if a caller forgot the empty-grant short-circuit
	// in listCorrelations.
	if !filter.AllScopes && len(filter.AllowedRepositoryIDs) == 0 && len(filter.AllowedScopeIDs) == 0 {
		return nil, nil
	}

	query := listKubernetesCorrelationsQuery
	args := []any{
		kubernetesCorrelationFactKind,
		filter.ScopeID,
		filter.ClusterID,
		filter.WorkloadObjectID,
		filter.Namespace,
		filter.ImageRef,
		filter.SourceDigest,
		filter.Outcome,
		filter.DriftKind,
		filter.AfterCorrelationID,
		filter.Limit,
	}
	if !filter.AllScopes {
		query = listKubernetesCorrelationsScopedQuery
		args = append(args, array.Of(filter.AllowedRepositoryIDs), array.Of(filter.AllowedScopeIDs))
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list kubernetes correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]CorrelationRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list kubernetes correlations: %w", err)
		}
		row, err := decodeKubernetesCorrelationRow(factID, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list kubernetes correlations: %w", err)
	}
	return out, nil
}

const listKubernetesCorrelationsQuery = `
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
  AND ($3 = '' OR fact.payload->>'cluster_id' = $3)
  AND ($4 = '' OR fact.payload->>'workload_object_id' = $4)
  AND ($5 = '' OR fact.payload->>'namespace' = $5)
  AND ($6 = '' OR fact.payload->>'image_ref' = $6)
  AND ($7 = '' OR fact.payload->>'source_digest' = $7)
  AND ($8 = '' OR fact.payload->>'outcome' = $8)
  AND ($9 = '' OR fact.payload->>'drift_kind' = $9)
  AND ($10 = '' OR fact.fact_id > $10)
ORDER BY fact.fact_id ASC
LIMIT $11
`

// listKubernetesCorrelationsScopedQuery is listKubernetesCorrelationsQuery with
// an additional #5167 access-scoping predicate: rows are restricted to the
// scoped caller's granted repositories/ingestion scopes. Bound only when
// filter.AllScopes is false (see CorrelationFilter's doc comment).
const listKubernetesCorrelationsScopedQuery = `
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
  AND ($3 = '' OR fact.payload->>'cluster_id' = $3)
  AND ($4 = '' OR fact.payload->>'workload_object_id' = $4)
  AND ($5 = '' OR fact.payload->>'namespace' = $5)
  AND ($6 = '' OR fact.payload->>'image_ref' = $6)
  AND ($7 = '' OR fact.payload->>'source_digest' = $7)
  AND ($8 = '' OR fact.payload->>'outcome' = $8)
  AND ($9 = '' OR fact.payload->>'drift_kind' = $9)
  AND ($10 = '' OR fact.fact_id > $10)
  AND (fact.scope_id = ANY($12) OR fact.scope_id = ANY($13))
ORDER BY fact.fact_id ASC
LIMIT $11
`

func (f CorrelationFilter) hasScope() bool {
	return f.ScopeID != "" ||
		f.ClusterID != "" ||
		f.WorkloadObjectID != "" ||
		f.Namespace != "" ||
		f.ImageRef != "" ||
		f.SourceDigest != ""
}

func decodeKubernetesCorrelationRow(
	factID string,
	payloadBytes []byte,
) (CorrelationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return CorrelationRow{}, fmt.Errorf("decode kubernetes correlation: %w", err)
	}
	return CorrelationRow{
		CorrelationID:          factID,
		ClusterID:              querycontract.StringVal(payload, "cluster_id"),
		WorkloadObjectID:       querycontract.StringVal(payload, "workload_object_id"),
		Namespace:              querycontract.StringVal(payload, "namespace"),
		WorkloadName:           querycontract.StringVal(payload, "workload_name"),
		WorkloadUID:            querycontract.StringVal(payload, "workload_uid"),
		ImageRef:               querycontract.StringVal(payload, "image_ref"),
		SourceDigest:           querycontract.StringVal(payload, "source_digest"),
		JoinMode:               querycontract.StringVal(payload, "join_mode"),
		IdentityEdgeKey:        querycontract.StringVal(payload, "identity_edge_key"),
		RelationshipType:       querycontract.StringVal(payload, "relationship_type"),
		Outcome:                querycontract.StringVal(payload, "outcome"),
		DriftKind:              querycontract.StringVal(payload, "drift_kind"),
		Reason:                 querycontract.StringVal(payload, "reason"),
		NonPromotion:           querycontract.StringVal(payload, "non_promotion"),
		ProvenanceOnly:         querycontract.BoolVal(payload, "provenance_only"),
		CandidateSourceDigests: querycontract.StringSliceVal(payload, "candidate_source_digests"),
		Warnings:               querycontract.StringSliceVal(payload, "warnings"),
		EvidenceFactIDs:        querycontract.StringSliceVal(payload, "evidence_fact_ids"),
	}, nil
}
