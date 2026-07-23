// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PostgresMultiCloudRuntimeDriftStore adapts active reducer-materialized
// provider-neutral runtime drift facts to the query package's stable readback
// contract. It is the Postgres-backed implementation of
// MultiCloudRuntimeDriftStore over reducer_multi_cloud_runtime_drift_finding rows.
type PostgresMultiCloudRuntimeDriftStore struct {
	store postgres.MultiCloudRuntimeDriftFindingStore
}

// NewPostgresMultiCloudRuntimeDriftStore creates a query adapter over
// provider-neutral runtime drift reducer facts in Postgres, instrumenting the
// underlying database so the readback inherits the shared store telemetry.
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

// ListActiveMultiCloudRuntimeDriftFindings returns one bounded page of active
// provider-neutral runtime drift findings for the caller's scope.
func (s *PostgresMultiCloudRuntimeDriftStore) ListActiveMultiCloudRuntimeDriftFindings(
	ctx context.Context,
	filter MultiCloudRuntimeDriftFilter,
) ([]MultiCloudRuntimeDriftFindingRow, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.store.ListActiveFindings(ctx, multiCloudRuntimeDriftFilterToStore(filter))
	if err != nil {
		return nil, err
	}
	out := make([]MultiCloudRuntimeDriftFindingRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, multiCloudRuntimeDriftRowFromStore(row))
	}
	return out, nil
}

// CountActiveMultiCloudRuntimeDriftFindings returns the total active finding
// count for the same bounded filters used by the list path.
func (s *PostgresMultiCloudRuntimeDriftStore) CountActiveMultiCloudRuntimeDriftFindings(
	ctx context.Context,
	filter MultiCloudRuntimeDriftFilter,
) (int, error) {
	if s == nil {
		return 0, nil
	}
	return s.store.CountActiveFindings(ctx, multiCloudRuntimeDriftFilterToStore(filter))
}

func multiCloudRuntimeDriftFilterToStore(
	filter MultiCloudRuntimeDriftFilter,
) postgres.MultiCloudRuntimeDriftFindingFilter {
	return postgres.MultiCloudRuntimeDriftFindingFilter{
		ScopeID:          filter.ScopeID,
		Provider:         filter.Provider,
		CloudResourceUID: filter.CloudResourceUID,
		FindingKinds:     filter.FindingKinds,
		Limit:            filter.Limit,
		Offset:           filter.Offset,
	}
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

// driftedAttributesFromEvidence pairs the reducer's declared_<attr>/
// observed_<attr> evidence atoms (emitted by
// cloudruntime.appendValueDriftEvidence /
// multicloud.appendValueDriftEvidence for an image_version_drift finding)
// into the bounded DriftedAttributeView projection. Every other evidence
// atom (arn, resource_address, finding_kind, tags, ...) is intentionally
// ignored here -- this function is the ONLY narrow exception to "the query
// layer never surfaces raw evidence atoms" (see cloud_runtime_drift.go),
// and it must never grow into a general evidence passthrough.
func driftedAttributesFromEvidence(evidence []postgres.MultiCloudRuntimeDriftEvidenceRow) []DriftedAttributeView {
	declared := map[string]string{}
	observed := map[string]string{}
	seen := map[string]struct{}{}
	var attrs []string
	for _, atom := range evidence {
		switch {
		case strings.HasPrefix(atom.Key, "declared_"):
			attr := strings.TrimPrefix(atom.Key, "declared_")
			declared[attr] = atom.Value
			if _, ok := seen[attr]; !ok {
				seen[attr] = struct{}{}
				attrs = append(attrs, attr)
			}
		case strings.HasPrefix(atom.Key, "observed_"):
			attr := strings.TrimPrefix(atom.Key, "observed_")
			observed[attr] = atom.Value
			if _, ok := seen[attr]; !ok {
				seen[attr] = struct{}{}
				attrs = append(attrs, attr)
			}
		}
	}
	if len(attrs) == 0 {
		return nil
	}
	sort.Strings(attrs)
	out := make([]DriftedAttributeView, 0, len(attrs))
	for _, attr := range attrs {
		out = append(out, DriftedAttributeView{Attribute: attr, Declared: declared[attr], Observed: observed[attr]})
	}
	return out
}
