// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retract

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Bounded label values on eshu_dp_reconciliation_drift_retractions_total.
const (
	// MetricDomain is the domain label for stale workload repository edges.
	MetricDomain = "workload_materialization"
	// PhaseDefines is the write_phase label for retracted DEFINES edges.
	PhaseDefines = "defines_retract"
	// PhaseRepositoryEndpoint is the write_phase label for retracted
	// repository-side EXPOSES_ENDPOINT edges.
	PhaseRepositoryEndpoint = "repository_endpoint_retract"
	kindEdge                = "edge"
)

// Observe records one completed retract: measured deletes on
// eshu_dp_reconciliation_drift_retractions_total (skipped when the executor
// chain could not count them) and one "workload repository edge retract
// completed" log line with the scope, generation, keep sizes, deletes, and
// duration. instruments may be nil.
func Observe(
	ctx context.Context,
	instruments *telemetry.Instruments,
	scopeID, generationID string,
	keepLists []KeepList,
	result Result,
	duration time.Duration,
) {
	if instruments != nil && instruments.ReconciliationDriftRetractions != nil && result.Counted {
		record(ctx, instruments, PhaseDefines, result.DefinesDeleted)
		record(ctx, instruments, PhaseRepositoryEndpoint, result.EndpointEdgesDeleted)
	}
	keptWorkloads, keptEndpoints := 0, 0
	for _, keep := range keepLists {
		keptWorkloads += len(keep.WorkloadIDs)
		keptEndpoints += len(keep.EndpointIDs)
	}
	slog.InfoContext(ctx, "workload repository edge retract completed",
		"scope_id", scopeID,
		"generation_id", generationID,
		"repository_count", result.Repositories,
		"kept_workload_count", keptWorkloads,
		"kept_endpoint_count", keptEndpoints,
		"defines_deleted", result.DefinesDeleted,
		"repository_endpoint_edges_deleted", result.EndpointEdgesDeleted,
		"deletes_counted", result.Counted,
		"duration_s", duration.Seconds(),
	)
}

func record(ctx context.Context, instruments *telemetry.Instruments, phase string, count int64) {
	if count <= 0 {
		return
	}
	instruments.ReconciliationDriftRetractions.Add(ctx, count, metric.WithAttributes(
		telemetry.AttrDomain(MetricDomain),
		telemetry.AttrWritePhase(phase),
		telemetry.AttrKind(kindEdge),
	))
}
