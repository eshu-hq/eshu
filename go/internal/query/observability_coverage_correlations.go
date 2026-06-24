// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const observabilityCoverageCorrelationFactKind = "reducer_observability_coverage_correlation"

// ObservabilityCoverageCorrelationStore reads reducer-owned observability
// coverage correlations: which monitored cloud resources or services have
// observability coverage (alarms, dashboards, log groups, traces) versus which
// are gaps.
type ObservabilityCoverageCorrelationStore interface {
	ListObservabilityCoverageCorrelations(context.Context, ObservabilityCoverageCorrelationFilter) ([]ObservabilityCoverageCorrelationRow, error)
}

// ObservabilityCoverageCorrelationFilter bounds coverage reads to a concrete
// scope, provider, coverage signal class, observability object, monitored
// target resource, or target service. At least one anchor is required so reads
// never fan out across the whole fact store.
type ObservabilityCoverageCorrelationFilter struct {
	ScopeID                string
	Provider               string
	CoverageSignal         string
	ObservabilityObjectRef string
	TargetUID              string
	TargetServiceRef       string
	Outcome                string
	CoverageStatus         string
	SourceClass            string
	ResourceClass          string
	AfterCorrelationID     string
	Limit                  int
}

// ObservabilityCoverageCorrelationRow is one durable observability coverage
// correlation fact. It carries IDs, classifications, and the six-outcome
// decision only; no metric values or dashboard body JSON are surfaced, so the
// "no health assertions from telemetry values" contract holds structurally.
type ObservabilityCoverageCorrelationRow struct {
	CorrelationID          string
	Provider               string
	CoverageSignal         string
	ObservabilityObjectRef string
	ObservabilityUID       string
	TargetUID              string
	TargetServiceRef       string
	Outcome                string
	Reason                 string
	CoverageStatus         string
	ProvenanceOnly         bool
	ResolutionMode         string
	SourceClass            string
	SourceClasses          []string
	SourceKind             string
	SourceKinds            []string
	SourceOutcome          string
	SourceOutcomes         []string
	ResourceClass          string
	FreshnessState         string
	CandidateTargetUIDs    []string
	EvidenceFactIDs        []string
}

type observabilityCoverageCorrelationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresObservabilityCoverageCorrelationStore reads active observability
// coverage correlation facts from Postgres using bounded payload predicates.
type PostgresObservabilityCoverageCorrelationStore struct {
	DB observabilityCoverageCorrelationQueryer
}

// NewPostgresObservabilityCoverageCorrelationStore creates the Postgres-backed
// observability coverage correlation read model.
func NewPostgresObservabilityCoverageCorrelationStore(
	db observabilityCoverageCorrelationQueryer,
) PostgresObservabilityCoverageCorrelationStore {
	return PostgresObservabilityCoverageCorrelationStore{DB: db}
}

// ListObservabilityCoverageCorrelations returns one bounded page of active
// reducer observability coverage correlation facts.
func (s PostgresObservabilityCoverageCorrelationStore) ListObservabilityCoverageCorrelations(
	ctx context.Context,
	filter ObservabilityCoverageCorrelationFilter,
) ([]ObservabilityCoverageCorrelationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("observability coverage correlation database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id, provider, coverage_signal, observability_object_ref, target_uid, or target_service_ref is required")
	}
	if filter.Limit <= 0 || filter.Limit > observabilityCoverageCorrelationMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d", observabilityCoverageCorrelationMaxLimit)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listObservabilityCoverageCorrelationsQuery,
		observabilityCoverageCorrelationFactKind,
		filter.ScopeID,
		filter.Provider,
		filter.CoverageSignal,
		filter.ObservabilityObjectRef,
		filter.TargetUID,
		filter.TargetServiceRef,
		filter.Outcome,
		filter.CoverageStatus,
		filter.SourceClass,
		filter.ResourceClass,
		filter.AfterCorrelationID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list observability coverage correlations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ObservabilityCoverageCorrelationRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list observability coverage correlations: %w", err)
		}
		row, err := decodeObservabilityCoverageCorrelationRow(factID, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list observability coverage correlations: %w", err)
	}
	return out, nil
}

const listObservabilityCoverageCorrelationsQuery = `
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
  AND ($4 = '' OR fact.payload->>'coverage_signal' = $4)
  AND ($5 = '' OR fact.payload->>'observability_object_ref' = $5)
  AND ($6 = '' OR fact.payload->>'target_uid' = $6)
  AND ($7 = '' OR fact.payload->>'target_service_ref' = $7)
  AND ($8 = '' OR fact.payload->>'outcome' = $8)
  AND ($9 = '' OR fact.payload->>'coverage_status' = $9)
  AND ($10 = '' OR fact.payload->>'source_class' = $10 OR fact.payload->'source_classes' ? $10)
  AND ($11 = '' OR fact.payload->>'resource_class' = $11)
  AND ($12 = '' OR fact.fact_id > $12)
ORDER BY fact.fact_id ASC
LIMIT $13
`

func (f ObservabilityCoverageCorrelationFilter) hasScope() bool {
	return f.ScopeID != "" ||
		f.Provider != "" ||
		f.CoverageSignal != "" ||
		f.ObservabilityObjectRef != "" ||
		f.TargetUID != "" ||
		f.TargetServiceRef != ""
}

func decodeObservabilityCoverageCorrelationRow(
	factID string,
	payloadBytes []byte,
) (ObservabilityCoverageCorrelationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return ObservabilityCoverageCorrelationRow{}, fmt.Errorf("decode observability coverage correlation: %w", err)
	}
	return ObservabilityCoverageCorrelationRow{
		CorrelationID:          factID,
		Provider:               StringVal(payload, "provider"),
		CoverageSignal:         StringVal(payload, "coverage_signal"),
		ObservabilityObjectRef: StringVal(payload, "observability_object_ref"),
		ObservabilityUID:       StringVal(payload, "observability_resource_uid"),
		TargetUID:              StringVal(payload, "target_uid"),
		TargetServiceRef:       StringVal(payload, "target_service_ref"),
		Outcome:                StringVal(payload, "outcome"),
		Reason:                 StringVal(payload, "reason"),
		CoverageStatus:         StringVal(payload, "coverage_status"),
		ProvenanceOnly:         BoolVal(payload, "provenance_only"),
		ResolutionMode:         StringVal(payload, "resolution_mode"),
		SourceClass:            StringVal(payload, "source_class"),
		SourceClasses:          StringSliceVal(payload, "source_classes"),
		SourceKind:             StringVal(payload, "source_kind"),
		SourceKinds:            StringSliceVal(payload, "source_kinds"),
		SourceOutcome:          StringVal(payload, "source_outcome"),
		SourceOutcomes:         StringSliceVal(payload, "source_outcomes"),
		ResourceClass:          StringVal(payload, "resource_class"),
		FreshnessState:         StringVal(payload, "freshness_state"),
		CandidateTargetUIDs:    StringSliceVal(payload, "candidate_target_uids"),
		EvidenceFactIDs:        StringSliceVal(payload, "evidence_fact_ids"),
	}, nil
}
