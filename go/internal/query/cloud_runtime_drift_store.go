// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PostgresMultiCloudRuntimeDriftStore adapts active reducer-materialized
// runtime drift facts to the query package's stable readback contract. It is
// the Postgres-backed implementation of MultiCloudRuntimeDriftStore,
// aggregating BOTH reducer_multi_cloud_runtime_drift_finding (gcp, azure) and
// reducer_aws_cloud_runtime_drift_finding (aws) rows via
// postgres.MultiCloudRuntimeDriftFindingStore.ListActiveFindingsAcrossProviders
// / CountActiveFindingsAcrossProviders (#5759 follow-up) -- one bounded,
// globally ordered SQL query per call, not a per-provider fan-out.
type PostgresMultiCloudRuntimeDriftStore struct {
	store postgres.MultiCloudRuntimeDriftFindingStore
}

// NewPostgresMultiCloudRuntimeDriftStore creates a query adapter over
// runtime drift reducer facts in Postgres, instrumenting the underlying
// database so the readback inherits the shared store telemetry.
func NewPostgresMultiCloudRuntimeDriftStore(db *sql.DB) *PostgresMultiCloudRuntimeDriftStore {
	storeDB := &postgres.InstrumentedDB{
		Inner:     postgres.SQLDB{DB: db},
		Tracer:    otel.Tracer(telemetry.DefaultSignalName),
		StoreName: "multi_cloud_runtime_drift",
	}
	return &PostgresMultiCloudRuntimeDriftStore{
		store: postgres.NewMultiCloudRuntimeDriftFindingStore(storeDB),
	}
}

// ListActiveMultiCloudRuntimeDriftFindings returns one bounded, globally
// ordered page of active runtime drift findings for the caller's scope,
// aggregated across the provider-neutral (gcp, azure) and AWS-specific fact
// kinds (#5759 follow-up) via postgres.ListActiveFindingsAcrossProviders.
func (s *PostgresMultiCloudRuntimeDriftStore) ListActiveMultiCloudRuntimeDriftFindings(
	ctx context.Context,
	filter MultiCloudRuntimeDriftFilter,
) ([]MultiCloudRuntimeDriftFindingRow, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.store.ListActiveFindingsAcrossProviders(ctx, cloudRuntimeDriftAggregateFilterToStore(filter))
	if err != nil {
		return nil, err
	}
	out := make([]MultiCloudRuntimeDriftFindingRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, cloudRuntimeDriftAggregateRowFromStore(row))
	}
	return out, nil
}

// CountActiveMultiCloudRuntimeDriftFindings returns the total active finding
// count for the same bounded, provider-aggregated filters used by the list
// path.
func (s *PostgresMultiCloudRuntimeDriftStore) CountActiveMultiCloudRuntimeDriftFindings(
	ctx context.Context,
	filter MultiCloudRuntimeDriftFilter,
) (int, error) {
	if s == nil {
		return 0, nil
	}
	return s.store.CountActiveFindingsAcrossProviders(ctx, cloudRuntimeDriftAggregateFilterToStore(filter))
}

func multiCloudRuntimeDriftRowFromStore(
	row postgres.MultiCloudRuntimeDriftFindingRow,
) MultiCloudRuntimeDriftFindingRow {
	return MultiCloudRuntimeDriftFindingRow{
		FactID:                       row.FactID,
		ScopeID:                      row.ScopeID,
		GenerationID:                 row.GenerationID,
		SourceSystem:                 row.SourceSystem,
		Provider:                     row.Provider,
		CloudResourceUID:             row.CloudResourceUID,
		RawIdentity:                  row.RawIdentity,
		FindingKind:                  row.FindingKind,
		ManagementStatus:             row.ManagementStatus,
		Confidence:                   row.Confidence,
		MatchedTerraformStateAddress: row.MatchedTerraformStateAddress,
		MissingEvidence:              row.MissingEvidence,
		WarningFlags:                 row.WarningFlags,
		RecommendedAction:            row.RecommendedAction,
		DriftedAttributes:            driftedAttributesFromEvidence(row.Evidence),
	}
}
