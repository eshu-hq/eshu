// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const kubernetesCorrelationFactKind = "reducer_kubernetes_correlation"

// KubernetesCorrelationStore reads reducer-owned Kubernetes correlations (issue
// #388, PR2). The store is the read half of the PR1 producer
// (reducer_kubernetes_correlation facts); it writes nothing and projects no
// graph edges.
type KubernetesCorrelationStore interface {
	ListKubernetesCorrelations(context.Context, KubernetesCorrelationFilter) ([]KubernetesCorrelationRow, error)
}

// KubernetesCorrelationFilter bounds correlation reads to a concrete cluster,
// workload, namespace, image reference, source digest, outcome, drift kind, or
// ingestion scope. At least one anchor is required so a read never scans the
// whole fact store.
type KubernetesCorrelationFilter struct {
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
}

// KubernetesCorrelationRow is one durable Kubernetes correlation fact. The
// fields mirror the reducer payload written by
// PostgresKubernetesCorrelationWriter; IDs, outcomes, and classifications only,
// preserving the metadata-only contract.
type KubernetesCorrelationRow struct {
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

type kubernetesCorrelationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresKubernetesCorrelationStore reads active Kubernetes correlation facts
// from Postgres using bounded payload predicates against the shared active-fact
// read model.
type PostgresKubernetesCorrelationStore struct {
	DB kubernetesCorrelationQueryer
}

// NewPostgresKubernetesCorrelationStore creates the Postgres-backed Kubernetes
// correlation read model.
func NewPostgresKubernetesCorrelationStore(
	db kubernetesCorrelationQueryer,
) PostgresKubernetesCorrelationStore {
	return PostgresKubernetesCorrelationStore{DB: db}
}

// ListKubernetesCorrelations returns one bounded page of active reducer
// Kubernetes correlation facts. It requires a concrete scope anchor and a
// bounded limit, and orders by fact_id so after_correlation_id pagination is
// deterministic.
func (s PostgresKubernetesCorrelationStore) ListKubernetesCorrelations(
	ctx context.Context,
	filter KubernetesCorrelationFilter,
) ([]KubernetesCorrelationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("kubernetes correlation database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id, cluster_id, workload_object_id, namespace, image_ref, or source_digest is required")
	}
	if filter.Limit <= 0 || filter.Limit > kubernetesCorrelationMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", kubernetesCorrelationMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listKubernetesCorrelationsQuery,
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
	)
	if err != nil {
		return nil, fmt.Errorf("list kubernetes correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]KubernetesCorrelationRow, 0, filter.Limit)
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

func (f KubernetesCorrelationFilter) hasScope() bool {
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
) (KubernetesCorrelationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return KubernetesCorrelationRow{}, fmt.Errorf("decode kubernetes correlation: %w", err)
	}
	return KubernetesCorrelationRow{
		CorrelationID:          factID,
		ClusterID:              StringVal(payload, "cluster_id"),
		WorkloadObjectID:       StringVal(payload, "workload_object_id"),
		Namespace:              StringVal(payload, "namespace"),
		WorkloadName:           StringVal(payload, "workload_name"),
		WorkloadUID:            StringVal(payload, "workload_uid"),
		ImageRef:               StringVal(payload, "image_ref"),
		SourceDigest:           StringVal(payload, "source_digest"),
		JoinMode:               StringVal(payload, "join_mode"),
		IdentityEdgeKey:        StringVal(payload, "identity_edge_key"),
		RelationshipType:       StringVal(payload, "relationship_type"),
		Outcome:                StringVal(payload, "outcome"),
		DriftKind:              StringVal(payload, "drift_kind"),
		Reason:                 StringVal(payload, "reason"),
		NonPromotion:           StringVal(payload, "non_promotion"),
		ProvenanceOnly:         BoolVal(payload, "provenance_only"),
		CandidateSourceDigests: StringSliceVal(payload, "candidate_source_digests"),
		Warnings:               StringSliceVal(payload, "warnings"),
		EvidenceFactIDs:        StringSliceVal(payload, "evidence_fact_ids"),
	}, nil
}
